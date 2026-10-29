package httpapi

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/notif-dev/notif/internal/engine"
)

func (s *Server) telegramEvents(w http.ResponseWriter, r *http.Request) {
	raw, err := io.ReadAll(io.LimitReader(r.Body, 131073))
	if err != nil || len(raw) > 131072 {
		writeError(w, engine.Bad("Invalid Telegram callback body."))
		return
	}
	c, scope, err := s.Store.InboundConnection(r.Context(), r.PathValue("id"))
	if err != nil || c.Kind != "telegram" {
		writeError(w, engine.Missing())
		return
	}
	if !engine.VerifyTelegramSecret(c, r.Header.Get("X-Telegram-Bot-Api-Secret-Token")) {
		writeError(w, engine.Denied())
		return
	}
	var in engine.TelegramUpdate
	if json.Unmarshal(raw, &in) != nil || in.ID == nil {
		writeError(w, engine.Bad("Invalid Telegram update."))
		return
	}
	if in.Message == nil && in.Callback == nil {
		output(w, 200, map[string]bool{"ignored": true})
		return
	}
	out, err := s.Store.ReceiveTelegram(r.Context(), c, scope, in)
	if err != nil {
		writeError(w, err)
		return
	}
	// The durable transaction committed before the provider receives success.
	if in.Callback != nil {
		s.Store.AcknowledgeTelegramCallback(r.Context(), c, in.Callback.ID)
	}
	output(w, 200, out)
}
