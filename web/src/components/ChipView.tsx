// Chip renders one metadata chip (agent switch / tool call / tool result / error).
import { Badge } from '@/components/ui/badge';

export type Chip =
  | { kind: 'switch'; to: string; argument: string }
  | { kind: 'tool'; name: string; args: string }
  | { kind: 'result'; name: string; result: string }
  | { kind: 'error'; message: string };

export default function ChipView({ c }: { c: Chip }) {
  switch (c.kind) {
    case 'switch':
      return (
        <Badge variant="secondary" className="bg-blue-600/20 text-blue-400 border-blue-600/30">
          → {c.to}: {c.argument}
        </Badge>
      );
    case 'tool':
      return (
        <Badge variant="outline" className="bg-orange-600/20 text-orange-400 border-orange-600/30">
          🔧 {c.name}({c.args})
        </Badge>
      );
    case 'result':
      return (
        <Badge variant="outline" className="bg-green-600/20 text-green-400 border-green-600/30">
          ✓ {c.name}: {c.result}
        </Badge>
      );
    case 'error':
      return (
        <Badge variant="destructive">
          ✗ {c.message}
        </Badge>
      );
  }
}
