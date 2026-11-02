package engine

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
)

type routeFixture struct {
	destinations []Destination
	counts       []*atomic.Int32
}

func testRoutes(t *testing.T, s *Store, a Actor, person string, statuses ...int) routeFixture {
	t.Helper()
	s.AllowLocal = true
	f := routeFixture{}
	for i, status := range statuses {
		count := &atomic.Int32{}
		f.counts = append(f.counts, count)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			count.Add(1)
			if status == 0 {
				conn, _, _ := w.(http.Hijacker).Hijack()
				conn.Close()
				return
			}
			w.WriteHeader(status)
		}))
		t.Cleanup(server.Close)
		c, err := s.SaveConnection(context.Background(), a, Connection{Name: fmt.Sprint("route-", i), Kind: "webhook", Enabled: true, Config: map[string]string{"url": server.URL}, Secret: "routing-fixture-secret"})
		if err != nil {
			t.Fatal(err)
		}
		mode := "fallback"
		if i == 0 {
			mode = "primary"
		}
		d, err := s.SaveDestination(context.Background(), a, Destination{ConnectionID: c.ID, PrincipalID: person, Address: "person", Mode: mode, Priority: i})
		if err != nil {
			t.Fatal(err)
		}
		f.destinations = append(f.destinations, d)
	}
	return f
}
func saveRouting(t *testing.T, s *Store, a Actor, p DeliveryPreferences) DeliveryPreferences {
	t.Helper()
	out, err := s.SaveDeliveryPreferences(context.Background(), a, p.PersonID, p)
	if err != nil {
		t.Fatal(err)
	}
	return out
}
func policy(f routeFixture, indices ...int) *ChannelPolicy {
	p := &ChannelPolicy{Mode: "preferred", Destinations: []string{}}
	for _, i := range indices {
		p.Destinations = append(p.Destinations, f.destinations[i].ID)
	}
	return p
}
func queuedRoute(t *testing.T, s *Store, op string) string {
	t.Helper()
	var conn string
	if err := s.Pool.QueryRow(context.Background(), `SELECT connection_id FROM deliveries WHERE operation_id=$1 AND connection_id<>'' AND state='queued'`, op).Scan(&conn); err != nil {
		t.Fatal(err)
	}
	return conn
}
func TestDeliveryPreferenceOwnershipAndValidation(t *testing.T) {
	s, owner := testStore(t)
	ctx := context.Background()
	person := testPerson(t, s, owner, "routing-person")
	other := testPerson(t, s, owner, "routing-other")
	session, err := s.Enroll(ctx, owner, EnrollInput{Agent: "routing-worker", Name: "Routing worker"})
	if err != nil {
		t.Fatal(err)
	}
	worker, err := s.Authenticate(ctx, session.Token)
	if err != nil {
		t.Fatal(err)
	}
	f := testRoutes(t, s, owner, person.ID, 202, 202)
	in := DeliveryPreferences{PersonID: person.ID, Default: policy(f, 0, 1), Overrides: []ChannelOverride{}}
	p := saveRouting(t, s, person, in)
	if _, err := s.SaveDeliveryPreferences(ctx, person, person.ID, in); ErrorStatus(err) != 409 {
		t.Fatal("stale preference accepted", err)
	}
	if _, err := s.DeliveryPreferences(ctx, other, person.ID); ErrorStatus(err) != 403 {
		t.Fatal("other Person read preferences", err)
	}
	if _, err := s.SaveDeliveryPreferences(ctx, worker, person.ID, p); ErrorStatus(err) != 403 {
		t.Fatal("Agent edited recipient preferences", err)
	}
	p.Default.Destinations = append(p.Default.Destinations, p.Default.Destinations[0])
	if _, err := s.SaveDeliveryPreferences(ctx, person, person.ID, p); ErrorStatus(err) != 400 {
		t.Fatal("duplicate destination", err)
	}
	p.Default = policy(f, 0)
	p.Default.FollowupSeconds = 1
	if _, err := s.SaveDeliveryPreferences(ctx, person, person.ID, p); ErrorStatus(err) != 400 {
		t.Fatal("unbounded timer", err)
	}
	p.Default = policy(f, 0)
	p.Default.Destinations = []string{"missing"}
	if _, err := s.SaveDeliveryPreferences(ctx, person, person.ID, p); ErrorStatus(err) != 400 {
		t.Fatal("foreign destination", err)
	}
	// Preferences cannot grant communication rights.
	p.Default = policy(f, 0)
	saveRouting(t, s, person, p)
	if _, err := s.Send(ctx, other, SendInput{Target: Target{"person", person.ID}, Body: "Forbidden"}, "forbidden"); ErrorStatus(err) != 403 {
		t.Fatal("policy granted contact rights", err)
	}
}
func TestChannelResolutionAgentAndRoleOverrideOrder(t *testing.T) {
	s, owner := testStore(t)
	ctx := context.Background()
	worker := testWorker(t, s, owner, "routing-specialist")
	f := testRoutes(t, s, owner, owner.ID, 202, 202, 202)
	roleA, err := s.SaveGroup(ctx, owner, Group{Name: "Reviewers", Purpose: "agent_role", Members: []string{worker.ID}})
	if err != nil {
		t.Fatal(err)
	}
	roleB, err := s.SaveGroup(ctx, owner, Group{Name: "Builders", Purpose: "agent_role", Members: []string{worker.ID}})
	if err != nil {
		t.Fatal(err)
	}
	p := DeliveryPreferences{PersonID: owner.ID, Default: policy(f, 0), Overrides: []ChannelOverride{{Source: Target{"group", roleA.ID}, Policy: *policy(f, 1)}, {Source: Target{"group", roleB.ID}, Policy: *policy(f, 2)}}}
	p = saveRouting(t, s, owner, p)
	send := func(want int, key string, conv string) string {
		t.Helper()
		out, err := s.Send(ctx, worker, SendInput{Target: Target{"person", owner.ID}, ConversationID: conv, Body: "Ready"}, key)
		if err != nil {
			t.Fatal(err)
		}
		if got := queuedRoute(t, s, out.Messages[0].ID); got != f.destinations[want].ConnectionID {
			t.Fatal("wrong channel", got, want)
		}
		return out.ConversationID
	}
	conv := send(1, "role-a", "")
	p.Overrides[0], p.Overrides[1] = p.Overrides[1], p.Overrides[0]
	p = saveRouting(t, s, owner, p)
	send(2, "role-b", conv)
	p.Overrides = append(p.Overrides, ChannelOverride{Source: Target{"agent", worker.ID}, Policy: *policy(f, 0)})
	p = saveRouting(t, s, owner, p)
	send(0, "agent", "")
	p.Overrides = nil
	p = saveRouting(t, s, owner, p)
	send(0, "default", conv)
}
func TestDeliveryPreferenceDelayBoundsAndLegacyEdits(t *testing.T) {
	s, owner := testStore(t)
	ctx := context.Background()
	f := testRoutes(t, s, owner, owner.ID, 202, 202)
	p := DeliveryPreferences{PersonID: owner.ID, Default: policy(f, 0, 1)}
	for _, seconds := range []int{5, 15, 30, 60, 180, 300, 600, 900} {
		p.Default.FollowupSeconds = seconds
		p = saveRouting(t, s, owner, p)
	}
	for _, seconds := range []int{-1, 1, 4, 901, 3600, 604800, 604801} {
		p.Default.FollowupSeconds = seconds
		if _, err := s.SaveDeliveryPreferences(ctx, owner, owner.ID, p); ErrorStatus(err) != 400 {
			t.Fatalf("invalid timer %d accepted: %v", seconds, err)
		}
	}
	// Previously saved policies stay editable without silently changing their timer.
	if _, err := s.Pool.Exec(ctx, `UPDATE delivery_preferences SET default_policy=jsonb_set(default_policy,'{followup_seconds}','3600') WHERE scope=$1 AND person_id=$2`, owner.Scope, owner.ID); err != nil {
		t.Fatal(err)
	}
	var err error
	p, err = s.DeliveryPreferences(ctx, owner, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	p.Default.Destinations[0], p.Default.Destinations[1] = p.Default.Destinations[1], p.Default.Destinations[0]
	p = saveRouting(t, s, owner, p)
	p.Default.FollowupSeconds = 1800
	if _, err = s.SaveDeliveryPreferences(ctx, owner, owner.ID, p); ErrorStatus(err) != 400 {
		t.Fatal("legacy timer could change outside the new bounds", err)
	}
	p.Default.FollowupSeconds = 30
	p = saveRouting(t, s, owner, p)
	p.Default.FollowupSeconds = 3600
	if _, err = s.SaveDeliveryPreferences(ctx, owner, owner.ID, p); ErrorStatus(err) != 400 {
		t.Fatal("removed legacy timer could be reintroduced", err)
	}
	p.Default.FollowupSeconds = 30
	worker := testWorker(t, s, owner, "legacy-delay")
	p.Overrides = []ChannelOverride{{Source: Target{"agent", worker.ID}, Policy: *policy(f, 0, 1)}}
	p = saveRouting(t, s, owner, p)
	if _, err = s.Pool.Exec(ctx, `UPDATE delivery_preferences SET overrides=jsonb_set(overrides,'{0,policy,followup_seconds}','3600') WHERE scope=$1 AND person_id=$2`, owner.Scope, owner.ID); err != nil {
		t.Fatal(err)
	}
	p, err = s.DeliveryPreferences(ctx, owner, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	p = saveRouting(t, s, owner, p)
	p.Overrides[0].Policy.FollowupSeconds = 1800
	if _, err = s.SaveDeliveryPreferences(ctx, owner, owner.ID, p); ErrorStatus(err) != 400 {
		t.Fatal("override timer could change outside the new bounds", err)
	}
	p.Overrides[0].Policy.FollowupSeconds = 5
	saveRouting(t, s, owner, p)
}
func TestRequestFollowupDurabilityAndResolution(t *testing.T) {
	s, owner := testStore(t)
	ctx := context.Background()
	worker := testWorker(t, s, owner, "routing-asker")
	f := testRoutes(t, s, owner, owner.ID, 202, 202, 202)
	p := DeliveryPreferences{PersonID: owner.ID, Default: policy(f, 0, 1, 2)}
	p.Default.FollowupSeconds = 5
	saveRouting(t, s, owner, p)
	q, err := s.Ask(ctx, worker, AskInput{Target: Target{"person", owner.ID}, Title: "Which release?"}, "timed-question")
	if err != nil {
		t.Fatal(err)
	}
	if err = s.DispatchOnce(ctx, "https://example.test"); err != nil {
		t.Fatal(err)
	}
	var wait float64
	if err = s.Pool.QueryRow(ctx, `SELECT extract(epoch FROM next_at-now()) FROM deliveries WHERE operation_id=$1 AND connection_id=$2`, q.ID, f.destinations[1].ConnectionID).Scan(&wait); err != nil || wait < 3 || wait > 5 {
		t.Fatal("missing durable timer", wait, err)
	}
	schema := strings.Split(s.Pool.Config().ConnConfig.RuntimeParams["search_path"], ",")[0]
	replica, err := OpenExisting(ctx, os.Getenv("NOTIF_TEST_DATABASE_URL"), schema, s.SecretKey)
	if err != nil {
		t.Fatal(err)
	}
	defer replica.Close()
	replica.AllowLocal = true
	if _, err = s.Pool.Exec(ctx, `UPDATE deliveries SET next_at=now() WHERE operation_id=$1 AND state='queued' AND connection_id<>''`, q.ID); err != nil {
		t.Fatal(err)
	}
	if err = replica.DispatchOnce(ctx, "https://example.test"); err != nil {
		t.Fatal(err)
	}
	if f.counts[0].Load() != 1 || f.counts[1].Load() != 1 || f.counts[2].Load() != 0 {
		t.Fatal("follow-up order", f.counts[0].Load(), f.counts[1].Load(), f.counts[2].Load())
	}
	if _, err = s.Respond(ctx, owner, q.ID, Response{Outcome: "answered", Answer: "V1", Version: q.Version}); err != nil {
		t.Fatal(err)
	}
	var pending int
	_ = s.Pool.QueryRow(ctx, `SELECT count(*) FROM deliveries WHERE operation_id=$1 AND connection_id<>'' AND state IN ('queued','standby')`, q.ID).Scan(&pending)
	if pending != 0 {
		t.Fatal("resolved request retained reminders", pending)
	}
	if err = replica.DispatchOnce(ctx, "https://example.test"); err != nil {
		t.Fatal(err)
	}
	if f.counts[2].Load() != 0 {
		t.Fatal("follow-up after response")
	}
}
func TestMessageHasNoResponseTimerAndAllChannelsAreExplicit(t *testing.T) {
	s, owner := testStore(t)
	ctx := context.Background()
	f := testRoutes(t, s, owner, owner.ID, 202, 202)
	p := DeliveryPreferences{PersonID: owner.ID, Default: policy(f, 0, 1)}
	p.Default.FollowupSeconds = 60
	p = saveRouting(t, s, owner, p)
	out, err := s.Send(ctx, owner, SendInput{Target: Target{"person", owner.ID}, Body: "Just an update"}, "no-message-reminder")
	if err != nil {
		t.Fatal(err)
	}
	if err = s.DispatchOnce(ctx, "https://example.test"); err != nil {
		t.Fatal(err)
	}
	var state string
	_ = s.Pool.QueryRow(ctx, `SELECT state FROM deliveries WHERE operation_id=$1 AND connection_id=$2`, out.Messages[0].ID, f.destinations[1].ConnectionID).Scan(&state)
	if state != "cancelled" {
		t.Fatal("ordinary message started no-response timer", state)
	}
	p.Default.Mode = "all"
	p.Default.FollowupSeconds = 0
	saveRouting(t, s, owner, p)
	out, err = s.Send(ctx, owner, SendInput{Target: Target{"person", owner.ID}, Body: "All selected"}, "all")
	if err != nil {
		t.Fatal(err)
	}
	var n int
	_ = s.Pool.QueryRow(ctx, `SELECT count(*) FROM deliveries WHERE operation_id=$1 AND connection_id<>'' AND state='queued'`, out.Messages[0].ID).Scan(&n)
	if n != 2 {
		t.Fatal("all mode", n)
	}
}
func TestUnknownProviderOutcomeDoesNotAdvanceRouting(t *testing.T) {
	s, owner := testStore(t)
	ctx := context.Background()
	f := testRoutes(t, s, owner, owner.ID, 0, 202)
	p := DeliveryPreferences{PersonID: owner.ID, Default: policy(f, 0, 1)}
	p.Default.FollowupSeconds = 60
	saveRouting(t, s, owner, p)
	q, err := s.Ask(ctx, owner, AskInput{Target: Target{"person", owner.ID}, Title: "Continue?"}, "unknown-route")
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err = s.DispatchOnce(ctx, "https://example.test"); err != nil {
			t.Fatal(err)
		}
	}
	var state string
	_ = s.Pool.QueryRow(ctx, `SELECT state FROM deliveries WHERE operation_id=$1 AND connection_id=$2`, q.ID, f.destinations[0].ConnectionID).Scan(&state)
	if state != "unknown" || f.counts[1].Load() != 0 {
		t.Fatal("ambiguous outcome fanned out", state)
	}
}
func TestChannelFollowupsRecheckPreferencesAndPermissions(t *testing.T) {
	for _, reason := range []string{"preferences", "disabled", "permission", "expiry", "cancel", "removed", "changed"} {
		t.Run(reason, func(t *testing.T) {
			s, owner := testStore(t)
			ctx := context.Background()
			worker := testWorker(t, s, owner, "reviewer")
			f := testRoutes(t, s, owner, owner.ID, 202, 202)
			p := DeliveryPreferences{PersonID: owner.ID, Default: policy(f, 0, 1)}
			p.Default.FollowupSeconds = 60
			p = saveRouting(t, s, owner, p)
			q, err := s.Ask(ctx, worker, AskInput{Target: Target{"person", owner.ID}, Title: "Approval context?"}, "pending")
			if err != nil {
				t.Fatal(err)
			}
			if err = s.DispatchOnce(ctx, "https://example.test"); err != nil {
				t.Fatal(err)
			}
			switch reason {
			case "preferences":
				p.Default = policy(f)
				saveRouting(t, s, owner, p)
			case "removed":
				err = s.RemoveDestination(ctx, owner, f.destinations[1].ID, f.destinations[1].Version)
			case "changed":
				d := f.destinations[1]
				d.Address = "changed-address"
				_, err = s.SaveDestination(ctx, owner, d)
			case "disabled":
				_, err = s.Pool.Exec(ctx, `UPDATE connections SET enabled=false WHERE id=$1`, f.destinations[1].ConnectionID)
			case "permission":
				_, err = s.Pool.Exec(ctx, `UPDATE credentials SET revoked=true WHERE id=$1`, worker.CredentialID)
			case "expiry":
				_, err = s.Pool.Exec(ctx, `UPDATE requests SET expires_at=now()-interval '1 second' WHERE id=$1`, q.ID)
			case "cancel":
				_, err = s.EndRequest(ctx, worker, q.ID, "cancelled", q.Version)
			}
			if err != nil {
				t.Fatal(err)
			}
			_, err = s.Pool.Exec(ctx, `UPDATE deliveries SET next_at=now() WHERE operation_id=$1 AND connection_id<>''`, q.ID)
			if err != nil {
				t.Fatal(err)
			}
			if err = s.DispatchOnce(ctx, "https://example.test"); err != nil {
				t.Fatal(err)
			}
			if f.counts[1].Load() != 0 {
				t.Fatal("invalid follow-up was sent", reason)
			}
		})
	}
}
func TestPreferredRoutingSkipsUnavailableAndDefiniteFailures(t *testing.T) {
	s, owner := testStore(t)
	ctx := context.Background()
	f := testRoutes(t, s, owner, owner.ID, 202, 400, 202)
	saveRouting(t, s, owner, DeliveryPreferences{PersonID: owner.ID, Default: policy(f, 0, 1, 2)})
	_, err := s.Pool.Exec(ctx, `UPDATE connections SET enabled=false WHERE id=$1`, f.destinations[0].ConnectionID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Send(ctx, owner, SendInput{Target: Target{"person", owner.ID}, Body: "Notify"}, "skip")
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err = s.DispatchOnce(ctx, "https://example.test"); err != nil {
			t.Fatal(err)
		}
	}
	if f.counts[0].Load() != 0 || f.counts[1].Load() != 1 || f.counts[2].Load() != 1 {
		t.Fatal("failure did not skip unavailable channel")
	}
}
func TestPreviewInvalidatedByDeliveryPreferences(t *testing.T) {
	s, owner := testStore(t)
	ctx := context.Background()
	f := testRoutes(t, s, owner, owner.ID, 202, 202)
	p := saveRouting(t, s, owner, DeliveryPreferences{PersonID: owner.ID, Default: policy(f, 0)})
	preview, err := s.DispatchPreview(ctx, owner, DispatchPreviewInput{Operation: "message", Targets: []Target{{"person", owner.ID}}})
	if err != nil {
		t.Fatal(err)
	}
	p.Default = policy(f, 1)
	saveRouting(t, s, owner, p)
	_, err = s.Send(ctx, owner, SendInput{Target: Target{"person", owner.ID}, Body: "Reviewed", PreviewToken: preview.Token}, "reviewed")
	if ErrorStatus(err) != 409 {
		t.Fatal("stale routing preview accepted", err)
	}
}

func TestOptionalCopyFailureDoesNotEscalateDeliveredPrimary(t *testing.T) {
	s, owner := testStore(t)
	ctx := context.Background()
	f := testRoutes(t, s, owner, owner.ID, 202, 400, 202)
	copy := f.destinations[1]
	copy.Mode = "copy"
	if _, err := s.SaveDestination(ctx, owner, copy); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Send(ctx, owner, SendInput{Target: Target{"person", owner.ID}, Body: "Update"}, "copy-failure"); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		if err := s.DispatchOnce(ctx, "https://example.test"); err != nil {
			t.Fatal(err)
		}
	}
	if f.counts[0].Load() != 1 || f.counts[1].Load() != 1 || f.counts[2].Load() != 0 {
		t.Fatal("optional copy failure escalated primary")
	}
}

