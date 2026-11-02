//go:build headless

// Package web supplies the optional community workspace handler.
package web

import "net/http"

const Available = false

// Handler returns nil so browser routes are not registered in a headless build.
func Handler() http.Handler { return nil }
