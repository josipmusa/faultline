'use client';

import { CircleDot, FileText, RotateCcw } from 'lucide-react';

import { configNotice } from '@/lib/configNotice';
import { cn } from '@/lib/utils';
import type { ConfigInfo } from '@/types';

interface HeaderProps {
  connected: boolean;
  eventCount: number;
  ruleCount: number;
  /** Where rule changes go, or null while that is still unknown. */
  config: ConfigInfo | null;
  /** Empties the recorder, which is what starts the session report over: the
   * report is a reading of the events, so the two are one action. */
  onReset: () => void;
}

export function Header({ connected, eventCount, ruleCount, config, onReset }: HeaderProps) {
  const notice = configNotice(config);

  return (
    <header className="flex h-16 items-center justify-between border-b border-zinc-800 bg-zinc-950 px-6">
      <div className="flex items-center gap-3">
        <span
          className={cn(
            'flex items-center gap-2 rounded-md border px-3 py-1.5 text-sm font-medium',
            connected
              ? 'border-emerald-500/20 bg-emerald-500/10 text-emerald-400'
              : 'border-zinc-700 bg-zinc-800 text-zinc-400',
          )}
        >
          <CircleDot className={cn('h-3 w-3', connected && 'animate-pulse')} />
          {connected ? 'Live' : 'Disconnected'}
        </span>
        <Count label="events" value={eventCount} />
        <Count label={ruleCount === 1 ? 'rule' : 'rules'} value={ruleCount} />

        {notice && (
          <span
            title={notice.title}
            className={cn(
              'flex items-center gap-1.5 text-sm',
              notice.persisted ? 'text-zinc-400' : 'text-zinc-500 italic',
            )}
          >
            <FileText className="h-3.5 w-3.5" />
            {notice.label}
          </span>
        )}
      </div>

      <button
        type="button"
        onClick={onReset}
        title="Forget every request recorded so far and start the session report over"
        className="flex cursor-pointer items-center gap-2 rounded-lg bg-zinc-800 px-4 py-2 text-sm font-medium text-zinc-300 transition-colors hover:bg-zinc-700"
      >
        <RotateCcw className="h-4 w-4" />
        Reset session
      </button>
    </header>
  );
}

function Count({ label, value }: { label: string; value: number }) {
  return (
    <span className="text-sm text-zinc-500">
      <span className="font-mono text-zinc-300">{value}</span> {label}
    </span>
  );
}
