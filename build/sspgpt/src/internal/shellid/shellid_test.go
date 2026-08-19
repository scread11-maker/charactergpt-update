package shellid

import "testing"

func TestKeyPrefersExactShellDirectory(t *testing.T) {
	if got := Key(`C:/SSP/ghost/x/shell/master/`, `表示名`); got != "master" {
		t.Fatalf("key=%q", got)
	}
	if got := Key(`C:/SSP/ghost/x/shell/My Shell/`, `別名`); got != "My Shell" {
		t.Fatalf("path basename must not be slugged: key=%q", got)
	}
}

func TestKeyFallsBackToSafeDisplayName(t *testing.T) {
	if got := Key("", `Cafe / Shell: A`); got != "Cafe_Shell_A" {
		t.Fatalf("key=%q", got)
	}
}

func TestValidRejectsNonLeafKeys(t *testing.T) {
	for _, bad := range []string{"", ".", "..", `a/b`, `a\\b`, `a:b`, "trailing."} {
		if Valid(bad) {
			t.Fatalf("unsafe key accepted: %q", bad)
		}
	}
}
