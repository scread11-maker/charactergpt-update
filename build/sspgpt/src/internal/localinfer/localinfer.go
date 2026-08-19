package localinfer

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type Asset struct {
	ID       string   `json:"id"`
	Filename string   `json:"filename"`
	URL      string   `json:"url"`
	SHA256   string   `json:"sha256"`
	Port     int      `json:"port"`
	Args     []string `json:"args"`
}
type Archive struct {
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
}
type Runner struct {
	ID            string    `json:"id"`
	ArchiveURL    string    `json:"archive_url,omitempty"`
	ArchiveSHA256 string    `json:"archive_sha256,omitempty"`
	Archives      []Archive `json:"archives,omitempty"`
	Binary        string    `json:"binary"`
	Subdir        string    `json:"subdir,omitempty"`
}
type DevicePolicy struct {
	Mode        string `json:"mode"`
	GPULayers   int    `json:"gpu_layers"`
	CPUFallback bool   `json:"cpu_fallback"`
}
type Config struct {
	FormatVersion       int          `json:"format_version"`
	AutoDownload        bool         `json:"auto_download"`
	Runner              Runner       `json:"runner"`
	CUDARunner          Runner       `json:"cuda_runner"`
	DevicePolicy        DevicePolicy `json:"device_policy"`
	MemoryLLM           Asset        `json:"memory_llm"`
	Embedder            Asset        `json:"embedder"`
	Reranker            Asset        `json:"reranker"`
	EmbeddingDimension  int          `json:"embedding_dimension"`
	EmbeddingGeneration int          `json:"embedding_generation"`
	Mock                bool         `json:"mock"`
}
type Status struct {
	Role       string `json:"role"`
	ModelID    string `json:"model_id"`
	Ready      bool   `json:"ready"`
	Downloaded bool   `json:"downloaded"`
	Device     string `json:"device,omitempty"`
	RunnerID   string `json:"runner_id,omitempty"`
	GPUName    string `json:"gpu_name,omitempty"`
	LastError  string `json:"last_error,omitempty"`
}
type runnerChoice struct {
	Device  string
	Runner  Runner
	Args    []string
	GPUName string
}
type Manager struct {
	Root   string
	mu     sync.Mutex
	procs  map[string]*exec.Cmd
	errs   map[string]string
	active map[string]runnerChoice
	client *http.Client
}

func New(root string) *Manager {
	migrateLegacyInferenceLogs(root)
	return &Manager{Root: root, procs: map[string]*exec.Cmd{}, errs: map[string]string{}, active: map[string]runnerChoice{}, client: &http.Client{Timeout: 45 * time.Second}}
}

func migrateLegacyInferenceLogs(root string) {
	for _, role := range []string{"memory_llm", "embedder", "reranker"} {
		src := filepath.Join(root, "memory", "inference", role+".log")
		dst := filepath.Join(root, "logs", "inference", role+".log")
		b, err := os.ReadFile(src)
		if err != nil {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
			continue
		}
		f, err := os.OpenFile(dst, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			continue
		}
		if st, _ := f.Stat(); st != nil && st.Size() > 0 && len(b) > 0 {
			_, _ = f.WriteString("\n")
		}
		_, werr := f.Write(b)
		cerr := f.Close()
		if werr == nil && cerr == nil {
			_ = os.Remove(src)
		}
	}
}
func (m *Manager) Config() Config {
	c := Config{AutoDownload: true, EmbeddingDimension: 512, EmbeddingGeneration: 1, DevicePolicy: DevicePolicy{Mode: "auto", GPULayers: 99, CPUFallback: true}}
	b, e := os.ReadFile(filepath.Join(m.Root, "config", "local_models.json"))
	if e == nil {
		_ = json.Unmarshal(b, &c)
	}
	if os.Getenv("SSPGPT_LOCAL_MOCK") == "1" {
		c.Mock = true
	}
	if c.EmbeddingDimension <= 0 {
		c.EmbeddingDimension = 512
	}
	if c.EmbeddingGeneration <= 0 {
		c.EmbeddingGeneration = 1
	}
	if strings.TrimSpace(c.DevicePolicy.Mode) == "" {
		c.DevicePolicy.Mode = "auto"
	}
	if c.DevicePolicy.GPULayers == 0 {
		c.DevicePolicy.GPULayers = 99
	}
	return c
}
func (m *Manager) asset(role string, c Config) (Asset, error) {
	switch role {
	case "memory_llm":
		return c.MemoryLLM, nil
	case "embedder":
		return c.Embedder, nil
	case "reranker":
		return c.Reranker, nil
	}
	return Asset{}, fmt.Errorf("unknown local inference role %q", role)
}
func (m *Manager) modelPath(a Asset) string {
	return filepath.Join(m.Root, "memory", "models", a.Filename)
}
func (m *Manager) runnerDir(r Runner) string {
	sub := strings.TrimSpace(r.Subdir)
	if sub == "" {
		sub = strings.TrimSpace(r.ID)
	}
	if sub == "" {
		sub = "llama"
	}
	sub = strings.NewReplacer("/", "_", "\\", "_", ":", "_").Replace(sub)
	return filepath.Join(m.Root, "memory", "inference", sub)
}
func (m *Manager) runnerPath(r Runner) string { return filepath.Join(m.runnerDir(r), r.Binary) }
func (m *Manager) endpoint(a Asset) string    { return fmt.Sprintf("http://127.0.0.1:%d", a.Port) }

