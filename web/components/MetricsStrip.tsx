'use client';

import { Line, LineChart, ResponsiveContainer, XAxis, YAxis } from 'recharts';

import type { Point } from '@/lib/metrics';

interface MetricsStripProps {
  points: Point[];
  /** Said under the charts when the stream's filters are narrowing what they
   * plot, so a quiet line is not read as quiet traffic. */
  filtering: boolean;
}

/** The last sixty seconds of traffic as a shape: a delay lifts the latency
 * line and drops the throughput one, which is the thing a number alone does
 * not show.
 *
 * Two panels rather than one chart with two axes. Requests per second and
 * milliseconds share no scale, so overlaying them makes the picture an
 * accident of whatever ranges the axes happened to pick. */
export function MetricsStrip({ points, filtering }: MetricsStripProps) {
  const last = points[points.length - 1];
  const latest = [...points].reverse().find((point) => point.avg_ms !== null);

  return (
    <div className="grid grid-cols-2 gap-px border-b border-zinc-800 bg-zinc-800">
      <Panel
        label="Requests/sec"
        value={last ? last.requests.toFixed(0) : '0'}
        points={points}
        dataKey="requests"
        stroke="#38bdf8"
        filtering={filtering}
      />
      <Panel
        label="Avg latency"
        value={latest?.avg_ms == null ? '-' : `${latest.avg_ms.toFixed(0)}ms`}
        points={points}
        dataKey="avg_ms"
        stroke="var(--brand)"
        dots
        filtering={filtering}
      />
    </div>
  );
}

interface PanelProps {
  label: string;
  value: string;
  points: Point[];
  dataKey: 'requests' | 'avg_ms';
  stroke: string;
  /** Marks each second that has a value. The latency line needs it: a delay
   * long enough to be worth injecting can leave whole seconds with no request
   * in them, and a lone second between two gaps is a point rather than a
   * segment, so without a mark it draws nothing at all. Throughput never has
   * gaps - a second with no requests is zero, not unknown - so a mark there
   * would only be noise. */
  dots?: boolean;
  filtering: boolean;
}

function Panel({ label, value, points, dataKey, stroke, dots, filtering }: PanelProps) {
  const domain = points.length > 0 ? [points[0].start, points[points.length - 1].start] : [0, 1];

  return (
    <div className="bg-zinc-950/50 px-4 py-2">
      <div className="mb-1 flex items-baseline justify-between">
        <span className="text-xs text-zinc-400">
          {label}
          {filtering && <span className="text-zinc-600"> (filtered)</span>}
        </span>
        <span className="font-mono text-sm text-zinc-200">{value}</span>
      </div>
      <div className="h-10">
        <ResponsiveContainer width="100%" height="100%">
          <LineChart data={points} margin={{ top: 2, right: 0, bottom: 0, left: 0 }}>
            {/* The x axis is pinned to the window rather than to the data, so
              * the line scrolls leftward at a steady rate instead of stretching
              * to fill whatever happens to be on screen. */}
            <XAxis dataKey="start" type="number" domain={domain} hide />
            {/* Zero is always the floor, so a line's height means the same
              * thing from one glance to the next, and the top of the data is
              * kept off the top of the panel: a steady value drawn flush
              * against the edge reads as a line that has been clipped. */}
            <YAxis domain={[0, (max: number) => Math.max(1, max * 1.3)]} hide />
            <Line
              type="monotone"
              dataKey={dataKey}
              stroke={stroke}
              strokeWidth={2}
              dot={dots ? { r: 1.5, fill: stroke, strokeWidth: 0 } : false}
              // A second with no requests has no latency, so the line breaks
              // there rather than drawing a dip to zero that never happened.
              connectNulls={false}
              isAnimationActive={false}
            />
          </LineChart>
        </ResponsiveContainer>
      </div>
    </div>
  );
}
