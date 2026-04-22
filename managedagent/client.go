// Package managedagent is thin HTTP glue for Anthropic Managed Agents (beta).
//
// REST reference: https://api.anthropic.com with paths under /v1/...
// Required headers: x-api-key, anthropic-version, anthropic-beta.
package managedagent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	BetaManagedAgents = "managed-agents-2026-04-01"
	AnthropicVersion  = "2023-06-01"
	DefaultBaseURL    = "https://api.anthropic.com"
)

// ErrStopStream is returned from StreamSessionEvents callbacks to end reading early
// (for example after session.status_idle).
var ErrStopStream = errors.New("managedagent: stop stream")

// Client calls the Anthropic HTTP API. It is safe for concurrent use if HTTPClient is.
type Client struct {
	APIKey     string
	BaseURL    string
	HTTPClient *http.Client
	// APIPrefix is the URL segment after the host, e.g. "/v1" (default). Official Managed Agents
	// docs use "/v1/agents", "/v1/sessions", etc. Set to "/v4" only if your deployment documents that path.
	APIPrefix string
}

func (c *Client) baseURL() string {
	if c.BaseURL != "" {
		return strings.TrimRight(c.BaseURL, "/")
	}
	return DefaultBaseURL
}

// apiPath joins APIPrefix (default "/v1") with a relative path like "agents" or "sessions/sid/events".
func (c *Client) apiPath(rel string) string {
	prefix := strings.TrimSpace(c.APIPrefix)
	if prefix == "" {
		prefix = "/v1"
	}
	if !strings.HasPrefix(prefix, "/") {
		prefix = "/" + prefix
	}
	prefix = strings.TrimRight(prefix, "/")
	rel = strings.Trim(rel, "/")
	if rel == "" {
		return prefix
	}
	return prefix + "/" + rel
}

func (c *Client) httpClient() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return &http.Client{Timeout: 0} // streaming uses no global timeout
}

func (c *Client) setCommonHeaders(h http.Header) {
	h.Set("x-api-key", c.APIKey)
	h.Set("anthropic-version", AnthropicVersion)
	h.Set("anthropic-beta", BetaManagedAgents)
}

func (c *Client) postJSON(ctx context.Context, rel string, body any, out any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}
	url := c.baseURL() + c.apiPath(rel)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	c.setCommonHeaders(req.Header)

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("anthropic POST %s %s: %s", c.apiPath(rel), resp.Status, bytes.TrimSpace(b))
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(b, out); err != nil {
		return fmt.Errorf("decode POST %s: %w body=%s", c.apiPath(rel), err, string(b))
	}
	return nil
}

// --- Requests / responses (minimal fields used by this glue) ---

type AgentToolset20260401 struct {
	Type          string                    `json:"type"` // "agent_toolset_20260401"
	DefaultConfig *AgentToolsetDefaultConfig `json:"default_config,omitempty"`
}

type AgentToolsetDefaultConfig struct {
	Enabled *bool `json:"enabled,omitempty"`
}

