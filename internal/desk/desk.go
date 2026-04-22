package desk

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"ocean/managedagent"
)

// Desk holds one Managed Agent session (cloud VM + agent) for the “desktop” app.
type Desk struct {
	client    *managedagent.Client
	sessionID string
	mu        sync.Mutex
}

func New(ctx context.Context) (*Desk, error) {
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("set ANTHROPIC_API_KEY")
	}

	client := &managedagent.Client{
		APIKey:     apiKey,
		HTTPClient: managedagent.DemoHTTPClient(),
	}

	agentID, err := ensureAgent(ctx, client)
	if err != nil {
		return nil, err
	}

	env, err := ensureEnvironment(ctx, client)
	if err != nil {
		return nil, err
	}

	sessionAgent := any(agentID)
	if v := strings.TrimSpace(os.Getenv("ANTHROPIC_AGENT_VERSION")); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return nil, fmt.Errorf("ANTHROPIC_AGENT_VERSION: %w", err)
		}
		sessionAgent = managedagent.AgentWithVersion(agentID, n)
	}

	session, err := client.CreateSession(ctx, managedagent.CreateSessionParams{
		Agent:         sessionAgent,
		EnvironmentID: env.ID,
		Title:         "Ocean desktop session",
	})
	if err != nil {
		return nil, err
	}

	return &Desk{client: client, sessionID: session.ID}, nil
}

func (d *Desk) SessionID() string { return d.sessionID }

func (d *Desk) Client() *managedagent.Client { return d.client }

// Chat sends a user message tagged with type and streams Anthropic session events to onEvent.
// One round at a time (mutex).
func (d *Desk) Chat(ctx context.Context, typ, userInput string, onEvent func(managedagent.StreamEvent) error) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	payload, err := json.Marshal(map[string]string{
		"type":  typ,
		"input": userInput,
	})
	if err != nil {
		return err
	}

	if err := d.client.SendEvents(ctx, d.sessionID, managedagent.SendEventsParams{
		Events: []managedagent.SessionEventInput{
			managedagent.NewUserMessageEvent(string(payload)),
		},
	}); err != nil {
		return fmt.Errorf("send: %w", err)
	}

	return d.client.StreamSessionEvents(ctx, d.sessionID, onEvent)
}

func ensureAgent(ctx context.Context, client *managedagent.Client) (string, error) {
	if id := strings.TrimSpace(os.Getenv("ANTHROPIC_AGENT_ID")); id != "" {
		return id, nil
	}
	agent, err := client.CreateAgent(ctx, managedagent.CreateAgentParams{
		Name:  "Ocean OS Agent",
		Model: "claude-sonnet-4-6",
		System: "You are the system intelligence behind a Linux-style desktop (terminal, browser, trash). " +
			"The user sends JSON with fields type and input. " +
			"Answer helpfully; when the type is terminal, assume shell-style questions; " +
			"for browser, URLs and page intent; for trash, file recovery or cleanup advice. " +
			"Be concise.",
		Tools: []any{
			managedagent.AgentToolset20260401{
				Type: "agent_toolset_20260401",
				DefaultConfig: &managedagent.AgentToolsetDefaultConfig{
					Enabled: ptr(true),
				},
			},
		},
	})
	if err != nil {
		return "", err
	}
	return agent.ID, nil
}

func ensureEnvironment(ctx context.Context, client *managedagent.Client) (*managedagent.Environment, error) {
	if id := strings.TrimSpace(os.Getenv("ANTHROPIC_ENVIRONMENT_ID")); id != "" {
		return &managedagent.Environment{ID: id}, nil
	}
	return client.CreateEnvironment(ctx, managedagent.CreateEnvironmentParams{
		Name: fmt.Sprintf("ocean-desk-%d", time.Now().Unix()),
		Config: map[string]any{
			"type": "cloud",
			"networking": map[string]any{
				"type": "unrestricted",
			},
		},
	})
}

func ptr[T any](v T) *T { return &v }
