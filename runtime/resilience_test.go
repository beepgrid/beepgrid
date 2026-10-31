package runtime

import (
	"context"
	"errors"
	"fmt"
	"github.com/beepgrid/beepgrid/internal/engine"
	sdk "github.com/beepgrid/beepgrid/sdk/go/notif"
	"io"
	"math/rand/v2"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TCP faults run outside the engine. Both PostgreSQL query pools and LISTEN
// connections lose their real network connection, rather than receiving mocks.
type databasePartition struct {
	listener    net.Listener
	target      string
	mu          sync.Mutex
	blocked     bool
	connections map[net.Conn]bool
	wg          sync.WaitGroup
}

func partitionProxy(t *testing.T, raw string) (*databasePartition, string) {
	t.Helper()
	u, e := url.Parse(raw)
	if e != nil {
		t.Fatal(e)
	}
	target := u.Host
	if u.Port() == "" {
		target = net.JoinHostPort(u.Hostname(), "5432")
	}
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	p := &databasePartition{listener: listener, target: target, connections: map[net.Conn]bool{}}
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		for {
			client, e := listener.Accept()
			if e != nil {
				return
			}
			p.wg.Add(1)
			go p.serve(client)
		}
	}()
	t.Cleanup(func() { listener.Close(); p.partition(true); p.wg.Wait() })
	u.Host = listener.Addr().String()
	return p, u.String()
}
func (p *databasePartition) serve(client net.Conn) {
	defer p.wg.Done()
	defer client.Close()
	server, e := net.DialTimeout("tcp", p.target, time.Second)
	if e != nil {
		return
	}
	defer server.Close()
	p.mu.Lock()
	if p.blocked {
		p.mu.Unlock()
		return
	}
	p.connections[client] = true
	p.connections[server] = true
	p.mu.Unlock()
	defer func() { p.mu.Lock(); delete(p.connections, client); delete(p.connections, server); p.mu.Unlock() }()
	done := make(chan struct{})
	go func() { io.Copy(server, client); server.Close(); close(done) }()
	io.Copy(client, server)
	client.Close()
	<-done
}
func (p *databasePartition) partition(blocked bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.blocked = blocked
	if blocked {
		for connection := range p.connections {
			connection.Close()
		}
	}
}

type loseSendAcknowledgement struct{ lost atomic.Bool }

func (l *loseSendAcknowledgement) RoundTrip(r *http.Request) (*http.Response, error) {
	response, e := http.DefaultTransport.RoundTrip(r)
	if e == nil && r.Method == "POST" && r.URL.Path == "/api/v1/messages" && response.StatusCode == 200 && l.lost.CompareAndSwap(false, true) {
		response.Body.Close()
		return nil, io.ErrUnexpectedEOF
	}
	return response, e
}
func retryFault(ctx context.Context, fn func() error) error {
	delay := 80 * time.Millisecond
	for {
		e := fn()
		if e == nil {
			return nil
		}
		var api *sdk.APIError
		if errors.As(e, &api) && api.Status < 500 && api.Status != 429 {
			return e
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay + time.Duration(rand.IntN(80))*time.Millisecond):
			if delay < 2*time.Second {
				delay *= 2
				if delay > 2*time.Second {
					delay = 2 * time.Second
				}
			}
		}
	}
}

