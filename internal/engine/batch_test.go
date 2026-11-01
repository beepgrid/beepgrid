package engine

import (
	"context"
	"sync"
	"testing"
)

func TestBatchAtomicPrivateDeduplicatedAndConcurrentReplay(t *testing.T) {
	s, owner := testStore(t)
	ctx := context.Background()
	p := testPerson(t, s, owner, "first")
	q := testPerson(t, s, owner, "second")
	targets := []Target{{"person", p.ID}, {"person", q.ID}, {"person", p.ID}}
	preview, e := s.DispatchPreview(ctx, owner, DispatchPreviewInput{Targets: targets, Operation: "message"})
	if e != nil || preview.Count != 2 || preview.Sends {
		t.Fatal(preview, e)
	}
	input := SendBatchInput{Targets: targets, Body: "Private update", PreviewToken: preview.Token}
	var wg sync.WaitGroup
	ids := make(chan string, 5)
	for range 5 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			items, e := s.SendBatch(ctx, owner, input, "same-batch")
			if e != nil || len(items) != 2 {
				t.Errorf("batch: %v %v", items, e)
				return
			}
			if items[0].Result.ConversationID == items[1].Result.ConversationID {
				t.Error("shared conversation")
			}
			ids <- items[0].Result.Messages[0].ID
		}()
	}
	wg.Wait()
	first := ""
	for range 5 {
		if len(ids) == 0 {
			break
		}
		id := <-ids
		if first != "" && id != first {
			t.Fatal("replay changed receipt")
		}
		first = id
	}
	var count int
	if e = s.Pool.QueryRow(ctx, `SELECT count(*) FROM messages WHERE scope=$1`, owner.Scope).Scan(&count); e != nil || count != 2 {
		t.Fatal(count, e)
	}
	// Current per-target permission is required and late failures roll back history.
	sender := testPerson(t, s, owner, "sender")
	grant(t, s, owner, sender, []Rule{{Action: "message", TargetKind: "person", TargetID: p.ID}})
	if _, e = s.SendBatch(ctx, sender, SendBatchInput{Targets: targets, Body: "Forbidden"}, "denied-batch"); ErrorStatus(e) != 403 {
		t.Fatal(e)
	}
	if e = s.Pool.QueryRow(ctx, `SELECT count(*) FROM messages WHERE scope=$1`, owner.Scope).Scan(&count); e != nil || count != 2 {
		t.Fatal(count, e)
	}
	// Ask returns independent requests instead of conflating separate decisions.
	for _, person := range []Actor{p, q} {
		grant(t, s, owner, person, []Rule{{Action: "view", TargetKind: "person", TargetID: owner.ID}})
	}
	requests, e := s.AskBatch(ctx, owner, AskBatchInput{Targets: targets, Request: AskInput{Title: "Which value?"}}, "questions")
	if e != nil || len(requests) != 2 || requests[0].Request.ID == requests[1].Request.ID {
		t.Fatal(requests, e)
	}
}
func TestBatchPreviewFencesRouteChangesAndDoesNotGrantAccess(t *testing.T) {
	s, owner := testStore(t)
	ctx := context.Background()
	p := testPerson(t, s, owner, "recipient")
	sender := testPerson(t, s, owner, "sender")
	targets := []Target{{"person", p.ID}}
	role := grant(t, s, owner, sender, []Rule{{Action: "view", TargetKind: "person", TargetID: p.ID}, {Action: "message", TargetKind: "person", TargetID: p.ID}})
	preview, e := s.DispatchPreview(ctx, sender, DispatchPreviewInput{Targets: targets, Operation: "message"})
	if e != nil || len(preview.Recipients) != 1 {
		t.Fatal(preview, e)
	}
	conn, e := s.SaveConnection(ctx, owner, Connection{Name: "SMTP", Kind: "email", Enabled: true, Config: map[string]string{"from": "from@example.com", "smtp_addr": "localhost:1025"}})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.SaveDestination(ctx, owner, Destination{ConnectionID: conn.ID, PrincipalID: p.ID, Address: "to@example.com"}); e != nil {
		t.Fatal(e)
	}
	if _, e = s.SendBatch(ctx, sender, SendBatchInput{Targets: targets, Body: "Changed routes", PreviewToken: preview.Token}, "stale-preview"); ErrorStatus(e) != 409 {
		t.Fatal(e)
	}
	preview, e = s.DispatchPreview(ctx, sender, DispatchPreviewInput{Targets: targets, Operation: "message"})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.BindRole(ctx, owner, Binding{role.ID, "principal", sender.ID}, true); e != nil {
		t.Fatal(e)
	}
	if _, e = s.SendBatch(ctx, sender, SendBatchInput{Targets: targets, Body: "Revoked", PreviewToken: preview.Token}, "revoked-preview"); ErrorStatus(e) != 403 {
		t.Fatal(e)
	}
	if _, e = s.SendBatch(ctx, p, SendBatchInput{Targets: targets, Body: "Other actor", PreviewToken: preview.Token}, "different-actor"); e == nil {
		t.Fatal("preview transferred authority")
	}
}
func TestBatchLateFailureAndExactInstanceBoundaries(t *testing.T) {
	s, owner := testStore(t)
	ctx := context.Background()
	a := testWorker(t, s, owner, "agent")
	p := testPerson(t, s, owner, "person")
	if _, e := s.SendBatch(ctx, owner, SendBatchInput{Targets: []Target{{"agent", a.ID}, {"instance", a.InstanceID}}, Body: "Duplicate replicas"}, "mixed"); ErrorStatus(e) != 400 {
		t.Fatal(e)
	}
	foreign, e := s.Bootstrap(ctx, "private/dev", "Private", "dev", Secret())
	if e != nil {
		t.Fatal(e)
	}
	f := testPerson(t, s, foreign, "foreign")
	if _, e = s.SendBatch(ctx, owner, SendBatchInput{Targets: []Target{{"person", p.ID}, {"person", f.ID}}, Body: "cross tenant"}, "cross-batch"); ErrorStatus(e) != 404 {
		t.Fatal(e)
	}
	// A database failure in the second dispatch must roll back the first dispatch.
	if _, e = s.Pool.Exec(ctx, `CREATE FUNCTION reject_batch() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.recipient_id='`+p.ID+`' THEN RAISE EXCEPTION 'test batch fault'; END IF; RETURN NEW; END $$; CREATE TRIGGER reject_batch BEFORE INSERT ON deliveries FOR EACH ROW EXECUTE FUNCTION reject_batch()`); e != nil {
		t.Fatal(e)
	}
	if _, e = s.SendBatch(ctx, owner, SendBatchInput{Targets: []Target{{"instance", a.InstanceID}, {"person", p.ID}}, Body: "rollback"}, "rollback"); e == nil {
		t.Fatal("injected fault ignored")
	}
	var n int
	if e = s.Pool.QueryRow(ctx, `SELECT count(*) FROM messages WHERE scope=$1`, owner.Scope).Scan(&n); e != nil || n != 0 {
		t.Fatal(n, e)
	}
}