type CreateAgentParams struct {
	Name    string `json:"name"`
	Model   string `json:"model"`
	System  string `json:"system,omitempty"`
	Tools   []any  `json:"tools,omitempty"`
	Skills  []any  `json:"skills,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

type Agent struct {
	ID      string `json:"id"`
	Version int    `json:"version"`
}

type CreateEnvironmentParams struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Config      map[string]any `json:"config"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

type Environment struct {
	ID string `json:"id"`
}

// AgentRef pins a saved agent to a specific configuration version (see API "agent" shorthand).
type AgentRef struct {
	Type    string `json:"type"` // always "agent"
	ID      string `json:"id"`
	Version int    `json:"version"`
}

func AgentWithVersion(id string, version int) AgentRef {
	return AgentRef{Type: "agent", ID: id, Version: version}
}

type CreateSessionParams struct {
	// Agent is either the agent id string (latest version) or AgentRef / map for a pinned version.
	Agent          any               `json:"agent"`
	EnvironmentID  string            `json:"environment_id"`
	Title          string            `json:"title,omitempty"`
	Resources      []any             `json:"resources,omitempty"`
	VaultIDs       []string          `json:"vault_ids,omitempty"`
	Metadata       map[string]string `json:"metadata,omitempty"`
}

type Session struct {
	ID string `json:"id"`
}

type SendEventsParams struct {
	Events []SessionEventInput `json:"events"`
}

type SessionEventInput struct {
	Type    string          `json:"type"`
	Content json.RawMessage `json:"content,omitempty"`
}

type TextBlock struct {
	Type string `json:"type"` // "text"
	Text string `json:"text"`
}

// CreateAgent POST {APIPrefix}/agents
func (c *Client) CreateAgent(ctx context.Context, p CreateAgentParams) (*Agent, error) {
	var out Agent
	if err := c.postJSON(ctx, "agents", p, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CreateEnvironment POST {APIPrefix}/environments
func (c *Client) CreateEnvironment(ctx context.Context, p CreateEnvironmentParams) (*Environment, error) {
	var out Environment
	if err := c.postJSON(ctx, "environments", p, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CreateSession POST {APIPrefix}/sessions
func (c *Client) CreateSession(ctx context.Context, p CreateSessionParams) (*Session, error) {
	var out Session
	if err := c.postJSON(ctx, "sessions", p, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// SendEvents POST {APIPrefix}/sessions/{id}/events
func (c *Client) SendEvents(ctx context.Context, sessionID string, p SendEventsParams) error {
	rel := fmt.Sprintf("sessions/%s/events", sessionID)
	return c.postJSON(ctx, rel, p, nil)
}

// StreamEvent is one JSON object from the SSE stream (shape varies by Type).
type StreamEvent map[string]json.RawMessage

func (e StreamEvent) Type() (string, error) {
	raw, ok := e["type"]
	if !ok {
		return "", fmt.Errorf("event missing type")
	}
	var t string
	if err := json.Unmarshal(raw, &t); err != nil {
		return "", err
	}
	return t, nil
}

// StreamSessionEvents GET {APIPrefix}/sessions/{id}/events/stream (SSE).
// Calls onEvent for each JSON payload in a "data: " line until ctx done, EOF, or onEvent returns error.
func (c *Client) StreamSessionEvents(ctx context.Context, sessionID string, onEvent func(StreamEvent) error) error {
	rel := fmt.Sprintf("sessions/%s/events/stream", sessionID)
	url := c.baseURL() + c.apiPath(rel)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	c.setCommonHeaders(req.Header)
	req.Header.Set("Accept", "text/event-stream")

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("anthropic GET %s: %s %s", c.apiPath(rel), resp.Status, bytes.TrimSpace(b))
	}

	sc := bufio.NewScanner(resp.Body)
	// SSE payloads can be large (tool outputs, messages).
	const maxToken = 1024 * 1024
	buf := make([]byte, 64*1024)
	sc.Buffer(buf, maxToken)

	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" || data == "[DONE]" {
			continue
		}
		var ev StreamEvent
		if err := json.Unmarshal([]byte(data), &ev); err != nil {
			return fmt.Errorf("decode sse data: %w data=%s", err, data)
		}
		if err := onEvent(ev); err != nil {
			if errors.Is(err, ErrStopStream) {
				return nil
			}
			return err
		}
	}
	if err := sc.Err(); err != nil {
		return err
	}
	return nil
}

// NewUserMessageEvent builds a user.message with one text block.
func NewUserMessageEvent(text string) SessionEventInput {
	return SessionEventInput{
		Type: "user.message",
		Content: mustJSON([]TextBlock{{
			Type: "text",
			Text: text,
		}}),
	}
}

func mustJSON(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return json.RawMessage(b)
}

// DemoHTTPClient returns an HTTP client suited for long-lived SSE reads.
func DemoHTTPClient() *http.Client {
	return &http.Client{
		Timeout: 0,
		Transport: &http.Transport{
			ResponseHeaderTimeout: 2 * time.Minute,
		},
	}
}
