package engine

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)

func anycastRole(t testing.TB, s *Store, a Actor, workers ...Actor) Group {
	t.Helper()
	ids := []string{}
	for _, w := range workers {
		ids = append(ids, w.ID)
	}
	g, e := s.SaveGroup(context.Background(), a, Group{Name: ID("builders"), Purpose: "agent_role", Members: ids})
	if e != nil {
		t.Fatal(e)
	}
	return g
}
func capacity(t testing.TB, s *Store, w Actor, n int) {
	t.Helper()
	if e := s.Heartbeat(context.Background(), w, map[string]any{"anycast": map[string]any{"capacity": n}}); e != nil {
		t.Fatal(e)
	}
}
func TestAnycastSelectsOneReadyRuntimeAndKeepsRepliesSticky(t *testing.T) {
	s, a := testStore(t)
	ctx := context.Background()
	b := testWorker(t, s, a, "b")
	c := testWorker(t, s, a, "c")
	g := anycastRole(t, s, a, b, c)
	capacity(t, s, b, 0)
	capacity(t, s, c, 1)
	d, e := s.Anycast(ctx, a, AnycastInput{RoleID: g.ID, Body: "Inspect this"}, "one")
	if e != nil || d.State != "assigned" || d.AgentID != c.ID {
		t.Fatal(d, e)
	}
	same, e := s.Anycast(ctx, a, AnycastInput{RoleID: g.ID, Body: "Inspect this"}, "one")
	if e != nil || same.ID != d.ID {
		t.Fatal(same, e)
	}
	if _, e = s.Anycast(ctx, a, AnycastInput{RoleID: g.ID, Body: "Changed"}, "one"); ErrorStatus(e) != 409 {
		t.Fatal("changed replay", e)
	}
	other, e := s.Inbox(ctx, b)
	if e != nil || len(other) != 0 {
		t.Fatal(other, e)
	}
	if _, e = s.Conversation(ctx, b, d.ConversationID); ErrorStatus(e) != 404 {
		t.Fatal("unselected recipient read private conversation", e)
	}
	inbox, e := s.Inbox(ctx, c)
	if e != nil || len(inbox) != 1 || inbox[0].Payload["anycast_id"] != d.ID {
		t.Fatal(inbox, e)
	}
	if e = s.Ack(ctx, c, inbox[0].ID, inbox[0].Generation); e != nil {
		t.Fatal(e)
	}
	queued, e := s.Anycast(ctx, a, AnycastInput{RoleID: g.ID, Body: "Next"}, "two")
	if e != nil || queued.State != "queued" {
		t.Fatal("ACK freed capacity", queued, e)
	}
	if e = s.CompleteAnycast(ctx, b, d.ID); ErrorStatus(e) != 409 {
		t.Fatal("wrong worker completed", e)
	}
	if e = s.CompleteAnycast(ctx, c, d.ID); e != nil {
		t.Fatal(e)
	}
	queued, e = s.AnycastDispatch(ctx, a, queued.ID)
	if e != nil || queued.State != "assigned" {
		t.Fatal(queued, e)
	}
	if _, e = s.Send(ctx, a, SendInput{ConversationID: d.ConversationID, Body: "Follow up"}, "follow"); e != nil {
		t.Fatal(e)
	}
	var n int
	if e = s.Pool.QueryRow(ctx, `SELECT count(*) FROM participants WHERE scope=$1 AND conversation_id=$2`, a.Scope, d.ConversationID).Scan(&n); e != nil || n != 2 {
		t.Fatal(n, e)
	}
}
func TestAnycastConcurrentCapacityAndRecovery(t *testing.T) {
	s, a := testStore(t)
	ctx := context.Background()
	ws := []Actor{}
	for i := 0; i < 4; i++ {
		ws = append(ws, testWorker(t, s, a, fmt.Sprint("builder", i)))
	}
	g := anycastRole(t, s, a, ws...)
	for _, w := range ws {
		capacity(t, s, w, 1)
	}
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if _, e := s.Anycast(ctx, a, AnycastInput{RoleID: g.ID, Body: "Work"}, fmt.Sprint("job", i)); e != nil {
				t.Error(e)
			}
		}(i)
	}
	wg.Wait()
	var assigned, queued int
	if e := s.Pool.QueryRow(ctx, `SELECT count(*) FILTER(WHERE state='assigned'),count(*) FILTER(WHERE state='queued') FROM anycast_dispatches WHERE scope=$1`, a.Scope).Scan(&assigned, &queued); e != nil || assigned != 4 || queued != 12 {
		t.Fatal(assigned, queued, e)
	}
	var maxPer int
	if e := s.Pool.QueryRow(ctx, `SELECT max(n) FROM (SELECT count(*) n FROM anycast_dispatches WHERE scope=$1 AND state='assigned' GROUP BY instance_id) x`, a.Scope).Scan(&maxPer); e != nil || maxPer != 1 {
		t.Fatal(maxPer, e)
	}
	// Disconnects and expired heartbeats cannot free work that may have started.
	if _, e := s.Pool.Exec(ctx, `UPDATE instances SET anycast_until=now()-interval '1 second' WHERE scope=$1`, a.Scope); e != nil {
		t.Fatal(e)
	}
	if _, e := s.ProcessAnycast(ctx); e != nil {
		t.Fatal(e)
	}
	for _, w := range ws {
		capacity(t, s, w, 1)
	}
	if e := s.Pool.QueryRow(ctx, `SELECT count(*) FROM anycast_dispatches WHERE scope=$1 AND state='assigned'`, a.Scope).Scan(&assigned); e != nil || assigned != 4 {
		t.Fatal(assigned, e)
	}
}
func TestAnycastQueuePermissionsExpiryAndFiles(t *testing.T) {
	s, a := testStore(t)
	ctx := context.Background()
	s.AttachmentsRoot = t.TempDir()
	w := testWorker(t, s, a, "builder")
	g := anycastRole(t, s, a, w)
	f, e := s.UploadAttachment(ctx, a, "", "brief.txt", "text/plain", []byte("Private brief"))
	if e != nil {
		t.Fatal(e)
	}
	d, e := s.Anycast(ctx, a, AnycastInput{RoleID: g.ID, Body: "Read", AttachmentIDs: []string{f.ID}}, "file")
	if e != nil || d.State != "queued" {
		t.Fatal(d, e)
	}
	capacity(t, s, w, 1)
	d, e = s.AnycastDispatch(ctx, a, d.ID)
	if e != nil || d.State != "assigned" {
		t.Fatal(d, e)
	}
	c, e := s.Conversation(ctx, w, d.ConversationID)
	if e != nil || len(c.Messages) != 1 || len(c.Messages[0].Attachments) != 1 {
		t.Fatal(c, e)
	}
	cancelled, e := s.Anycast(ctx, a, AnycastInput{RoleID: g.ID, Body: "Cancel"}, "cancel")
	if e != nil {
		t.Fatal(e)
	}
	if e = s.CancelAnycast(ctx, a, cancelled.ID); e != nil {
		t.Fatal(e)
	}
	expired, e := s.Anycast(ctx, a, AnycastInput{RoleID: g.ID, Body: "Expire", ExpiresAt: time.Now().Add(time.Second)}, "expire")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Pool.Exec(ctx, `UPDATE anycast_dispatches SET expires_at=now()-interval '1 second',input=jsonb_set(input,'{expires_at}',to_jsonb(now()-interval '1 second')) WHERE scope=$1 AND id=$2`, a.Scope, expired.ID); e != nil {
		t.Fatal(e)
	}
	if _, e = s.ProcessAnycast(ctx); e != nil {
		t.Fatal(e)
	}
	expired, e = s.AnycastDispatch(ctx, a, expired.ID)
	if e != nil || expired.State != "expired" {
		t.Fatal(expired, e)
	}
	// Revoke a queued sender before capacity becomes free.
	p := testPerson(t, s, a, "sender")
	grant(t, s, a, p, []Rule{{Action: "*", TargetKind: "*", TargetID: "*"}})
	denied, e := s.Anycast(ctx, p, AnycastInput{RoleID: g.ID, Body: "No longer permitted"}, "denied")
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Revoke(ctx, a, p.CredentialID); e != nil {
		t.Fatal(e)
	}
	capacity(t, s, w, 2)
	denied, e = s.AnycastDispatch(ctx, a, denied.ID)
	if e != nil || denied.State != "blocked" {
		t.Fatal(denied, e)
	}
}
func TestAnycastRequestResolutionAndMembershipFence(t *testing.T) {
	s, a := testStore(t)
	ctx := context.Background()
	w := testWorker(t, s, a, "builder")
	g := anycastRole(t, s, a, w)
	capacity(t, s, w, 1)
	d, e := s.Anycast(ctx, a, AnycastInput{RoleID: g.ID, Kind: "question", Body: "Which one?", Choices: []string{"A", "B"}}, "question")
	if e != nil || d.RequestID == "" {
		t.Fatal(d, e)
	}
	if _, e = s.Respond(ctx, w, d.RequestID, Response{Version: 1, Outcome: "answered", Answer: "A"}); e != nil {
		t.Fatal(e)
	}
	if _, e = s.ProcessAnycast(ctx); e != nil {
		t.Fatal(e)
	}
	d, e = s.AnycastDispatch(ctx, a, d.ID)
	if e != nil || d.State != "completed" {
		t.Fatal(d, e)
	}
	next, e := s.Anycast(ctx, a, AnycastInput{RoleID: g.ID, Body: "Private"}, "removed")
	if e != nil || next.State != "assigned" {
		t.Fatal(next, e)
	}
	if _, e = s.Pool.Exec(ctx, `DELETE FROM memberships WHERE scope=$1 AND group_id=$2 AND principal_id=$3`, a.Scope, g.ID, w.ID); e != nil {
		t.Fatal(e)
	}
	messages, e := s.Inbox(ctx, w)
	if e != nil || len(messages) != 0 {
		t.Fatal(messages, e)
	}
}
func TestAnycastRejectsInvalidAndOrdinaryGroups(t *testing.T) {
	s, a := testStore(t)
	ctx := context.Background()
	w := testWorker(t, s, a, "builder")
	g := anycastRole(t, s, a, w)
	for _, bad := range []map[string]any{{"anycast": nil}, {"anycast": map[string]any{"capacity": -1}}, {"anycast": map[string]any{"capacity": 1.2}}} {
		if e := s.Heartbeat(ctx, w, bad); ErrorStatus(e) != 400 {
			t.Fatal(e)
		}
	}
	for i, in := range []AnycastInput{{RoleID: g.ID, Body: "x", Kind: "bad"}, {RoleID: g.ID, Body: "x", Kind: "message", Choices: []string{"a"}}, {RoleID: g.ID, Body: "x", Action: &Action{Name: "bad"}}, {RoleID: g.ID, Body: "x", ExpiresAt: time.Now().Add(-time.Second)}} {
		if _, e := s.Anycast(ctx, a, in, fmt.Sprint(i)); ErrorStatus(e) != 400 {
			t.Fatal(e)
		}
	}
	g.Purpose = "contacts"
	g.Members = []string{w.ID}
	g.Version++ // direct SQL isolates group-purpose validation
	if _, e := s.Pool.Exec(ctx, `UPDATE groups SET purpose='contacts' WHERE scope=$1 AND id=$2`, a.Scope, g.ID); e != nil {
		t.Fatal(e)
	}
	if _, e := s.Anycast(ctx, a, AnycastInput{RoleID: g.ID, Body: "x"}, "notrole"); ErrorStatus(e) != 400 {
		t.Fatal(e)
	}
}

