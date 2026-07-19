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
import { listMCPs, createMCP, updateMCP, deleteMCP } from '@/lib/api'
import type { MCPConfig } from '@/lib/types'

const emptyForm = (): MCPConfig => ({
  name: '',
  transport: 'inproc',
  enabled_if: 'always',
  provider: 'builtin-demo',
  default_root: '/agents',
  command: '',
  args: [],
  env: {},
  init_timeout: '30s',
})

export default function ConfigMCP() {
  const [mcps, setMcps] = useState<MCPConfig[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const [open, setOpen] = useState(false)
  const [editName, setEditName] = useState<string | null>(null)
  const [form, setForm] = useState<MCPConfig>(emptyForm())

  const load = () => {
    setLoading(true)
    listMCPs()
      .then(setMcps)
      .catch((e) => setError(e.message))
      .finally(() => setLoading(false))
  }

  useEffect(() => { load() }, [])

  const openNew = () => {
    setEditName(null)
    setForm(emptyForm())
    setOpen(true)
  }

  const openEdit = (m: MCPConfig) => {
    setEditName(m.name)
    setForm({ ...m })
    setOpen(true)
  }

  const save = async () => {
    try {
      if (editName) {
        await updateMCP(editName, form)
      } else {
        await createMCP(form)
      }
      setOpen(false)
      load()
    } catch (e) {
      alert(String(e))
    }
  }

  const doDelete = async (name: string) => {
    if (!window.confirm(`确定删除 MCP "${name}"？`)) return
    try {
      await deleteMCP(name)
      load()
    } catch (e) {
      alert(String(e))
    }
  }

  const isEnabled = (m: MCPConfig) =>
    m.enabled_if === 'always' || (m.enabled_if && m.enabled_if.startsWith('env:'))

  if (loading) return <div className="p-8 text-muted-foreground">Loading MCP servers…</div>
  if (error) return <div className="p-8 text-destructive">Error: {error}</div>

  return (
    <div className="p-6 space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold">MCP Servers</h1>
          <p className="text-sm text-muted-foreground">Manage MCP server connections</p>
        </div>
        <Button onClick={openNew}>+ New</Button>
      </div>

      {mcps.length === 0 && (
        <p className="text-muted-foreground">No MCP servers configured.</p>
      )}

      <div className="grid gap-4">
        {mcps.map((m) => (
          <Card key={m.name}>
            <CardHeader className="pb-2">
              <div className="flex items-start justify-between">
                <div className="flex items-center gap-2">
                  <span className={`w-2 h-2 rounded-full ${isEnabled(m) ? 'bg-green-500' : 'bg-gray-400'}`} />
                  <CardTitle className="text-lg">{m.name}</CardTitle>
                  <Badge variant="outline">{m.transport}</Badge>
                </div>
                <div className="flex gap-2 shrink-0">
                  <Button variant="outline" size="sm" onClick={() => openEdit(m)}>Config</Button>
                  <Button variant="destructive" size="sm" onClick={() => doDelete(m.name)}>Delete</Button>
                </div>
              </div>
            </CardHeader>
            <CardContent>
              <div className="text-sm text-muted-foreground space-y-1">
                <p>enabled_if: <code className="bg-muted px-1 rounded">{m.enabled_if}</code></p>
                {m.transport === 'inproc' && <p>Provider: {m.provider}</p>}
                {m.transport === 'stdio' && <p>Command: {m.command} {m.args?.join(' ')}</p>}
              </div>
            </CardContent>
          </Card>
        ))}
      </div>

      {/* Edit / New dialog */}
      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent className="max-w-lg">
          <DialogHeader>
            <DialogTitle>{editName ? `Edit ${editName}` : 'New MCP Server'}</DialogTitle>
          </DialogHeader>
          <div className="space-y-4">
            <div>
              <label className="text-sm font-medium">Name</label>
              <Input
                value={form.name}
                onChange={(e) => setForm({ ...form, name: e.target.value })}
                disabled={!!editName}
                placeholder="my_mcp"
              />
            </div>
            <div>
              <label className="text-sm font-medium">Transport</label>
              <select
                className="w-full rounded-lg border border-input bg-background px-3 py-2 text-sm"
                value={form.transport}
                onChange={(e) => setForm({ ...form, transport: e.target.value as 'inproc' | 'stdio' })}
              >
                <option value="inproc">inproc</option>
                <option value="stdio">stdio</option>
              </select>
            </div>
            <div>
              <label className="text-sm font-medium">Enabled If</label>
              <Input
                value={form.enabled_if}
                onChange={(e) => setForm({ ...form, enabled_if: e.target.value })}
                placeholder="always | env:VAR | env:VAR=value"
              />
            </div>

            {form.transport === 'inproc' && (
              <>
                <div>
                  <label className="text-sm font-medium">Provider</label>
                  <Input
                    value={form.provider || ''}
                    onChange={(e) => setForm({ ...form, provider: e.target.value })}
                    placeholder="builtin-demo"
                  />
                </div>
                <div>
                  <label className="text-sm font-medium">Default Root</label>
                  <Input
                    value={form.default_root || ''}
                    onChange={(e) => setForm({ ...form, default_root: e.target.value })}
                    placeholder="/agents"
                  />
                </div>
              </>
            )}

            {form.transport === 'stdio' && (
              <>
                <div>
                  <label className="text-sm font-medium">Command</label>
                  <Input
                    value={form.command || ''}
                    onChange={(e) => setForm({ ...form, command: e.target.value })}
                    placeholder="npx"
                  />
                </div>
                <div>
                  <label className="text-sm font-medium">Args (comma-separated)</label>
                  <Input
                    value={(form.args || []).join(', ')}
                    onChange={(e) => setForm({ ...form, args: e.target.value.split(',').map((s) => s.trim()).filter(Boolean) })}
                    placeholder="-y, @modelcontextprotocol/server-filesystem, ."
                  />
                </div>
                <div>
                  <label className="text-sm font-medium">Init Timeout</label>
                  <Input
                    value={form.init_timeout || '30s'}
                    onChange={(e) => setForm({ ...form, init_timeout: e.target.value })}
                    placeholder="30s"
                  />
                </div>
              </>
            )}

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
