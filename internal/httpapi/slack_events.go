package httpapi

import (
	"encoding/json"
	"github.com/beepgrid/beepgrid/internal/engine"
	"io"
	"net/http"
	"time"
)

func (s *Server) slackEvents(w http.ResponseWriter, r *http.Request) {
	raw, e := io.ReadAll(io.LimitReader(r.Body, 131073))
	if e != nil || len(raw) > 131072 {
		writeError(w, engine.Bad("Invalid Slack event"))
		return
	}
	c, scope, e := s.Store.InboundConnection(r.Context(), r.PathValue("id"))
	if e != nil || c.Kind != "slack" {
		writeError(w, engine.Missing())
		return
	}
	var secrets struct {
		SigningSecret string `json:"signing_secret"`
	}
	_ = json.Unmarshal([]byte(c.Secret), &secrets)
	if secrets.SigningSecret == "" || !engine.VerifySlack(secrets.SigningSecret, r.Header.Get("X-Slack-Request-Timestamp"), r.Header.Get("X-Slack-Signature"), raw, time.Now()) {
		writeError(w, engine.Denied())
		return
	}
	var event struct {
		Type      string `json:"type"`
		Challenge string `json:"challenge"`
		Team      string `json:"team_id"`
		ID        string `json:"event_id"`
		Event     struct {
			Type        string `json:"type"`
			User        string `json:"user"`
			Channel     string `json:"channel"`
			ChannelType string `json:"channel_type"`
			Text        string `json:"text"`
			TS          string `json:"ts"`
			Thread      string `json:"thread_ts"`
			BotID       string `json:"bot_id"`
			Subtype     string `json:"subtype"`
		} `json:"event"`
	}
	if json.Unmarshal(raw, &event) != nil {
		writeError(w, engine.Bad("Invalid Slack JSON"))
		return
	}
	if event.Type == "url_verification" {
		output(w, 200, map[string]string{"challenge": event.Challenge})
		return
	}
	if event.Team != c.Config["team_id"] {
		writeError(w, engine.Denied())
		return
	}
	channelReply := event.Event.Type == "message" && (event.Event.ChannelType == "channel" || event.Event.ChannelType == "group") && event.Event.Thread != ""
	if event.Type != "event_callback" || event.Event.BotID != "" || event.Event.Subtype != "" || (event.Event.Type != "app_mention" && !(event.Event.Type == "message" && event.Event.ChannelType == "im") && !channelReply) {
		output(w, 200, map[string]bool{"ignored": true})
		return
	}
	if !engine.ValidText(event.ID, 160) || !engine.ValidText(event.Event.Channel, 100) || !engine.ValidText(event.Event.TS, 100) {
		writeError(w, engine.Bad("Missing Slack identifiers"))
		return
	}
	a, e := s.Store.SlackActor(r.Context(), scope, event.Event.User, event.Team)
	if e != nil {
		writeError(w, e)
		return
	}
	a.ProviderConnection = c.ID
	thread := event.Event.Thread
	if thread == "" {
		thread = event.Event.TS
	}
	// Slack can deliver a mention through both subscriptions. The immutable
	// channel/message timestamp identifies the same message across those events.
	result, e := s.Store.SlackMessage(r.Context(), a, c, event.Event.Channel, thread, event.Event.Text, event.Event.Channel+"/"+event.Event.TS, channelReply)
	if channelReply && engine.ErrorStatus(e) == 404 {
		output(w, 200, map[string]bool{"ignored": true})
		return
	}
	if e != nil {
		writeError(w, e)
		return
	}
	output(w, 200, map[string]string{"conversation_id": result.ConversationID})
}
