package core

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

// NotifierConfig binds one cc-connect project to its trusted Notifier service.
// Credentials remain in a local file; execution stays in Notifier's queues.
type NotifierConfig struct{ BaseURL, TokenFile string }
type notifierField struct {
	Key       string   `json:"key"`
	Label     string   `json:"label"`
	Type      string   `json:"type"`
	Required  bool     `json:"required"`
	Options   []string `json:"options"`
	Min       *float64 `json:"min"`
	Max       *float64 `json:"max"`
	MaxLength int      `json:"maxLength"`
}
type notifierTask struct {
	ID       string          `json:"id"`
	Label    string          `json:"label"`
	Revision string          `json:"revision"`
	Pinned   bool            `json:"pinned"`
	Enabled  bool            `json:"enabled"`
	Inputs   []notifierField `json:"inputs"`
}
type notifierDeployment struct {
	TaskID       string   `json:"taskId"`
	Label        string   `json:"label"`
	Branch       string   `json:"branch"`
	Environments []string `json:"environments"`
	Targets      []string `json:"targets"`
}
type notifierCatalog struct {
	Version     int                  `json:"version"`
	Tasks       []notifierTask       `json:"tasks"`
	Deployments []notifierDeployment `json:"deployments"`
}
type notifierRun struct {
	Kind      string `json:"kind"`
	ID        string `json:"id"`
	TaskID    string `json:"taskId"`
	Label     string `json:"label"`
	Status    string `json:"status"`
	CreatedAt string `json:"createdAt"`
	Message   string `json:"message"`
}
type notifierClient struct {
	cfg           NotifierConfig
	http          *http.Client
	mu            sync.Mutex
	authenticated time.Time
}

func newNotifierClient(cfg NotifierConfig) (*notifierClient, error) {
	u, err := url.Parse(cfg.BaseURL)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("notifier: invalid base URL")
	}
	if u.Scheme != "https" && !(u.Scheme == "http" && (u.Hostname() == "127.0.0.1" || u.Hostname() == "localhost" || u.Hostname() == "::1")) {
		return nil, errors.New("notifier: remote service requires HTTPS")
	}
	if cfg.TokenFile == "" {
		return nil, errors.New("notifier: token_file is required")
	}
	cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")
	jar, _ := cookiejar.New(nil)
	return &notifierClient{cfg: cfg, http: &http.Client{Timeout: 20 * time.Second, Jar: jar, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}
func (c *notifierClient) call(ctx context.Context, path, action string, body, out any) error {
	if err := c.login(ctx); err != nil {
		return err
	}
	return c.request(ctx, path, action, body, out, false)
}
func (c *notifierClient) login(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if time.Since(c.authenticated) < 10*time.Hour {
		return nil
	}
	token, err := os.ReadFile(c.cfg.TokenFile)
	if err != nil || len(bytes.TrimSpace(token)) < 32 {
		return errors.New("notifier: credential unavailable")
	}
	var result struct {
		Authenticated bool `json:"authenticated"`
	}
	if err = c.request(ctx, "/auth/login", "login", map[string]string{"token": strings.TrimSpace(string(token))}, &result, true); err != nil {
		return err
	}
	if !result.Authenticated {
		return errors.New("notifier: login not confirmed")
	}
	c.authenticated = time.Now()
	return nil
}
func (c *notifierClient) request(ctx context.Context, path, action string, body, out any, login bool) error {
	var data []byte
	var err error
	method := http.MethodGet
	if body != nil {
		method = http.MethodPost
		data, err = json.Marshal(body)
		if err != nil {
			return errors.New("notifier: invalid input")
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, c.cfg.BaseURL+path, bytes.NewReader(data))
	if err != nil {
		return errors.New("notifier: invalid request")
	}
	req.Header.Set("Content-Type", "application/json")
	if action != "" {
		req.Header.Set("X-Admin-Action", action)
	}
	response, err := c.http.Do(req)
	if err != nil {
		return errors.New("notifier: request outcome unknown")
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusUnauthorized && !login {
		c.mu.Lock()
		c.authenticated = time.Time{}
		c.mu.Unlock()
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("notifier: HTTP %d", response.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, 4<<20))
	if err != nil {
		return errors.New("notifier: response unavailable")
	}
	if login {
		return json.Unmarshal(raw, out)
	}
	var envelope struct {
		OK   bool            `json:"ok"`
		Data json.RawMessage `json:"data"`
	}
	if err = json.Unmarshal(raw, &envelope); err != nil || !envelope.OK {
		return errors.New("notifier: invalid response")
	}
	return json.Unmarshal(envelope.Data, out)
}
func (c *notifierClient) catalog(ctx context.Context) (notifierCatalog, error) {
	var result notifierCatalog
	err := c.call(ctx, "/wn/catalog", "", nil, &result)
	if err == nil && result.Version != 1 {
		err = errors.New("notifier: unsupported catalog version")
	}
	return result, err
}
func (c *notifierClient) runs(ctx context.Context) ([]notifierRun, error) {
	var result struct {
		Items []notifierRun `json:"items"`
	}
	err := c.call(ctx, "/wn/runs", "", nil, &result)
	return result.Items, err
}
func (c *notifierClient) run(ctx context.Context, kind, id string) (notifierRun, error) {
	var result notifierRun
	err := c.call(ctx, "/wn/runs/"+url.PathEscape(kind)+"/"+url.PathEscape(id), "", nil, &result)
	if err == nil && (result.ID != id || result.Kind != kind) {
		err = errors.New("notifier: execution identity mismatch")
	}
	return result, err
}
