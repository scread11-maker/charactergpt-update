package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	tunnelclient "github.com/openai/tunnel-client"

	"sspgpt/v07/internal/paths"
	"sspgpt/v07/internal/singleinstance"
)

const version = "0.7.1-fix15-mcp"
const statusAddr = "127.0.0.1:8781"
const runtimeBase = "http://127.0.0.1:8770"

var tunnelIDPattern = regexp.MustCompile(`^tunnel_[0-9a-f]{32}$`)

type linkConfig struct {
	TunnelID            string `json:"tunnel_id"`
	RuntimeAPIKeyEnv    string `json:"runtime_api_key_env"`
	ControlPlaneBaseURL string `json:"control_plane_base_url,omitempty"`
	OrganizationID      string `json:"organization_id,omitempty"`
	PollTimeoutMS       int64  `json:"poll_timeout_ms,omitempty"`
}

type app struct {
	root   string
	log    *log.Logger
	client *http.Client

	mu           sync.Mutex
	state        string
	lastError    string
	tunnelID     string
	sessionID    string
	turnID       string
	lastActivity time.Time
	cancel       context.CancelFunc
}

type activateInput struct{}
type contextInput struct {
	SessionID     string `json:"session_id" jsonschema:"active linked session id"`
	SemanticQuery string `json:"semantic_query,omitempty" jsonschema:"optional bounded semantic recall query; omit when recall is unnecessary"`
}
type beginReactionInput struct {
	SessionID      string  `json:"session_id"`
	ExternalTurnID string  `json:"external_turn_id"`
	Expression     string  `json:"expression,omitempty"`
	Intensity      float64 `json:"intensity,omitempty"`
}
type bridgeReactionInput struct {
	SessionID      string `json:"session_id"`
	ExternalTurnID string `json:"external_turn_id"`
	SemanticDigest string `json:"semantic_digest" jsonschema:"bounded semantic digest only; never raw user prompt or hidden reasoning"`
	ReactionIntent string `json:"reaction_intent,omitempty"`
}
type thinkingInput struct {
	SessionID      string  `json:"session_id"`
	ExternalTurnID string  `json:"external_turn_id"`
	Milestone      string  `json:"milestone" jsonschema:"observable milestone only: start, progress, difficulty, or resolved"`
	Expression     string  `json:"expression,omitempty"`
	Intensity      float64 `json:"intensity,omitempty"`
}
type responseInput struct {
	SessionID      string `json:"session_id"`
	ExternalTurnID string `json:"external_turn_id"`
	Expression     string `json:"expression,omitempty"`
	ResponseLength int    `json:"response_length,omitempty"`
}
type commitInput struct {
	SessionID       string `json:"session_id"`
	ExternalTurnID  string `json:"external_turn_id"`
	Status          string `json:"status,omitempty"`
	UserRequest     string `json:"user_request,omitempty"`
	WebResponse     string `json:"web_response,omitempty"`
	RequestSummary  string `json:"request_summary,omitempty"`
	ResponseSummary string `json:"response_summary,omitempty"`
	Topic           string `json:"topic,omitempty"`
	Outcome         string `json:"outcome,omitempty"`
	ReactionEmotion string `json:"reaction_emotion"`
	Expression      string `json:"expression,omitempty"`
}
type abortInput struct {
	SessionID      string `json:"session_id"`
	ExternalTurnID string `json:"external_turn_id"`
	Reason         string `json:"reason,omitempty"`
}
type deactivateInput struct {
	SessionID string `json:"session_id"`
}

func main() {
	root := paths.GhostRoot()
	if !singleinstance.Acquire("Link", root) {
		return
	}
	_ = os.MkdirAll(filepath.Join(root, "logs"), 0755)
	lf, _ := os.OpenFile(filepath.Join(root, "logs", "link.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	a := &app{
		root: root,
		log: log.New(lf, "", log.LstdFlags|log.Lmicroseconds),
		client: &http.Client{Timeout: 45 * time.Second},
		state: "starting",
		lastActivity: time.Now(),
		cancel: cancel,
	}
	go a.serveStatus(ctx)
	a.log.Printf("CharacterGPTLink %s mode=embedded_secure_mcp_tunnel mcp_sdk=v1.7.0 tunnel_sdk=v0.0.11", version)
	if err := a.run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		a.log.Printf("LINK_EXIT error=%v", err)
	}
	a.releaseLinked("link_exit")
}

