# @faultline/client

A thin typed client for the [Faultline](../../README.md) admin API, for driving
a fault from inside a test: inject one, let the application meet it, and read
what the application did about it.

There is no business logic here. Every method is one API call, so what a test
asserts is what Faultline said.

## Install

The package is not published yet. Point at the checkout:

```bash
npm install --save-dev ../faultline/clients/ts
```

## Use

```ts
import { FaultlineClient } from '@faultline/client';

const faultline = new FaultlineClient('http://localhost:9000');

test('checkout survives the payment API failing twice', async () => {
  const rule = await faultline.addRule({
    name: 'payments are down',
    enabled: true,
    match: { host: 'api.stripe.com', method: 'POST', path: '/v1/charges/*' },
    fault: { type: 'status', code: 503 },
    behavior: { type: 'first_n', n: 2 },
  });
  // A warning means the rule is stored but cannot apply yet, usually because
  // the host has only ever been seen encrypted. Read it before believing a
  // faulted count of zero.
  expect(rule.warnings ?? []).toEqual([]);

  await checkout();

  const report = await faultline.report();
  expect(report.faulted).toBe(2);
  expect(report.retries).toBe(2);

  await faultline.removeRule(rule.id);
});
```

`resetSession()` is what to call between two tests sharing one instance: it
clears what was observed and re-arms every rule, so a spent `first_n` applies
again without rebuilding it.

`waitForEvent()` resolves with the first matching call recorded after the wait
begins, and rejects with a `WaitTimeoutError` when none arrives - usually
meaning the application never made the call at all.

## The fault catalogue

A fault is `{ type, ...parameters }` and a behavior is the same, so a fault
added to Faultline is usable from here without a new release of this client.
`faultline rule add --fault <name>` names the parameters each one takes, and
the API refuses a wrong one with the field it blames.

## Developing

```bash
npm ci      # or make clients from the repository root
npm test    # drives a real Faultline; needs ./bin/faultline, built by make build
npm run lint
```

`FAULTLINE_BIN` points the tests at a binary somewhere else.
