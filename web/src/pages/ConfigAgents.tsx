import { useEffect, useState } from 'react'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Badge } from '@/components/ui/badge'
import { listAgents, createAgent, updateAgent, deleteAgent } from '@/lib/api'
import type { AgentConfig } from '@/lib/types'

export default function ConfigAgents() {
  const [agents, setAgents] = useState<AgentConfig[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  // Dialog state
  const [open, setOpen] = useState(false)
  const [editName, setEditName] = useState<string | null>(null)
  const [form, setForm] = useState({ name: '', description: '', system_prompt: '', tools: '', max_step: 12 })

  const load = () => {
    setLoading(true)
    listAgents()
      .then(setAgents)
      .catch((e) => setError(e.message))
      .finally(() => setLoading(false))
  }

  useEffect(() => { load() }, [])

  const openNew = () => {
    setEditName(null)
    setForm({ name: '', description: '', system_prompt: '', tools: '', max_step: 12 })
    setOpen(true)
  }

  const openEdit = (a: AgentConfig) => {
    setEditName(a.name)
    setForm({
      name: a.name,
      description: a.description,
      system_prompt: a.system_prompt,
      tools: (a.tools || []).join(', '),
      max_step: a.max_step,
    })
    setOpen(true)
  }

  const save = async () => {
    const cfg: AgentConfig = {
      name: form.name,
      description: form.description,
      system_prompt: form.system_prompt,
      tools: form.tools.split(',').map((s) => s.trim()).filter(Boolean),
      max_step: form.max_step,
    }
    try {
      if (editName) {
        await updateAgent(editName, cfg)
      } else {
        await createAgent(cfg)
      }
      setOpen(false)
      load()
    } catch (e) {
      alert(String(e))
    }
  }

  const doDelete = async (name: string) => {
    if (!window.confirm(`确定删除 agent "${name}"？`)) return
    try {
      await deleteAgent(name)
      load()
    } catch (e) {
      alert(String(e))
    }
  }

  if (loading) return <div className="p-8 text-muted-foreground">Loading agents…</div>
  if (error) return <div className="p-8 text-destructive">Error: {error}</div>

  return (
    <div className="p-6 space-y-6">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold">Agents</h1>
          <p className="text-sm text-muted-foreground">Configure sub-agent specialists</p>
        </div>
        <Button onClick={openNew}>+ New</Button>
      </div>

      {/* Agent list */}
      {agents.length === 0 && (
        <p className="text-muted-foreground">No agents configured. Click "+ New" to create one.</p>
      )}

      <div className="grid gap-4">
        {agents.map((a) => (
          <Card key={a.name}>
            <CardHeader className="pb-2">
              <div className="flex items-start justify-between">
                <div>
                  <CardTitle className="text-lg">{a.name}</CardTitle>
                  <p className="text-sm text-muted-foreground mt-1">{a.description}</p>
                </div>
                <div className="flex gap-2 shrink-0">
                  <Button variant="outline" size="sm" onClick={() => openEdit(a)}>Edit</Button>
                  <Button variant="destructive" size="sm" onClick={() => doDelete(a.name)}>Delete</Button>
                </div>
              </div>
            </CardHeader>
            <CardContent>
              <div className="flex flex-wrap gap-1 items-center text-sm">
                <span className="text-muted-foreground">Tools:</span>
                {a.tools?.map((t) => (
                  <Badge key={t} variant="secondary" className="text-xs">{t}</Badge>
                ))}
                <span className="text-muted-foreground ml-3">Max steps: {a.max_step}</span>
              </div>
            </CardContent>
          </Card>
        ))}
      </div>

      {/* Edit / New dialog */}
      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent className="max-w-lg">
          <DialogHeader>
            <DialogTitle>{editName ? `Edit ${editName}` : 'New Agent'}</DialogTitle>
          </DialogHeader>
          <div className="space-y-4">
            <div>
              <label className="text-sm font-medium">Name</label>
              <Input
                value={form.name}
                onChange={(e) => setForm({ ...form, name: e.target.value })}
                disabled={!!editName}
                placeholder="my_agent"
              />
            </div>
            <div>
              <label className="text-sm font-medium">Description</label>
              <Input
                value={form.description}
                onChange={(e) => setForm({ ...form, description: e.target.value })}
                placeholder="What does this agent do? (used by host for routing)"
              />
            </div>
            <div>
              <label className="text-sm font-medium">System Prompt</label>
              <textarea
                className="w-full rounded-lg border border-input bg-background p-3 text-sm focus:outline-none focus:ring-2 focus:ring-ring min-h-[100px]"
                value={form.system_prompt}
                onChange={(e) => setForm({ ...form, system_prompt: e.target.value })}
                placeholder="You are..."
              />
            </div>
            <div>
              <label className="text-sm font-medium">Tools (comma-separated)</label>
              <Input
                value={form.tools}
                onChange={(e) => setForm({ ...form, tools: e.target.value })}
                placeholder="calculator, weather"
              />
            </div>
            <div>
              <label className="text-sm font-medium">Max Steps</label>
              <Input
                type="number"
                value={form.max_step}
                onChange={(e) => setForm({ ...form, max_step: parseInt(e.target.value) || 12 })}
                min={1}
                max={50}
              />
            </div>
            <div className="flex justify-end gap-2">
              <Button variant="outline" onClick={() => setOpen(false)}>Cancel</Button>
              <Button onClick={save}>Save</Button>
            </div>
          </div>
        </DialogContent>
      </Dialog>
    </div>
  )
}