func (a *app) run(ctx context.Context) error {
	backoff := time.Second
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		cfg, apiKey, err := a.loadConfig()
		if err != nil {
			a.setState("waiting_config", publicError(err), "")
			if !sleepContext(ctx, 2*time.Second) {
				return ctx.Err()
			}
			continue
		}
		a.setState("connecting", "", cfg.TunnelID)
		err = a.runTunnelOnce(ctx, cfg, apiKey)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		a.releaseLinked("tunnel_disconnected")
		a.setState("degraded", publicError(err), cfg.TunnelID)
		a.log.Printf("TUNNEL_RETRY error=%v backoff=%s", err, backoff)
		if !sleepContext(ctx, backoff) {
			return ctx.Err()
		}
		if backoff < 15*time.Second {
			backoff *= 2
			if backoff > 15*time.Second {
				backoff = 15 * time.Second
			}
		}
	}
}

func (a *app) runTunnelOnce(ctx context.Context, cfg linkConfig, apiKey string) error {
	server := a.newMCPServer()
	serverTransport, tunnelTransport := mcp.NewInMemoryTransports()
	serverCtx, stopServer := context.WithCancel(ctx)
	defer stopServer()
	serverDone := make(chan error, 1)
	go func() { serverDone <- server.Run(serverCtx, serverTransport) }()

	tc := tunnelclient.Config{
		TunnelID:            cfg.TunnelID,
		APIKey:              apiKey,
		ControlPlaneBaseURL: strings.TrimSpace(cfg.ControlPlaneBaseURL),
		OrganizationID:      strings.TrimSpace(cfg.OrganizationID),
	}
	if cfg.PollTimeoutMS > 0 {
		tc.PollTimeout = time.Duration(cfg.PollTimeoutMS) * time.Millisecond
	}
	client, err := tunnelclient.New(tc, tunnelTransport)
	if err != nil {
		return fmt.Errorf("create tunnel client: %w", err)
	}
	if err := client.Start(ctx); err != nil {
		return fmt.Errorf("start tunnel client: %w", err)
	}
	defer func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = client.Stop(stopCtx)
	}()
	if err := client.WaitUntilReady(ctx); err != nil {
		return fmt.Errorf("wait tunnel ready: %w", err)
	}
	a.setState("ready", "", cfg.TunnelID)
	a.log.Printf("TUNNEL_READY tunnel_configured=true")

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-client.Done():
		return errors.New("secure tunnel runtime stopped")
	case err := <-serverDone:
		if err == nil || errors.Is(err, context.Canceled) {
			return errors.New("MCP server stopped")
		}
		return fmt.Errorf("MCP server stopped: %w", err)
	}
}

func (a *app) newMCPServer() *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: "SSPGPT-Muna", Version: version}, &mcp.ServerOptions{
		Capabilities: &mcp.ServerCapabilities{},
		Instructions: "This server is Muna's linked embodiment bridge. Runtime owns current state and affect. Use observable milestones only; never send chain-of-thought or scratchpad.",
	})
	mcp.AddTool(s, &mcp.Tool{Name: "activate_character_link", Description: "Activate Muna's single linked embodiment session and return its stable local character profile."}, func(ctx context.Context, _ *mcp.CallToolRequest, in activateInput) (*mcp.CallToolResult, map[string]any, error) {
		out, err := invokeTool(a, ctx, "activate_character_link", in)
		return nil, out, err
	})
	mcp.AddTool(s, &mcp.Tool{Name: "get_character_context", Description: "Read bounded authoritative NOW, current embodiment, hot memory, and optional semantic recall near the start of a linked turn."}, func(ctx context.Context, _ *mcp.CallToolRequest, in contextInput) (*mcp.CallToolResult, map[string]any, error) {
		out, err := invokeTool(a, ctx, "get_character_context", in)
		return nil, out, err
	})
	mcp.AddTool(s, &mcp.Tool{Name: "begin_character_reaction", Description: "Mark a new web turn as received and give Muna immediate local presence. Do not send raw user text."}, func(ctx context.Context, _ *mcp.CallToolRequest, in beginReactionInput) (*mcp.CallToolResult, map[string]any, error) {
		out, err := invokeTool(a, ctx, "begin_character_reaction", in)
		return nil, out, err
	})
	mcp.AddTool(s, &mcp.Tool{Name: "request_bridge_reaction", Description: "Ask Bridge as Secondary Brain for one bounded acknowledgement using only a semantic digest."}, func(ctx context.Context, _ *mcp.CallToolRequest, in bridgeReactionInput) (*mcp.CallToolResult, map[string]any, error) {
		out, err := invokeTool(a, ctx, "request_bridge_reaction", in)
		return nil, out, err
	})
	mcp.AddTool(s, &mcp.Tool{Name: "update_character_thinking", Description: "Send one observable thinking milestone only. Never send hidden reasoning, chain-of-thought, or scratchpad."}, func(ctx context.Context, _ *mcp.CallToolRequest, in thinkingInput) (*mcp.CallToolResult, map[string]any, error) {
		out, err := invokeTool(a, ctx, "update_character_thinking", in)
		return nil, out, err
	})
	mcp.AddTool(s, &mcp.Tool{Name: "begin_character_response", Description: "Transition the linked turn to responding immediately before the web answer becomes visible."}, func(ctx context.Context, _ *mcp.CallToolRequest, in responseInput) (*mcp.CallToolResult, map[string]any, error) {
		out, err := invokeTool(a, ctx, "begin_character_response", in)
		return nil, out, err
	})
	mcp.AddTool(s, &mcp.Tool{Name: "commit_linked_chat", Description: "Commit exactly one completed linked turn through Runtime affect and the ordinary EpisodeCommitV2 memory path."}, func(ctx context.Context, _ *mcp.CallToolRequest, in commitInput) (*mcp.CallToolResult, map[string]any, error) {
		out, err := invokeTool(a, ctx, "commit_linked_chat", in)
		return nil, out, err
	})
	mcp.AddTool(s, &mcp.Tool{Name: "abort_linked_chat", Description: "Idempotently abort an active linked turn without final affect mutation or completed memory."}, func(ctx context.Context, _ *mcp.CallToolRequest, in abortInput) (*mcp.CallToolResult, map[string]any, error) {
		out, err := invokeTool(a, ctx, "abort_linked_chat", in)
		return nil, out, err
	})
	mcp.AddTool(s, &mcp.Tool{Name: "deactivate_character_link", Description: "Idempotently release the linked session and restore ordinary local Primary-Brain mode."}, func(ctx context.Context, _ *mcp.CallToolRequest, in deactivateInput) (*mcp.CallToolResult, map[string]any, error) {
		out, err := invokeTool(a, ctx, "deactivate_character_link", in)
		return nil, out, err
	})
	return s
}

