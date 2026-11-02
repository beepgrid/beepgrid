//go:build !darwin && !linux

package cli

// Process-level CPU and peak RSS are unavailable on these platforms.
// Go heap, GC, goroutine and database-pool metrics remain available.
func processUsage() (any, any) { return nil, nil }