func TestAnycastFairnessAndFreshness(t *testing.T) {
	s, a := testStore(t)
	ctx := context.Background()
	ws := []Actor{testWorker(t, s, a, "one"), testWorker(t, s, a, "two"), testWorker(t, s, a, "three")}
	g := anycastRole(t, s, a, ws...)
	for _, w := range ws {
		capacity(t, s, w, 1)
	}
	counts := map[string]int{}
	for i := 0; i < 9; i++ {
		d, e := s.Anycast(ctx, a, AnycastInput{RoleID: g.ID, Body: "Work"}, fmt.Sprint(i))
		if e != nil || d.State != "assigned" {
			t.Fatal(d, e)
		}
		counts[d.AgentID]++
		var w Actor
		for _, candidate := range ws {
			if candidate.ID == d.AgentID {
				w = candidate
			}
		}
		items, e := s.Inbox(ctx, w)
		if e != nil || len(items) != 1 {
			t.Fatal(items, e)
		}
		if e = s.Ack(ctx, w, items[0].ID, items[0].Generation); e != nil {
			t.Fatal(e)
		}
		if e = s.CompleteAnycast(ctx, w, d.ID); e != nil {
			t.Fatal(e)
		}
	}
	for _, w := range ws {
		if counts[w.ID] != 3 {
			t.Fatal(counts)
		}
	}
	_, e := s.Pool.Exec(ctx, `UPDATE instances SET anycast_until=now()-interval '1 second' WHERE scope=$1`, a.Scope)
	if e != nil {
		t.Fatal(e)
	}
	// Polling is presence, not a capacity advertisement.
	if _, e = s.Inbox(ctx, ws[0]); e != nil {
		t.Fatal(e)
	}
	d, e := s.Anycast(ctx, a, AnycastInput{RoleID: g.ID, Body: "No fresh capacity"}, "stale")
	if e != nil || d.State != "queued" {
		t.Fatal(d, e)
	}
	capacity(t, s, ws[1], 1)
	d, e = s.AnycastDispatch(ctx, a, d.ID)
	if e != nil || d.AgentID != ws[1].ID {
		t.Fatal(d, e)
	}
}
func TestAnycastBusyRoleCannotStarveAnotherRole(t *testing.T) {
	s, a := testStore(t)
	ctx := context.Background()
	busy := testWorker(t, s, a, "busy")
	ready := testWorker(t, s, a, "ready")
	g1 := anycastRole(t, s, a, busy)
	g2 := anycastRole(t, s, a, ready)
	for i := 0; i < 40; i++ {
		if _, e := s.Anycast(ctx, a, AnycastInput{RoleID: g1.ID, Body: "Wait"}, fmt.Sprint(i)); e != nil {
			t.Fatal(e)
		}
	}
	d, e := s.Anycast(ctx, a, AnycastInput{RoleID: g2.ID, Body: "Ready"}, "ready")
	if e != nil {
		t.Fatal(e)
	}
	capacity(t, s, ready, 1)
	for i := 0; i < 3; i++ {
		if _, e = s.ProcessAnycast(ctx); e != nil {
			t.Fatal(e)
		}
	}
	d, e = s.AnycastDispatch(ctx, a, d.ID)
	if e != nil || d.State != "assigned" {
		t.Fatal(d, e)
	}
}
func TestAnycastUnclaimedDeadlineDoesNotHoldCapacity(t *testing.T) {
	s, a := testStore(t)
	ctx := context.Background()
	w := testWorker(t, s, a, "builder")
	g := anycastRole(t, s, a, w)
	capacity(t, s, w, 1)
	d, e := s.Anycast(ctx, a, AnycastInput{RoleID: g.ID, Body: "Work"}, "deadline")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Pool.Exec(ctx, `UPDATE anycast_dispatches SET expires_at=now()-interval '1 second' WHERE scope=$1 AND id=$2`, a.Scope, d.ID); e != nil {
		t.Fatal(e)
	}
	if _, e = s.ProcessAnycast(ctx); e != nil {
		t.Fatal(e)
	}
	d, e = s.AnycastDispatch(ctx, a, d.ID)
	if e != nil || d.State != "expired" {
		t.Fatal(d, e)
	}
	next, e := s.Anycast(ctx, a, AnycastInput{RoleID: g.ID, Body: "Next"}, "after")
	if e != nil || next.State != "assigned" {
		t.Fatal(next, e)
	}
}

