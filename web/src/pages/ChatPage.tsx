import { useCallback, useRef, useState } from 'react'
import { streamChat, type ChatEvent } from '@/sseClient'
import ChipView, { type Chip } from '@/components/ChipView'

type Turn = {
  role: 'user' | 'agent'
  text: string
  chips: Chip[]
}

function trunc(s: string, n: number) {
  return s.length > n ? s.slice(0, n) + '…' : s
}

function applyEvent(turn: Turn, ev: ChatEvent) {
  switch (ev.type) {
    case 'token':
      turn.text += ev.data.delta
      break
    case 'agent_switch':
      turn.chips.push({ kind: 'switch', to: ev.data.to, argument: trunc(ev.data.argument, 60) })
      break
    case 'tool_call':
      turn.chips.push({ kind: 'tool', name: ev.data.name, args: trunc(ev.data.args, 60) })
      break
    case 'tool_result':
      turn.chips.push({ kind: 'result', name: ev.data.name, result: trunc(ev.data.result, 60) })
      break
    case 'error':
      turn.chips.push({ kind: 'error', message: ev.data.message })
      break
    case 'done':
      break
  }
}

export default function ChatPage() {
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
    <div className="flex flex-col h-full">
      {/* Header */}
      <div className="p-4 border-b border-border">
        <h1 className="text-lg font-bold">Eino 多 Agent Demo</h1>
        <p className="text-sm text-muted-foreground mt-1">
          Host router → math_agent / research_agent / ops_agent → 内置 Skill + 进程内 MCP
        </p>
      </div>

      {/* Transcript area */}
      <div className="flex-1 overflow-y-auto p-4 space-y-4">
        {turns.map((t, i) => (
          <div key={i} className={`flex ${t.role === 'user' ? 'justify-end' : 'justify-start'}`}>
            <div className={`max-w-[80%] space-y-1`}>
              {t.chips.length > 0 && (
                <div className="flex flex-wrap gap-1">
                  {t.chips.map((c, j) => (
                    <ChipView key={j} c={c} />
                  ))}
                </div>
              )}
              <div
                className={`rounded-xl px-4 py-2 text-sm ${
                  t.role === 'user'
                    ? 'bg-blue-600 text-white'
                    : 'bg-card border border-border text-card-foreground'
                }`}
              >
                {t.text || (t.role === 'agent' && busy && i === turns.length - 1 ? '…' : '')}
              </div>
            </div>
          </div>
        ))}
      </div>

      {/* Input form */}
      <form
        onSubmit={(e) => {
          e.preventDefault()
          send()
        }}
        className="border-t border-border p-4 bg-background/80 backdrop-blur"
      >
        <div className="flex gap-2">
          <input
            type="text"
            value={input}
            placeholder="Ask anything…"
            onChange={(e) => setInput(e.target.value)}
            disabled={busy}
            autoFocus
            className="flex-1 rounded-lg border border-input bg-background px-4 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-ring disabled:opacity-50"
          />
          <button
            type="submit"
            disabled={busy || !input.trim()}
            className="rounded-lg bg-primary text-primary-foreground px-4 py-2 text-sm font-medium transition-colors hover:bg-primary/90 disabled:opacity-50 disabled:cursor-not-allowed"
          >
            {busy ? '…' : 'Send'}
          </button>
        </div>
      </form>
    </div>
  )
}
