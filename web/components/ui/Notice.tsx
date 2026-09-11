'use client';

import type { ReactNode } from 'react';
import { CircleAlert, Info, TriangleAlert, type LucideIcon } from 'lucide-react';

import { cn } from '@/lib/utils';

/** `error` is something that failed, `warning` something that will not work
 * the way it looks, `info` something worth knowing. Amber is kept for warnings
 * because a warning here is always about a fault: one that cannot apply, or
 * one that applies where it looks like it should not. */
export type NoticeTone = 'error' | 'warning' | 'info';

interface NoticeProps {
  tone: NoticeTone;
  children: ReactNode;
  /** The host this is about, named in the notice so it cannot be read as
   * belonging to a neighbouring row. */
  host?: string;
  /** A more specific icon than the tone's own, when the message has one:
   * a padlock for encrypted traffic, a shield for a certificate. */
  icon?: LucideIcon;
  className?: string;
}

const tones: Record<NoticeTone, { className: string; icon: LucideIcon }> = {
  error: { className: 'border-red-500/20 bg-red-500/10 text-red-400', icon: CircleAlert },
  warning: { className: 'border-amber-500/20 bg-amber-500/10 text-amber-300', icon: TriangleAlert },
  info: { className: 'border-zinc-700 bg-zinc-800/60 text-zinc-300', icon: Info },
};

export function Notice({ tone, children, host, icon, className }: NoticeProps) {
  const Icon = icon ?? tones[tone].icon;

  return (
    <p
      role={tone === 'error' ? 'alert' : undefined}
      className={cn('flex items-start gap-2 rounded-md border px-3 py-2 text-xs', tones[tone].className, className)}
    >
      <Icon className="mt-0.5 h-3.5 w-3.5 shrink-0" aria-hidden />
      <span className="min-w-0 break-words">
        {host && (
          <>
            <span className="font-mono font-medium">{host}</span>
            <span className="px-1.5 opacity-50">·</span>
          </>
        )}
        {children}
      </span>
    </p>
  );
}