func BenchmarkAnycastRouting(b *testing.B) {
	for _, size := range []int{4, 100} {
		b.Run(fmt.Sprint(size, "_agents"), func(b *testing.B) {
			b.StopTimer()
			s, a := testStore(b)
			ctx := context.Background()
			ws := make([]Actor, 0, size)
			byID := map[string]Actor{}
			for i := 0; i < size; i++ {
				w := testWorker(b, s, a, fmt.Sprint("worker", i))
				ws = append(ws, w)
				byID[w.ID] = w
				capacity(b, s, w, 1)
			}
			g := anycastRole(b, s, a, ws...)
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				b.StartTimer()
				d, e := s.Anycast(ctx, a, AnycastInput{RoleID: g.ID, Body: "Inspect the current build"}, fmt.Sprint("bench", i))
				b.StopTimer()
				if e != nil || d.State != "assigned" {
					b.Fatal(d, e)
				}
				w := byID[d.AgentID]
				deliveries, e := s.Inbox(ctx, w)
				if e != nil || len(deliveries) != 1 {
					b.Fatal(deliveries, e)
				}
				if e = s.Ack(ctx, w, deliveries[0].ID, deliveries[0].Generation); e != nil {
					b.Fatal(e)
				}
				if e = s.CompleteAnycast(ctx, w, d.ID); e != nil {
					b.Fatal(e)
				}
			}
		})
	}
}

