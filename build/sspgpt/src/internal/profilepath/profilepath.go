package profilepath

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
)

func Generated(root string) string { return filepath.Join(root, "profile", "generated") }
func State(root string) string     { return filepath.Join(root, "profile", "state") }
func Settings(root string) string  { return filepath.Join(root, "profile", "settings") }
func Secrets(root string) string   { return filepath.Join(root, "profile", "secrets") }

func Affect(root string) string          { return filepath.Join(State(root), "affect.json") }
func Appearance(root string) string      { return filepath.Join(State(root), "appearance.json") }
func Touch(root string) string           { return filepath.Join(State(root), "touch.json") }
func RuntimeSettings(root string) string { return filepath.Join(Settings(root), "runtime.json") }
func CredentialsDAT(root string) string  { return filepath.Join(Secrets(root), "credentials.dat") }
func CredentialsJSON(root string) string { return filepath.Join(Secrets(root), "credentials.json") }

// Ensure creates only the ownership directories. It never scans profile/ and
// therefore cannot accidentally expose generated/state/settings/secrets data to
// a caller that should own only one subtree.
func Ensure(root string) error {
	for _, p := range []struct {
		path string
		mode os.FileMode
	}{
		{Generated(root), 0755},
		{State(root), 0755},
		{Settings(root), 0755},
		{Secrets(root), 0700},
	} {
		if err := os.MkdirAll(p.path, p.mode); err != nil {
			return err
		}
	}
	return nil
}

type moveSpec struct{ old, new string }

// Migration is deliberately split by owner. Bridge owns credentials,
// Runtime owns affect/appearance/settings, and TouchProgress owns touch state.
// This avoids a startup process touching another component's persistent state
// merely because all of it lives beneath profile/.
func MigrateRuntime(root string) []string {
	_ = Ensure(root)
	return migrateSpecs(root, []moveSpec{
		{filepath.Join(root, "profile", "emotional_state.json"), Affect(root)},
		{filepath.Join(root, "profile", "appearance_state.json"), Appearance(root)},
		{filepath.Join(root, "profile", "settings.json"), RuntimeSettings(root)},
	})
}

func MigrateTouch(root string) []string {
	_ = Ensure(root)
	return migrateSpecs(root, []moveSpec{
		{filepath.Join(root, "profile", "touch_state.json"), Touch(root)},
	})
}

func MigrateCredentials(root string) []string {
	_ = Ensure(root)
	return migrateSpecs(root, []moveSpec{
		{filepath.Join(root, "profile", "credentials.dat"), CredentialsDAT(root)},
		{filepath.Join(root, "profile", "credentials.json"), CredentialsJSON(root)},
	})
}

// MigrateLegacy remains as a test/maintenance convenience and composes the
// three owner-scoped migrations. Runtime code should prefer the owner-specific
// function above.
func MigrateLegacy(root string) []string {
	out := []string{}
	out = append(out, MigrateRuntime(root)...)
	out = append(out, MigrateTouch(root)...)
	out = append(out, MigrateCredentials(root)...)
	return out
}

func migrateSpecs(root string, specs []moveSpec) []string {
	out := []string{}
	for _, s := range specs {
		if _, err := os.Stat(s.old); err != nil {
			continue
		}
		if nb, err := os.ReadFile(s.new); err == nil {
			ob, oerr := os.ReadFile(s.old)
			if oerr == nil && bytes.Equal(ob, nb) {
				if os.Remove(s.old) == nil {
					out = append(out, "removed_duplicate="+rel(root, s.old))
				}
			} else {
				out = append(out, "migration_conflict="+rel(root, s.old)+"->"+rel(root, s.new))
			}
			continue
		}
		if err := moveFile(s.old, s.new); err == nil {
			out = append(out, "migrated="+rel(root, s.old)+"->"+rel(root, s.new))
		} else {
			out = append(out, "migration_error="+rel(root, s.old)+":"+err.Error())
		}
	}
	return out
}

func moveFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	b, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	st, err := os.Stat(src)
	if err != nil {
		return err
	}
	tmp := dst + ".tmp"
	if err := os.WriteFile(tmp, b, st.Mode().Perm()); err != nil {
		return err
	}
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Remove(src); err != nil {
		return fmt.Errorf("copied but legacy remove failed: %w", err)
	}
	return nil
}

func rel(root, p string) string {
	r, err := filepath.Rel(root, p)
	if err != nil {
		return p
	}
	return filepath.ToSlash(r)
}
