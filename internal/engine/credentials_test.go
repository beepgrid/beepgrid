package engine

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestCredentialMetadataExpiryAndPermissionCeiling(t *testing.T) {
	s, owner := testStore(t)
	ctx := context.Background()
	worker := testWorker(t, s, owner, "limited-key")
	until := time.Now().Add(7 * 24 * time.Hour)
	result, e := s.NewCredential(ctx, owner, worker.ID, "api", "", CredentialOptions{Name: "Reporting", ExpiresAt: &until, Permissions: []string{"read"}})
	if e != nil {
		t.Fatal(e)
	}
	raw := result["token"].(string)
	actor, e := s.Authenticate(ctx, raw)
	if e != nil {
		t.Fatal(e)
	}
	if actor.TokenPermissions == nil || !actor.CredentialPermits("read") || actor.CredentialPermits("message") {
		t.Fatal("permission ceiling not loaded")
	}
	if _, e = s.Send(ctx, actor, SendInput{Target: Target{"person", owner.ID}, Body: "not allowed"}, "limited"); ErrorStatus(e) != 403 {
		t.Fatal("read-only token sent a message", e)
	}
	if _, e = s.NewCredential(ctx, actor, worker.ID, "api", ""); ErrorStatus(e) != 403 {
		t.Fatal("restricted token minted a broader key", e)
	}
	c, e := credential(ctx, s.Pool, owner.Scope, result["id"].(string))
	if e != nil {
		t.Fatal(e)
	}
	if c.Name != "Reporting" || c.OwnerName != "limited-key" || c.LastUsedAt == nil || c.CreatedAt.IsZero() || c.TokenHint == "" || c.ExpiresAt.Sub(until) > time.Second {
		t.Fatal("missing metadata", c.ID)
	}
	data, _ := json.Marshal(c)
	if strings.Contains(string(data), raw) || strings.Contains(string(data), Digest(raw)) {
		t.Fatal("secret material in metadata")
	}
	version := c.Version
	name := "Renamed"
	c, e = s.EditCredential(ctx, owner, c.ID, CredentialEdit{Version: version, Name: &name})
	if e != nil || c.Name != name || c.Version != version+1 {
		t.Fatal(e)
	}
	if _, e = s.EditCredential(ctx, owner, c.ID, CredentialEdit{Version: version, Name: &name}); ErrorStatus(e) != 409 {
		t.Fatal("stale edit succeeded", e)
	}
	if _, e = s.Pool.Exec(ctx, `UPDATE credentials SET expires_at=now()-interval '1 second' WHERE id=$1`, c.ID); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Authenticate(ctx, raw); ErrorStatus(e) != 401 {
		t.Fatal("expired key authenticated", e)
	}
	if _, e = s.EditCredential(ctx, owner, c.ID, CredentialEdit{Version: c.Version, ExpiresAt: &until}); ErrorStatus(e) != 409 {
		t.Fatal("edit revived expired key", e)
	}
}

func TestRenewableConnectionRetainsInstanceAndHardDeadline(t *testing.T) {
	s, owner := testStore(t)
	ctx := context.Background()
	hard := time.Now().Add(3 * time.Hour)
	session, e := s.Enroll(ctx, owner, EnrollInput{Agent: "renewable", Name: "Renewable", Renewable: true, ExpiresAt: &hard})
	if e != nil {
		t.Fatal(e)
	}
	actor, e := s.Authenticate(ctx, session.Token)
	if e != nil {
		t.Fatal(e)
	}
	id := actor.CredentialID
	if _, e = s.Pool.Exec(ctx, `UPDATE credentials SET expires_at=now()+interval '10 minutes',last_used_at=now() WHERE id=$1`, id); e != nil {
		t.Fatal(e)
	}
	actor, e = s.Authenticate(ctx, session.Token)
	if e != nil || actor.InstanceID != session.InstanceID || actor.CredentialID != id {
		t.Fatal("renewal changed connection", e)
	}
	c, e := credential(ctx, s.Pool, owner.Scope, id)
	if e != nil {
		t.Fatal(e)
	}
	if c.ExpiresAt.Before(time.Now().Add(2*time.Hour)) || c.ExpiresAt.After(hard) {
		t.Fatal("renewal exceeded hard deadline or did not extend")
	}
	c, e = s.RenewCredential(ctx, actor)
	if e != nil || c.ExpiresAt.After(hard) {
		t.Fatal(e)
	}
	// Once capped at the hard deadline, ordinary traffic must still coalesce
	// last-use writes rather than attempting an impossible renewal every call.
	lastUsed := *c.LastUsedAt
	for range 3 {
		if _, e = s.Authenticate(ctx, session.Token); e != nil {
			t.Fatal(e)
		}
	}
	c, e = credential(ctx, s.Pool, owner.Scope, id)
	if e != nil || !c.LastUsedAt.Equal(lastUsed) {
		t.Fatal("deadline-capped connection bypassed use-write throttling", e)
	}
	if _, e = s.Pool.Exec(ctx, `UPDATE credentials SET expires_at=now()-interval '1 second' WHERE id=$1`, id); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Authenticate(ctx, session.Token); ErrorStatus(e) != 401 {
		t.Fatal("expired connection renewed itself", e)
	}
	fixed, e := s.Enroll(ctx, owner, EnrollInput{Agent: "fixed", Name: "Fixed"})
	if e != nil {
		t.Fatal(e)
	}
	a, e := s.Authenticate(ctx, fixed.Token)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.RenewCredential(ctx, a); ErrorStatus(e) != 409 {
		t.Fatal("fixed credential renewed", e)
	}
}