func invokeTool[T any](a *app, ctx context.Context, name string, in T) (map[string]any, error) {
	b, err := json.Marshal(in)
	if err != nil {
		return nil, err
	}
	out, err := a.callRuntime(ctx, name, b)
	if err != nil {
		return nil, err
	}
	a.observeSuccess(name, b, out)
	return out, nil
}

var runtimeRoutes = map[string]string{
	"activate_character_link":   "/linked/session/activate",
	"get_character_context":     "/linked/context",
	"begin_character_reaction":  "/linked/turn/begin",
	"request_bridge_reaction":   "/linked/turn/bridge-reaction",
	"update_character_thinking": "/linked/turn/thinking",
	"begin_character_response":  "/linked/turn/response",
	"commit_linked_chat":        "/linked/turn/commit",
	"abort_linked_chat":         "/linked/turn/abort",
	"deactivate_character_link": "/linked/session/deactivate",
}

func (a *app) callRuntime(ctx context.Context, name string, args []byte) (map[string]any, error) {
	route := runtimeRoutes[name]
	if route == "" {
		return nil, fmt.Errorf("unknown linked tool %q", name)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, runtimeBase+route, bytes.NewReader(args))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := a.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Runtime unavailable")
	}
	defer resp.Body.Close()
	var out map[string]any
	if err := json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&out); err != nil {
		return nil, fmt.Errorf("Runtime returned invalid response")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if msg, _ := out["error"].(string); strings.TrimSpace(msg) != "" {
			return nil, fmt.Errorf("Runtime rejected linked operation: %s", msg)
		}
		return nil, fmt.Errorf("Runtime rejected linked operation (%s)", resp.Status)
	}
	return out, nil
}

func (a *app) observeSuccess(name string, args []byte, out map[string]any) {
	var ref struct {
		SessionID      string `json:"session_id"`
		ExternalTurnID string `json:"external_turn_id"`
	}
	_ = json.Unmarshal(args, &ref)
	a.mu.Lock()
	defer a.mu.Unlock()
	a.lastActivity = time.Now()
	switch name {
	case "activate_character_link":
		if sid, _ := out["session_id"].(string); sid != "" {
			a.sessionID = sid
			a.turnID = ""
		}
	case "begin_character_reaction":
		a.sessionID = ref.SessionID
		a.turnID = ref.ExternalTurnID
	case "commit_linked_chat", "abort_linked_chat":
		a.turnID = ""
	case "deactivate_character_link":
		a.sessionID = ""
		a.turnID = ""
	}
}

