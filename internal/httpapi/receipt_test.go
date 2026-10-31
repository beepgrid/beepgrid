package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/beepgrid/beepgrid/internal/engine"
	"github.com/beepgrid/beepgrid/sdk/go/notif"
)

func TestCompactAcceptancePreservesDurabilityReplayAndFullDefault(t *testing.T) {
	s, client, server := setup(t)
	ctx := context.Background()
	owner, err := s.Authenticate(ctx, client.Token)
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{}
	for _, name := range []string{"one", "two"} {
		p, e := s.CreatePerson(ctx, owner, engine.CreatePerson{ExternalID: name, Name: name})
		if e != nil {
			t.Fatal(e)
		}
		ids = append(ids, p.ID)
	}
	group, err := s.SaveGroup(ctx, owner, engine.Group{Name: "Receipt group", Members: ids})
	if err != nil {
		t.Fatal(err)
	}
	input := notif.SendInput{Target: notif.Target{Kind: "group", ID: group.ID}, Body: strings.Repeat("<tag>é\n", 3000)}
	receipt, err := client.Notify(ctx, input, "compact-once")
	if err != nil || receipt.MessageCount != 2 || receipt.ConversationID == "" {
		t.Fatal(receipt, err)
	}
	full, err := client.Send(ctx, input, "compact-once")
	if err != nil || len(full.Messages) != 2 || full.ConversationID != receipt.ConversationID {
		t.Fatal(full, err)
	}
	raw, _ := json.Marshal(input)
	req, _ := http.NewRequest("POST", server.URL+"/api/v1/messages", bytes.NewReader(raw))
	req.Header.Set("Authorization", "Bearer "+client.Token)
	req.Header.Set("Idempotency-Key", "compact-once")
	req.Header.Set("Prefer", "wait=0, Return=Minimal")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != 200 || res.Header.Get("Preference-Applied") != "return=minimal" || len(body) > 160 || bytes.Contains(body, []byte("messages")) {
		t.Fatal(res.StatusCode, len(body), res.Header)
	}
	var messages, deliveries int
	if err = s.Pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM messages WHERE scope=$1),(SELECT count(*) FROM deliveries WHERE scope=$1)`, owner.Scope).Scan(&messages, &deliveries); err != nil || messages != 2 || deliveries != 2 {
		t.Fatal(messages, deliveries, err)
	}
	// A representation preference never bypasses input/idempotency validation.
	input.Body = "changed"
	if _, err = client.Notify(ctx, input, "compact-once"); err == nil {
		t.Fatal("conflicting operation acknowledged")
	}
	for _, m := range full.Messages {
		if m.Body != strings.Repeat("<tag>é\n", 3000) {
			t.Fatal("full default lost content")
		}
	}
}

func TestLargeReceiptEncodingMatchesCanonicalJSONBytes(t *testing.T) {
	result := engine.SendResult{ConversationID: "con<é>"}
	for i := range 96 {
		result.Messages = append(result.Messages, engine.Message{ID: engine.ID("msg"), ConversationID: result.ConversationID, SenderID: "sender", InstanceID: "instance", Body: strings.Repeat("<tag>\n\\\"é", 2000), CreatedAt: time.Unix(1700000000, int64(i+1))})
	}
	recorder := httptest.NewRecorder()
	output(recorder, 200, result)
	want := append(engine.JSON(result), '\n')
	if !bytes.Equal(recorder.Body.Bytes(), want) {
		t.Fatal("streamed encoding changed public bytes")
	}
}
