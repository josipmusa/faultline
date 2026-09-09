// Calls one URL in a loop with Node's native fetch and logs what happened.
// Native fetch is the interesting case for Faultline: unlike most HTTP
// clients it does not read the proxy environment variables on its own, so
// this example is what the wrapper's Node support is tested against.
import { parseArgs } from "node:util";

const { values } = parseArgs({
  options: {
    url: { type: "string", default: "https://httpbin.org/get" },
    every: { type: "string", default: "2000" },
    count: { type: "string", default: "0" },
    timeout: { type: "string", default: "30000" },
  },
});

const url = values.url;
const every = Number(values.every);
const count = Number(values.count);
const timeout = Number(values.timeout);

for (const [name, value] of Object.entries({ every, count, timeout })) {
  if (!Number.isFinite(value) || value < 0) {
    console.error(`--${name} must be a number of milliseconds`);
    process.exit(2);
  }
}

// Interrupts end the loop rather than the process, so a call in flight is
// still logged.
let running = true;
process.on("SIGINT", () => {
  running = false;
});

const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms));

// One request, read to the end so the reported duration covers the whole
// response and not just its headers.
async function call() {
  const response = await fetch(url, { signal: AbortSignal.timeout(timeout) });
  const body = await response.arrayBuffer();
  return { status: response.status, size: body.byteLength };
}

console.log(`calling ${url} every ${every}ms`);

for (let n = 1; running && (count === 0 || n <= count); n++) {
  if (n > 1) {
    await sleep(every);
    if (!running) break;
  }

  const started = performance.now();
  try {
    const { status, size } = await call();
    const elapsed = Math.round(performance.now() - started);
    console.log(`call ${n}: ${status}, ${size} bytes, ${elapsed}ms`);
  } catch (error) {
    // A fault injected upstream is the point of the exercise, so the loop
    // logs the failure and carries on.
    const elapsed = Math.round(performance.now() - started);
    console.log(`call ${n} failed after ${elapsed}ms: ${error.message}`);
  }
}
