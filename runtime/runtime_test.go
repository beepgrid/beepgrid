package runtime

import (
	"bufio"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/notif-dev/notif/internal/engine"
	"github.com/notif-dev/notif/sdk/go/notif"
)

func service(t *testing.T) (*Service, Config) {
	return serviceWithBudget(t, 4)
}

func serviceWithBudget(t *testing.T, budget int32) (*Service, Config) {
	t.Helper()
	db := os.Getenv("NOTIF_TEST_DATABASE_URL")
	if db == "" {
		t.Skip("PostgreSQL is required")
	}
	c := Config{DatabaseURL: db, Schema: "runtime_" + strings.TrimPrefix(engine.ID(""), "_"), PublicURL: "http://localhost", SecretKey: []byte("01234567890123456789012345678901"), MaxConnections: budget}
	if err := Migrate(context.Background(), c.DatabaseURL, c.Schema); err != nil {
		t.Fatal(err)
	}
	s, err := Open(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.store.Pool.Exec(context.Background(), "DROP SCHEMA "+c.Schema+" CASCADE"); s.Close() })
	return s, c
}

func TestSharedRuntimeTenantIsolationAndSuspension(t *testing.T) {
	s, c := service(t)
	stats, err := s.Workload(context.Background())
	if err != nil || stats.Queued != 0 || stats.DatabaseConnections > 4 {
		t.Fatal(stats, err)
	}
	ctx := context.Background()
	tokenA, tokenB := engine.Secret(), engine.Secret()
	if err := s.ProvisionWorkspace(ctx, "tenant_a", "A", tokenA); err != nil {
		t.Fatal(err)
	}
	if err := s.ProvisionWorkspace(ctx, "tenant_b", "B", tokenB); err != nil {
		t.Fatal(err)
	}
	if err := s.ProvisionWorkspace(ctx, "tenant_a", "A", tokenA); err != nil {
		t.Fatal("replay", err)
	}
	if err := s.ProvisionWorkspace(ctx, "tenant_a", "A", tokenB); err == nil {
		t.Fatal("credential hijack")
	}
	if err := s.ProvisionWorkspace(ctx, "tenant_a", "Changed", tokenA); err == nil {
		t.Fatal("conflicting replay")
	}
	server := httptest.NewServer(s.Handler())
	defer server.Close()
	a, b := notif.New(server.URL, tokenA), notif.New(server.URL, tokenB)
	var person struct {
		Person notif.Person `json:"person"`
	}
	if err := a.Do(ctx, "GET", "/api/v1/me", nil, &person, ""); err != nil {
		t.Fatal(err)
	}
	_, session, err := b.Enroll(ctx, notif.EnrollInput{Agent: "same-name", Name: "Scout"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = a.Send(ctx, notif.SendInput{Target: notif.Target{Kind: "agent", ID: session.Agent.ID}, Body: "cross tenant"}, "cross"); err == nil {
		t.Fatal("cross-tenant target accepted")
	}
	if _, err = b.Ask(ctx, notif.AskInput{Target: notif.Target{Kind: "person", ID: person.Person.ID}, Title: "Approve?", Choices: []string{"Yes"}}, "cross"); err == nil {
		t.Fatal("cross-tenant approval accepted")
	}
	if err = s.SetSuspended(ctx, "tenant_a", true); err != nil {
		t.Fatal(err)
	}
	if err = a.Do(ctx, "GET", "/api/v1/me", nil, nil, ""); err == nil {
		t.Fatal("suspended credential works")
	}
	if err = b.Do(ctx, "GET", "/api/v1/me", nil, nil, ""); err != nil {
		t.Fatal("other tenant affected", err)
	}
	replica, err := Open(ctx, c)
	if err != nil {
		t.Fatal(err)
	}
	defer replica.Close()
	if _, err = replica.store.Authenticate(ctx, tokenA); err == nil {
		t.Fatal("replica ignored suspension")
	}
	if err = s.SetSuspended(ctx, "tenant_a", false); err != nil {
		t.Fatal(err)
	}
	if err = a.Do(ctx, "GET", "/api/v1/me", nil, nil, ""); err != nil {
		t.Fatal("resume", err)
	}
	if err = s.ApplyWorkspaceState(ctx, "tenant_a", 20, true); err != nil {
		t.Fatal(err)
	}
	if err = s.ApplyWorkspaceState(ctx, "tenant_a", 19, false); err == nil {
		t.Fatal("stale command reactivated tenant")
	}
	if err = s.ApplyWorkspaceState(ctx, "tenant_a", 20, false); err == nil {
		t.Fatal("same-version payload conflict")
	}
	if err = s.ProvisionWorkspace(ctx, "tenant_a", "A", tokenA); err != nil {
		t.Fatal("suspended provisioning replay", err)
	}
	if err = a.Do(ctx, "GET", "/api/v1/me", nil, nil, ""); err == nil {
		t.Fatal("replay removed suspension")
	}
	if err = s.ApplyWorkspaceState(ctx, "tenant_a", 21, false); err != nil {
		t.Fatal(err)
	}
	if s.store.Pool.Config().MaxConns != 4 {
		t.Fatal("pool budget ignored")
	}
}

func TestWorkersJoinAndPoolBounds(t *testing.T) {
	s, c := service(t)
	for _, bad := range []int32{1, 101} {
		c.MaxConnections = bad
		if _, err := Open(context.Background(), c); err == nil {
			t.Fatal("invalid pool", bad)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { s.RunWorkers(ctx); close(done) }()
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("workers did not drain")
	}
}

func TestWorkspaceDeadlineSurvivesControlPlaneOutageAndRenewal(t *testing.T) {
	s, c := service(t)
	ctx := context.Background()
	tokenA, tokenB := engine.Secret(), engine.Secret()
	for id, token := range map[string]string{"deadline_a": tokenA, "deadline_b": tokenB} {
		if err := s.ProvisionWorkspace(ctx, id, id, token); err != nil {
			t.Fatal(err)
		}
	}
	replica, err := Open(ctx, c)
	if err != nil {
		t.Fatal(err)
	}
	defer replica.Close()
	server := httptest.NewServer(replica.Handler())
	defer server.Close()
	a, b := notif.New(server.URL, tokenA), notif.New(server.URL, tokenB)
	_, session, err := a.Enroll(ctx, notif.EnrollInput{Agent: "worker", Name: "Worker"})
	if err != nil {
		t.Fatal(err)
	}
	worker := notif.New(server.URL, session.Token)
	cached, err := s.store.Authenticate(ctx, session.Token)
	if err != nil {
		t.Fatal(err)
	}
	var person struct {
		Person notif.Person `json:"person"`
	}
	if err = a.Do(ctx, "GET", "/api/v1/me", nil, &person, ""); err != nil {
		t.Fatal(err)
	}
	sent, err := a.Send(ctx, notif.SendInput{Target: notif.Target{Kind: "agent", ID: session.Agent.ID}, Body: "Durable before expiry"}, "before-expiry")
	if err != nil {
		t.Fatal(err)
	}
	var dbNow time.Time
	if err = s.store.Pool.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&dbNow); err != nil {
		t.Fatal(err)
	}
	until := dbNow.Add(1500 * time.Millisecond).Truncate(time.Microsecond)
	if err = s.ApplyWorkspaceAccess(ctx, "deadline_a", 1, false, until); err != nil {
		t.Fatal(err)
	}
	if err = a.Do(ctx, "GET", "/api/v1/me", nil, nil, ""); err != nil {
		t.Fatal(err)
	}
	streamCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(streamCtx, "GET", server.URL+"/api/v1/events?stream=1", nil)
	req.Header.Set("Authorization", "Bearer "+tokenA)
	stream, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Body.Close()
	if stream.StatusCode != 200 {
		t.Fatal(stream.StatusCode)
	}
	// No management calls occur while waiting. The database independently expires
	// existing credentials, mutation actors and already-open streams.
	time.Sleep(time.Until(until) + 50*time.Millisecond)
	if err = a.Do(ctx, "GET", "/api/v1/me", nil, nil, ""); err == nil {
		t.Fatal("expired person admitted")
	}
	if err = worker.Heartbeat(ctx, nil); err == nil {
		t.Fatal("expired agent admitted")
	}
	if err = s.store.Heartbeat(ctx, cached, nil); err == nil {
		t.Fatal("cached actor bypassed deadline")
	}
	if _, err = s.store.RefreshActor(ctx, cached); err == nil {
		t.Fatal("live actor bypassed deadline")
	}
	if err = b.Do(ctx, "GET", "/api/v1/me", nil, nil, ""); err != nil {
		t.Fatal("other tenant affected", err)
	}
	done := make(chan error, 1)
	go func() { _, err := io.Copy(io.Discard, stream.Body); done <- err }()
	select {
	case err = <-done:
		if err != nil {
			t.Fatal("stream did not close cleanly", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("expired stream remains open")
	}
	if err = s.ApplyWorkspaceState(ctx, "deadline_a", 2, false); err != nil {
		t.Fatal(err)
	}
	if err = a.Do(ctx, "GET", "/api/v1/me", nil, nil, ""); err == nil {
		t.Fatal("security change cleared expired access")
	}
	if err = s.ApplyWorkspaceAccess(ctx, "deadline_a", 2, false, time.Time{}); err == nil {
		t.Fatal("same version changed deadline")
	}
	if err = s.ApplyWorkspaceAccess(ctx, "deadline_a", 1, false, time.Time{}); err == nil {
		t.Fatal("stale grant changed deadline")
	}
	if err = s.ApplyWorkspaceAccess(ctx, "deadline_a", 3, true, dbNow.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err = a.Do(ctx, "GET", "/api/v1/me", nil, nil, ""); err == nil {
		t.Fatal("renewal bypassed suspension")
	}
	if err = s.ApplyWorkspaceAccess(ctx, "deadline_a", 4, false, time.Time{}); err != nil {
		t.Fatal(err)
	}
	if err = a.Do(ctx, "GET", "/api/v1/me", nil, nil, ""); err != nil {
		t.Fatal("renewal failed", err)
	}
	var conversation struct {
		Messages []notif.Message `json:"messages"`
	}
	if err = a.Do(ctx, "GET", "/api/v1/conversations/"+sent.ConversationID, nil, &conversation, ""); err != nil {
		t.Fatal(err)
	}
	if len(conversation.Messages) != 1 || conversation.Messages[0].Body != "Durable before expiry" {
		t.Fatal("accepted history lost", conversation.Messages)
	}
}

func TestReplicaStreamDrainReplaysCanonicalEvents(t *testing.T) {
	s, c := service(t)
	ctx := context.Background()
	token := engine.Secret()
	if err := s.ProvisionWorkspace(ctx, "drain_workspace", "Drain", token); err != nil {
		t.Fatal(err)
	}
	other, err := Open(ctx, c)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	first, second := httptest.NewServer(s.Handler()), httptest.NewServer(other.Handler())
	defer first.Close()
	defer second.Close()
	client := notif.New(second.URL, token)
	_, session, err := client.Enroll(ctx, notif.EnrollInput{Agent: "worker", Name: "Worker"})
	if err != nil {
		t.Fatal(err)
	}
	streamCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(streamCtx, "GET", first.URL+"/api/v1/events?stream=1&summary=1", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	stream, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Body.Close()
	// Draining twice must be safe and close an idle stream promptly, without
	// stopping the other replica or changing a canonical operation.
	started := time.Now()
	s.DrainLiveStreams()
	s.DrainLiveStreams()
	data, err := io.ReadAll(stream.Body)
	if err != nil {
		t.Fatal(err)
	}
	if time.Since(started) > time.Second {
		t.Fatal("idle stream delayed deployment", time.Since(started))
	}
	var cursor string
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "id: ") {
			cursor = strings.TrimPrefix(line, "id: ")
		}
	}
	if cursor == "" {
		t.Fatal("missing durable cursor")
	}
	sent, err := client.Send(ctx, notif.SendInput{Target: notif.Target{Kind: "agent", ID: session.Agent.ID}, Body: "During rolling restart"}, "rolling-message")
	if err != nil {
		t.Fatal(err)
	}
	req, _ = http.NewRequestWithContext(streamCtx, "GET", second.URL+"/api/v1/events?stream=1", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Last-Event-ID", cursor)
	resumed, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resumed.Body.Close()
	reader := bufio.NewReader(resumed.Body)
	found := false
	for i := 0; i < 20; i++ {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(line, sent.Messages[0].ID) {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("resumed replica missed event")
	}
	replay, err := client.Send(ctx, notif.SendInput{Target: notif.Target{Kind: "agent", ID: session.Agent.ID}, Body: "During rolling restart"}, "rolling-message")
	if err != nil || replay.Messages[0].ID != sent.Messages[0].ID {
		t.Fatal("replay duplicated accepted work", err)
	}
}
