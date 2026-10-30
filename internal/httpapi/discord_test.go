package httpapi

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/notif-dev/notif/internal/engine"
)

func TestDiscordHTTPVerifiesSignaturesAppAndDisabledInstallation(t *testing.T) {
	s, client, server := setup(t)
	ctx := context.Background()
	a, err := s.Authenticate(ctx, client.Token)
	if err != nil {
		t.Fatal(err)
	}
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	c, err := s.SaveConnection(ctx, a, engine.Connection{Name: "Discord", Kind: "discord", Enabled: true, Config: map[string]string{"application_id": "12345", "public_key": hex.EncodeToString(public)}, Secret: `{"bot_token":"fixture_discord_bot_not_a_real_token"}`})
	if err != nil {
		t.Fatal(err)
	}
	post := func(body, signatureBody string, stamp string, valid bool) (int, map[string]any) {
		t.Helper()
		req, _ := http.NewRequest("POST", server.URL+"/integrations/discord/"+c.ID, strings.NewReader(body))
		req.Header.Set("X-Signature-Timestamp", stamp)
		signature := ed25519.Sign(private, append([]byte(stamp), []byte(signatureBody)...))
		if !valid {
			signature[0] ^= 1
		}
		req.Header.Set("X-Signature-Ed25519", hex.EncodeToString(signature))
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		raw, _ := io.ReadAll(res.Body)
		if bytes.Contains(raw, []byte("fixture_discord")) {
			t.Fatal("credential leaked")
		}
		var out map[string]any
		_ = json.Unmarshal(raw, &out)
		return res.StatusCode, out
	}
	ping := `{"id":"1","application_id":"12345","type":1}`
	now := strconv.FormatInt(time.Now().Unix(), 10)
	if status, out := post(ping, ping, now, true); status != 200 || out["type"] != float64(1) || len(out) != 1 {
		t.Fatal("PING", status, out)
	}
	for name, test := range map[string]struct {
		body, signed, stamp string
		valid               bool
		status              int
	}{
		"forged":       {ping, ping, now, false, 403},
		"changed body": {ping + " ", ping, now, true, 403},
		"expired":      {ping, ping, strconv.FormatInt(time.Now().Add(-6*time.Minute).Unix(), 10), true, 403},
		"wrong app":    {`{"id":"2","application_id":"12346","type":1}`, `{"id":"2","application_id":"12346","type":1}`, now, true, 403},
		"unlinked DM":  {`{"id":"3","application_id":"12345","type":2,"context":1,"channel_id":"91","channel":{"id":"91","type":1},"user":{"id":"42"}}`, `{"id":"3","application_id":"12345","type":2,"context":1,"channel_id":"91","channel":{"id":"91","type":1},"user":{"id":"42"}}`, now, true, 403},
		"invalid JSON": {"broken", "broken", now, true, 400},
	} {
		if status, out := post(test.body, test.signed, test.stamp, test.valid); status != test.status {
			t.Fatal(name, status, out)
		}
	}
	c.Enabled = false
	if _, err = s.SaveConnection(ctx, a, c); err != nil {
		t.Fatal(err)
	}
	if status, out := post(ping, ping, now, true); status != 404 {
		t.Fatal("disabled", status, out)
	}
}
