package profilepath

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMigrateLegacyProfileOwnership(t *testing.T) {
	root := t.TempDir()
	old := map[string]string{
		"emotional_state.json":  `{"revision":1}`,
		"appearance_state.json": `{"shell_name":"x"}`,
		"touch_state.json":      `{"targets":{}}`,
		"settings.json":         `{"balloon_timeout_ms":15000}`,
		"credentials.dat":       "cipher",
	}
	for name, body := range old {
		p := filepath.Join(root, "profile", name)
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	got := MigrateLegacy(root)
	if len(got) != len(old) {
		t.Fatalf("migration actions=%#v", got)
	}
	for _, p := range []string{Affect(root), Appearance(root), Touch(root), RuntimeSettings(root), CredentialsDAT(root)} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("missing migrated path %s: %v", p, err)
		}
	}
	for name := range old {
		if _, err := os.Stat(filepath.Join(root, "profile", name)); !os.IsNotExist(err) {
			t.Fatalf("legacy file survived: %s", name)
		}
	}
}