func TestGroupPreviewRejectsMembershipBroadening(t *testing.T) {
	s, owner := testStore(t)
	ctx := context.Background()
	p := testPerson(t, s, owner, "first")
	q := testPerson(t, s, owner, "second")
	group, e := s.SaveGroup(ctx, owner, Group{Name: "Team", Members: []string{p.ID}})
	if e != nil {
		t.Fatal(e)
	}
	target := Target{"group", group.ID}
	preview, e := s.DispatchPreview(ctx, owner, DispatchPreviewInput{Targets: []Target{target}, Operation: "message"})
	if e != nil {
		t.Fatal(e)
	}
	group.Members = append(group.Members, q.ID)
	if _, e = s.SaveGroup(ctx, owner, group); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Send(ctx, owner, SendInput{Target: target, Body: "Previewed instruction", PreviewToken: preview.Token}, "changed-group"); ErrorStatus(e) != 409 {
		t.Fatal(e)
	}
	preview, e = s.DispatchPreview(ctx, owner, DispatchPreviewInput{Targets: []Target{target}, Operation: "message"})
	if e != nil {
		t.Fatal(e)
	}
	out, e := s.Send(ctx, owner, SendInput{Target: target, Body: "Reviewed instruction", PreviewToken: preview.Token}, "fresh-group")
	if e != nil || len(out.Messages) != 2 {
		t.Fatal(out, e)
	}
}
