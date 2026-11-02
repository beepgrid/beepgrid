package engine

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestConversationSnapshotBoundsReadsAndPreservesCurrentAuthority(t *testing.T) {
	s, owner := testStore(t)
	ctx := context.Background()
	viewer := testPerson(t, s, owner, "snapshot-viewer")
	other := testPerson(t, s, owner, "snapshot-other")
	outsider := testPerson(t, s, owner, "snapshot-outsider")
	g, err := s.SaveGroup(ctx, owner, Group{Name: "Readable contacts", Members: []string{owner.ID}})
	if err != nil {
		t.Fatal(err)
	}
	rules := []Rule{{Action: "view", TargetKind: "group", TargetID: g.ID}, {Action: "message", TargetKind: "person", TargetID: owner.ID}}
	grant(t, s, owner, viewer, rules)
	grant(t, s, owner, outsider, rules)
	seed, err := s.Send(ctx, owner, SendInput{Target: Target{"person", viewer.ID}, Body: "Private canonical body"}, "snapshot-seed")
	if err != nil {
		t.Fatal(err)
	}
	config := s.Pool.Config()
	queries := &queryCounter{}
	config.ConnConfig.Tracer = queries
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	old := s.Pool
	s.Pool = pool
	old.Close()
	queries.total.Store(0)
	c, err := s.Conversation(ctx, viewer, seed.ConversationID)
	if err != nil || len(c.Messages) != 1 || len(c.Participants) != 2 {
		t.Fatal(c, err)
	}
	if count := queries.total.Load(); count > 4 {
		t.Fatal("repeated conversation metadata or membership reads", count)
	}
	if _, err = s.Conversation(ctx, outsider, seed.ConversationID); ErrorStatus(err) != 404 {
		t.Fatal("view permission is not membership", err)
	}
	if _, err = s.Pool.Exec(ctx, `INSERT INTO participants(scope,conversation_id,principal_id) VALUES($1,$2,$3)`, owner.Scope, c.ID, other.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Conversation(ctx, viewer, c.ID); ErrorStatus(err) != 404 {
		t.Fatal("unviewable third participant disclosed", err)
	}
	g.Members = append(g.Members, other.ID)
	g, err = s.SaveGroup(ctx, owner, g)
	if err != nil {
		t.Fatal(err)
	}
	queries.total.Store(0)
	c, err = s.Conversation(ctx, viewer, c.ID)
	if err != nil || len(c.Participants) != 3 {
		t.Fatal(c, err)
	}
	if count := queries.total.Load(); count > 4 {
		t.Fatal("participant count repeats policy reads", count)
	}
	// Disabled historical participants still require View permission, matching
	// the existing conversation disclosure boundary rather than the send audience.
	if _, err = s.Pool.Exec(ctx, `UPDATE principals SET disabled=true WHERE scope=$1 AND id=$2`, owner.Scope, other.ID); err != nil {
		t.Fatal(err)
	}
	g.Members = []string{owner.ID}
	if _, err = s.SaveGroup(ctx, owner, g); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Conversation(ctx, viewer, c.ID); ErrorStatus(err) != 404 {
		t.Fatal("disabled participant bypassed revoked View", err)
	}
	if _, err = s.Send(ctx, viewer, SendInput{ConversationID: c.ID, Body: "Must be denied"}, "snapshot-denied"); ErrorStatus(err) != 404 {
		t.Fatal("send bypassed current read authority", err)
	}
	// Administrators can inspect surviving canonical history even when cleanup
	// has removed every participant. Preserve this preexisting behavior.
	if _, err = s.Pool.Exec(ctx, `DELETE FROM participants WHERE scope=$1 AND conversation_id=$2`, owner.Scope, c.ID); err != nil {
		t.Fatal(err)
	}
	c, err = s.Conversation(ctx, owner, c.ID)
	if err != nil || len(c.Participants) != 0 || len(c.Messages) != 1 {
		t.Fatal("admin history changed", c, err)
	}
}
