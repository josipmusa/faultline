import type { ReactNode } from 'react';
import type { LucideIcon } from 'lucide-react';

import { cn } from '@/lib/utils';

/** One palette for every pill in the UI. Amber is the fault colour and
 * nothing else wears it; the rest name a hue rather than a meaning, because a
 * method badge and a tier badge share a shape and nothing else. */
export type BadgeTone =
  | 'neutral'
  | 'amber'
  | 'sky'
  | 'violet'
  | 'blue'
  | 'emerald'
  | 'yellow'
  | 'orange'
  | 'red';

const tones: Record<BadgeTone, string> = {
  neutral: 'border-zinc-600/40 bg-zinc-500/10 text-zinc-400',
  amber: 'border-amber-500/30 bg-amber-500/15 text-amber-300',
  sky: 'border-sky-500/30 bg-sky-500/10 text-sky-300',
  violet: 'border-violet-500/30 bg-violet-500/10 text-violet-300',
  blue: 'border-blue-500/20 bg-blue-500/10 text-blue-400',
  emerald: 'border-emerald-500/20 bg-emerald-500/10 text-emerald-400',
  yellow: 'border-yellow-500/20 bg-yellow-500/10 text-yellow-400',
  orange: 'border-orange-500/20 bg-orange-500/10 text-orange-400',
  red: 'border-red-500/20 bg-red-500/10 text-red-400',
};

interface BadgeProps {
  tone: BadgeTone;
  children: ReactNode;
  icon?: LucideIcon;
  /** Filled rather than outlined, for the one icon that has to read at a
   * glance in a dense table. */
  iconFilled?: boolean;
  title?: string;
  className?: string;
}

export function Badge({ tone, children, icon: Icon, iconFilled, title, className }: BadgeProps) {
  return (
    <span
      title={title}
      className={cn(
        'inline-flex max-w-full items-center gap-1 rounded border px-1.5 py-0.5 text-2xs font-medium',
        tones[tone],
        className,
      )}
    >
      {Icon && <Icon className="h-3 w-3 shrink-0" fill={iconFilled ? 'currentColor' : 'none'} aria-hidden />}
      <span className="truncate">{children}</span>
    </span>
  );
}

/** The badge's classes on their own, for the one place a badge has to be a
 * button rather than a span. */
export function badgeClass(tone: BadgeTone, className?: string): string {
  return cn(
    'inline-flex max-w-full items-center gap-1 rounded border px-1.5 py-0.5 text-2xs font-medium',
    tones[tone],
    className,
  );
}