func (a *app) releaseLinked(reason string) {
	a.mu.Lock()
	sid, turn := a.sessionID, a.turnID
	a.sessionID, a.turnID = "", ""
	a.mu.Unlock()
	if sid == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	if turn != "" {
		b, _ := json.Marshal(abortInput{SessionID: sid, ExternalTurnID: turn, Reason: reason})
		_, _ = a.callRuntime(ctx, "abort_linked_chat", b)
	}
	b, _ := json.Marshal(deactivateInput{SessionID: sid})
	_, _ = a.callRuntime(ctx, "deactivate_character_link", b)
	a.log.Printf("LINKED_RELEASE reason=%s had_turn=%t", reason, turn != "")
}

func (a *app) loadConfig() (linkConfig, string, error) {
	cfg := linkConfig{RuntimeAPIKeyEnv: "CONTROL_PLANE_API_KEY"}
	p := filepath.Join(a.root, "Plug", "link_config.json")
	if b, err := os.ReadFile(p); err == nil {
		if err := json.Unmarshal(b, &cfg); err != nil {
			return cfg, "", fmt.Errorf("invalid Plug/link_config.json")
		}
	}
	if v := strings.TrimSpace(os.Getenv("CONTROL_PLANE_TUNNEL_ID")); v != "" {
		cfg.TunnelID = v
	}
	cfg.TunnelID = strings.TrimSpace(cfg.TunnelID)
	if cfg.TunnelID == "" {
		return cfg, "", errors.New("tunnel id is not configured")
	}
	if !tunnelIDPattern.MatchString(cfg.TunnelID) {
		return cfg, "", errors.New("tunnel id has invalid format")
	}
	keyEnv := strings.TrimSpace(cfg.RuntimeAPIKeyEnv)
	if keyEnv == "" {
		keyEnv = "CONTROL_PLANE_API_KEY"
	}
	apiKey := strings.TrimSpace(os.Getenv(keyEnv))
	if apiKey == "" && keyEnv != "CONTROL_PLANE_API_KEY" {
		apiKey = strings.TrimSpace(os.Getenv("CONTROL_PLANE_API_KEY"))
	}
	if apiKey == "" {
		apiKey = strings.TrimSpace(os.Getenv("OPENAI_API_KEY"))
	}
	if apiKey == "" {
		return cfg, "", errors.New("runtime tunnel API key is not available in the configured environment")
	}
	if cfg.ControlPlaneBaseURL == "" {
		cfg.ControlPlaneBaseURL = strings.TrimSpace(os.Getenv("CONTROL_PLANE_BASE_URL"))
	}
	if cfg.OrganizationID == "" {
		cfg.OrganizationID = strings.TrimSpace(os.Getenv("CONTROL_PLANE_ORGANIZATION_ID"))
	}
	return cfg, apiKey, nil
}

func (a *app) setState(state, errText, tunnelID string) {
	a.mu.Lock()
	a.state = state
	a.lastError = errText
	if tunnelID != "" {
		a.tunnelID = tunnelID
	}
	a.mu.Unlock()
}

func (a *app) serveStatus(ctx context.Context) {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", a.health)
	mux.HandleFunc("/status", a.status)
	mux.HandleFunc("/shutdown", a.shutdown)
	srv := &http.Server{Addr: statusAddr, Handler: mux, ReadHeaderTimeout: 2 * time.Second}
	go func() {
		<-ctx.Done()
		stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(stopCtx)
	}()
	a.log.Printf("STATUS_LISTEN addr=%s", statusAddr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		a.log.Printf("STATUS_SERVER error=%v", err)
	}
}

func (a *app) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, 200, map[string]any{"ok": true, "service": "CharacterGPTLink", "version": version})
}

func (a *app) status(w http.ResponseWriter, _ *http.Request) {
	a.mu.Lock()
	state, lastErr := a.state, a.lastError
	configured := a.tunnelID != ""
	sessionActive := a.sessionID != ""
	turnActive := a.turnID != ""
	last := a.lastActivity
	a.mu.Unlock()
	writeJSON(w, 200, map[string]any{
		"ok": true,
		"version": version,
		"state": state,
		"ready": state == "ready",
		"tunnel_configured": configured,
		"session_active": sessionActive,
		"turn_active": turnActive,
		"last_activity": last.UTC().Format(time.RFC3339),
		"error": lastErr,
	})
}

func (a *app) shutdown(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "POST required"})
		return
	}
	a.setState("stopping", "", "")
	writeJSON(w, http.StatusAccepted, map[string]any{"ok": true, "started": true})
	go a.cancel()
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func sleepContext(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

func publicError(err error) string {
	if err == nil {
		return ""
	}
	s := strings.TrimSpace(err.Error())
	if len([]rune(s)) > 240 {
		s = string([]rune(s)[:240])
	}
	return s
}
