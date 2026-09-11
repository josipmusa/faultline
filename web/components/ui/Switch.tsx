'use client';

import { Loader2 } from 'lucide-react';

import { cn } from '@/lib/utils';

interface SwitchProps {
  on: boolean;
  onToggle: () => void;
  /** Waiting on the server: the knob becomes a spinner and the switch cannot
   * be flipped again until the answer arrives. */
  busy?: boolean;
  disabled?: boolean;
  /** What is being switched, since the control itself has no text. */
  'aria-label': string;
  title?: string;
  className?: string;
}

/** An on/off control. On is amber, because what it switches on is a fault. */
export function Switch({ on, onToggle, busy, disabled, title, className, ...rest }: SwitchProps) {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={on}
      aria-label={rest['aria-label']}
      title={title}
      onClick={onToggle}
      disabled={disabled || busy}
      className={cn(
        'inline-flex h-5 w-9 shrink-0 cursor-pointer items-center rounded-full border px-0.5 transition-colors disabled:cursor-not-allowed disabled:opacity-50',
        on ? 'justify-end border-amber-500/40 bg-amber-500/25' : 'justify-start border-zinc-700 bg-zinc-800',
        className,
      )}
    >
      {busy ? (
        <Loader2 className="h-3.5 w-3.5 animate-spin text-zinc-400" aria-hidden />
      ) : (
        <span className={cn('h-3.5 w-3.5 rounded-full', on ? 'bg-amber-400' : 'bg-zinc-500')} />
      )}
    </button>
  );
}
