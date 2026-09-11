'use client';

import { useEffect, type ReactNode } from 'react';
import { X } from 'lucide-react';

import { Button } from './Button';

interface SidePanelProps {
  title: ReactNode;
  subtitle?: ReactNode;
  onClose: () => void;
  children: ReactNode;
  footer?: ReactNode;
}

/** The panel that opens beside a table: the event inspector and the rule
 * editor. One shape for both, so opening either feels like the same thing
 * happening. Escape closes it, from anywhere on the page: the panel is the
 * thing most recently opened, so it is the thing Escape should undo. */
export function SidePanel({ title, subtitle, onClose, children, footer }: SidePanelProps) {
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape' && !e.defaultPrevented) {
        onClose();
      }
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [onClose]);

  return (
    // Narrower below 1440px: at a laptop width the table beside it has to
    // stay readable, and the panel's own content wraps rather than needing
    // the room.
    <aside className="flex w-[26rem] shrink-0 flex-col overflow-hidden border-l border-zinc-800 bg-zinc-950 min-[90rem]:w-[32rem]">
      <header className="flex items-start justify-between gap-3 border-b border-zinc-800 px-4 py-3">
        <div className="min-w-0">
          <p className="truncate text-sm text-zinc-100">{title}</p>
          {subtitle && <p className="mt-1 flex min-w-0 flex-wrap items-center gap-2 text-xs text-zinc-500">{subtitle}</p>}
        </div>
        <Button variant="ghost" icon={X} aria-label="Close" onClick={onClose} />
      </header>

      {children}

      {footer && (
        <footer className="flex items-center justify-end gap-3 border-t border-zinc-800 px-4 py-3">{footer}</footer>
      )}
    </aside>
  );
}
