/** The Faultline icon, inlined so it takes its color from `currentColor` and
 * its size from the class list. Source of truth is docs/assets/icon.svg; the
 * favicon at app/icon.svg is the same path with a fixed fill. */
export function FaultlineMark({ className }: { className?: string }) {
  return (
    <svg viewBox="0 0 32 32" role="img" aria-label="Faultline" className={className}>
      <path
        fill="currentColor"
        d="M2 20 H15 V22 H30 V24 H13 V22 H2 Z M16 18 H26 V24 H16 Z M23 19 H20 L11 8 H14 Z M12 2 A4 4 0 1 1 12 10 A4 4 0 1 1 12 2 Z"
      />
    </svg>
  );
}
