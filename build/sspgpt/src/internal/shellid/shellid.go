package shellid

import (
	"path/filepath"
	"strings"
	"unicode"
)

// Key returns a stable filesystem-safe Shell key.
//
// When SSP supplies a Shell path, the actual Shell directory basename is the
// authority. It is already a real filesystem name and therefore must not be
// slugged: changing spaces/punctuation would create avoidable collisions.
// The user-facing Shell display name is used only as a degraded fallback when
// no path is available.
func Key(shellPath, shellName string) string {
	p := strings.TrimSpace(strings.ReplaceAll(shellPath, "\\", "/"))
	if p != "" {
		clean := filepath.Clean(filepath.FromSlash(p))
		base := strings.TrimSpace(filepath.Base(clean))
		if Valid(base) {
			return base
		}
	}
	return sanitizeDisplay(shellName)
}

// Valid reports whether key is a single safe filename component on Windows
// and the local build host. Shell directory basenames supplied by SSP should
// already satisfy this contract.
func Valid(key string) bool {
	if key == "" || key == "." || key == ".." || strings.TrimSpace(key) != key {
		return false
	}
	if strings.HasSuffix(key, ".") || strings.HasSuffix(key, " ") {
		return false
	}
	for _, r := range key {
		if r < 0x20 || strings.ContainsRune(`<>:"/\\|?*`, r) {
			return false
		}
	}
	return true
}

func sanitizeDisplay(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	var b strings.Builder
	lastUnderscore := false
	count := 0
	for _, r := range s {
		if count >= 80 {
			break
		}
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_' || r == '.':
			b.WriteRune(r)
			lastUnderscore = false
			count++
		case unicode.IsSpace(r):
			if !lastUnderscore && b.Len() > 0 {
				b.WriteByte('_')
				lastUnderscore = true
				count++
			}
		}
	}
	out := strings.Trim(b.String(), "._-")
	if !Valid(out) {
		return ""
	}
	return out
}