func shaFile(path string) (string, error) {
	f, e := os.Open(path)
	if e != nil {
		return "", e
	}
	defer f.Close()
	h := sha256.New()
	if _, e = io.Copy(h, f); e != nil {
		return "", e
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
func verify(path, want string) error {
	if strings.TrimSpace(want) == "" {
		return nil
	}
	got, e := shaFile(path)
	if e != nil {
		return e
	}
	if !strings.EqualFold(got, want) {
		return fmt.Errorf("sha256 mismatch %s got=%s want=%s", filepath.Base(path), got, want)
	}
	return nil
}
func download(ctx context.Context, url, dst, want string) error {
	if url == "" {
		return errors.New("download URL missing")
	}
	if e := os.MkdirAll(filepath.Dir(dst), 0755); e != nil {
		return e
	}
	tmp := dst + ".partial"
	_ = os.Remove(tmp)
	req, e := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if e != nil {
		return e
	}
	req.Header.Set("User-Agent", "SSPGPT-v0.7.1-GPU1")
	resp, e := http.DefaultClient.Do(req)
	if e != nil {
		return e
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("download %s: %s", url, resp.Status)
	}
	f, e := os.Create(tmp)
	if e != nil {
		return e
	}
	_, ce := io.Copy(f, resp.Body)
	closee := f.Close()
	if ce != nil {
		return ce
	}
	if closee != nil {
		return closee
	}
	if e = verify(tmp, want); e != nil {
		_ = os.Remove(tmp)
		return e
	}
	return os.Rename(tmp, dst)
}
func unzip(src, dst string) error {
	z, e := zip.OpenReader(src)
	if e != nil {
		return e
	}
	defer z.Close()
	for _, f := range z.File {
		clean := filepath.Clean(f.Name)
		if strings.Contains(clean, "..") || filepath.IsAbs(clean) {
			continue
		}
		out := filepath.Join(dst, clean)
		if f.FileInfo().IsDir() {
			_ = os.MkdirAll(out, 0755)
			continue
		}
		_ = os.MkdirAll(filepath.Dir(out), 0755)
		r, e := f.Open()
		if e != nil {
			return e
		}
		w, e := os.Create(out)
		if e != nil {
			r.Close()
			return e
		}
		_, e = io.Copy(w, r)
		r.Close()
		w.Close()
		if e != nil {
			return e
		}
	}
	return nil
}

func runnerArchives(r Runner) []Archive {
	if len(r.Archives) > 0 {
		return append([]Archive(nil), r.Archives...)
	}
	if strings.TrimSpace(r.ArchiveURL) != "" {
		return []Archive{{URL: r.ArchiveURL, SHA256: r.ArchiveSHA256}}
	}
	return nil
}

func archiveBase(raw string) string {
	raw = strings.Split(raw, "?")[0]
	b := filepath.Base(raw)
	if b == "." || b == "" || b == string(filepath.Separator) {
		return "llama-runtime.zip"
	}
	return b
}

func (m *Manager) ensureRunner(ctx context.Context, c Config, r Runner) error {
	if c.Mock {
		return nil
	}
	if strings.TrimSpace(r.Binary) == "" {
		return errors.New("runner binary missing")
	}
	p := m.runnerPath(r)
	if _, e := os.Stat(p); e == nil {
		return nil
	}
	if !c.AutoDownload {
		return fmt.Errorf("local runner missing: %s", p)
	}
	archives := runnerArchives(r)
	if len(archives) == 0 {
		return fmt.Errorf("runner %s has no archives", r.ID)
	}
	dir := m.runnerDir(r)
	cache := filepath.Join(m.Root, "memory", "inference", "downloads")
	_ = os.MkdirAll(dir, 0755)
	_ = os.MkdirAll(cache, 0755)
	for _, a := range archives {
		if strings.TrimSpace(a.URL) == "" {
			return fmt.Errorf("runner %s archive URL missing", r.ID)
		}
		archive := filepath.Join(cache, archiveBase(a.URL))
		if _, e := os.Stat(archive); e == nil {
			if ve := verify(archive, a.SHA256); ve != nil {
				_ = os.Remove(archive)
			}
		}
		if _, e := os.Stat(archive); e != nil {
			if e = download(ctx, a.URL, archive, a.SHA256); e != nil {
				return e
			}
		}
		if e := unzip(archive, dir); e != nil {
			return e
		}
	}
	// Some archives contain a top-level directory. Copy the executable to the backend root.
	if _, e := os.Stat(p); e != nil {
		var found string
		_ = filepath.Walk(dir, func(x string, info os.FileInfo, e error) error {
			if e == nil && info != nil && !info.IsDir() && strings.EqualFold(info.Name(), r.Binary) && found == "" {
				found = x
			}
			return nil
		})
		if found != "" && found != p {
			b, re := os.ReadFile(found)
			if re == nil {
				_ = os.WriteFile(p, b, 0755)
			}
		}
	}
	if _, e := os.Stat(p); e != nil {
		return fmt.Errorf("runner %s archives did not contain %s", r.ID, r.Binary)
	}
	return nil
}

func (m *Manager) EnsureAsset(ctx context.Context, role string) error {
	c := m.Config()
	if c.Mock {
		return nil
	}
	a, e := m.asset(role, c)
	if e != nil {
		return e
	}
	p := m.modelPath(a)
	if _, e = os.Stat(p); e == nil {
		return verify(p, a.SHA256)
	}
	if !c.AutoDownload {
		return fmt.Errorf("model missing: %s", p)
	}
	return download(ctx, a.URL, p, a.SHA256)
}

func (m *Manager) healthy(ctx context.Context, a Asset) bool {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, m.endpoint(a)+"/health", nil)
	r, e := m.client.Do(req)
	if r != nil {
		r.Body.Close()
	}
	return e == nil && r.StatusCode >= 200 && r.StatusCode < 300
}

func (m *Manager) runnerCandidates(c Config) []runnerChoice {
	mode := strings.ToLower(strings.TrimSpace(c.DevicePolicy.Mode))
	if mode == "" {
		mode = "auto"
	}
	name := ""
	hasCUDA, gpuName := detectNVIDIAGPU()
	if hasCUDA {
		name = gpuName
	}
	gpuLayers := c.DevicePolicy.GPULayers
	if gpuLayers <= 0 {
		gpuLayers = 99
	}
	cuda := runnerChoice{Device: "cuda", Runner: c.CUDARunner, Args: []string{"--gpu-layers", fmt.Sprint(gpuLayers)}, GPUName: name}
	cpu := runnerChoice{Device: "cpu", Runner: c.Runner}
	switch mode {
	case "cpu":
		return []runnerChoice{cpu}
	case "cuda":
		out := []runnerChoice{cuda}
		if c.DevicePolicy.CPUFallback {
			out = append(out, cpu)
		}
		return out
	default:
		if hasCUDA && strings.TrimSpace(c.CUDARunner.Binary) != "" {
			out := []runnerChoice{cuda}
			if c.DevicePolicy.CPUFallback {
				out = append(out, cpu)
			}
			return out
		}
		return []runnerChoice{cpu}
	}
}

func (m *Manager) killRole(role string) {
	m.mu.Lock()
	cmd := m.procs[role]
	delete(m.procs, role)
	delete(m.active, role)
	m.mu.Unlock()
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}

func (m *Manager) startServer(ctx context.Context, role string, a Asset, choice runnerChoice) error {
	c := m.Config()
	if e := m.ensureRunner(ctx, c, choice.Runner); e != nil {
		return e
	}
	args := []string{"-m", m.modelPath(a), "--host", "127.0.0.1", "--port", fmt.Sprint(a.Port)}
	args = append(args, choice.Args...)
	args = append(args, a.Args...)
	cmd := exec.Command(m.runnerPath(choice.Runner), args...)
	cmd.Dir = m.runnerDir(choice.Runner)
	cmd.SysProcAttr = hiddenSysProcAttr()
	logPath := filepath.Join(m.Root, "logs", "inference", role+".log")
	_ = os.MkdirAll(filepath.Dir(logPath), 0755)
	lf, _ := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if lf != nil {
		fmt.Fprintf(lf, "\n[%s] START role=%s device=%s runner=%s gpu=%q args=%q\n", time.Now().Format(time.RFC3339), role, choice.Device, choice.Runner.ID, choice.GPUName, args)
	}
	cmd.Stdout = lf
	cmd.Stderr = lf
	if e := cmd.Start(); e != nil {
		if lf != nil {
			lf.Close()
		}
		return e
	}
	m.mu.Lock()
	m.procs[role] = cmd
	m.active[role] = choice
	m.mu.Unlock()
	done := make(chan error, 1)
	go func() {
		e := cmd.Wait()
		done <- e
		m.mu.Lock()
		if m.procs[role] == cmd {
			delete(m.procs, role)
			delete(m.active, role)
		}
		if e != nil {
			m.errs[role] = e.Error()
		}
		m.mu.Unlock()
		if lf != nil {
			lf.Close()
		}
	}()
	deadline := time.NewTimer(45 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(300 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			_ = cmd.Process.Kill()
			return ctx.Err()
		case e := <-done:
			if e == nil {
				e = errors.New("local model runner exited before becoming ready")
			}
			return e
		case <-deadline.C:
			_ = cmd.Process.Kill()
			return fmt.Errorf("%s %s runner did not become ready", role, choice.Device)
		case <-tick.C:
			hc, cancel := context.WithTimeout(ctx, 800*time.Millisecond)
			ok := m.healthy(hc, a)
			cancel()
			if ok {
				m.mu.Lock()
				delete(m.errs, role)
				m.active[role] = choice
				m.mu.Unlock()
				return nil
			}
		}
	}
}

func (m *Manager) EnsureServer(ctx context.Context, role string) error {
	c := m.Config()
	if c.Mock {
		return nil
	}
	a, e := m.asset(role, c)
	if e != nil {
		return e
	}
	hc, cancel := context.WithTimeout(ctx, 600*time.Millisecond)
	ok := m.healthy(hc, a)
	cancel()
	if ok {
		return nil
	}
	// A role owns one localhost port. Stop any stale runner before reselecting a backend.
	m.killRole(role)
	prep, pcancel := context.WithTimeout(ctx, 30*time.Minute)
	e = m.EnsureAsset(prep, role)
	pcancel()
	if e != nil {
		m.mu.Lock()
		m.errs[role] = e.Error()
		m.mu.Unlock()
		return e
	}
	var failures []string
	for _, choice := range m.runnerCandidates(c) {
		if strings.TrimSpace(choice.Runner.Binary) == "" {
			continue
		}
		startCtx, scancel := context.WithTimeout(ctx, 30*time.Minute)
		e = m.startServer(startCtx, role, a, choice)
		scancel()
		if e == nil {
			return nil
		}
		failures = append(failures, fmt.Sprintf("%s:%v", choice.Device, e))
		m.killRole(role)
	}
	if len(failures) == 0 {
		e = errors.New("no usable local inference runner")
	} else {
		e = errors.New(strings.Join(failures, "; "))
	}
	m.mu.Lock()
	m.errs[role] = e.Error()
	m.mu.Unlock()
	return e
}

func (m *Manager) Status(role string) Status {
	c := m.Config()
	a, e := m.asset(role, c)
	if e != nil {
		return Status{Role: role, LastError: e.Error()}
	}
	st := Status{Role: role, ModelID: a.ID}
	if c.Mock {
		st.Ready = true
		st.Downloaded = true
		st.Device = "mock"
		return st
	}
	_, e = os.Stat(m.modelPath(a))
	st.Downloaded = e == nil
	ctx, cancel := context.WithTimeout(context.Background(), 350*time.Millisecond)
	st.Ready = m.healthy(ctx, a)
	cancel()
	m.mu.Lock()
	st.LastError = m.errs[role]
	if x, ok := m.active[role]; ok {
		st.Device = x.Device
		st.RunnerID = x.Runner.ID
		st.GPUName = x.GPUName
	}
	m.mu.Unlock()
	return st
}

func (m *Manager) Stop() {
	m.mu.Lock()
	procs := make([]*exec.Cmd, 0, len(m.procs))
	for k, c := range m.procs {
		if c != nil {
			procs = append(procs, c)
		}
		delete(m.procs, k)
		delete(m.active, k)
	}
	m.mu.Unlock()
	for _, c := range procs {
		if c.Process != nil {
			_ = c.Process.Kill()
		}
	}
}

func extractJSONText(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.Index(s, "<think>"); i >= 0 {
		if j := strings.Index(s, "</think>"); j >= 0 {
			s = s[j+8:]
		}
	}
	i := strings.Index(s, "{")
	j := strings.LastIndex(s, "}")
	if i >= 0 && j > i {
		return s[i : j+1]
	}
	return s
}
func (m *Manager) ChatJSON(ctx context.Context, system, user string, out any) error {
	return m.ChatJSONLimit(ctx, system, user, 768, out)
}

func (m *Manager) ChatJSONLimit(ctx context.Context, system, user string, maxTokens int, out any) error {
	c := m.Config()
	if c.Mock {
		return errors.New("mock ChatJSON requires caller deterministic mock")
	}
	if e := m.EnsureServer(ctx, "memory_llm"); e != nil {
		return e
	}
	if maxTokens <= 0 {
		maxTokens = 768
	}
	a := c.MemoryLLM
	body := map[string]any{"model": a.ID, "messages": []map[string]string{{"role": "system", "content": system + "\n/no_think"}, {"role": "user", "content": user + "\n/no_think"}}, "temperature": 0.0, "max_tokens": maxTokens, "stream": false, "response_format": map[string]any{"type": "json_object"}}
	b, _ := json.Marshal(body)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, m.endpoint(a)+"/v1/chat/completions", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	r, e := m.client.Do(req)
	if e != nil {
		return e
	}
	defer r.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(r.Body, 4<<20))
	if r.StatusCode < 200 || r.StatusCode >= 300 {
		return fmt.Errorf("local chat %s: %s", r.Status, string(raw))
	}
	var x struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if e = json.Unmarshal(raw, &x); e != nil {
		return e
	}
	if len(x.Choices) == 0 {
		return errors.New("local chat returned no choices")
	}
	rawJSON := []byte(extractJSONText(x.Choices[0].Message.Content))
	return json.Unmarshal(normalizeStructuredJSON(rawJSON), out)
}

