import { spawn, type ChildProcessByStdio } from 'node:child_process';
import type { Readable } from 'node:stream';
import { once } from 'node:events';
import { existsSync } from 'node:fs';
import { createServer, get, type Server } from 'node:http';
import { createServer as createSocketServer, type AddressInfo } from 'node:net';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));

/** The spawned binary: no stdin of its own, both output streams read. */
type Child = ChildProcessByStdio<null, Readable, Readable>;

/** The binary under test, built by `make build`. */
export const BINARY = process.env.FAULTLINE_BIN ?? resolve(here, '../../../bin/faultline');

/**
 * A Faultline started by the test: the admin address the client talks to, and
 * the address the application sends its calls to.
 *
 * The application is attached by an explicit route rather than by the forward
 * proxy, because the upstream a test spins up is on loopback and loopback is
 * always on the proxy's bypass list - Faultline must never proxy itself. A
 * route is the attach mode for an upstream you can name, and it goes through
 * the same rules and records the same events.
 */
export interface Faultline {
  admin: string;
  route: string;
  stop(): Promise<void>;
}

export async function startFaultline(upstream: Upstream, args: string[] = []): Promise<Faultline> {
  if (!existsSync(BINARY)) {
    throw new Error(`${BINARY} does not exist; build it with \`make build\` or point FAULTLINE_BIN at one`);
  }

  // The admin and proxy ports are left to the operating system, so a suite
  // never fights the instance a developer is already running on the defaults.
  // A route needs a port named up front, so one nothing is on is borrowed.
  const routePort = await freePort();
  const child = spawn(
    BINARY,
    [
      'serve',
      '--admin-port', '0',
      '--proxy-port', '0',
      '--intercept=false',
      '--route', `up=${upstream.url}`,
      '--route-port', `up=${routePort}`,
      ...args,
    ],
    { stdio: ['ignore', 'pipe', 'pipe'] },
  );

  let output = '';
  child.stdout.setEncoding('utf8');
  child.stderr.setEncoding('utf8');
  child.stdout.on('data', (chunk: string) => (output += chunk));
  child.stderr.on('data', (chunk: string) => (output += chunk));

  try {
    const admin = await waitForLine(child, () => output, 'admin: ');
    const route = await waitForLine(child, () => output, 'route up: http://');
    return { admin, route, stop: () => stop(child) };
  } catch (cause) {
    await stop(child);
    throw new Error(`faultline did not start:\n${output}`, { cause });
  }
}

/** Reads an address out of the banner, waiting for the line to appear. */
async function waitForLine(
  child: Child,
  output: () => string,
  prefix: string,
): Promise<string> {
  const deadline = Date.now() + 10_000;
  for (;;) {
    const line = output()
      .split('\n')
      .find((candidate) => candidate.startsWith(prefix));
    if (line !== undefined) {
      return line.slice(prefix.length).split(' ')[0]!.trim();
    }
    if (child.exitCode !== null || Date.now() > deadline) {
      throw new Error(`no line starting ${JSON.stringify(prefix)}`);
    }
    await new Promise((r) => setTimeout(r, 20));
  }
}

async function stop(child: Child): Promise<void> {
  if (child.exitCode !== null) {
    return;
  }
  child.kill('SIGINT');
  await Promise.race([once(child, 'exit'), new Promise((r) => setTimeout(r, 5000))]);
  child.kill('SIGKILL');
}

/** A port nothing is listening on, for the one address that cannot be left to chance. */
async function freePort(): Promise<number> {
  const probe = createSocketServer();
  probe.listen(0, '127.0.0.1');
  await once(probe, 'listening');
  const { port } = probe.address() as AddressInfo;
  await new Promise<void>((done, fail) => probe.close((err) => (err ? fail(err) : done())));
  return port;
}

/** An upstream to break: a plain HTTP server on a port nothing else is on. */
export interface Upstream {
  host: string;
  url: string;
  stop(): Promise<void>;
}

export async function startUpstream(): Promise<Upstream> {
  const server: Server = createServer((_req, res) => {
    res.writeHead(200, { 'content-type': 'text/plain' });
    res.end('upstream');
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');

  const { port } = server.address() as AddressInfo;
  const host = `127.0.0.1:${port}`;
  return {
    host,
    url: `http://${host}`,
    stop: () => new Promise((done, fail) => server.close((err) => (err ? fail(err) : done()))),
  };
}

/** One call from the application's side, which is a plain request to the route. */
export function call(
  route: string,
  path: string,
): Promise<{ status: number; headers: Record<string, string | string[] | undefined> }> {
  return new Promise((done, fail) => {
    const req = get(`http://${route}${path}`, (res) => {
      res.resume();
      res.on('end', () => done({ status: res.statusCode ?? 0, headers: res.headers }));
    });
    req.on('error', fail);
  });
}
