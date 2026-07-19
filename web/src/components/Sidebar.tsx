import { NavLink } from 'react-router-dom';
import { cn } from '@/lib/utils';

const links = [
  { to: '/', label: '🗨  Chat', end: true },
  { to: '/config/agents', label: '⚙  Agents' },
  { to: '/config/mcp', label: '⚙  MCP Servers' },
  { to: '/config/skills', label: '⚙  Skills' },
];

export default function Sidebar() {
  return (
    <aside className="w-56 shrink-0 border-r border-border bg-card p-4 flex flex-col gap-1">
      <div className="text-sm font-semibold text-muted-foreground px-3 py-2 mb-2">
        Navigation
      </div>
      {links.map((link) => (
        <NavLink
          key={link.to}
          to={link.to}
          end={link.end}
          className={({ isActive }) =>
            cn(
              'block rounded-md px-3 py-2 text-sm transition-colors',
              isActive
                ? 'bg-primary text-primary-foreground font-medium'
                : 'text-muted-foreground hover:bg-accent hover:text-accent-foreground',
            )
          }
        >
          {link.label}
        </NavLink>
      ))}
    </aside>
  );
}