// normalizeStructuredJSON is deliberately narrow: it repairs only the common
// list-shape drift observed from sub-billion local models while preserving the
// semantic content. Policy still belongs to MemoryService; this function does
// not invent missing scores or memories.
func normalizeStructuredJSON(raw []byte) []byte {
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return raw
	}
	normalizeStructuredValue(v)
	b, err := json.Marshal(v)
	if err != nil {
		return raw
	}
	return b
}

func normalizeStructuredValue(v any) {
	switch x := v.(type) {
	case map[string]any:
		for k, val := range x {
			switch k {
			case "reason_tags", "entities", "contradicts", "identity", "stable_behavior", "world_context", "stable_appearance", "unknowns", "detail_routes":
				x[k] = coerceStringList(val)
			case "semantic_candidates":
				if m, ok := val.(map[string]any); ok {
					x[k] = []any{m}
				}
			}
			normalizeStructuredValue(x[k])
		}
	case []any:
		for _, item := range x {
			normalizeStructuredValue(item)
		}
	}
}

func coerceStringList(v any) []any {
	out := []any{}
	appendString := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" {
			return
		}
		for _, old := range out {
			if old == s {
				return
			}
		}
		out = append(out, s)
	}
	var walk func(any)
	walk = func(x any) {
		switch y := x.(type) {
		case string:
			appendString(y)
		case []any:
			for _, item := range y {
				walk(item)
			}
		case map[string]any:
			for _, key := range []string{"name", "text", "value", "label", "id"} {
				if z, ok := y[key]; ok {
					walk(z)
				}
			}
			if len(out) == 0 {
				for _, z := range y {
					walk(z)
				}
			}
		}
	}
	walk(v)
	return out
}

