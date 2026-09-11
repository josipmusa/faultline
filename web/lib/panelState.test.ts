import { describe, expect, it } from 'vitest';

import { panelState } from './panelState';

describe('panelState', () => {
  it('is loading before the first read has answered', () => {
    expect(panelState(null, null)).toBe('loading');
  });

  it('is an error when the first read failed and there is nothing to show', () => {
    expect(panelState(null, 'connection refused')).toBe('error');
  });

  it('is empty once a read has answered with nothing', () => {
    expect(panelState([], null)).toBe('empty');
  });

  it('shows rows when there are any, even if a later read failed', () => {
    expect(panelState([{ host: 'a' }], null)).toBe('rows');
    expect(panelState([{ host: 'a' }], 'timed out')).toBe('rows');
  });

  it('stays empty rather than claiming an error when a later read failed on an empty list', () => {
    // The panel says why in its notice; the list area should not swap its
    // honest "nothing yet" for a second copy of the same message.
    expect(panelState([], 'timed out')).toBe('empty');
  });
});
