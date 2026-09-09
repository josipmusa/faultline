import { useState } from "react";

type Result =
  | { state: "idle" }
  | { state: "loading" }
  | { state: "done"; status: number; durationMs: number; body: string }
  | { state: "failed"; durationMs: number; message: string };

/**
 * One button, one call to /api/get, and an honest view of what came back. The
 * loading state is deliberately plain and visible: with a delay fault in front
 * of the upstream it is the thing you watch.
 */
export function App() {
  const [result, setResult] = useState<Result>({ state: "idle" });

  async function call() {
    setResult({ state: "loading" });
    const started = performance.now();
    try {
      const response = await fetch("/api/get");
      const body = await response.text();
      setResult({
        state: "done",
        status: response.status,
        durationMs: Math.round(performance.now() - started),
        body,
      });
    } catch (error) {
      setResult({
        state: "failed",
        durationMs: Math.round(performance.now() - started),
        message: error instanceof Error ? error.message : String(error),
      });
    }
  }

  return (
    <main>
      <h1>vite-frontend</h1>
      <p>
        Calls <code>/api/get</code>, which the Vite dev server forwards to
        whatever <code>API_TARGET</code> points at.
      </p>

      <button type="button" onClick={call} disabled={result.state === "loading"}>
        {result.state === "loading" ? "Calling…" : "Call the API"}
      </button>

      <Outcome result={result} />
    </main>
  );
}

function Outcome({ result }: { result: Result }) {
  switch (result.state) {
    case "idle":
      return <p className="muted">Nothing called yet.</p>;
    case "loading":
      return <p className="muted">Waiting for the response…</p>;
    case "done":
      return (
        <section>
          <p>
            <strong>{result.status}</strong> in {result.durationMs} ms
          </p>
          <pre>{result.body}</pre>
        </section>
      );
    case "failed":
      return (
        <section>
          <p className="failed">
            Failed after {result.durationMs} ms: {result.message}
          </p>
        </section>
      );
  }
}
