// Package httpapi exposes the multi-agent runtime over HTTP + SSE.
package httpapi

// Event is the envelope for every Server-Sent Event line sent to the browser.
// One JSON object per `data:` frame; the React client discriminates on Type.
type Event struct {
	Type string `json:"type"`
	Data any    `json:"data,omitempty"`
}

// Event type constants.
const (
	EventToken       = "token"        // assistant text delta
	EventAgentSwitch = "agent_switch" // host handed off to a specialist
	EventToolCall    = "tool_call"    // a tool was invoked (with args)
	EventToolResult  = "tool_result"  // that tool returned
	EventDone        = "done"         // stream complete
	EventError       = "error"        // fatal error mid-stream
)

type TokenData struct {
	Delta string `json:"delta"`
}

type AgentSwitchData struct {
	To       string `json:"to"`
	Argument string `json:"argument"`
}

type ToolCallData struct {
	Name string `json:"name"`
	Args string `json:"args"`
}

type ToolResultData struct {
	Name   string `json:"name"`
	Result string `json:"result"`
}

type DoneData struct {
	Reason string `json:"reason"`
}

type ErrorData struct {
	Message string `json:"message"`
}
