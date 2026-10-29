package httpapi

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"testing"

	"github.com/notif-dev/notif/internal/engine"
)

func TestTelegramIngressVerifiesSecretAndDisabledInstallation(t *testing.T) {
	s, client, server := setup(t)
	ctx := context.Background()
	a, err := s.Authenticate(ctx, client.Token)
	if err != nil {
		t.Fatal(err)
	}
	c, err := s.SaveConnection(ctx, a, engine.Connection{Name: "Telegram", Kind: "telegram", Enabled: true, Config: map[string]string{"bot_id": "12345"}, Secret: `{"bot_token":"12345:fixture_token_not_real","webhook_secret":"fixture_webhook_secret"}`})
	if err != nil {
		t.Fatal(err)
	}
	post := func(raw, secret string) int {
		req, _ := http.NewRequest("POST", server.URL+"/integrations/telegram/"+c.ID, bytes.NewBufferString(raw))
		req.Header.Set("X-Telegram-Bot-Api-Secret-Token", secret)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		io.Copy(io.Discard, res.Body)
		res.Body.Close()
		return res.StatusCode
	}
	raw := `{"update_id":1,"message":{"message_id":1,"from":{"id":42},"chat":{"id":42,"type":"private"},"text":"Hello"}}`
	if status := post(raw, "wrong"); status != 403 {
		t.Fatal("forged callback", status)
	}
	if status := post(raw, "fixture_webhook_secret"); status != 403 {
		t.Fatal("unlinked person", status)
	}
	if status := post(`{"update_id":2,"edited_message":{}}`, "fixture_webhook_secret"); status != 200 {
		t.Fatal("irrelevant update", status)
	}
	if status := post(`broken`, "fixture_webhook_secret"); status != 400 {
		t.Fatal("invalid JSON", status)
	}
	c.Enabled = false
	if _, err = s.SaveConnection(ctx, a, c); err != nil {
		t.Fatal(err)
	}
	if status := post(raw, "fixture_webhook_secret"); status != 404 {
		t.Fatal("disabled", status)
	}
}
