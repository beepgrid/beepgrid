package main

import (
	"context"
	"testing"
)

func TestProfilingRefusesNonLoopbackAndIsDisabledByDefault(t *testing.T) {
	stop, e := startDiagnostics(context.Background(), nil, "")
	if e != nil {
		t.Fatal(e)
	}
	stop()
	for _, address := range []string{"0.0.0.0:6060", "localhost:6060", "[::]:6060", "192.0.2.1:6060", "bad"} {
		if _, e = startDiagnostics(context.Background(), nil, address); e == nil {
			t.Fatal("public diagnostics listener accepted", address)
		}
	}
}
