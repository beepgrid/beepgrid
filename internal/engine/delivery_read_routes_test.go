package engine

import (
	"context"
	"testing"
	"time"
)

func TestDeliveryReadRouteProofMatchesCurrentDispatchPolicy(t *testing.T) {
	s, owner := testStore(t)
	ctx := context.Background()
	w := testWorker(t, s, owner, "route-worker")
	p := testPerson(t, s, owner, "recipient")
	g, e := s.SaveGroup(ctx, owner, Group{Name: "Route audience", Members: []string{p.ID}})
	if e != nil {
		t.Fatal(e)
	}
	c, e := s.SaveConnection(ctx, owner, Connection{Name: "Provider", Kind: "webhook", Enabled: true, Config: map[string]string{"url": "https://example.com/receive"}, Secret: "local-route-proof-fixture-secret"})
	if e != nil {
		t.Fatal(e)
	}
	r, e := s.Ask(ctx, w, AskInput{Target: Target{"person", p.ID}, Title: "Which release?"}, "request")
	if e != nil {
		t.Fatal(e)
	}
	makeDelivery := func(id, sender, credential, provider string, target Target) Delivery {
		return Delivery{ID: id, RecipientID: p.ID, Payload: map[string]any{"type": "message", "_policy": map[string]any{"sender_id": sender, "credential_id": credential, "provider_connection": provider, "target": target, "action": "message", "action_name": ""}}}
	}
	ds := []Delivery{
		makeDelivery("native", w.ID, w.CredentialID, "", Target{"person", p.ID}),
		makeDelivery("group", w.ID, w.CredentialID, "", Target{"group", g.ID}),
		makeDelivery("provider", w.ID, "", c.ID, Target{"person", p.ID}),
		makeDelivery("unknown-credential", w.ID, "missing", "", Target{"person", p.ID}),
		makeDelivery("wrong-owner", w.ID, owner.CredentialID, "", Target{"person", p.ID}),
		makeDelivery("unknown-provider", w.ID, "", "missing", Target{"person", p.ID}),
		makeDelivery("missing-sender", "missing", w.CredentialID, "", Target{"person", p.ID}),
		makeDelivery("admin", owner.ID, owner.CredentialID, "", Target{"person", p.ID}),
		{ID: "malformed", RecipientID: p.ID, Payload: map[string]any{"_policy": "invalid"}},
		{ID: "result", RecipientID: w.ID, Payload: map[string]any{"_policy": map[string]string{"engine_result": r.ID}}},
		{ID: "wrong-result-audience", RecipientID: p.ID, Payload: map[string]any{"_policy": map[string]string{"engine_result": r.ID}}},
	}
	question := makeDelivery("question", w.ID, w.CredentialID, "", Target{"person", p.ID})
	question.Payload["type"] = "request"
	question.Payload["request"] = map[string]any{"id": r.ID}
	ds = append(ds, question)
	check := func(label string, scope string) {
		t.Helper()
		got, e := s.deliveryReadRoutes(withPolicySnapshot(ctx), scope, ds)
		if e != nil {
			t.Fatal(label, e)
		}
		for _, d := range ds {
			want := s.currentDeliveryPolicy(withPolicySnapshot(ctx), s.Pool, scope, d)
			if got[d.ID] != want {
				t.Fatal(label, d.ID, "read proof differs from dispatch", got[d.ID], want)
			}
		}
	}
	check("current", owner.Scope)
	foreign, e := s.Bootstrap(ctx, "foreign/delivery-proof", "Foreign", "test", Secret())
	if e != nil {
		t.Fatal(e)
	}
	check("foreign scope", foreign.Scope)
	if _, e = s.Pool.Exec(ctx, `UPDATE connections SET enabled=false WHERE scope=$1 AND id=$2`, owner.Scope, c.ID); e != nil {
		t.Fatal(e)
	}
	check("provider disabled", owner.Scope)
	if _, e = s.Pool.Exec(ctx, `DELETE FROM memberships WHERE scope=$1 AND group_id=$2`, owner.Scope, g.ID); e != nil {
		t.Fatal(e)
	}
	check("group membership removed", owner.Scope)
	if _, e = s.Pool.Exec(ctx, `UPDATE requests SET expires_at=$3 WHERE scope=$1 AND id=$2`, owner.Scope, r.ID, time.Now().Add(-time.Hour)); e != nil {
		t.Fatal(e)
	}
	check("question expired", owner.Scope)
	if _, e = s.Pool.Exec(ctx, `UPDATE credentials SET revoked=true WHERE scope=$1 AND id=$2`, owner.Scope, w.CredentialID); e != nil {
		t.Fatal(e)
	}
	check("credential revoked", owner.Scope)
	if _, e = s.Pool.Exec(ctx, `UPDATE principals SET disabled=true WHERE scope=$1 AND id=$2`, owner.Scope, p.ID); e != nil {
		t.Fatal(e)
	}
	check("recipient disabled", owner.Scope)
	if _, e = s.Pool.Exec(ctx, `UPDATE principals SET disabled=true WHERE scope=$1 AND id=$2`, owner.Scope, w.ID); e != nil {
		t.Fatal(e)
	}
	check("sender disabled", owner.Scope)
}
