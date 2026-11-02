// Package api contains the public HTTP contract distributed with the engine.
package api

import _ "embed"

//go:embed openapi.json
var OpenAPI []byte