func TestAnycastListUsesPrivateBoundedPreviews(t *testing.T) {
	s, a := testStore(t)
	ctx := context.Background()
	w := testWorker(t, s, a, "builder")
	g := anycastRole(t, s, a, w)
	body := ""
	for i := 0; i < 1000; i++ {
		body += "x"
	}
	d, e := s.Anycast(ctx, a, AnycastInput{RoleID: g.ID, Kind: "question", Body: body, Choices: []string{"A", "B"}}, "preview")
	if e != nil {
		t.Fatal(e)
	}
	listed, e := s.AnycastDispatches(ctx, a)
	if e != nil || len(listed) != 1 || len(listed[0].Input.Body) != 240 || len(listed[0].Input.Choices) != 0 {
		t.Fatal(listed, e)
	}
	full, e := s.AnycastDispatch(ctx, a, d.ID)
	if e != nil || full.Input.Body != body || len(full.Input.Choices) != 2 {
		t.Fatal(full, e)
	}
	hidden, e := s.AnycastDispatches(ctx, w)
	if e != nil || len(hidden) != 0 {
		t.Fatal(hidden, e)
	}
	if _, e = s.AnycastDispatch(ctx, w, d.ID); ErrorStatus(e) != 404 {
		t.Fatal(e)
	}
}
