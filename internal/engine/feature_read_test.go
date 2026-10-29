package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func featureCounter(t testing.TB, s *Store) *queryCounter {
	t.Helper()
	config := s.Pool.Config()
	q := &queryCounter{}
	config.ConnConfig.Tracer = q
	pool, err := pgxpool.NewWithConfig(context.Background(), config)
	if err != nil {
		t.Fatal(err)
	}
	old := s.Pool
	s.Pool = pool
	old.Close()
	return q
}

func TestFeatureReadBudgetsAndFreshAuthority(t *testing.T) {
	s, owner := testStore(t)
	ctx := context.Background()
	reader := testPerson(t, s, owner, "reader")
	role := grant(t, s, owner, reader, []Rule{{Action: "view", TargetKind: "agent", TargetID: "*"}, {Action: "view", TargetKind: "group", TargetID: "*"}, {Action: "message", TargetKind: "agent", TargetID: "*"}, {Action: "manage", TargetKind: "answer", TargetID: "$self"}})
	workers := []Actor{}
	for i := range 32 {
		w := testWorker(t, s, owner, fmt.Sprint("reader-budget-", i))
		workers = append(workers, w)
		if _, err := s.SaveGroup(ctx, owner, Group{Name: fmt.Sprint("Team ", i), Members: []string{w.ID, reader.ID}}); err != nil {
			t.Fatal(err)
		}
	}
	in := AskInput{Target: Target{"person", reader.ID}, Title: "Which region policy applies?", SharedKey: "budget/v1"}
	source, err := s.Ask(ctx, workers[0], in, "source")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Respond(ctx, reader, source.ID, Response{Outcome: "answered", Answer: "Region v1", Version: 1}); err != nil {
		t.Fatal(err)
	}
	for i := range 32 {
		if _, err = s.PublishAnswer(ctx, reader, PublishInput{RequestID: source.ID, Audience: []string{workers[0].ID}}); err != nil {
			t.Fatal(err)
		}
		if r, e := s.Ask(ctx, workers[0], in, fmt.Sprint("reuse-", i)); e != nil || r.State != "resolved" {
			t.Fatal(r, e)
		}
	}
	q := featureCounter(t, s)
	checks := []struct {
		name string
		max  int
		run  func() error
	}{
		{"groups", 3, func() error {
			x, e := s.Groups(ctx, reader)
			if e == nil && (len(x) != 32 || !x[0].MembersComplete || len(x[0].Members) != 2) {
				t.Fatal(x)
			}
			return e
		}},
		{"roles", 3, func() error {
			x, e := s.Roles(ctx, reader)
			if e == nil && (len(x) != 1 || x[0].ID != role.ID) {
				t.Fatal(x)
			}
			return e
		}},
		{"bindings", 4, func() error {
			x, e := s.Bindings(ctx, reader)
			if e == nil && (len(x) != 1 || x[0].RoleID != role.ID) {
				t.Fatal(x)
			}
			return e
		}},
		{"answers", 3, func() error {
			x, e := s.Answers(ctx, reader)
			if e == nil && len(x) != 32 {
				t.Fatal(len(x))
			}
			return e
		}},
		{"requests", 4, func() error {
			x, e := s.Requests(ctx, reader)
			if e == nil && len(x) != 33 {
				t.Fatal(len(x))
			}
			for _, r := range x {
				if r.Response.SourceInvalidated {
					t.Fatal("valid publisher marked invalid")
				}
			}
			return e
		}},
	}
	for _, collection := range []string{"agents", "groups", "roles", "requests", "conversations", "answers", "credentials"} {
		checks = append(checks, struct {
			name string
			max  int
			run  func() error
		}{"query_" + collection, 5, func() error { _, e := s.Query(ctx, reader, QueryInput{Collection: collection, Limit: 200}); return e }})
	}
	for _, c := range checks {
		t.Run(c.name, func(t *testing.T) {
			q.total.Store(0)
			if err := c.run(); err != nil {
				t.Fatal(err)
			}
			if count := q.total.Load(); count > int64(c.max) {
				t.Fatalf("read work grows with rows: %d queries, budget %d", count, c.max)
			}
		})
	}
	// Reusing the caller's context in a later operation must not reuse authority.
	role.Rules = []Rule{}
	if _, err = s.SaveRole(ctx, owner, role); err != nil {
		t.Fatal(err)
	}
	if x, e := s.Answers(ctx, owner); e != nil || len(x) != 0 {
		t.Fatal("revoked publisher remained current", len(x), e)
	}
	if x, e := s.Requests(ctx, owner); e != nil || len(x) != 33 {
		t.Fatal(len(x), e)
	} else {
		for _, r := range x {
			if r.Response.SourceAnswerID != "" && !r.Response.SourceInvalidated {
				t.Fatal("revocation not shown", r.ID)
			}
		}
	}
	if x, e := s.Requests(ctx, reader); e != nil || len(x) != 0 {
		t.Fatal("revoked View retained", len(x), e)
	}
	if x, e := s.Query(ctx, reader, QueryInput{Collection: "agents", Limit: 200}); e != nil || len(x.Items) != 0 {
		t.Fatal("revoked page leaked agents", x, e)
	}
	groups, e := s.Groups(ctx, reader)
	if e != nil || len(groups) != 32 {
		t.Fatal(groups, e)
	}
	for _, g := range groups {
		if !g.Member || g.MembersComplete || !reflect.DeepEqual(g.Members, []string{reader.ID}) {
			t.Fatal("member listing disclosed peers without View", g)
		}
	}
}

