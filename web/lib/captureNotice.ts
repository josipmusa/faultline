import type { Capture } from '@/types';

/** What the inspector should say about a capture taken with bodies turned
 * off, or null when there is nothing to explain. An empty body under
 * --no-bodies is not the same fact as a request that had no body, and the
 * panel must not let the two look alike - the same reason the encrypted tier
 * gets a notice of its own rather than an empty panel. */
export function captureNotice(capture: Capture): string | null {
  if (capture.bodies) {
    return null;
  }
  return 'Faultline is running with --no-bodies, so payloads are not captured. Headers below are what it holds for this exchange.';
}
