package localinfer

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestConfigDevicePolicyDefaultsAndCUDAConfig(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "config"), 0755); err != nil {
		t.Fatal(err)
	}
	raw := `{
      "auto_download": true,
      "device_policy": {"mode":"cuda","gpu_layers":99,"cpu_fallback":true},
      "cuda_runner": {"id":"cuda","subdir":"llama-cuda","binary":"llama-server.exe","archives":[{"url":"https://example.invalid/cuda.zip"}]},
      "runner": {"id":"cpu","subdir":"llama","binary":"llama-server.exe","archive_url":"https://example.invalid/cpu.zip"},
      "embedding_dimension":512,
      "embedding_generation":1
    }`
	if err := os.WriteFile(filepath.Join(root, "config", "local_models.json"), []byte(raw), 0644); err != nil {
		t.Fatal(err)
	}
	m := New(root)
	c := m.Config()
	if c.DevicePolicy.Mode != "cuda" || c.DevicePolicy.GPULayers != 99 || !c.DevicePolicy.CPUFallback {
		t.Fatalf("bad device policy: %#v", c.DevicePolicy)
	}
	got := m.runnerCandidates(c)
	if len(got) != 2 || got[0].Device != "cuda" || got[1].Device != "cpu" {
		t.Fatalf("bad candidates: %#v", got)
	}
	if got[0].Args[0] != "--gpu-layers" || got[0].Args[1] != "99" {
		t.Fatalf("bad cuda args: %#v", got[0].Args)
	}
}

func TestRunnerArchivesLegacyAndMultiArchive(t *testing.T) {
	legacy := runnerArchives(Runner{ArchiveURL: "https://example.invalid/cpu.zip", ArchiveSHA256: "abc"})
	if len(legacy) != 1 || legacy[0].SHA256 != "abc" {
		t.Fatalf("legacy runner broken: %#v", legacy)
	}
	multi := runnerArchives(Runner{Archives: []Archive{{URL: "a"}, {URL: "b"}}})
	if len(multi) != 2 {
		t.Fatalf("multi archive runner broken: %#v", multi)
	}
}

func TestNormalizeStructuredJSONRepairsSmallModelListDrift(t *testing.T) {
	raw := []byte(`{"evaluation":{"reason_tags":"emotional"},"semantic_candidates":[{"kind":"fact","text":"x","entities":{"name":"京都","type":"place"},"contradicts":"old"}]}`)
	var out struct {
		Evaluation struct {
			ReasonTags []string `json:"reason_tags"`
		} `json:"evaluation"`
		Candidates []struct {
			Entities    []string `json:"entities"`
			Contradicts []string `json:"contradicts"`
		} `json:"semantic_candidates"`
	}
	if err := json.Unmarshal(normalizeStructuredJSON(raw), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Evaluation.ReasonTags) != 1 || out.Evaluation.ReasonTags[0] != "emotional" {
		t.Fatalf("reason_tags not normalized: %#v", out.Evaluation.ReasonTags)
	}
	if len(out.Candidates) != 1 || len(out.Candidates[0].Entities) == 0 || out.Candidates[0].Entities[0] != "京都" {
		t.Fatalf("entities not normalized: %#v", out.Candidates)
	}
	if len(out.Candidates[0].Contradicts) != 1 || out.Candidates[0].Contradicts[0] != "old" {
		t.Fatalf("contradicts not normalized: %#v", out.Candidates[0].Contradicts)
	}
}

func TestLegacyInferenceLogsMigrateToUnifiedLogs(t *testing.T) {
	root := t.TempDir()
	legacyDir := filepath.Join(root, "memory", "inference")
	if err := os.MkdirAll(legacyDir, 0755); err != nil {
		t.Fatal(err)
	}
	legacy := filepath.Join(legacyDir, "memory_llm.log")
	if err := os.WriteFile(legacy, []byte("old-local-log\n"), 0644); err != nil {
		t.Fatal(err)
	}
	_ = New(root)
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Fatalf("legacy log not removed: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(root, "logs", "inference", "memory_llm.log"))
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "old-local-log\n" {
		t.Fatalf("bad migrated log: %q", b)
	}
}
