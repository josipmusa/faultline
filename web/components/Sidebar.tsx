'use client';

import { Activity, Layers, PanelLeftClose, PanelLeftOpen, Server, Zap } from 'lucide-react';

import { FaultlineMark } from '@/components/FaultlineMark';
import { cn } from '@/lib/utils';

/** The nav carries one entry per panel that exists. Later panels add their
 * own, so there is never a tab that leads nowhere. */
const navItems = [
  { id: 'live', icon: Activity, label: 'Live' },
  { id: 'upstreams', icon: Server, label: 'Upstreams' },
  { id: 'rules', icon: Zap, label: 'Rules' },
  { id: 'scenarios', icon: Layers, label: 'Scenarios' },
];

interface SidebarProps {
  activeView: string;
  onViewChange: (view: string) => void;
  /** Showing labels beside the icons, or icons alone. */
  expanded: boolean;
  onToggle: () => void;
}

export function Sidebar({ activeView, onViewChange, expanded, onToggle }: SidebarProps) {
  return (
    <nav
      aria-label="Views"
      className={cn(
        'flex shrink-0 flex-col gap-6 border-r border-zinc-800 bg-zinc-950 py-6 transition-[width] duration-150',
        expanded ? 'w-48 px-3' : 'w-16 items-center',
      )}
    >
      {/* Exactly 32px: the icon is drawn on a 32 unit grid, so any other size
        * lands its edges between pixels and blurs them. */}
      <div className={cn('flex h-10 items-center gap-3', expanded ? 'px-2' : 'w-10 justify-center')}>
        <FaultlineMark className="h-8 w-8 shrink-0 text-brand" />
        {expanded && <span className="text-sm font-semibold text-zinc-100">Faultline</span>}
      </div>

      <div className="flex flex-1 flex-col gap-1">
        {navItems.map((item) => {
          const active = activeView === item.id;
          return (
            <button
              key={item.id}
              type="button"
              onClick={() => onViewChange(item.id)}
              // The title carries the name when the label is hidden; with the
              // label showing it would only repeat it on hover.
              title={expanded ? undefined : item.label}
              aria-label={expanded ? undefined : item.label}
              aria-current={active ? 'page' : undefined}
              className={cn(
                'relative flex h-11 cursor-pointer items-center gap-3 rounded-lg text-sm transition-colors',
                expanded ? 'w-full px-3' : 'w-12 justify-center',
                // Neutral rather than amber: amber is the fault colour, and
                // which tab is open is not a fault. The brand bar marks it.
                active ? 'bg-zinc-800 text-zinc-100' : 'text-zinc-500 hover:bg-zinc-900 hover:text-zinc-300',
              )}
            >
              <item.icon className="h-5 w-5 shrink-0" aria-hidden />
              {expanded && <span className="truncate">{item.label}</span>}
              {active && <span className={cn('absolute h-7 w-1 rounded-r bg-brand', expanded ? '-left-3' : 'left-0')} />}
            </button>
          );
        })}
      </div>

      <button
        type="button"
        onClick={onToggle}
        aria-expanded={expanded}
        aria-label={expanded ? 'Collapse the navigation' : 'Expand the navigation'}
        title={expanded ? 'Collapse' : 'Expand'}
        className={cn(
          'flex h-11 cursor-pointer items-center gap-3 rounded-lg text-sm text-zinc-500 transition-colors hover:bg-zinc-900 hover:text-zinc-300',
          expanded ? 'w-full px-3' : 'w-12 justify-center',
        )}
      >
        {expanded ? (
          <PanelLeftClose className="h-5 w-5 shrink-0" aria-hidden />
        ) : (
          <PanelLeftOpen className="h-5 w-5 shrink-0" aria-hidden />
        )}
        {expanded && <span>Collapse</span>}
      </button>
    </nav>
  );
}
