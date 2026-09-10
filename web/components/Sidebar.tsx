'use client';

import { Activity, Layers, Server, Zap } from 'lucide-react';

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
}

export function Sidebar({ activeView, onViewChange }: SidebarProps) {
  return (
    <nav className="flex w-16 flex-col items-center gap-6 border-r border-zinc-800 bg-zinc-950 py-6">
      {/* Exactly 32px: the icon is drawn on a 32 unit grid, so any other size
        * lands its edges between pixels and blurs them. */}
      <div className="flex h-10 w-10 items-center justify-center">
        <FaultlineMark className="h-8 w-8 text-brand" />
      </div>
      <div className="flex flex-1 flex-col gap-2">
        {navItems.map((item) => (
          <button
            key={item.id}
            type="button"
            onClick={() => onViewChange(item.id)}
            title={item.label}
            aria-current={activeView === item.id ? 'page' : undefined}
            className={cn(
              'relative flex h-12 w-12 cursor-pointer items-center justify-center rounded-lg transition-colors',
              activeView === item.id
                ? 'bg-zinc-800 text-amber-400'
                : 'text-zinc-500 hover:bg-zinc-900 hover:text-zinc-300',
            )}
          >
            <item.icon className="h-5 w-5" />
            {activeView === item.id && (
              <span className="absolute left-0 h-8 w-1 rounded-r bg-amber-400" />
            )}
          </button>
        ))}
      </div>
    </nav>
  );
}
