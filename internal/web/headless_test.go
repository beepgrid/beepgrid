//go:build headless

package web

import "testing"

func TestHeadlessHasNoEmbeddedWorkspace(t *testing.T) {
	if Available || Handler() != nil {
		t.Fatal("headless build exposes a workspace handler")
	}
}