func TestApprovalResponseDuringProviderSendDoesNotRearmFollowup(t *testing.T) {
	s, owner := testStore(t)
	ctx := context.Background()
	worker := testWorker(t, s, owner, "approval-reviewer")
	f := testRoutes(t, s, owner, owner.ID, 202, 202)
	p := DeliveryPreferences{PersonID: owner.ID, Default: policy(f, 0, 1)}
	p.Default.FollowupSeconds = 60
	saveRouting(t, s, owner, p)
	q, err := s.Ask(ctx, worker, AskInput{Target: Target{"person", owner.ID}, Kind: "approval", Title: "Publish this report?", Action: &Action{Name: "publish", ExecutorID: worker.ID, Resource: "report", ResourceVersion: "1", OperationKey: "report-v1", Arguments: map[string]any{}}}, "approval-race")
	if err != nil {
		t.Fatal(err)
	}
	job, worked, err := s.claimProvider(ctx)
	if err != nil || !worked || job == nil {
		t.Fatal(worked, err)
	}
	if _, err = s.Respond(ctx, owner, q.ID, Response{Outcome: "approved", Version: q.Version}); err != nil {
		t.Fatal(err)
	}
	if err = s.finishProvider(ctx, "https://example.test", job); err != nil {
		t.Fatal(err)
	}
	var pending int
	_ = s.Pool.QueryRow(ctx, `SELECT count(*) FROM deliveries WHERE operation_id=$1 AND connection_id<>'' AND state IN ('queued','standby')`, q.ID).Scan(&pending)
	if pending != 0 {
		t.Fatal("late provider acceptance rearmed follow-up")
	}
}

