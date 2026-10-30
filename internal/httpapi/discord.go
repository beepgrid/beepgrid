package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/notif-dev/notif/internal/engine"
)

func (s *Server) discordEvents(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2500*time.Millisecond)
	defer cancel()
	r = r.WithContext(ctx)
	raw, err := io.ReadAll(io.LimitReader(r.Body, 131073))
	if err != nil || len(raw) > 131072 {
		writeError(w, engine.Bad("Invalid Discord callback body."))
		return
	}
	c, scope, err := s.Store.InboundConnection(r.Context(), r.PathValue("id"))
	if err != nil || c.Kind != "discord" {
		writeError(w, engine.Missing())
		return
	}
	if !engine.VerifyDiscord(c.Config["public_key"], r.Header.Get("X-Signature-Timestamp"), r.Header.Get("X-Signature-Ed25519"), raw, time.Now()) {
		writeError(w, engine.Denied())
		return
	}
	var in engine.DiscordInteraction
	if json.Unmarshal(raw, &in) != nil {
		writeError(w, engine.Bad("Invalid Discord interaction."))
		return
	}
	out, err := s.Store.ReceiveDiscord(ctx, c, scope, in)
	if err != nil {
		writeError(w, err)
		return
	}
	// Provider receipts retain correlation separately; Discord receives only
	// its interaction response schema, without internal receipt metadata.
	response := map[string]any{"type": out["type"]}
	if data, ok := out["data"]; ok {
		response["data"] = data
	}
	output(w, 200, response)
}
