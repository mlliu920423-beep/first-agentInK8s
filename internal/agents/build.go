// Package agents composes the multi-agent runtime.
//
// The design follows the "handoff as tool call" pattern popularised by
// OpenAI Swarm and Coze: a Host chat-model agent is presented with each
// specialist as a routable tool (via the specialist's IntendedUse). When
// the host decides to hand off, Eino runs the specialist's Invokable /
// Streamable, then returns to the host loop.
//
// Each specialist here is itself a ReAct agent with its own tool subset —
// isolated context, own system prompt.
package agents

import (
	"context"
	"fmt"

	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/compose"
	einoflowagent "github.com/cloudwego/eino/flow/agent"
	"github.com/cloudwego/eino/flow/agent/multiagent/host"
	"github.com/cloudwego/eino/flow/agent/react"
	"github.com/cloudwego/eino/schema"

	"github.com/bigmay/first-agentink8s/internal/agentcfg"
	"github.com/bigmay/first-agentink8s/internal/tools"
)

// BuildSpecialist wraps a ReAct agent as a host.Specialist.
//
// The `description` in the YAML becomes AgentMeta.IntendedUse — the string
// the host LLM sees when deciding whether to route here. The `system_prompt`
// is injected via a MessageModifier that prepends a system message to
// every invocation of the underlying ReAct loop.
func BuildSpecialist(
	ctx context.Context,
	cm einomodel.ToolCallingChatModel,
	cfg agentcfg.AgentConfig,
	reg *tools.Registry,
) (*host.Specialist, error) {
	resolved, err := reg.MustResolve(cfg.Tools)
	if err != nil {
		return nil, fmt.Errorf("resolve tools for agent %q: %w", cfg.Name, err)
	}
	sysMsg := schema.SystemMessage(cfg.SystemPrompt)
	modifier := func(_ context.Context, in []*schema.Message) []*schema.Message {
		// Prepend the system prompt unless the caller already provided one.
		if len(in) > 0 && in[0].Role == schema.System {
			return in
		}
		return append([]*schema.Message{sysMsg}, in...)
	}

	ra, err := react.NewAgent(ctx, &react.AgentConfig{
		ToolCallingModel: cm,
		ToolsConfig:      compose.ToolsNodeConfig{Tools: resolved},
		MessageModifier:  modifier,
		MaxStep:          cfg.MaxStep,
	})
	if err != nil {
		return nil, fmt.Errorf("build react agent %q: %w", cfg.Name, err)
	}

	sp := &host.Specialist{
		AgentMeta: host.AgentMeta{
			Name:        cfg.Name,
			IntendedUse: cfg.Description,
		},
		Invokable: func(ctx context.Context, in []*schema.Message, opts ...einoflowagent.AgentOption) (*schema.Message, error) {
			return ra.Generate(ctx, in, opts...)
		},
		Streamable: func(ctx context.Context, in []*schema.Message, opts ...einoflowagent.AgentOption) (*schema.StreamReader[*schema.Message], error) {
			return ra.Stream(ctx, in, opts...)
		},
	}
	return sp, nil
}

// BuildHost assembles the host multi-agent from a shared ChatModel and the
// prebuilt Specialists.
func BuildHost(
	ctx context.Context,
	cm einomodel.ToolCallingChatModel,
	systemPrompt string,
	specialists []*host.Specialist,
) (*host.MultiAgent, error) {
	if len(specialists) == 0 {
		return nil, fmt.Errorf("no specialists provided")
	}
	return host.NewMultiAgent(ctx, &host.MultiAgentConfig{
		Host: host.Host{
			ToolCallingModel: cm,
			SystemPrompt:     systemPrompt,
		},
		Specialists: specialists,
		Name:        "router",
	})
}

// DefaultHostPrompt is the routing brain. Kept in code (not YAML) so the
// contract "one supervisor picks a specialist" is explicit.
const DefaultHostPrompt = `You are the routing agent for a multi-agent system.
You have several specialist sub-agents available as tools. For every user
message, pick the single most appropriate specialist and hand off. Do NOT
try to answer domain questions yourself — always route.

If the user's request is pure chit-chat with no clear specialist match,
you may answer briefly yourself. Otherwise, hand off.`