func TestCredentialRotationOverlapRevocationAndConcurrentVersion(t *testing.T) {
	s, owner := testStore(t)
	ctx := context.Background()
	session, e := s.Enroll(ctx, owner, EnrollInput{Agent: "rotating", Name: "Rotating", Renewable: true})
	if e != nil {
		t.Fatal(e)
	}
	before, e := s.Authenticate(ctx, session.Token)
	if e != nil {
		t.Fatal(e)
	}
	c, e := credential(ctx, s.Pool, owner.Scope, before.CredentialID)
	if e != nil {
		t.Fatal(e)
	}
	var success atomic.Int32
	var wg sync.WaitGroup
	results := make(chan map[string]any, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, e := s.RotateCredential(ctx, owner, c.ID, CredentialRotation{Version: c.Version, OverlapSeconds: 300})
			if e == nil {
				success.Add(1)
				results <- r
			} else if ErrorStatus(e) != 409 {
				t.Errorf("unexpected rotation failure: %v", e)
			}
		}()
	}
	wg.Wait()
	close(results)
	if success.Load() != 1 {
		t.Fatal("concurrent rotation was not fenced")
	}
	result := <-results
	latest := result["token"].(string)
	current, e := s.Authenticate(ctx, latest)
	if e != nil || current.InstanceID != session.InstanceID || current.CredentialID != c.ID {
		t.Fatal("rotation replaced worker identity", e)
	}
	previous, e := s.Authenticate(ctx, session.Token)
	if e != nil || !previous.CredentialPrevious {
		t.Fatal("missing bounded overlap", e)
	}
	if _, e = s.RenewCredential(ctx, previous); ErrorStatus(e) != 403 {
		t.Fatal("old secret renewed current lease", e)
	}
	if _, e = s.Pool.Exec(ctx, `UPDATE credentials SET previous_until=now()-interval '1 second' WHERE id=$1`, c.ID); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Authenticate(ctx, session.Token); ErrorStatus(e) != 401 {
		t.Fatal("old secret survived overlap", e)
	}
	if _, e = s.RefreshActor(ctx, before); ErrorStatus(e) != 403 {
		t.Fatal("old stream survived overlap", e)
	}
	if e = s.Revoke(ctx, owner, c.ID); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Authenticate(ctx, latest); ErrorStatus(e) != 401 {
		t.Fatal("revoked replacement accepted", e)
	}
}

func TestEnrollmentRevocationCanExplicitlyStopItsWorkers(t *testing.T) {
	s, owner := testStore(t)
	ctx := context.Background()
	person := testPerson(t, s, owner, "launcher-owner")
	key, e := s.NewCredential(ctx, owner, person.ID, "enrollment", "team/")
	if e != nil {
		t.Fatal(e)
	}
	launcher, e := s.Authenticate(ctx, key["token"].(string))
	if e != nil {
		t.Fatal(e)
	}
	session, e := s.Enroll(ctx, launcher, EnrollInput{Agent: "team/one", Name: "One", Renewable: true})
	if e != nil {
		t.Fatal(e)
	}
	unrelated, e := s.Enroll(ctx, owner, EnrollInput{Agent: "unrelated", Name: "Unrelated"})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Revoke(ctx, owner, key["id"].(string)); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Authenticate(ctx, session.Token); e != nil {
		t.Fatal("implicit cascading revocation", e)
	}
	if e = s.Revoke(ctx, owner, key["id"].(string), true); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Authenticate(ctx, session.Token); ErrorStatus(e) != 401 {
		t.Fatal("derived worker still active", e)
	}
	if _, e = s.Authenticate(ctx, unrelated.Token); e != nil {
		t.Fatal("revoked unrelated connection", e)
	}
}

func TestCredentialNarrowingStopsQueuedDelivery(t *testing.T) {
	s, owner := testStore(t)
	ctx := context.Background()
	sender := testWorker(t, s, owner, "key-sender")
	receiver := testWorker(t, s, owner, "key-receiver")
	issued, e := s.NewCredential(ctx, owner, sender.ID, "api", "")
	if e != nil {
		t.Fatal(e)
	}
	a, e := s.Authenticate(ctx, issued["token"].(string))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Send(ctx, a, SendInput{Target: Target{"agent", receiver.ID}, Body: "queued before narrowing"}, "narrow"); e != nil {
		t.Fatal(e)
	}
	c, e := credential(ctx, s.Pool, owner.Scope, a.CredentialID)
	if e != nil {
		t.Fatal(e)
	}
	limits := []string{"read"}
	if _, e = s.EditCredential(ctx, owner, c.ID, CredentialEdit{Version: c.Version, Permissions: &limits}); e != nil {
		t.Fatal(e)
	}
	deliveries, e := s.Inbox(ctx, receiver)
	if e != nil || len(deliveries) != 0 {
		t.Fatal("queued content survived narrowed authority", e)
	}
}
