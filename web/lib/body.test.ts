import { describe, expect, it } from 'vitest';

import { decodeBody } from './body';

/** base64 of the bytes given, the way the API encodes a captured body. */
function b64(bytes: number[] | string): string {
  const data = typeof bytes === 'string' ? new TextEncoder().encode(bytes) : Uint8Array.from(bytes);
  return btoa(String.fromCharCode(...data));
}

describe('decodeBody', () => {
  it('reads a UTF-8 body as text', () => {
    expect(decodeBody(b64('{"ok":true}'))).toEqual({ kind: 'text', text: '{"ok":true}', bytes: 11 });
  });

  it('reads text beyond ASCII', () => {
    expect(decodeBody(b64('héllo ☠'))).toMatchObject({ kind: 'text', text: 'héllo ☠' });
  });

  // A PNG header is not text, and showing its bytes as mojibake helps nobody.
  it('reports a body that is not UTF-8 as binary, with its length', () => {
    expect(decodeBody(b64([0x89, 0x50, 0x4e, 0x47, 0xff, 0xfe]))).toEqual({ kind: 'binary', bytes: 6 });
  });

  it('reads an absent body as empty', () => {
    expect(decodeBody(undefined)).toEqual({ kind: 'empty' });
    expect(decodeBody('')).toEqual({ kind: 'empty' });
  });
});
