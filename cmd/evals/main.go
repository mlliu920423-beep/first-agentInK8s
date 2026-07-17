// Command evals runs routing regression cases against the host multi-agent.
//
// Reads a YAML file with cases (see evals/routing.yaml), boots the same
// runtime that cmd/server does (Ark model + tools + MCP + specialists +
// host), then for each case runs one Host.Stream call and asserts:
//   - which specialist the host handed off to
//   - (optional) which tool was invoked inside that specialist
//
// Exit code 0 on all-pass; 1 on any failure. Prints a compact table so a
// CI job can grep it or a human can eyeball it.
//
// Env: same as the server (ARK_API_KEY, ARK_MODEL_ID). Costs one LLM
// invocation per case; keep the case list small.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"sync"
	"time"

	"github.com/bigmay/first-agentink8s/internal/agentcfg"
	"github.com/bigmay/first-agentink8s/internal/agents"
	"github.com/bigmay/first-agentink8s/internal/llm"
	mcpbridge "github.com/bigmay/first-agentink8s/internal/mcp"
	// Register transport drivers so mcpbridge.LoadAll can dispatch.
	_ "github.com/bigmay/first-agentink8s/internal/mcp/inproc"
	_ "github.com/bigmay/first-agentink8s/internal/mcp/stdio"
	"github.com/bigmay/first-agentink8s/internal/tools"

	"github.com/cloudwego/eino/callbacks"
	toolcomp "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/flow/agent/multiagent/host"
	"github.com/cloudwego/eino/schema"
	utilcb "github.com/cloudwego/eino/utils/callbacks"
	"gopkg.in/yaml.v3"
)

type Case struct {
	Name        string `yaml:"name"`
	Input       string `yaml:"input"`
	ExpectAgent string `yaml:"expect_agent"`
	ExpectTool  string `yaml:"expect_tool"`
}

type File struct {
	Cases []Case `yaml:"cases"`
}

// Trace holds per-case observations collected from callbacks.
type Trace struct {
	mu          sync.Mutex
	AgentSwitch []string
	ToolCalls   []string
	Tokens      int
	StreamErr   error
}

func (t *Trace) addSwitch(name string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.AgentSwitch = append(t.AgentSwitch, name)
}

func (t *Trace) addToolCall(name string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.ToolCalls = append(t.ToolCalls, name)
}

// ctxKey isolates the per-case trace pointer.
type ctxKey struct{}

func withTrace(ctx context.Context, t *Trace) context.Context {
	return context.WithValue(ctx, ctxKey{}, t)
}

func traceFrom(ctx context.Context) *Trace {
	t, _ := ctx.Value(ctxKey{}).(*Trace)
	return t
}

// hostCB captures host handoff events.
type hostCB struct{}

func (hostCB) OnHandOff(ctx context.Context, info *host.HandOffInfo) context.Context {
	if t := traceFrom(ctx); t != nil {
		t.addSwitch(info.ToAgentName)
	}
	return ctx
}

// installToolCB registers a global tool callback that pipes tool names to
// the per-case Trace. Mirrors httpapi.InstallToolCallbacks — kept separate
// so evals stay decoupled from the HTTP layer.
func installToolCB() {
	h := utilcb.NewHandlerHelper().Tool(&utilcb.ToolCallbackHandler{
		OnStart: func(ctx context.Context, info *callbacks.RunInfo, _ *toolcomp.CallbackInput) context.Context {
			if t := traceFrom(ctx); t != nil && info != nil {
				t.addToolCall(info.Name)
			}
			return ctx
		},
	}).Handler()
	callbacks.AppendGlobalHandlers(h)
}

