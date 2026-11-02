package engine

import (
	"context"
	"encoding/json"
	"testing"
)

func TestSummaryEventsPreserveVisibilityCursorsAndRevocation(t *testing.T) {
	s, owner := testStore(t)
	ctx := context.Background()
	w := testWorker(t, s, owner, "summary")
	other := testWorker(t, s, owner, "other-summary")
	for i, recipient := range []Actor{w, other} {
		if _, e := s.Send(ctx, owner, SendInput{Target: Target{"agent", recipient.ID}, Body: "private body"}, string(rune('a'+i))); e != nil {
			t.Fatal(e)
		}
	}
	full, cursor, e := s.EventPage(ctx, w, 0, false)
	if e != nil {
		t.Fatal(e)
	}
	summary, summaryCursor, e := s.EventSummaryPage(ctx, w, 0)
	if e != nil || len(full) == 0 || len(full) != len(summary) || cursor != summaryCursor {
		t.Fatal("cursor/visibility differs", e)
	}
	for i := range full {
		if summary[i].Data != nil || full[i].Sequence != summary[i].Sequence || full[i].ConversationID != summary[i].ConversationID || full[i].Kind != summary[i].Kind {
			t.Fatal("summary changed event identity")
		}
	}
	if _, e = s.Pool.Exec(ctx, `DELETE FROM role_bindings WHERE scope=$1 AND subject_id=$2`, w.Scope, w.ID); e != nil {
		t.Fatal(e)
	}
	summary, _, e = s.EventSummaryPage(ctx, w, 0)
	if e != nil {
		t.Fatal(e)
	}
	for _, event := range summary {
		if event.ConversationID != "" {
			t.Fatal("revoked conversation remained visible")
		}
	}
}

func TestConversationEventsEnrichOnlySelectedReadableMessages(t *testing.T) {
	s, owner := testStore(t)
	ctx := context.Background()
	w := testWorker(t, s, owner, "selected-events")
	other := testWorker(t, s, owner, "unselected-events")
	one, e := s.Send(ctx, owner, SendInput{Target: Target{"agent", w.ID}, Body: "selected canonical body <é>"}, "selected-event")
	if e != nil {
		t.Fatal(e)
	}
	two, e := s.Send(ctx, owner, SendInput{Target: Target{"agent", other.ID}, Body: "unselected private body"}, "other-event")
	if e != nil {
		t.Fatal(e)
	}
	full, cursor, e := s.EventPage(ctx, owner, 0, false)
	if e != nil {
		t.Fatal(e)
	}
	projected, projectedCursor, e := s.EventConversationPage(ctx, owner, 0, one.ConversationID)
	if e != nil || projectedCursor != cursor || len(projected) != len(full) {
		t.Fatal("projection changed visibility/cursor", e)
	}
	selected := 0
	for i, ev := range projected {
		if ev.Sequence != full[i].Sequence || ev.Kind != full[i].Kind {
			t.Fatal("projection changed event identity")
		}
		if ev.Kind == "message.created" && ev.ConversationID == one.ConversationID {
			if string(JSON(ev.Data)) != string(JSON(full[i].Data)) {
				t.Fatal("canonical message changed")
			}
			selected++
		} else if ev.Data != nil {
			t.Fatal("unselected payload included")
		}
	}
	if selected != 1 {
		t.Fatal("missing selected message")
	}
	// A guessed conversation cannot reveal a body or otherwise broaden access.
	projected, _, e = s.EventConversationPage(ctx, w, 0, two.ConversationID)
	if e != nil {
		t.Fatal(e)
	}
	for _, ev := range projected {
		if ev.ConversationID == two.ConversationID || ev.Data != nil {
			t.Fatal("foreign body visible")
		}
	}
	projected, _, e = s.EventConversationPage(ctx, w, 0, one.ConversationID)
	if e != nil {
		t.Fatal(e)
	}
	visible := false
	for _, ev := range projected {
		if ev.Kind == "message.created" {
			var message Message
			if json.Unmarshal([]byte(JSON(ev.Data)), &message) != nil || message.ID != one.Messages[0].ID {
				t.Fatal("message identity changed")
			}
			visible = true
		}
	}
	if !visible {
		t.Fatal("own canonical body missing")
	}
	if _, e = s.Pool.Exec(ctx, `DELETE FROM role_bindings WHERE scope=$1 AND subject_id=$2`, w.Scope, w.ID); e != nil {
		t.Fatal(e)
	}
	projected, _, e = s.EventConversationPage(ctx, w, 0, one.ConversationID)
	if e != nil {
		t.Fatal(e)
	}
	for _, ev := range projected {
		if ev.ConversationID != "" {
			t.Fatal("revoked conversation visible")
		}
	}
}
