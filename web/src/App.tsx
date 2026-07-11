import { useCallback, useRef, useState } from 'react'
import { streamChat, type ChatEvent } from './sseClient'

type Chip =
  | { kind: 'switch'; to: string; argument: string }
  | { kind: 'tool'; name: string; args: string }
  | { kind: 'result'; name: string; result: string }
  | { kind: 'error'; message: string }

type Turn = {
  role: 'user' | 'agent'
  text: string
  chips: Chip[]
}

export default function App() {
  const [turns, setTurns] = useState<Turn[]>([])
  const [input, setInput] = useState('')
  const [busy, setBusy] = useState(false)
  const abortRef = useRef<AbortController | null>(null)

  const send = useCallback(async () => {
    const msg = input.trim()
    if (!msg || busy) return
    setInput('')
    setBusy(true)
    setTurns((t) => [
      ...t,
      { role: 'user', text: msg, chips: [] },
      { role: 'agent', text: '', chips: [] },
    ])

    const ac = new AbortController()
    abortRef.current = ac
    try {
      for await (const ev of streamChat(msg, ac.signal)) {
        setTurns((t) => {
          const copy = t.slice()
          const last = copy[copy.length - 1]
          const next = { ...last, chips: last.chips.slice() }
          applyEvent(next, ev)
          copy[copy.length - 1] = next
          return copy
        })
        if (ev.type === 'done' || ev.type === 'error') break
      }
    } catch (e) {
      setTurns((t) => {
        const copy = t.slice()
        const last = copy[copy.length - 1]
        copy[copy.length - 1] = {
          ...last,
          chips: [...last.chips, { kind: 'error', message: String(e) }],
        }
        return copy
      })
    } finally {
      setBusy(false)
      abortRef.current = null
    }
  }, [input, busy])

  return (
    <div className="app">
      <h1>Eino 多 Agent Demo</h1>
      <p className="hint">
        Host router → math_agent / research_agent / ops_agent → 内置 Skill + 进程内 MCP。
        试试："12 乘以 7 等于多少"、"UTC 现在几点"、"list files in the current directory"。
      </p>
      <div className="transcript">
        {turns.map((t, i) => (
          <div key={i} className={`turn turn-${t.role}`}>
            {t.chips.length > 0 && (
              <div className="meta">
                {t.chips.map((c, j) => (
                  <ChipView key={j} c={c} />
                ))}
              </div>
            )}
            <div className="bubble">{t.text || (t.role === 'agent' && busy && i === turns.length - 1 ? '…' : '')}</div>
          </div>
        ))}
      </div>
      <form
        onSubmit={(e) => {
          e.preventDefault()
          send()
        }}
      >
        <div className="row">
          <input
            type="text"
            value={input}
            placeholder="Ask anything…"
            onChange={(e) => setInput(e.target.value)}
            disabled={busy}
            autoFocus
          />
          <button type="submit" disabled={busy || !input.trim()}>
            {busy ? '…' : 'Send'}
          </button>
        </div>
      </form>
    </div>
  )
}

function applyEvent(turn: Turn, ev: ChatEvent) {
  switch (ev.type) {
    case 'token':
      turn.text += ev.data.delta
      break
    case 'agent_switch':
      turn.chips.push({ kind: 'switch', to: ev.data.to, argument: ev.data.argument })
      break
    case 'tool_call':
      turn.chips.push({ kind: 'tool', name: ev.data.name, args: ev.data.args })
      break
    case 'tool_result':
      turn.chips.push({ kind: 'result', name: ev.data.name, result: ev.data.result })
      break
    case 'error':
      turn.chips.push({ kind: 'error', message: ev.data.message })
      break
    case 'done':
      break
  }
}

function ChipView({ c }: { c: Chip }) {
  switch (c.kind) {
    case 'switch':
      return (
        <span className="chip switch">→ {c.to}{c.argument ? `: ${trunc(c.argument, 60)}` : ''}</span>
      )
    case 'tool':
      return <span className="chip tool">🔧 {c.name}({trunc(c.args, 60)})</span>
    case 'result':
      return <span className="chip result">✓ {c.name}: {trunc(c.result, 60)}</span>
    case 'error':
      return <span className="chip error">⚠ {c.message}</span>
  }
}

function trunc(s: string, n: number) {
  return s.length > n ? s.slice(0, n) + '…' : s
}
