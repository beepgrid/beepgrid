package engine

import (
	"context"
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
