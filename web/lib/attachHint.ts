import type { ConfigInfo } from '@/types';

/** One way to send traffic through this instance, for an empty state to name.
 * `label` is what to set or where to point, `value` the address. */
export interface AttachOption {
  label: string;
  value: string;
}

/** The addresses an application can be pointed at, read off `GET /api/config`:
 * the forward proxy, for anything that honours `HTTP_PROXY`, and each explicit
 * route, for a client that does not. Empty before the config has been read or
 * when the instance is a plain handler with no proxy behind it, in which case
 * the empty state falls back to naming `faultline run` alone. */
export function attachOptions(config: ConfigInfo | null): AttachOption[] {
  if (config === null) {
    return [];
  }
  const out: AttachOption[] = [];
  if (config.proxy_url) {
    out.push({ label: 'HTTP_PROXY', value: config.proxy_url });
  }
  for (const route of config.routes ?? []) {
    out.push({ label: `${route.name} (${route.upstream})`, value: route.addr });
  }
  return out;
}
