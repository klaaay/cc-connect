package core

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"
)

type notifierReceipt struct {
	Run        notifierRun `json:"run"`
	Platform   string      `json:"platform"`
	SessionKey string      `json:"sessionKey"`
	CreatedAt  time.Time   `json:"createdAt"`
}

func (e *Engine) notifierReceiptPath() string {
	if e.dataDir == "" {
		return ""
	}
	digest := sha256.Sum256([]byte(e.name + "\x00" + e.notifier.client.cfg.BaseURL))
	return filepath.Join(e.dataDir, "webhook-notifier", fmt.Sprintf("%x.json", digest[:16]))
}

// Caller holds trackingMu. Receipts contain only identity, never input or credentials.
func (e *Engine) notifierReceipts() ([]notifierReceipt, error) {
	path := e.notifierReceiptPath()
	if path == "" {
		return nil, nil
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var rows []notifierReceipt
	err = json.Unmarshal(data, &rows)
	return rows, err
}
func (e *Engine) saveNotifierReceipt(receipt notifierReceipt, remove bool) error {
	if e.notifierReceiptPath() == "" {
		return nil
	}
	n := e.notifier
	n.trackingMu.Lock()
	defer n.trackingMu.Unlock()
	rows, err := e.notifierReceipts()
	if err != nil {
		return err
	}
	next := make([]notifierReceipt, 0, len(rows)+1)
	for _, row := range rows {
		if row.Run.ID != receipt.Run.ID || row.Run.Kind != receipt.Run.Kind {
			next = append(next, row)
		}
	}
	if !remove {
		next = append(next, receipt)
	}
	data, err := json.Marshal(next)
	if err != nil {
		return err
	}
	path := e.notifierReceiptPath()
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	if err = os.WriteFile(path+".tmp", data, 0600); err != nil {
		return err
	}
	return os.Rename(path+".tmp", path)
}
func (e *Engine) watchNotifierRun(p Platform, msg *Message, run notifierRun) {
	receipt := notifierReceipt{Run: run, Platform: p.Name(), SessionKey: msg.SessionKey, CreatedAt: time.Now()}
	if err := e.saveNotifierReceipt(receipt, false); err != nil {
		slog.Error("notifier receipt persistence failed", "execution", run.ID)
		e.reply(p, msg.ReplyCtx, e.i18n.Tf(MsgWNTrackingStopped, run.ID))
		return
	}
	e.startNotifierTracking(p, msg.ReplyCtx, receipt)
}
func (e *Engine) resumeNotifierRuns(p Platform) {
	if e.notifier == nil {
		return
	}
	reconstructor, ok := p.(ReplyContextReconstructor)
	if !ok {
		return
	}
	e.notifier.trackingMu.Lock()
	rows, err := e.notifierReceipts()
	e.notifier.trackingMu.Unlock()
	if err != nil {
		slog.Error("notifier receipts unavailable", "project", e.name)
		return
	}
	for _, row := range rows {
		if row.Platform != p.Name() {
			continue
		}
		replyCtx, err := reconstructor.ReconstructReplyCtx(row.SessionKey)
		if err != nil {
			slog.Warn("notifier receipt destination unavailable", "execution", row.Run.ID)
			continue
		}
		e.startNotifierTracking(p, replyCtx, row)
	}
}
func (e *Engine) startNotifierTracking(p Platform, replyCtx any, receipt notifierReceipt) {
	n := e.notifier
	key := receipt.Run.Kind + ":" + receipt.Run.ID
	n.trackingMu.Lock()
	if n.tracking[key] {
		n.trackingMu.Unlock()
		return
	}
	n.tracking[key] = true
	n.trackingMu.Unlock()
	go func() {
		defer func() { n.trackingMu.Lock(); delete(n.tracking, key); n.trackingMu.Unlock() }()
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-e.ctx.Done():
				return
			case <-ticker.C:
			}
			text := ""
			if time.Since(receipt.CreatedAt) > 24*time.Hour {
				text = e.i18n.Tf(MsgWNTrackingStopped, receipt.Run.ID)
			} else {
				run, err := n.client.run(e.ctx, receipt.Run.Kind, receipt.Run.ID)
				if err != nil {
					continue
				}
				if run.Status == "queued" || run.Status == "running" {
					continue
				}
				text = e.notifierRunText(run)
			}
			if err := p.Reply(e.ctx, replyCtx, text); err != nil {
				slog.Warn("notifier result delivery failed", "execution", receipt.Run.ID)
				continue
			}
			if err := e.saveNotifierReceipt(receipt, true); err != nil {
				slog.Error("notifier receipt cleanup failed", "execution", receipt.Run.ID)
			}
			return
		}
	}()
}
