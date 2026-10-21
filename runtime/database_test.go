package runtime

import (
	"context"
	"testing"
)

func TestDatabaseRoleDiagnosticsUsesTheActualConnection(t *testing.T) {
	s, _ := service(t)
	user, err := s.DatabaseUser(context.Background())
	if err != nil || user == "" {
		t.Fatalf("role=%q err=%v", user, err)
	}
	var expected string
	if err = s.store.Pool.QueryRow(context.Background(), "SELECT current_user").Scan(&expected); err != nil || user != expected {
		t.Fatalf("role=%q expected=%q err=%v", user, expected, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = s.DatabaseUser(ctx); err == nil {
		t.Fatal("cancelled diagnostic returned successful readiness")
	}
}