func TestDeliveryPageBatchMatchesIndividualPolicyAndAttemptCursor(t *testing.T) {
	s, owner := testStore(t)
	ctx := context.Background()
	sender := testWorker(t, s, owner, "sender")
	recipient := testPerson(t, s, owner, "recipient")
	peer := testPerson(t, s, owner, "diagnostic-peer")
	stranger := testPerson(t, s, owner, "stranger")
	role := grant(t, s, owner, recipient, []Rule{{Action: "view", TargetKind: "agent", TargetID: sender.ID}, {Action: "message", TargetKind: "agent", TargetID: sender.ID}})
	grant(t, s, owner, peer, []Rule{{Action: "view", TargetKind: "person", TargetID: "*"}, {Action: "view", TargetKind: "agent", TargetID: "*"}, {Action: "view", TargetKind: "delivery", TargetID: "*"}})
	group, err := s.SaveGroup(ctx, owner, Group{Name: "Recipients", Members: []string{recipient.ID, peer.ID}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Send(ctx, sender, SendInput{Target: Target{"person", recipient.ID}, Body: "Private direct message"}, "direct"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Send(ctx, sender, SendInput{Target: Target{"group", group.ID}, Body: "Private group fanout"}, "group"); err != nil {
		t.Fatal(err)
	}
	request, err := s.Ask(ctx, sender, AskInput{Target: Target{"group", group.ID}, Title: "Which environment?", SharedKey: "env/v1"}, "question")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Respond(ctx, recipient, request.ID, Response{Outcome: "answered", Answer: "Staging", Version: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.PublishAnswer(ctx, owner, PublishInput{RequestID: request.ID, Audience: []string{sender.ID}}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Ask(ctx, sender, AskInput{Target: Target{"group", group.ID}, Title: "Which environment?", SharedKey: "env/v1"}, "reused"); err != nil {
		t.Fatal(err)
	}
	deliveries, err := s.Deliveries(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range deliveries {
		if _, err = s.Pool.Exec(ctx, `INSERT INTO delivery_attempts(id,scope,delivery_id,generation,outcome,detail,created_at) SELECT $1||n,$2,$3,n,'failed','retained diagnostic',to_timestamp(1700000000)+n*interval '1 microsecond' FROM generate_series(1,13) n`, d.ID+"-attempt-", owner.Scope, d.ID); err != nil {
			t.Fatal(err)
		}
	}
	check := func(label string) {
		t.Helper()
		for _, a := range []Actor{owner, sender, recipient, peer, stranger} {
			page, e := s.Query(ctx, a, QueryInput{Collection: "deliveries", Limit: 200})
			if e != nil {
				t.Fatal(label, a.Name, e)
			}
			got := map[string]DeliveryEvidence{}
			for _, item := range page.Items {
				x := item.(DeliveryEvidence)
				got[x.ID] = x
			}
			for _, d := range deliveries {
				item, e := s.queryRecord(withPolicySnapshot(ctx), a, "deliveries", d.ID)
				x, ok := got[d.ID]
				if e != nil {
					if ErrorStatus(e) != 403 && ErrorStatus(e) != 404 {
						t.Fatal(label, e)
					}
					if ok {
						t.Fatal(label, "batch leaked denied delivery", a.Name, d.ID)
					}
					continue
				}
				if !ok {
					t.Fatal(label, "batch omitted allowed delivery", a.Name, d.ID)
				}
				want := item.(DeliveryEvidence)
				cursor := x.Attempts.NextCursor
				x.Attempts.NextCursor = ""
				hasCursor := want.Attempts.NextCursor != ""
				want.Attempts.NextCursor = ""
				if !reflect.DeepEqual(x, want) || (cursor != "") != hasCursor {
					t.Fatalf("%s %s delivery mismatch\ngot %s\nwant %s", label, a.Name, JSON(x), JSON(want))
				}
				if cursor != "" {
					next, e := s.Query(ctx, a, QueryInput{Collection: "delivery_attempts", Filters: map[string]string{"delivery_id": d.ID}, Limit: 10, Cursor: cursor})
					if e != nil || len(next.Items) != 3 || next.NextCursor != "" {
						t.Fatal("attempt cursor mismatch", next, e)
					}
				}
			}
		}
	}
	check("current")
	group.Members = []string{peer.ID}
	if _, err = s.SaveGroup(ctx, owner, group); err != nil {
		t.Fatal(err)
	}
	check("removed group member")
	role.Rules = []Rule{}
	if _, err = s.SaveRole(ctx, owner, role); err != nil {
		t.Fatal(err)
	}
	check("recipient View revoked")
	if _, err = s.Pool.Exec(ctx, `UPDATE credentials SET revoked=true WHERE scope=$1 AND id=$2`, owner.Scope, sender.CredentialID); err != nil {
		t.Fatal(err)
	}
	check("sender credential revoked")
	if _, err = s.Pool.Exec(ctx, `UPDATE requests SET expires_at=$2 WHERE scope=$1`, owner.Scope, time.Now().Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	check("expired requests")
}

func TestQueryBatchMatchesSingleReadersWithoutProfileMetadata(t *testing.T) {
	s, owner := testStore(t)
	ctx := context.Background()
	w := testWorker(t, s, owner, "agent")
	p := testPerson(t, s, owner, "person")
	grant(t, s, owner, p, []Rule{{Action: "view", TargetKind: "agent", TargetID: w.ID}})
	sent, err := s.Send(ctx, w, SendInput{Target: Target{"person", p.ID}, Body: "private message"}, "chat")
	if err != nil {
		t.Fatal(err)
	}
	s.AttachmentsRoot = t.TempDir()
	if _, err = s.UploadAttachment(ctx, w, sent.ConversationID, "proof.txt", "text/plain", []byte("private evidence")); err != nil {
		t.Fatal(err)
	}
	for _, a := range []Actor{owner, w, p} {
		for _, collection := range []string{"agents", "people", "groups", "roles", "requests", "conversations", "answers", "credentials", "instances", "messages", "attachments"} {
			filters := map[string]string{}
			if collection == "messages" || collection == "attachments" {
				filters["conversation_id"] = sent.ConversationID
			}
			page, e := s.Query(ctx, a, QueryInput{Collection: collection, Limit: 200, Filters: filters})
			if e != nil {
				t.Fatal(collection, e)
			}
			for _, item := range page.Items {
				var identity struct {
					ID string `json:"id"`
				}
				if e = json.Unmarshal(JSON(item), &identity); e != nil {
					t.Fatal(e)
				}
				want, e := s.queryRecord(withPolicySnapshot(ctx), a, collection, identity.ID)
				if e != nil || !reflect.DeepEqual(item, want) {
					t.Fatalf("%s batch differs from single reader: %s / %s, %v", collection, JSON(item), JSON(want), e)
				}
			}
		}
	}
}

func TestBindingBatchMatchesProtectedAndGroupAssignmentVisibility(t *testing.T) {
	s, owner := testStore(t)
	ctx := context.Background()
	manager := testPerson(t, s, owner, "binding-manager")
	member := testPerson(t, s, owner, "binding-member")
	role := grant(t, s, owner, manager, []Rule{{Action: "manage", TargetKind: "role_binding", TargetID: "*"}, {Action: "manage", TargetKind: "person", TargetID: "*"}, {Action: "manage", TargetKind: "group", TargetID: "*"}})
	if e := s.BindRole(ctx, owner, Binding{role.ID, "principal", owner.ID}, false); e != nil {
		t.Fatal(e)
	}
	if e := s.BindRole(ctx, owner, Binding{role.ID, "principal", member.ID}, false); e != nil {
		t.Fatal(e)
	}
	g, e := s.SaveGroup(ctx, owner, Group{Name: "Assignment group", Members: []string{member.ID}})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.BindRole(ctx, owner, Binding{role.ID, "group", g.ID}, false); e != nil {
		t.Fatal(e)
	}
	all, e := s.Bindings(ctx, owner)
	if e != nil {
		t.Fatal(e)
	}
	for _, a := range []Actor{manager, member} {
		got, e := s.Bindings(ctx, a)
		if e != nil {
			t.Fatal(e)
		}
		seen := map[Binding]bool{}
		for _, b := range got {
			seen[b] = true
		}
		for _, b := range all {
			want := s.bindingVisible(withPolicySnapshot(ctx), s.Pool, a, b)
			if seen[b] != want {
				t.Fatal("assignment visibility changed", a.Name, b, seen[b], want)
			}
			if a.ID == manager.ID && b.SubjectID == owner.ID && seen[b] {
				t.Fatal("protected administrator assignment exposed")
			}
		}
	}
}
