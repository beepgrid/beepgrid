package runtime

import "context"

// DatabaseUser is operator diagnostics for a rolling credential handoff. It
// exposes the active database role, never its password or connection string.
func (s *Service) DatabaseUser(ctx context.Context) (string, error) {
	var user string
	err := s.store.Pool.QueryRow(ctx, "SELECT current_user").Scan(&user)
	return user, err
}