func TestFleetRecoversFromDatabasePartitionWithoutDuplicateAcceptedWork(t *testing.T) {
	for _, fleet := range []int{25, 250} {
		t.Run(fmt.Sprint(fleet), func(t *testing.T) {
			direct, c := serviceWithBudget(t, 8)
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
			defer cancel()
			proxy, endpoint := partitionProxy(t, c.DatabaseURL)
			c.DatabaseURL = endpoint
			replicas := []*Service{}
			servers := []*httptest.Server{}
			for i := 0; i < 2; i++ {
				s, e := Open(ctx, c)
				if e != nil {
					t.Fatal(e)
				}
				replicas = append(replicas, s)
				server := httptest.NewServer(s.Handler())
				servers = append(servers, server)
				t.Cleanup(func() { server.Close(); s.Close() })
			}
			owners := []*sdk.Client{}
			ownerIDs := []string{}
			members := make([][]string, 4)
			for i := 0; i < 4; i++ {
				id := fmt.Sprintf("fleet_%d", i)
				token := engine.Secret()
				if e := direct.ProvisionWorkspace(ctx, id, id, token); e != nil {
					t.Fatal(e)
				}
				if e := direct.ApplyWorkspaceBudget(ctx, id, WorkspaceBudget{Version: 1, Messages: 10000, Requests: 100, UploadBytes: 10000}); e != nil {
					t.Fatal(e)
				}
				owner := sdk.New(servers[i%2].URL, token)
				var me struct {
					Person sdk.Person `json:"person"`
				}
				if e := owner.Do(ctx, "GET", "/api/v1/me", nil, &me, ""); e != nil {
					t.Fatal(e)
				}
				owners = append(owners, owner)
				ownerIDs = append(ownerIDs, me.Person.ID)
			}
			workers := []*sdk.Client{}
			sessions := []sdk.Session{}
			losses := []*loseSendAcknowledgement{}
			for i := 0; i < fleet; i++ {
				worker, session, e := owners[i%4].Enroll(ctx, sdk.EnrollInput{Agent: fmt.Sprintf("fleet-worker-%d", i), Name: fmt.Sprintf("Worker %d", i)})
				if e != nil {
					t.Fatal(e)
				}
				worker.BaseURL = servers[i%2].URL
				loss := &loseSendAcknowledgement{}
				// Keep250 callers concurrent. Allow real queueing on the8-connection
				// replicas under race instrumentation, matching the hosted client budget.
				worker.HTTP = &http.Client{Transport: loss, Timeout: 15 * time.Second}
				workers = append(workers, worker)
				sessions = append(sessions, session)
				losses = append(losses, loss)
				members[i%4] = append(members[i%4], session.Agent.ID)
			}
			for i, owner := range owners {
				var group, role struct {
					ID string `json:"id"`
				}
				if e := owner.Do(ctx, "POST", "/api/v1/groups", map[string]any{"name": "Fleet", "members": members[i]}, &group, ""); e != nil {
					t.Fatal(e)
				}
				if e := owner.Do(ctx, "POST", "/api/v1/roles", map[string]any{"name": "Report progress", "rules": []map[string]string{{"action": "message", "target_kind": "person", "target_id": ownerIDs[i]}}}, &role, ""); e != nil {
					t.Fatal(e)
				}
				if e := owner.Do(ctx, "POST", "/api/v1/role-bindings", map[string]string{"role_id": role.ID, "subject_kind": "group", "subject_id": group.ID}, nil, ""); e != nil {
					t.Fatal(e)
				}
				if _, e := owner.Send(ctx, sdk.SendInput{Target: sdk.Target{Kind: "group", ID: group.ID}, Body: "Start work"}, "fleet-kickoff"); e != nil {
					t.Fatal(e)
				}
			}
			// Revocation while the runtime is disconnected must still be effective on
			// reconnect. It cannot be cached away by retries or SDK session identity.
			proxy.partition(true)
			if _, e := direct.store.Pool.Exec(ctx, `UPDATE credentials SET revoked=true WHERE id=(SELECT credential_id FROM instances WHERE id=$1)`, sessions[0].InstanceID); e != nil {
				t.Fatal(e)
			}
			seconds := 2
			if value := os.Getenv("NOTIF_RESILIENCE_PARTITION_SECONDS"); value != "" {
				n, e := strconv.Atoi(value)
				if e != nil || n < 2 || n > 90 {
					t.Fatal("invalid partition duration")
				}
				seconds = n
			}
			go func() {
				select {
				case <-ctx.Done():
				case <-time.After(time.Duration(seconds) * time.Second):
					proxy.partition(false)
				}
			}()
			var wg sync.WaitGroup
			failures := make(chan error, fleet)
			started := time.Now()
			for i, worker := range workers {
				wg.Add(1)
				go func(i int, worker *sdk.Client) {
					defer wg.Done()
					e := retryFault(ctx, func() error {
						_, e := worker.Send(ctx, sdk.SendInput{Target: sdk.Target{Kind: "person", ID: ownerIDs[i%4]}, Body: fmt.Sprintf("Worker %d result 0", i)}, fmt.Sprintf("result-%d-0", i))
						return e
					})
					if i == 0 {
						var api *sdk.APIError
						if !errors.As(e, &api) || api.Status != 401 {
							failures <- fmt.Errorf("revoked worker: %v", e)
						}
						return
					}
					if e != nil {
						failures <- e
						return
					}
					for n := 1; n < 3; n++ {
						if e = retryFault(ctx, func() error {
							_, e := worker.Send(ctx, sdk.SendInput{Target: sdk.Target{Kind: "person", ID: ownerIDs[i%4]}, Body: fmt.Sprintf("Worker %d result %d", i, n)}, fmt.Sprintf("result-%d-%d", i, n))
							return e
						}); e != nil {
							failures <- e
							return
						}
					}
					if !losses[i].lost.Load() {
						failures <- fmt.Errorf("worker %d did not exercise a lost acknowledgement", i)
					}
				}(i, worker)
			}
			wg.Wait()
			close(failures)
			for e := range failures {
				t.Error(e)
			}
			var messages, unique, metered int
			if e := direct.store.Pool.QueryRow(ctx, `SELECT count(*),count(DISTINCT scope||'/'||body) FILTER(WHERE body<>'Start work') FROM messages`).Scan(&messages, &unique); e != nil {
				t.Fatal(e)
			}
			expected := fleet + (fleet-1)*3
			if messages != expected || unique != (fleet-1)*3 {
				t.Fatalf("messages=%d expected=%d unique replies=%d", messages, expected, unique)
			}
			if e := direct.store.Pool.QueryRow(ctx, `SELECT COALESCE(sum(units),0) FROM workspace_usage WHERE metric='messages'`).Scan(&metered); e != nil || metered != expected {
				t.Fatal("usage disagrees with accepted work", metered, e)
			}
			t.Logf("fleet=%d replicas=2 tenants=4 partition=%ds canonical_messages=%d recovered_in=%s lost_acknowledgements=%d", fleet, seconds, messages, time.Since(started), fleet-1)
		})
	}
}
