package httpapi

import (
	"bytes"
	"github.com/beepgrid/beepgrid/internal/engine"
	"io"
	"net/http"
	"time"
)

// Generic incoming webhooks have one administrator-bound sender. A body can never choose its identity.
func (s *Server) incomingWebhook(w http.ResponseWriter, r *http.Request) {
	raw, e := io.ReadAll(io.LimitReader(r.Body, 131073))
	if e != nil || len(raw) > 131072 {
		writeError(w, engine.Bad("Invalid callback body."))
		return
	}
	c, scope, e := s.Store.InboundConnection(r.Context(), r.PathValue("id"))
	if e != nil || c.Kind != "webhook" {
		writeError(w, engine.Missing())
		return
	}
	if !engine.VerifySignature(c.Secret, r.Header.Get("X-Beepgrid-Timestamp"), r.Header.Get("X-Beepgrid-Signature"), raw, time.Now()) {
		writeError(w, engine.Denied())
		return
	}
	eventID := r.Header.Get("X-Beepgrid-Event-ID")
	if !engine.ValidText(eventID, 160) {
		writeError(w, engine.Bad("X-Beepgrid-Event-ID is required."))
		return
	}
	sender, e := s.Store.WebhookActor(r.Context(), scope, c.ID, c.Config["inbound_sender_id"])
	if e != nil {
		writeError(w, e)
		return
	}
	r.Body = io.NopCloser(bytes.NewReader(raw))
	var in engine.SendInput
	if e = decode(r, &in); e != nil {
		writeError(w, e)
		return
	}
	result, e := s.Store.Send(r.Context(), sender, in, "webhook/"+c.ID+"/"+eventID)
	if e != nil {
		writeError(w, e)
		return
	}
	output(w, 200, result)
}
