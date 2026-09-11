'use client';

import { useEffect } from 'react';
import { CircleCheck } from 'lucide-react';

/** How long a toast stays. Long enough to be read after the click that caused
 * it, short enough not to still be there after the next action. */
export const toastMs = 3000;

interface ToastProps {
  /** What was done, as a short sentence: "Rule slow-stripe saved". */
  message: string;
  /** Called when the toast has been up for its time. The caller drops it. */
  onDone: () => void;
}

/** A confirmation of something that just happened, pinned to the corner of
 * the window so it is seen wherever the eye was. Used for outcomes the view
 * does not otherwise show, such as a save that leaves the form looking the
 * same as before it. Not for errors, which belong next to what failed. */
export function Toast({ message, onDone }: ToastProps) {
  useEffect(() => {
    const timer = setTimeout(onDone, toastMs);
    return () => clearTimeout(timer);
  }, [message, onDone]);

  return (
    <div
      role="status"
      className="toast-enter fixed right-6 bottom-6 z-50 flex items-center gap-2 rounded-md border border-emerald-500/30 bg-zinc-900 px-3 py-2 text-sm text-zinc-100 shadow-lg shadow-black/40"
    >
      <CircleCheck className="h-4 w-4 shrink-0 text-emerald-400" aria-hidden />
      {message}
    </div>
  );
}
