package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRuntimeRoutesAreCanonicalNine(t *testing.T) {
	want := []string{
		"activate_character_link",
		"get_character_context",
		"begin_character_reaction",
		"request_bridge_reaction",
		"update_character_thinking",
		"begin_character_response",
		"commit_linked_chat",
		"abort_linked_chat",
		"deactivate_character_link",
	}
	if len(runtimeRoutes) != len(want) {
		t.Fatalf("runtime route count=%d want=%d", len(runtimeRoutes), len(want))
	}
	for _, name := range want {
		if runtimeRoutes[name] == "" {
			t.Fatalf("missing canonical linked tool %q", name)
		}
	}
}

func TestLoadConfigReadsRuntimeKeyFromEnvironmentOnly(t *testing.T) {
	root := t.TempDir()
	plug := filepath.Join(root, "Plug")
	if err := os.MkdirAll(plug, 0o755); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(plug, "link_config.json")
	cfgText := `{"tunnel_id":"tunnel_0123456789abcdef0123456789abcdef","runtime_api_key_env":"CONTROL_PLANE_API_KEY"}`
	if err := os.WriteFile(cfgPath, []byte(cfgText), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CONTROL_PLANE_API_KEY", "runtime-secret-must-not-be-persisted")
	t.Setenv("OPENAI_API_KEY", "")

	a := &app{root: root}
	cfg, key, err := a.loadConfig()
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if cfg.TunnelID != "tunnel_0123456789abcdef0123456789abcdef" {
		t.Fatalf("unexpected tunnel id %q", cfg.TunnelID)
	}
	if key != "runtime-secret-must-not-be-persisted" {
		t.Fatalf("unexpected runtime key")
	}
	after, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(after), key) {
		t.Fatal("runtime API key was persisted into link_config.json")
	}
}

func TestLoadConfigRejectsMissingRuntimeKey(t *testing.T) {
	root := t.TempDir()
	plug := filepath.Join(root, "Plug")
	if err := os.MkdirAll(plug, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(plug, "link_config.json"), []byte(`{"tunnel_id":"tunnel_0123456789abcdef0123456789abcdef"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CONTROL_PLANE_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	if _, _, err := (&app{root: root}).loadConfig(); err == nil {
		t.Fatal("expected missing runtime tunnel key to be rejected")
	}
}

func TestPublicErrorIsBounded(t *testing.T) {
	long := strings.Repeat("x", 400)
	got := publicError(assertErr(long))
	if len([]rune(got)) != 240 {
		t.Fatalf("bounded public error length=%d want=240", len([]rune(got)))
	}
}

type assertErr string

func (e assertErr) Error() string { return string(e) }
