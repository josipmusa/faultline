'use client';

import type { ButtonHTMLAttributes, ReactNode } from 'react';
import { Loader2, type LucideIcon } from 'lucide-react';

import { cn } from '@/lib/utils';

/** What a button is for, which is what decides its colour.
 *
 * `primary` is amber, and amber means one thing in this UI: a fault is in
 * force, or this puts one in force. Every primary action here creates a rule,
 * saves one, or turns a scenario on, so the colour is the meaning. */
export type ButtonVariant = 'primary' | 'secondary' | 'ghost' | 'danger';

/** `sm` sits in toolbars and table rows; `md` is the header's size. */
export type ButtonSize = 'sm' | 'md';

interface Base extends Omit<ButtonHTMLAttributes<HTMLButtonElement>, 'children'> {
  variant?: ButtonVariant;
  size?: ButtonSize;
  /** Waiting on the server: the icon becomes a spinner and the button is
   * disabled, so a second click cannot send the same request twice. */
  busy?: boolean;
  icon?: LucideIcon;
}

/** A button with text names itself; one with only an icon has to be given a
 * name, or a screen reader announces "button" and nothing else. The type
 * makes forgetting that a compile error rather than a finding in a review. */
export type ButtonProps =
  | (Base & { children: ReactNode })
  | (Base & { children?: undefined; 'aria-label': string });

const variants: Record<ButtonVariant, string> = {
  primary:
    'border-amber-500/40 bg-amber-500/15 text-amber-300 enabled:hover:bg-amber-500/25',
  secondary:
    'border-zinc-700 bg-zinc-800/60 text-zinc-300 enabled:hover:bg-zinc-700 enabled:hover:text-zinc-100',
  ghost: 'border-transparent bg-transparent text-zinc-500 enabled:hover:bg-zinc-800 enabled:hover:text-zinc-200',
  danger: 'border-transparent bg-transparent text-zinc-500 enabled:hover:bg-zinc-800 enabled:hover:text-red-400',
};

const sizes: Record<ButtonSize, { text: string; icon: string; iconOnly: string; spin: string }> = {
  sm: { text: 'px-2 py-1 text-xs', icon: 'h-3.5 w-3.5', iconOnly: 'p-1', spin: 'h-3.5 w-3.5' },
  md: { text: 'px-3 py-1.5 text-sm', icon: 'h-4 w-4', iconOnly: 'p-1.5', spin: 'h-4 w-4' },
};

export function Button({
  variant = 'secondary',
  size = 'sm',
  busy = false,
  icon: Icon,
  children,
  className,
  disabled,
  type = 'button',
  ...rest
}: ButtonProps) {
  const s = sizes[size];
  const iconOnly = children === undefined || children === null;

  return (
    <button
      type={type}
      disabled={disabled || busy}
      className={cn(
        'inline-flex shrink-0 cursor-pointer items-center justify-center gap-1.5 rounded-md border font-medium transition-colors disabled:cursor-not-allowed disabled:opacity-50',
        variants[variant],
        iconOnly ? s.iconOnly : s.text,
        className,
      )}
      {...rest}
    >
      {busy ? (
        <Loader2 className={cn(s.spin, 'animate-spin')} aria-hidden />
      ) : (
        Icon && <Icon className={s.icon} aria-hidden />
      )}
      {children}
    </button>
  );
}
