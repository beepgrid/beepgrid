package engine

import (
	"context"
	"testing"
)

func TestSparseEventHistoryPreservesScopeRecipientsAndRevocation(t *testing.T) {
	s, owner := testStore(t)
	ctx := context.Background()
	worker := testWorker(t, s, owner, "sparse-history")
	_, err := s.Pool.Exec(ctx, `WITH bumped AS (UPDATE scopes SET sequence=sequence+20000 WHERE id=$1 RETURNING sequence) INSERT INTO events(scope,sequence,kind,actor_id,conversation_id,data) SELECT $1,b.sequence-20000+n,'hidden-history',$2,'unrelated-conversation','{}' FROM bumped b CROSS JOIN generate_series(1,20000) n`, owner.Scope, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	// Matching principal IDs in another scope cannot turn historical events public.
	if _, err = s.Pool.Exec(ctx, `INSERT INTO events(scope,sequence,kind,actor_id,recipient_id,data) VALUES('foreign/production',1,'foreign',$1,$2,'{}')`, owner.ID, worker.ID); err != nil {
		t.Fatal(err)
	}
	sent, err := s.Send(ctx, owner, SendInput{Target: Target{"agent", worker.ID}, Body: "Visible after hidden history"}, "sparse-visible")
	if err != nil {
		t.Fatal(err)
	}
	var notice int64
	err = s.Pool.QueryRow(ctx, `WITH bumped AS (UPDATE scopes SET sequence=sequence+1 WHERE id=$1 RETURNING sequence) INSERT INTO events(scope,sequence,kind,actor_id,recipient_id,data) SELECT $1,sequence,'direct-notice',$2,$3,'{}' FROM bumped RETURNING sequence`, owner.Scope, owner.ID, worker.ID).Scan(&notice)
	if err != nil {
		t.Fatal(err)
	}
	events, cursor, err := s.EventPage(ctx, worker, 0, false)
	if err != nil {
		t.Fatal(err)
	}
	messageSeen, noticeSeen := false, false
	for _, event := range events {
		if event.Kind == "hidden-history" || event.Kind == "foreign" {
			t.Fatal("historical scope/member visibility leaked")
		}
		if event.Kind == "message.created" && event.ConversationID == sent.ConversationID {
			messageSeen = true
		}
		if event.Kind == "direct-notice" {
			noticeSeen = true
		}
	}
	if !messageSeen || !noticeSeen || cursor != notice {
		t.Fatal("sparse history lost visible work or cursor", messageSeen, noticeSeen, cursor, notice)
	}
	if _, err = s.Pool.Exec(ctx, `DELETE FROM role_bindings WHERE scope=$1 AND subject_id=$2`, worker.Scope, worker.ID); err != nil {
		t.Fatal(err)
	}
	events, _, err = s.EventPage(ctx, worker, 0, false)
	if err != nil {
		t.Fatal(err)
	}
	noticeSeen = false
	for _, event := range events {
		if event.ConversationID != "" {
			t.Fatal("revoked conversation leaked from membership candidates")
		}
		if event.Kind == "direct-notice" {
			noticeSeen = true
		}
	}
	if !noticeSeen {
		t.Fatal("direct recipient event was removed by conversation revocation")
	}
}
