package runtime

import (
	"context"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/beepgrid/beepgrid/internal/engine"
	"github.com/beepgrid/beepgrid/sdk/go/notif"
)

func TestVerifiedPersonCompositionCannotReplaceOwnerOrCrossWorkspace(t *testing.T) {
	s, _ := service(t)
	ctx := context.Background()
	owner := VerifiedPerson{Issuer: "https://host.example", Subject: "membership-a", Name: "Owner", Email: "owner@example.test"}
	bootstrap := engine.Secret()
	if err := s.ProvisionIdentityWorkspace(ctx, "identity_a", "A", bootstrap, owner); err != nil {
		t.Fatal(err)
	}
	if err := s.ProvisionIdentityWorkspace(ctx, "identity_a", "A", bootstrap, owner); err != nil {
		t.Fatal("replay", err)
	}
	other := owner
	other.Subject = "different"
	if err := s.ProvisionIdentityWorkspace(ctx, "identity_a", "A", bootstrap, other); err == nil {
		t.Fatal("replaced owner")
	}
	if err := s.ProvisionIdentityWorkspace(ctx, "identity_b", "B", engine.Secret(), other); err != nil {
		t.Fatal(err)
	}
	a, err := s.EstablishPersonSession(ctx, "identity_a", owner)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.PersonSessionScope(ctx, a.Token, "identity_b"); err == nil {
		t.Fatal("crossed workspace")
	}
	if err = s.CheckProgrammaticCredential(ctx, a.Token); err == nil {
		t.Fatal("browser credential accepted by SDK fence")
	}
	if err = s.CheckProgrammaticCredential(ctx, bootstrap); err != nil {
		t.Fatal("bootstrap rejected", err)
	}
	principal, err := s.ProgrammaticCredentialIdentity(ctx, bootstrap)
	if err != nil || principal.Scope != "identity_a/production" || principal.Kind != "person" || principal.ID != a.PersonID {
		t.Fatal("authenticated host principal", principal, err)
	}
	if _, err = s.ProgrammaticCredentialIdentity(ctx, a.Token); err == nil {
		t.Fatal("browser identity escaped programmatic fence")
	}
	if _, err = s.ProgrammaticCredentialIdentity(ctx, "invalid-token"); err == nil {
		t.Fatal("invalid credential resolved")
	}

	server := httptest.NewServer(s.Handler())
	defer server.Close()
	var me struct {
		Admin  bool
		Person notif.Person
	}
	if err = notif.New(server.URL, a.Token).Do(ctx, "GET", "/api/v1/me", nil, &me, ""); err != nil || !me.Admin {
		t.Fatal("intentional owner", me, err)
	}
	member := owner
	member.Subject = "membership-member"
	member.Name = "Member"
	b, err := s.EstablishPersonSession(ctx, "identity_a", member)
	if err != nil {
		t.Fatal(err)
	}
	if b.PersonID == a.PersonID {
		t.Fatal("merged by email")
	}
	if err = notif.New(server.URL, b.Token).Do(ctx, "GET", "/api/v1/me", nil, &me, ""); err != nil || me.Admin {
		t.Fatal("new member elevated", me, err)
	}
	if err = s.RevokePersonSession(ctx, a.Token); err != nil {
		t.Fatal(err)
	}
	if err = s.PersonSessionScope(ctx, a.Token, "identity_a"); err == nil {
		t.Fatal("revoked session accepted")
	}
	if err = s.PersonSessionScope(ctx, b.Token, "identity_a"); err != nil {
		t.Fatal("revoked other person", err)
	}
}

func TestVerifiedOwnerConcurrentBindingHasOneWinner(t *testing.T) {
	s, _ := service(t)
	ctx := context.Background()
	bootstrap := engine.Secret()
	if err := s.ProvisionWorkspace(ctx, "identity_race", "Race", bootstrap); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, subject := range []string{"first", "second"} {
		wg.Add(1)
		go func(sub string) {
			defer wg.Done()
			results <- s.ProvisionIdentityWorkspace(ctx, "identity_race", "Race", bootstrap, VerifiedPerson{Issuer: "https://host.example", Subject: sub, Name: sub})
		}(subject)
	}
	wg.Wait()
	close(results)
	wins := 0
	for err := range results {
		if err == nil {
			wins++
		}
	}
	if wins != 1 {
		t.Fatal("owner race", wins)
	}
}
