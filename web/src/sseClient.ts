// SSE client. We use fetch + a manual reader instead of EventSource because
// EventSource only supports GET; keeping POST as an option makes it trivial
// to grow the API later (session ids, tool overrides, etc.). For the demo,
// GET with ?q= works too — and it's what the code below actually uses so
// curl `-N` still works.

export type ChatEvent =
  | { type: 'token'; data: { delta: string } }
  | { type: 'agent_switch'; data: { to: string; argument: string } }
  | { type: 'tool_call'; data: { name: string; args: string } }
  | { type: 'tool_result'; data: { name: string; result: string } }
  | { type: 'done'; data: { reason: string } }
  | { type: 'error'; data: { message: string } };

export async function* streamChat(message: string, signal: AbortSignal): AsyncGenerator<ChatEvent> {
  const url = `/api/chat?q=${encodeURIComponent(message)}`;
  const resp = await fetch(url, { signal });
  if (!resp.ok || !resp.body) {
    throw new Error(`chat request failed: ${resp.status}`);
  }
  const reader = resp.body.getReader();
  const decoder = new TextDecoder();
  let buf = '';

  while (true) {
    const { value, done } = await reader.read();
    if (done) break;
    buf += decoder.decode(value, { stream: true });
    // SSE frames are separated by a blank line (\n\n).
    let idx;
    while ((idx = buf.indexOf('\n\n')) !== -1) {
      const frame = buf.slice(0, idx);
      buf = buf.slice(idx + 2);
      const line = frame.split('\n').find((l) => l.startsWith('data: '));
      if (!line) continue;
      const jsonStr = line.slice('data: '.length);
      try {
        yield JSON.parse(jsonStr) as ChatEvent;
      } catch (e) {
        console.warn('bad SSE frame', jsonStr, e);
      }
    }
  }
}