func main() {
	log.SetFlags(0)

	file := flag.String("file", "evals/routing.yaml", "path to YAML case file")
	timeout := flag.Duration("timeout", 60*time.Second, "per-case timeout")
	agentsDir := flag.String("agents-dir", "agents", "sub-agent yaml directory")
	mcpDir := flag.String("mcp-dir", "mcp", "MCP server yaml directory")
	flag.Parse()

	ctx := context.Background()

	// Boot the same runtime as cmd/server.
	arkModel, err := llm.NewArkModel(ctx)
	if err != nil {
		log.Fatalf("llm: %v", err)
	}
	reg := tools.NewRegistry()
	if err := tools.RegisterBuiltins(ctx, reg); err != nil {
		log.Fatalf("register builtins: %v", err)
	}
	// MCP: declarative loader; fail-fast on any startup error, same as
	// cmd/server. See docs/adr/005-mcp-driver-abstraction.md.
	if _, err := mcpbridge.LoadAll(ctx, *mcpDir, reg); err != nil {
		log.Fatalf("mcp: %v", err)
	}

	cfgs, err := agentcfg.Load(*agentsDir)
	if err != nil {
		log.Fatalf("agentcfg: %v", err)
	}
	specialists := make([]*host.Specialist, 0, len(cfgs))
	for _, cfg := range cfgs {
		sp, err := agents.BuildSpecialist(ctx, arkModel, cfg, reg)
		if err != nil {
			log.Fatalf("build specialist %q: %v", cfg.Name, err)
		}
		specialists = append(specialists, sp)
	}
	hostMA, err := agents.BuildHost(ctx, arkModel, agents.DefaultHostPrompt, specialists)
	if err != nil {
		log.Fatalf("build host multi-agent: %v", err)
	}
	installToolCB()

	// Load cases.
	f, err := loadFile(*file)
	if err != nil {
		log.Fatalf("load cases: %v", err)
	}
	if len(f.Cases) == 0 {
		log.Fatalf("no cases in %s", *file)
	}

	// Run.
	log.Printf("running %d cases from %s\n", len(f.Cases), *file)
	log.Printf("%-32s  %-15s  %-15s  %-15s  %s", "case", "want-agent", "got-agent", "want-tool", "verdict")
	log.Printf("%-32s  %-15s  %-15s  %-15s  %s", "----", "----------", "---------", "---------", "-------")

	failCount := 0
	for _, c := range f.Cases {
		tr, err := runCase(ctx, hostMA, c, *timeout)
		verdict := judge(c, tr, err)
		gotAgent := "-"
		if len(tr.AgentSwitch) > 0 {
			gotAgent = tr.AgentSwitch[0]
		}
		wantTool := c.ExpectTool
		if wantTool == "" {
			wantTool = "-"
		}
		log.Printf("%-32s  %-15s  %-15s  %-15s  %s", c.Name, c.ExpectAgent, gotAgent, wantTool, verdict)
		if verdict != "PASS" {
			failCount++
		}
	}

	log.Printf("")
	log.Printf("total: %d, pass: %d, fail: %d", len(f.Cases), len(f.Cases)-failCount, failCount)
	if failCount > 0 {
		os.Exit(1)
	}
}

func loadFile(path string) (*File, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var f File
	if err := yaml.Unmarshal(b, &f); err != nil {
		return nil, err
	}
	return &f, nil
}

func runCase(ctx context.Context, hostMA *host.MultiAgent, c Case, timeout time.Duration) (*Trace, error) {
	tr := &Trace{}
	rctx, cancel := context.WithTimeout(withTrace(ctx, tr), timeout)
	defer cancel()

	in := []*schema.Message{schema.UserMessage(c.Input)}
	sr, err := hostMA.Stream(rctx, in, host.WithAgentCallbacks(hostCB{}))
	if err != nil {
		tr.StreamErr = err
		return tr, err
	}
	defer sr.Close()

	for {
		chunk, err := sr.Recv()
		if errors.Is(err, io.EOF) {
			return tr, nil
		}
		if err != nil {
			tr.StreamErr = err
			return tr, err
		}
		if chunk != nil && chunk.Content != "" {
			tr.Tokens++
		}
	}
}

func judge(c Case, tr *Trace, runErr error) string {
	if runErr != nil {
		return fmt.Sprintf("ERROR: %v", runErr)
	}
	// Agent check
	agentOK := false
	for _, a := range tr.AgentSwitch {
		if a == c.ExpectAgent {
			agentOK = true
			break
		}
	}
	if !agentOK {
		if len(tr.AgentSwitch) == 0 {
			return fmt.Sprintf("FAIL: no handoff, want %s", c.ExpectAgent)
		}
		return fmt.Sprintf("FAIL: routed to %v, want %s", tr.AgentSwitch, c.ExpectAgent)
	}
	// Tool check (optional)
	if c.ExpectTool != "" {
		toolOK := false
		for _, t := range tr.ToolCalls {
			if t == c.ExpectTool {
				toolOK = true
				break
			}
		}
		if !toolOK {
			return fmt.Sprintf("FAIL: tool %s not called (called: %v)", c.ExpectTool, tr.ToolCalls)
		}
	}
	return "PASS"
}
