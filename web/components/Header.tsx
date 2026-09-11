'use client';

import { CircleDot, FileText, RotateCcw } from 'lucide-react';

import { configNotice } from '@/lib/configNotice';
import { cn } from '@/lib/utils';
import type { ConfigInfo } from '@/types';

import { Button } from './ui/Button';

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
    <header className="flex h-16 items-center justify-between gap-4 border-b border-zinc-800 bg-zinc-950 px-6">
      <div className="flex min-w-0 items-center gap-3">
        {/* "Connected" rather than "Live": Live is the name of a tab, and
            this pill is about the socket. A dropped socket reconnects on its
            own, so the other state says what is happening rather than what
            went wrong. */}
        <span
          title={connected ? 'Receiving events from Faultline' : 'The event stream dropped; reconnecting on its own'}
          className={cn(
            'flex items-center gap-2 rounded-md border px-3 py-1.5 text-sm font-medium',
            connected
              ? 'border-emerald-500/20 bg-emerald-500/10 text-emerald-400'
              : 'border-zinc-700 bg-zinc-800 text-zinc-400',
          )}
        >
          <CircleDot className={cn('h-3 w-3', connected && 'animate-pulse')} aria-hidden />
          {connected ? 'Connected' : 'Reconnecting'}
        </span>
        <Count label="events" value={eventCount} />
        <Count label={ruleCount === 1 ? 'rule' : 'rules'} value={ruleCount} />

        {notice && (
          <span
            title={notice.title}
            className={cn(
              'flex min-w-0 items-center gap-1.5 text-sm',
              notice.persisted ? 'text-zinc-400' : 'text-zinc-500 italic',
            )}
          >
            <FileText className="h-3.5 w-3.5 shrink-0" aria-hidden />
            <span className="truncate">{notice.label}</span>
          </span>
        )}
      </div>

      <Button
        size="md"
        icon={RotateCcw}
        onClick={onReset}
        title="Forget every request recorded so far and start the session report over"
      >
        Reset session
      </Button>
    </header>
  );
}

function Count({ label, value }: { label: string; value: number }) {
  return (
    <span className="shrink-0 text-sm text-zinc-500">
      <span className="font-mono text-zinc-300">{value}</span> {label}
    </span>
  );
}