func normalize(v []float64, dim int) []float64 {
	if dim > 0 && len(v) > dim {
		v = append([]float64(nil), v[:dim]...)
	} else {
		v = append([]float64(nil), v...)
	}
	sum := 0.0
	for _, x := range v {
		sum += x * x
	}
	if sum > 0 {
		d := math.Sqrt(sum)
		for i := range v {
			v[i] /= d
		}
	}
	return v
}
func (m *Manager) Embed(ctx context.Context, text string) ([]float64, error) {
	c := m.Config()
	if c.Mock {
		return deterministicVector(text, c.EmbeddingDimension), nil
	}
	if e := m.EnsureServer(ctx, "embedder"); e != nil {
		return nil, e
	}
	a := c.Embedder
	body := map[string]any{"model": a.ID, "input": []string{text}}
	b, _ := json.Marshal(body)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, m.endpoint(a)+"/v1/embeddings", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	r, e := m.client.Do(req)
	if e != nil {
		return nil, e
	}
	defer r.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(r.Body, 8<<20))
	if r.StatusCode < 200 || r.StatusCode >= 300 {
		return nil, fmt.Errorf("embedding %s: %s", r.Status, string(raw))
	}
	var x struct {
		Data []struct {
			Embedding []float64 `json:"embedding"`
		} `json:"data"`
	}
	if e = json.Unmarshal(raw, &x); e != nil {
		return nil, e
	}
	if len(x.Data) == 0 {
		return nil, errors.New("embedding returned no data")
	}
	return normalize(x.Data[0].Embedding, c.EmbeddingDimension), nil
}
func deterministicVector(text string, dim int) []float64 {
	if dim <= 0 {
		dim = 512
	}
	v := make([]float64, dim)
	r := []rune(strings.ToLower(text))
	for i, ch := range r {
		h := uint64(ch)*11400714819323198485 + uint64(i+1)*0x9e3779b97f4a7c15
		idx := int(h % uint64(dim))
		sign := 1.0
		if h&1 == 1 {
			sign = -1
		}
		v[idx] += sign
	}
	return normalize(v, dim)
}
func (m *Manager) Rerank(ctx context.Context, query string, docs []string) ([]int, error) {
	if len(docs) == 0 {
		return nil, nil
	}
	c := m.Config()
	if c.Mock {
		return lexicalOrder(query, docs), nil
	}
	if e := m.EnsureServer(ctx, "reranker"); e != nil {
		return nil, e
	}
	a := c.Reranker
	body := map[string]any{"model": a.ID, "query": query, "documents": docs, "top_n": len(docs)}
	b, _ := json.Marshal(body)
	urls := []string{m.endpoint(a) + "/v1/rerank", m.endpoint(a) + "/reranking"}
	var last error
	for _, u := range urls {
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(b))
		req.Header.Set("Content-Type", "application/json")
		r, e := m.client.Do(req)
		if e != nil {
			last = e
			continue
		}
		raw, _ := io.ReadAll(io.LimitReader(r.Body, 4<<20))
		r.Body.Close()
		if r.StatusCode < 200 || r.StatusCode >= 300 {
			last = fmt.Errorf("rerank %s: %s", r.Status, string(raw))
			continue
		}
		var x struct {
			Results []struct {
				Index int     `json:"index"`
				Score float64 `json:"relevance_score"`
			} `json:"results"`
		}
		if e = json.Unmarshal(raw, &x); e != nil {
			last = e
			continue
		}
		if len(x.Results) == 0 {
			last = errors.New("rerank returned no results")
			continue
		}
		sort.SliceStable(x.Results, func(i, j int) bool { return x.Results[i].Score > x.Results[j].Score })
		out := make([]int, 0, len(x.Results))
		for _, z := range x.Results {
			if z.Index >= 0 && z.Index < len(docs) {
				out = append(out, z.Index)
			}
		}
		return out, nil
	}
	return nil, last
}
func lexicalOrder(q string, docs []string) []int {
	qt := runeSet(q)
	type x struct {
		i int
		s int
	}
	xs := make([]x, len(docs))
	for i, d := range docs {
		s := 0
		ds := runeSet(d)
		for r := range qt {
			if ds[r] {
				s++
			}
		}
		xs[i] = x{i, s}
	}
	sort.SliceStable(xs, func(i, j int) bool { return xs[i].s > xs[j].s })
	o := make([]int, len(xs))
	for i, z := range xs {
		o[i] = z.i
	}
	return o
}
func runeSet(s string) map[rune]bool {
	m := map[rune]bool{}
	for _, r := range []rune(strings.ToLower(s)) {
		if r > ' ' {
			m[r] = true
		}
	}
	return m
}
