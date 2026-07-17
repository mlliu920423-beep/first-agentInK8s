package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"

	"github.com/cloudwego/eino/callbacks"
	toolcomp "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/flow/agent/multiagent/host"
	"github.com/cloudwego/eino/schema"
	utilcb "github.com/cloudwego/eino/utils/callbacks"

	"github.com/bigmay/first-agentink8s/internal/agents"
)

// Sink serializes events from three concurrent sources — the model token
// stream, host handoff callbacks, and tool callbacks — onto one ordered
// channel consumed by the SSE writer goroutine.
type sink struct {
	mu     sync.Mutex
	ch     chan Event
	closed bool
}

func newSink(buf int) *sink { return &sink{ch: make(chan Event, buf)} }

func (s *sink) push(ev Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	select {
	case s.ch <- ev:
	default:
		// Extremely unlikely for buffer=64; drop rather than block the model loop.
		log.Printf("sse: dropped event (buffer full): %s", ev.Type)
	}
}

func (s *sink) close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.closed = true
	close(s.ch)
}

// ctxKey isolates the per-request sink pointer we ferry to callbacks.
type ctxKey struct{}

func withSink(ctx context.Context, s *sink) context.Context {
	return context.WithValue(ctx, ctxKey{}, s)
}
func sinkFrom(ctx context.Context) *sink {
	s, _ := ctx.Value(ctxKey{}).(*sink)
	return s
}

// hostCallback implements host.MultiAgentCallback: pushes an agent_switch
// event to the current request's sink on every hand-off.
type hostCallback struct{}

func (hostCallback) OnHandOff(ctx context.Context, info *host.HandOffInfo) context.Context {
	if s := sinkFrom(ctx); s != nil {
		s.push(Event{Type: EventAgentSwitch, Data: AgentSwitchData{
			To:       info.ToAgentName,
			Argument: info.Argument,
		}})
	}
	return ctx
}

// InstallToolCallbacks registers a global handler that emits tool_call /
// tool_result events. Global is fine here — the handler is a no-op when
// the request context has no sink attached, so out-of-request tool calls
// (there aren't any in this demo, but the pattern is defensive) are silent.
func InstallToolCallbacks() {
	handler := utilcb.NewHandlerHelper().Tool(&utilcb.ToolCallbackHandler{
		OnStart: func(ctx context.Context, info *callbacks.RunInfo, in *toolcomp.CallbackInput) context.Context {
			if s := sinkFrom(ctx); s != nil && info != nil {
				s.push(Event{Type: EventToolCall, Data: ToolCallData{
					Name: info.Name,
					Args: in.ArgumentsInJSON,
				}})
			}
			return ctx
		},
		OnEnd: func(ctx context.Context, info *callbacks.RunInfo, out *toolcomp.CallbackOutput) context.Context {
			if s := sinkFrom(ctx); s != nil && info != nil {
				resp := out.Response
				if resp == "" && out.ToolOutput != nil && len(out.ToolOutput.Parts) > 0 {
					resp = out.ToolOutput.Parts[0].Text
				}
				s.push(Event{Type: EventToolResult, Data: ToolResultData{
					Name: info.Name, Result: resp,
				}})
			}
			return ctx
		},
	}).Handler()
	callbacks.AppendGlobalHandlers(handler)
}

// Server is the wired-up HTTP endpoint.
//
// Sup owns the current *host.MultiAgent behind an atomic pointer. Every
// HandleChat call reads the pointer once (at the top) and uses that
// snapshot for the whole request, so a concurrent Supervisor.Rebuild
// doesn't split the request across two host instances. See
// docs/adr/006-registry-mutation-host-swap.md for the atomic swap
// design and docs/specs/phase-2-registry-mutation-host-swap.md §Rebuild
// for the transactional semantics.
type Server struct {
	Sup *agents.Supervisor
}

func (s *Server) HandleChat(w http.ResponseWriter, r *http.Request) {
	// Snapshot the current host once — never re-read Sup during this
	// request. Rebuild may swap Sup.current while we're mid-stream;
	// that's fine, the in-flight request keeps its old *MultiAgent
	// reference until it finishes (see ADR-006 §Rebuild sequence).
	hostMA := s.Sup.Current()

	msg := extractQuery(r)
	if msg == "" {
		http.Error(w, "missing query: use ?q=... or POST {\"message\":\"...\"}", http.StatusBadRequest)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no") // hint to any reverse proxy

	sk := newSink(64)
	ctx := withSink(r.Context(), sk)

	// Writer goroutine: drain sk.ch → SSE frames until closed.
	writerDone := make(chan struct{})
	go func() {
		defer close(writerDone)
		enc := json.NewEncoder(w)
		enc.SetEscapeHTML(false)
		for ev := range sk.ch {
			_, _ = w.Write([]byte("data: "))
			if err := enc.Encode(ev); err != nil {
				return
			}
			// enc.Encode already writes trailing \n; SSE frames need \n\n.
			_, _ = w.Write([]byte("\n"))
			flusher.Flush()
		}
	}()

	// Producer: run the host multi-agent, pump tokens into the sink.
	err := s.runStream(ctx, sk, hostMA, msg)
	if err != nil && !errors.Is(err, context.Canceled) {
		sk.push(Event{Type: EventError, Data: ErrorData{Message: err.Error()}})
	}
	sk.push(Event{Type: EventDone, Data: DoneData{Reason: "complete"}})
	sk.close()
	<-writerDone
}

func (s *Server) runStream(ctx context.Context, sk *sink, hostMA *host.MultiAgent, userMsg string) error {
	in := []*schema.Message{schema.UserMessage(userMsg)}

	sr, err := hostMA.Stream(ctx, in, host.WithAgentCallbacks(hostCallback{}))
	if err != nil {
		return fmt.Errorf("stream: %w", err)
	}
	defer sr.Close()

	for {
		chunk, err := sr.Recv()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if chunk == nil {
			continue
		}
		if chunk.Content != "" {
			sk.push(Event{Type: EventToken, Data: TokenData{Delta: chunk.Content}})
		}
	}
}

func extractQuery(r *http.Request) string {
	if v := r.URL.Query().Get("q"); v != "" {
		return v
	}
	if r.Method != http.MethodPost {
		return ""
	}
	ct := r.Header.Get("Content-Type")
	if strings.HasPrefix(ct, "application/json") {
		var body struct {
			Message string `json:"message"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err == nil {
			return body.Message
		}
	}
	return ""
}
