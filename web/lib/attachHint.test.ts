import { describe, expect, it } from 'vitest';

import { attachOptions } from './attachHint';

describe('attachOptions', () => {
  it('says nothing before the config has been read', () => {
    expect(attachOptions(null)).toEqual([]);
  });

  it('names the proxy for HTTP_PROXY', () => {
    expect(attachOptions({ persisted: false, proxy_url: 'http://localhost:9001' })).toEqual([
      { label: 'HTTP_PROXY', value: 'http://localhost:9001' },
    ]);
  });

  it('names each route by what it stands in for', () => {
    const options = attachOptions({
      persisted: false,
      proxy_url: 'http://localhost:9001',
      routes: [{ name: 'stripe', addr: 'http://localhost:9100', upstream: 'https://api.stripe.com' }],
    });
    expect(options).toEqual([
      { label: 'HTTP_PROXY', value: 'http://localhost:9001' },
      { label: 'stripe (https://api.stripe.com)', value: 'http://localhost:9100' },
    ]);
  });

  it('leaves out a proxy the instance does not report', () => {
    expect(attachOptions({ persisted: true, path: '/x/faultline.yaml' })).toEqual([]);
  });
});
