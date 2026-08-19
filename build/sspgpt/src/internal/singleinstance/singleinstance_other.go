//go:build !windows

package singleinstance

// Acquire is a no-op on non-Windows development/test hosts. SSP production
// binaries are Windows-only and use a named mutex there.
func Acquire(component, root string) bool { return true }
