import { describe, expect, it } from 'vitest';

import { configNotice } from './configNotice';

describe('configNotice', () => {
  it('names the file by its base name and its path in full', () => {
    const notice = configNotice({ persisted: true, path: '/Users/me/ops/faultline.yaml' });

    expect(notice).not.toBeNull();
    expect(notice?.label).toBe('faultline.yaml');
    expect(notice?.title).toContain('/Users/me/ops/faultline.yaml');
    expect(notice?.title).toContain('saved');
    expect(notice?.persisted).toBe(true);
  });

  it('reads a Windows path the same way', () => {
    expect(configNotice({ persisted: true, path: 'C:\\ops\\faultline.yaml' })?.label).toBe(
      'faultline.yaml',
    );
  });

  // A persister with no single path on disk is still not running in memory,
  // so the absence of a path must not be read as the in-memory case.
  it('falls back to a generic label for a file with no path', () => {
    const notice = configNotice({ persisted: true });

    expect(notice?.persisted).toBe(true);
    expect(notice?.label).toBe('config file');
  });

  it('says where changes go when nothing persists them', () => {
    const notice = configNotice({ persisted: false });

    expect(notice?.persisted).toBe(false);
    expect(notice?.label).toBe('in-memory');
    expect(notice?.title).toContain('faultline init');
  });

  // Nothing is claimed before the answer arrives: a chip asserting either
  // state would be wrong half the time.
  it('shows nothing until the config has been read', () => {
    expect(configNotice(null)).toBeNull();
  });
});
