/** A captured body as the inspector can show it: text when it decodes as
 * UTF-8, a byte count when it does not, and nothing at all when there was no
 * body. Rendering binary as mojibake would be worse than saying it is binary. */
export type DecodedBody =
  | { kind: 'empty' }
  | { kind: 'text'; text: string; bytes: number }
  | { kind: 'binary'; bytes: number };

/** Decodes the base64 the API sends for a captured body. Go marshals `[]byte`
 * as base64, so this is the one place that knows it. */
export function decodeBody(encoded: string | undefined): DecodedBody {
  if (!encoded) {
    return { kind: 'empty' };
  }

  const bytes = Uint8Array.from(atob(encoded), (c) => c.charCodeAt(0));
  try {
    // `fatal` is the whole point: a body that is not UTF-8 must be reported
    // as binary rather than silently replaced with question marks.
    return { kind: 'text', text: new TextDecoder('utf-8', { fatal: true }).decode(bytes), bytes: bytes.length };
  } catch {
    return { kind: 'binary', bytes: bytes.length };
  }
}
