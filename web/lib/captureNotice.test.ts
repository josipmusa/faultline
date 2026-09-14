import { describe, expect, it } from 'vitest';

import { captureNotice } from './captureNotice';
import type { Capture } from '@/types';

const capture = (bodies: boolean): Capture => ({
  event_id: '1',
  bodies,
  request: { headers: {}, truncated: false },
  response: { headers: {}, truncated: false },
});

describe('captureNotice', () => {
  it('says nothing when bodies were captured', () => {
    expect(captureNotice(capture(true))).toBeNull();
  });

  // An empty body under --no-bodies is not a request that had none, and the
  // panel must not let the two look the same.
  it('explains that bodies are turned off', () => {
    const notice = captureNotice(capture(false));
    expect(notice).toContain('--no-bodies');
    expect(notice).toContain('Headers');
  });
});
