import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import type { ToolInfo } from '@/lib/types'

const builtinTools: ToolInfo[] = [
  {
    name: 'calculator',
    description: 'Perform arithmetic operations (+, -, *, /) on two numbers',
    used_by: ['math_agent'],
  },
  {
    name: 'current_time',
    description: 'Returns the current UTC date and time',
    used_by: ['ops_agent'],
  },
  {
    name: 'weather',
    description: 'Get current weather for a city (canned data)',
    used_by: ['research_agent'],
  },
]

export default function ConfigSkills() {
  return (
    <div className="p-6 space-y-6">
      <div>
        <h1 className="text-2xl font-bold">Built-in Skills</h1>
        <p className="text-sm text-muted-foreground">
          These tools are compiled into the Go binary. They cannot be added or removed at runtime.
        </p>
      </div>

      <div className="grid gap-4">
        {builtinTools.map((t) => (
          <Card key={t.name}>
            <CardHeader className="pb-2">
              <CardTitle className="text-lg">{t.name}</CardTitle>
            </CardHeader>
            <CardContent>
              <p className="text-sm text-muted-foreground mb-3">{t.description}</p>
              <div className="flex items-center gap-2 text-sm">
                <span className="text-muted-foreground">Used by:</span>
                {t.used_by.map((agent) => (
                  <Badge key={agent} variant="secondary">{agent}</Badge>
                ))}
              </div>
            </CardContent>
          </Card>
        ))}
      </div>
    </div>
  )
}
