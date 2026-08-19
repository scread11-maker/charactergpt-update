package paths

import (
	"os"
	"path/filepath"
)

// GhostRoot returns ghost/master regardless of which component subdirectory
// (bridge/core, memory, Plug) contains the executable.
func GhostRoot() string {
	if p := os.Getenv("SSPGPT_GHOST_ROOT"); p != "" {
		return p
	}
	exe, err := os.Executable()
	if err == nil {
		d := filepath.Dir(exe)
		for i := 0; i < 5; i++ {
			if exists(filepath.Join(d, "descript.txt")) || exists(filepath.Join(d, "config")) && exists(filepath.Join(d, "character")) {
				return d
			}
			parent := filepath.Dir(d)
			if parent == d {
				break
			}
			d = parent
		}
		return filepath.Dir(exe)
	}
	return "."
}
func exists(p string) bool { _, e := os.Stat(p); return e == nil }
func Join(parts ...string) string {
	all := append([]string{GhostRoot()}, parts...)
	return filepath.Join(all...)
}
