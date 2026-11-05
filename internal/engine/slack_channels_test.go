package engine

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestSlackChannelAccessIsSharedAcrossDestinationsAndRechecked(t *testing.T) {
	s, owner := testStore(t)
	ctx := context.Background()
	a := testWorker(t, s, owner, "builder-a")
	b := testWorker(t, s, owner, "builder-b")
	person := testPerson(t, s, owner, "reviewer")
	c, err := s.SaveConnection(ctx, owner, Connection{Name: "Slack", Kind: "slack", Enabled: true, Config: map[string]string{"team_id": "T1"}, Secret: `{"bot_token":"fixture-token-not-real","signing_secret":"fixture-signing-secret"}`})
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{owner.ID, person.ID} {
		if _, err = s.SaveDestination(ctx, owner, Destination{ConnectionID: c.ID, PrincipalID: p, Address: "C1"}); err != nil {
			t.Fatal(err)
		}
	}
	role, err := s.SaveGroup(ctx, owner, Group{Name: "Engineering", Purpose: "agent_role", Members: []string{a.ID}})
	if err != nil {
		t.Fatal(err)
	}
	setting, err := s.SaveProviderChannel(ctx, owner, c.ID, ProviderChannel{Address: "C1", Name: "#engineering", Restricted: true, Allowed: []Target{{Kind: "group", ID: role.ID}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.SaveProviderChannel(ctx, b, c.ID, setting); ErrorStatus(err) != 403 {
		t.Fatal("agent edited channel access", err)
	}
	if _, err = s.SaveProviderChannel(ctx, owner, c.ID, ProviderChannel{Address: "C1", Name: "#engineering"}); ErrorStatus(err) != 409 {
		t.Fatal("stale update accepted", err)
	}
	assertCount := func(worker Actor, recipient, key string, want int) SendResult {
		t.Helper()
		out, e := s.Send(ctx, worker, SendInput{Target: Target{Kind: "person", ID: recipient}, Body: "Review change"}, key)
		if e != nil {
			t.Fatal(e)
		}
		var n int
		_ = s.Pool.QueryRow(ctx, `SELECT count(*) FROM deliveries WHERE operation_id=$1 AND connection_id=$2`, out.Messages[0].ID, c.ID).Scan(&n)
		if n != want {
			t.Fatalf("route count %d, want %d", n, want)
		}
		return out
	}
	first := assertCount(a, owner.ID, "allowed", 1)
	assertCount(b, owner.ID, "blocked-owner", 0)
	assertCount(b, person.ID, "blocked-other-recipient", 0)
	// An existing inbound thread cannot reintroduce a forbidden channel.
	if err = s.recordInboundThread(ctx, s.Pool, owner.Scope, c.ID, "C1", "123.4", first.ConversationID, owner.ID); err != nil {
		t.Fatal(err)
	}
	setting.Allowed = []Target{{Kind: "agent", ID: b.ID}}
	setting, err = s.SaveProviderChannel(ctx, owner, c.ID, setting)
	if err != nil {
		t.Fatal(err)
	}
	reply, err := s.Send(ctx, a, SendInput{ConversationID: first.ConversationID, Body: "Still forbidden"}, "forbidden-affinity")
	if err != nil {
		t.Fatal(err)
	}
	var n int
	_ = s.Pool.QueryRow(ctx, `SELECT count(*) FROM deliveries WHERE operation_id=$1 AND connection_id=$2`, reply.Messages[0].ID, c.ID).Scan(&n)
	if n != 0 {
		t.Fatal("thread bypassed channel restriction")
	}
	// Queued deliveries are checked against the latest access at dispatch.
	s.providerTransport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Error("revoked delivery reached Slack")
		return nil, io.EOF
	})
	if err = s.DispatchOnce(ctx, "https://beepgrid.test"); err != nil {
		t.Fatal(err)
	}
	var state string
	_ = s.Pool.QueryRow(ctx, `SELECT state FROM deliveries WHERE operation_id=$1 AND connection_id=$2`, first.Messages[0].ID, c.ID).Scan(&state)
	if state != "cancelled" {
		t.Fatal(state)
	}
	setting.Allowed = []Target{{Kind: "group", ID: role.ID}}
	setting, err = s.SaveProviderChannel(ctx, owner, c.ID, setting)
	if err != nil {
		t.Fatal(err)
	}
	queued := assertCount(a, owner.ID, "member-before-removal", 1)
	role.Members = []string{}
	if _, err = s.SaveGroup(ctx, owner, role); err != nil {
		t.Fatal(err)
	}
	assertCount(a, owner.ID, "removed-member", 0)
	if err = s.DispatchOnce(ctx, "https://beepgrid.test"); err != nil {
		t.Fatal(err)
	}
	_ = s.Pool.QueryRow(ctx, `SELECT state FROM deliveries WHERE operation_id=$1 AND connection_id=$2`, queued.Messages[0].ID, c.ID).Scan(&state)
	if state != "cancelled" {
		t.Fatal("role revocation not applied", state)
	}
}

func TestSlackChannelDirectoryIsBoundedAndAuthorized(t *testing.T) {
	s, owner := testStore(t)
	ctx := context.Background()
	agent := testPerson(t, s, owner, "directory-outsider")
	c, err := s.SaveConnection(ctx, owner, Connection{Name: "Slack", Kind: "slack", Enabled: true, Config: map[string]string{"team_id": "T1"}, Secret: `{"bot_token":"fixture-secret-not-real","signing_secret":"fixture-signing-secret"}`})
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	s.providerTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Method != "GET" || r.URL.Host != "slack.com" || r.URL.Path != "/api/conversations.list" || r.URL.Query().Get("cursor") != "next-page" || r.URL.Query().Get("team_id") != "T1" || r.Header.Get("Authorization") != "Bearer fixture-secret-not-real" {
			t.Error("incorrect channel lookup")
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"ok":true,"channels":[{"id":"C1","name":"engineering","is_member":true},{"id":"C2","name":"not-joined","is_member":false},{"id":"G3","name":"private","is_member":true},{"id":"C4","name":"archived","is_member":true,"is_archived":true}],"response_metadata":{"next_cursor":"page-3"}}`))}, nil
	})
	if _, err = s.SlackChannels(ctx, agent, c.ID, ""); ErrorStatus(err) != 403 || calls != 0 {
		t.Fatal("unauthorized lookup", err, calls)
	}
	page, err := s.SlackChannels(ctx, owner, c.ID, "next-page")
	if err != nil || len(page.Channels) != 2 || page.Channels[0].Name != "#engineering" || page.NextCursor != "page-3" {
		t.Fatal(page, err)
	}
	raw, _ := json.Marshal(page)
	if strings.Contains(string(raw), "fixture-secret") {
		t.Fatal("credential leaked")
	}
}

func TestProviderAttributionUsesServerIdentity(t *testing.T) {
	s, owner := testStore(t)
	ctx := context.Background()
	agent := testWorker(t, s, owner, "release-agent")
	c, err := s.SaveConnection(ctx, owner, Connection{Name: "Slack", Kind: "slack", Enabled: true, Config: map[string]string{"team_id": "T1"}, Secret: `{"bot_token":"fixture-token-not-real","signing_secret":"fixture-signing-secret"}`})
	if err != nil {
		t.Fatal(err)
	}
	for _, payload := range []map[string]any{{"message": map[string]any{"sender_id": agent.ID, "body": "Body"}}, {"request": map[string]any{"requester_id": agent.ID, "title": "Approve?"}}} {
		prefix, err := s.providerAttribution(ctx, c.ID, payload)
		if err != nil || prefix != "Agent: "+agent.Name+"\n\n" {
			t.Fatal(prefix, err)
		}
	}
	prefix, err := s.providerAttribution(ctx, c.ID, map[string]any{"message": map[string]any{"sender_id": owner.ID}})
	if err != nil || prefix != "" {
		t.Fatal("person mislabeled as agent", prefix, err)
	}
	if _, err = s.providerAttribution(ctx, c.ID, map[string]any{"message": map[string]any{"sender_id": "agt_missing"}}); err == nil {
		t.Fatal("missing sender accepted")
	}
	if _, err = s.Pool.Exec(ctx, `UPDATE principals SET name=$1 WHERE scope=$2 AND id=$3`, "Builder <!channel> & review\nteam", owner.Scope, agent.ID); err != nil {
		t.Fatal(err)
	}
	prefix, err = s.providerAttribution(ctx, c.ID, map[string]any{"message": map[string]any{"sender_id": agent.ID}})
	if err != nil || prefix != "Agent: Builder &lt;!channel&gt; &amp; review team\n\n" {
		t.Fatal("attribution must not introduce Slack mentions or new lines", prefix, err)
	}
}

func TestSlackSharedThreadKeepsPeopleSeparateFromChannel(t *testing.T) {
	s, owner := testStore(t)
	ctx := context.Background()
	agent := testWorker(t, s, owner, "shared-thread-agent")
	alice := testPerson(t, s, owner, "Alice")
	bob := testPerson(t, s, owner, "Bob")
	outsider := testPerson(t, s, owner, "Outside")
	for _, p := range []Actor{alice, bob, outsider} {
		grant(t, s, owner, p, []Rule{{Action: "view", TargetKind: "*", TargetID: "*"}, {Action: "message", TargetKind: "*", TargetID: "*"}})
	}
	c, err := s.SaveConnection(ctx, owner, Connection{Name: "Slack", Kind: "slack", Enabled: true, Config: map[string]string{"team_id": "T1"}, Secret: `{"bot_token":"fixture-token-not-real","signing_secret":"fixture-signing-secret"}`})
	if err != nil {
		t.Fatal(err)
	}
	chat, err := s.StartChat(ctx, owner, StartChatInput{Targets: []Target{{Kind: "agent", ID: agent.ID}, {Kind: "person", ID: alice.ID}, {Kind: "person", ID: bob.ID}}, Body: "Shared review"}, "shared-review")
	if err != nil {
		t.Fatal(err)
	}
	for _, person := range []Actor{alice, bob} {
		if _, err = s.SaveDestination(ctx, owner, Destination{ConnectionID: c.ID, PrincipalID: person.ID, Address: "C1"}); err != nil {
			t.Fatal(err)
		}
	}
	if err = s.recordThread(ctx, s.Pool, owner.Scope, c.ID, "C1", "1.0", chat.ConversationID, alice.ID); err != nil {
		t.Fatal(err)
	}
	for i, p := range []Actor{alice, bob, outsider} {
		user := []string{"U1", "U2", "U3"}[i]
		if err = s.LinkSlackAccount(ctx, owner, p.ID, user, "T1"); err != nil {
			t.Fatal(err)
		}
		linked, e := s.SlackActor(ctx, owner.Scope, user, "T1")
		if e != nil {
			t.Fatal(e)
		}
		linked.ProviderConnection = c.ID
		result, e := s.SlackMessage(ctx, linked, c, "C1", "1.0", p.Name+" reply", user, true)
		if i == 2 {
			if e == nil {
				t.Fatal("channel membership silently granted conversation access")
			}
			continue
		}
		if e != nil || result.Messages[0].SenderID != p.ID {
			t.Fatal("reply sender replaced by channel", result, e)
		}
		var reflected int
		_ = s.Pool.QueryRow(ctx, `SELECT count(*) FROM deliveries WHERE operation_id=$1 AND connection_id=$2 AND address='C1'`, result.Messages[0].ID, c.ID).Scan(&reflected)
		if reflected != 0 {
			t.Fatal("human reply reflected back into its source channel")
		}
	}
	seen := map[string]bool{}
	for round := 0; round < 10; round++ {
		deliveries, err := s.Inbox(ctx, agent)
		if err != nil {
			t.Fatal(err)
		}
		if len(deliveries) == 0 {
			break
		}
		for _, d := range deliveries {
			m, ok := d.Payload["message"].(map[string]any)
			if ok {
				body, _ := m["body"].(string)
				if body == "Alice reply" || body == "Bob reply" {
					sender, _ := d.Payload["sender"].(map[string]any)
					source, _ := d.Payload["source"].(map[string]any)
					if sender["kind"] != "person" || sender["id"] != m["sender_id"] || source["channel_id"] != "C1" || source["provider"] != "slack" || source["thread_id"] != "1.0" {
						t.Fatal("runtime identity/context lost", d.Payload)
					}
					seen[sender["id"].(string)] = true
				}
			}
			if err = s.Ack(ctx, agent, d.ID, d.Generation); err != nil {
				t.Fatal(err)
			}
		}
	}
	if !seen[alice.ID] || !seen[bob.ID] || len(seen) != 2 {
		t.Fatal("Agent could not distinguish people", seen)
	}
}

func TestRequestDeliveriesPreserveSenderAcrossAudiencePaths(t *testing.T) {
	s, owner := testStore(t)
	ctx := context.Background()
	a := testWorker(t, s, owner, "builder-a")
	b := testWorker(t, s, owner, "builder-b")
	role, err := s.SaveGroup(ctx, owner, Group{Name: "Engineering", Purpose: "agent_role", Members: []string{a.ID, b.ID}})
	if err != nil {
		t.Fatal(err)
	}
	chat, err := s.Send(ctx, owner, SendInput{Target: Target{Kind: "agent", ID: a.ID}, Body: "Review"}, "request-chat")
	if err != nil {
		t.Fatal(err)
	}
	for i, in := range []AskInput{
		{Target: Target{Kind: "agent", ID: a.ID}, Title: "New private question"},
		{Target: Target{Kind: "group", ID: role.ID}, Title: "Role question"},
		{ConversationID: chat.ConversationID, Title: "Follow-up question"},
	} {
		q, err := s.Ask(ctx, owner, in, in.Title)
		if err != nil {
			t.Fatal(err)
		}
		var count, attributed int
		if err = s.Pool.QueryRow(ctx, `SELECT count(*),count(*) FILTER(WHERE payload->'sender'->>'id'=$3 AND payload->'sender'->>'kind'='person' AND payload->'sender'->>'name'=$4) FROM deliveries WHERE scope=$1 AND operation_id=$2`, owner.Scope, q.ID, owner.ID, owner.Name).Scan(&count, &attributed); err != nil {
			t.Fatal(err)
		}
		want := 1
		if i == 1 {
			want = 2
		}
		if count != want || attributed != want {
			t.Fatalf("request audience path %d lost sender identity: %d of %d", i, attributed, count)
		}
	}
}
