import type { ReactNode } from 'react';

interface ToolbarProps {
  /** What the list under it holds, in a sentence: "2 rules, applied in this
   * order". */
  summary: ReactNode;
  /** The actions on the right, when the panel has any. */
  children?: ReactNode;
}

/** The line every list opens with, so the four panels start the same way. */
export function Toolbar({ summary, children }: ToolbarProps) {
  return (
    <div className="flex min-h-11 items-center justify-between gap-3 border-b border-zinc-800 px-4 py-2">
      <p className="text-xs text-zinc-500">{summary}</p>
      {children && <div className="flex items-center gap-2">{children}</div>}
    </div>
  );
}