func TestReplyAffinityRequiresInboundAndRespectsSelectedChannels(t *testing.T) {
	s, owner := testStore(t)
	ctx := context.Background()
	worker := testWorker(t, s, owner, "affinity-worker")
	f := testRoutes(t, s, owner, owner.ID, 202, 202)
	p := saveRouting(t, s, owner, DeliveryPreferences{PersonID: owner.ID, Default: policy(f, 0, 1)})
	start, err := s.Send(ctx, owner, SendInput{Target: Target{"agent", worker.ID}, Body: "Start a conversation"}, "affinity-start")
	if err != nil {
		t.Fatal(err)
	}
	if err = s.recordThread(ctx, s.Pool, owner.Scope, f.destinations[1].ConnectionID, "thread-channel", "thread-1", start.ConversationID, owner.ID); err != nil {
		t.Fatal(err)
	}
	assertRoute := func(key, connection, address string, count int) {
		t.Helper()
		message, err := s.Send(ctx, worker, SendInput{ConversationID: start.ConversationID, Body: "Continue the same conversation"}, key)
		if err != nil {
			t.Fatal(err)
		}
		var n int
		if err = s.Pool.QueryRow(ctx, `SELECT count(*) FROM deliveries WHERE operation_id=$1 AND connection_id<>''`, message.Messages[0].ID).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != count {
			t.Fatalf("external route count %d, want %d", n, count)
		}
		if count == 0 {
			return
		}
		var c, a string
		if err = s.Pool.QueryRow(ctx, `SELECT connection_id,address FROM deliveries WHERE operation_id=$1 AND connection_id<>'' AND state='queued'`, message.Messages[0].ID).Scan(&c, &a); err != nil {
			t.Fatal(err)
		}
		if c != connection || a != address {
			t.Fatalf("route %s/%s, want %s/%s", c, a, connection, address)
		}
	}
	assertRoute("outbound-receipt", f.destinations[0].ConnectionID, "person", 2)
	if err = s.recordInboundThread(ctx, s.Pool, owner.Scope, f.destinations[1].ConnectionID, "thread-channel", "thread-1", start.ConversationID, owner.ID); err != nil {
		t.Fatal(err)
	}
	assertRoute("inbound-affinity", f.destinations[1].ConnectionID, "thread-channel", 2)
	p.Default = policy(f, 0)
	p = saveRouting(t, s, owner, p)
	assertRoute("excluded-origin", f.destinations[0].ConnectionID, "person", 1)
	p.Default.Destinations = []string{}
	saveRouting(t, s, owner, p)
	assertRoute("workspace-only", "", "", 0)
}
