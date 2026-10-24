package engine

import (
	"context"
	"sync"
	"testing"
)

func TestConfigurationRevisionsPreventLostUpdates(t *testing.T) {
	s, owner := testStore(t)
	ctx := context.Background()
	group, e := s.SaveGroup(ctx, owner, Group{Name: "Original"})
	if e != nil || group.Version != 1 {
		t.Fatal(group, e)
	}
	role, e := s.SaveRole(ctx, owner, Role{Name: "Role"})
	if e != nil || role.Version != 1 {
		t.Fatal(role, e)
	}
	connection, e := s.SaveConnection(ctx, owner, Connection{Name: "Email", Kind: "email", Enabled: true, Config: map[string]string{"from": "sender@example.com", "smtp_addr": "127.0.0.1:1125"}})
	if e != nil || connection.Version != 1 {
		t.Fatal(connection, e)
	}
	dest, e := s.SaveDestination(ctx, owner, Destination{ConnectionID: connection.ID, PrincipalID: owner.ID, Address: "to@example.com"})
	if e != nil || dest.Version != 1 {
		t.Fatal(dest, e)
	}
	for _, tc := range []struct {
		name string
		save func() error
	}{
		{"group", func() error { _, e := s.SaveGroup(ctx, owner, group); return e }},
		{"role", func() error { _, e := s.SaveRole(ctx, owner, role); return e }},
		{"connection", func() error { _, e := s.SaveConnection(ctx, owner, connection); return e }},
		{"destination", func() error { _, e := s.SaveDestination(ctx, owner, dest); return e }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var wg sync.WaitGroup
			accepted := make(chan bool, 8)
			for range 8 {
				wg.Add(1)
				go func() {
					defer wg.Done()
					e := tc.save()
					if e == nil {
						accepted <- true
					} else if ErrorStatus(e) != 409 {
						t.Errorf("unexpected save: %v", e)
					}
				}()
			}
			wg.Wait()
			if len(accepted) != 1 {
				t.Fatalf("concurrent accepted writes: %d", len(accepted))
			}
		})
	}
	groups, e := s.Groups(ctx, owner)
	if e != nil || groups[0].Version != 2 {
		t.Fatal(groups, e)
	}
	roles, e := s.Roles(ctx, owner)
	if e != nil || roles[0].Version != 2 {
		t.Fatal(roles, e)
	}
	connections, e := s.Connections(ctx, owner)
	if e != nil || connections[0].Version != 2 {
		t.Fatal(connections, e)
	}
	destinations, e := s.Destinations(ctx, owner)
	if e != nil || destinations[0].Version != 2 {
		t.Fatal(destinations, e)
	}
}

func TestLegacyConfigurationSaveRetainsCompatibility(t *testing.T) {
	s, owner := testStore(t)
	ctx := context.Background()
	group, e := s.SaveGroup(ctx, owner, Group{Name: "Original"})
	if e != nil {
		t.Fatal(e)
	}
	// An old client did not decode the additive version field. New callers retain it.
	group.Version = 0
	group.Name = "Legacy update"
	updated, e := s.SaveGroup(ctx, owner, group)
	if e != nil || updated.Version != 2 {
		t.Fatal(updated, e)
	}
	updated.Version = -1
	if _, e = s.SaveGroup(ctx, owner, updated); ErrorStatus(e) != 400 {
		t.Fatal("negative version", e)
	}
}

func TestAssistanceSettingsInitialRevisionDetectsStaleToggle(t *testing.T) {
	s, owner := testStore(t)
	ctx := context.Background()
	settings, e := s.AssistanceSettings(ctx, owner)
	if e != nil || settings.Version < 1 || settings.Overseer || settings.AutomaticAnswers {
		t.Fatal(settings, e)
	}
	s.OverseerPlanner = func(context.Context, string, []FleetStatus) (OverseerDecision, error) { return OverseerDecision{}, nil }
	on, off := true, false
	enabled, e := s.UpdateAssistance(ctx, owner, AssistanceUpdate{Version: settings.Version, Overseer: &on})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.UpdateAssistance(ctx, owner, AssistanceUpdate{Version: settings.Version, Overseer: &off}); ErrorStatus(e) != 409 {
		t.Fatal("stale initial toggle", e)
	}
	if _, e = s.UpdateAssistance(ctx, owner, AssistanceUpdate{Version: enabled.Version, Overseer: &off}); e != nil {
		t.Fatal(e)
	}
}
