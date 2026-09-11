import type { ReactNode } from 'react';

interface EmptyStateProps {
  /** What is missing and what to do about it. */
  children: ReactNode;
  /** A control that does the next thing, when the panel has one. */
  action?: ReactNode;
}

/** The centred message a list shows when it has nothing to list. It says what
 * to do next, not only what is absent: a panel with nothing in it is where
 * somebody new to the product is most likely to be standing. */
export function EmptyState({ children, action }: EmptyStateProps) {
  return (
    <div className="flex flex-1 flex-col items-center justify-center gap-3 px-6 py-16 text-center">
      <p className="max-w-md text-sm leading-relaxed text-zinc-500">{children}</p>
      {action}
    </div>
  );
}
