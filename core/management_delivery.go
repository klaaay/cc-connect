package core

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

type managedDeliveryRequest struct {
	LookupOnly bool   `json:"lookup_only,omitempty"`
	SessionKey string `json:"session_key"`
	SessionID  string `json:"session_id"`
	RequestID  string `json:"request_id"`
	Prompt     string `json:"prompt"`
}
type managedDeliveryReceipt struct {
	Fingerprint string `json:"fingerprint"`
	RequestID   string `json:"request_id"`
	SessionID   string `json:"session_id"`
	Status      string `json:"status"`
}

func digestManaged(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}

// Save intent before handing a message to the engine. A crash leaves unknown,
// which is never re-sent automatically. Receipts survive /new and process restart.
func saveManagedReceipt(path string, receipt managedDeliveryReceipt) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	raw, err := json.Marshal(receipt)
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".receipt-")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if err = file.Chmod(0600); err == nil {
		_, err = file.Write(raw)
	}
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err = os.Rename(file.Name(), path); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
func (m *ManagementServer) handleManagedDelivery(w http.ResponseWriter, r *http.Request, e *Engine) {
	if r.Method != http.MethodPost {
		mgmtError(w, http.StatusMethodNotAllowed, e.i18n.T(MsgManagedDeliveryInvalid))
		return
	}
	var req managedDeliveryRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 131072))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&req) != nil || strings.TrimSpace(req.SessionKey) == "" || strings.TrimSpace(req.SessionID) == "" || strings.TrimSpace(req.RequestID) == "" || len(req.RequestID) > 200 || strings.TrimSpace(req.Prompt) == "" || strings.HasPrefix(strings.TrimSpace(req.Prompt), "/") {
		mgmtError(w, http.StatusBadRequest, e.i18n.T(MsgManagedDeliveryInvalid))
		return
	}
	// Multi-workspace routing has a different identity namespace; do not guess it.
	if e.sessions.storePath == "" || e.multiWorkspace {
		mgmtError(w, http.StatusConflict, e.i18n.T(MsgManagedDeliveryConflict))
		return
	}
	e.managedRequestMu.Lock()
	defer e.managedRequestMu.Unlock()
	lookupOnly := req.LookupOnly
	req.LookupOnly = false
	body, _ := json.Marshal(req)
	receipt := managedDeliveryReceipt{Fingerprint: digestManaged(body), RequestID: req.RequestID, SessionID: req.SessionID, Status: "unknown"}
	path := filepath.Join(e.sessions.storePath+".deliveries", digestManaged([]byte(req.RequestID))+".json")
	previous, err := os.ReadFile(path)
	if err == nil {
		var old managedDeliveryReceipt
		if json.Unmarshal(previous, &old) != nil || old.Fingerprint != receipt.Fingerprint {
			mgmtError(w, http.StatusConflict, e.i18n.T(MsgManagedDeliveryConflict))
			return
		}
		mgmtJSON(w, http.StatusOK, old)
		return
	}
	if !errors.Is(err, os.ErrNotExist) {
		mgmtError(w, http.StatusServiceUnavailable, e.i18n.T(MsgManagedDeliveryUnavailable))
		return
	}
	if lookupOnly {
		receipt.Status = "not_found"
		mgmtJSON(w, http.StatusOK, receipt)
		return
	}
	if saveManagedReceipt(path, receipt) != nil {
		mgmtError(w, http.StatusServiceUnavailable, e.i18n.T(MsgManagedDeliveryUnavailable))
		return
	}
	delivery, sendErr := (&WebhookServer{}).acceptPrompt(e, req.SessionKey, req.Prompt, true, "managed-delivery", webhookIngressGuard{sessionID: req.SessionID})
	if sendErr == nil {
		switch {
		case delivery == "started":
			receipt.Status = "accepted"
		case strings.HasPrefix(delivery, "rejected:"):
			receipt.Status = delivery
		}
	}
	if saveManagedReceipt(path, receipt) != nil {
		mgmtError(w, http.StatusServiceUnavailable, e.i18n.T(MsgManagedDeliveryUnavailable))
		return
	}
	mgmtJSON(w, http.StatusOK, receipt)
}
