'use client';

import { useState } from 'react';
import { Layers, Play, Plus, Square, X } from 'lucide-react';

import { createScenario, setScenarioActive } from '@/lib/api';
import { panelState } from '@/lib/panelState';
import { enabledRuleIDs, reportTiles, resolveScenarioRules, scenarioSaveBlocked } from '@/lib/scenarios';
import { cn } from '@/lib/utils';
import type { Report, Rule, Scenario } from '@/types';

import { Badge } from './ui/Badge';
import { Button } from './ui/Button';
import { EmptyState } from './ui/EmptyState';
import { Notice } from './ui/Notice';
import { Toolbar } from './ui/Toolbar';

interface ScenariosPanelProps {
  /** Null until the first read answers. */
  scenarios: Scenario[] | null;
  /** The rules as the stream keeps them, which is where a row reads what its
   * ids are called and whether each one is on right now. */
  rules: Rule[];
  report: Report | null;
  /** Set when the panel's reads failed, so it can say why rather than look
   * like a session with nothing in it. */
  error: string | null;
  /** Re-reads the scenarios and the report, after an action changed either. */
  onChanged: () => void;
}

export function ScenariosPanel({ scenarios, rules, report, error, onChanged }: ScenariosPanelProps) {
  const [pending, setPending] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const [creating, setCreating] = useState(false);

  const act = async (name: string, run: () => Promise<unknown>) => {
    setPending(name);
    setNotice(null);
    try {
      await run();
      onChanged();
      return true;
    } catch (err: unknown) {
      setNotice(err instanceof Error ? err.message : String(err));
      return false;
    } finally {
      setPending(null);
    }
  };

  const toggle = (scenario: Scenario) =>
    act(scenario.name, () => setScenarioActive(scenario.name, !scenario.active));

  const state = panelState(scenarios, error);
  const rows = scenarios ?? [];

  return (
    <div className="flex flex-1 flex-col overflow-auto">
      <SessionReport report={report} />

      <Toolbar
        summary={
          state === 'loading' || state === 'error'
            ? 'Scenarios'
            : rows.length === 0
              ? 'No scenarios yet'
              : `${rows.length} scenario${rows.length === 1 ? '' : 's'}, one on at a time`
        }
      >
        <Button
          variant="primary"
          icon={Plus}
          onClick={() => {
            setNotice(null);
            setCreating(true);
          }}
          disabled={creating}
        >
          New scenario
        </Button>
      </Toolbar>

      {error && (
        <Notice tone="error" className="mx-4 mt-3">
          The scenarios and the report could not be read: {error}
        </Notice>
      )}

      {creating && (
        <NewScenario
          rules={rules}
          busy={pending === newScenarioKey}
          onCancel={() => setCreating(false)}
          onCreate={async (name, ids) => {
            if (await act(newScenarioKey, () => createScenario({ name, rules: ids }))) {
              setCreating(false);
            }
          }}
        />
      )}

      {notice && (
        <Notice tone="error" className="mx-4 mt-3">
          {notice}
        </Notice>
      )}

      {state === 'loading' && <EmptyState>Loading…</EmptyState>}
      {state === 'empty' && (
        <EmptyState>
          No scenarios yet. Arrange the rules for a situation, then save them as one so you can rehearse it
          again with one click or one command.
        </EmptyState>
      )}
      {state === 'rows' && (
        <ul>
          {rows.map((scenario) => (
            <Row
              key={scenario.name}
              scenario={scenario}
              rules={rules}
              busy={pending === scenario.name}
              onToggle={() => void toggle(scenario)}
            />
          ))}
        </ul>
      )}
    </div>
  );
}

/** The key the create action is pending under. A scenario name cannot be
 * empty, so no row can collide with it. */
const newScenarioKey = '';

function SessionReport({ report }: { report: Report | null }) {
  return (
    <section className="border-b border-zinc-800 px-4 py-3">
      <div className="mb-2 flex flex-wrap items-baseline gap-x-3 gap-y-1">
        <h2 className="text-sm font-medium text-zinc-300">This session</h2>
        <p className="text-xs text-zinc-500">
          Everything Faultline has seen since it started, or since the last reset. Reset session, in
          the header, starts these over.
        </p>
      </div>

      <dl className="flex flex-wrap gap-2">
        {report === null
          ? reportTiles({ total: 0, faulted: 0, retries: 0, max_retry_wait_ms: 0, abandoned: 0 }).map(
              (tile) => (
                <div key={tile.label} className="min-w-32 rounded-lg border border-zinc-800 bg-zinc-950 px-3 py-2">
                  <dt className="text-xs text-zinc-500">{tile.label}</dt>
                  <dd className="font-mono text-lg text-zinc-700">&ndash;</dd>
                </div>
              ),
            )
          : reportTiles(report).map((tile) => (
              <div
                key={tile.label}
                title={tile.hint}
                className="min-w-32 rounded-lg border border-zinc-800 bg-zinc-950 px-3 py-2"
              >
                <dt className="text-xs text-zinc-500">{tile.label}</dt>
                <dd
                  className={cn(
                    'font-mono text-lg',
                    tile.label === 'Faulted' && report.faulted > 0 ? 'text-amber-300' : 'text-zinc-200',
                  )}
                >
                  {tile.value}
                </dd>
              </div>
            ))}
      </dl>

      {report?.warnings?.map((warning) => (
        <Notice key={warning} tone="warning" className="mt-2">
          {warning}
        </Notice>
      ))}
    </section>
  );
}

interface RowProps {
  scenario: Scenario;
  rules: Rule[];
  busy: boolean;
  onToggle: () => void;
}

function Row({ scenario, rules, busy, onToggle }: RowProps) {
  const named = resolveScenarioRules(scenario, rules);

  return (
    <li
      className={cn(
        'flex items-center gap-4 border-b border-zinc-800/50 px-4 py-3',
        scenario.active && 'bg-amber-500/5',
      )}
    >
      {scenario.active && <span className="-ml-4 h-10 w-1 rounded-r bg-amber-400" />}

      <Layers className={cn('h-4 w-4 shrink-0', scenario.active ? 'text-amber-400' : 'text-zinc-600')} aria-hidden />

      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-2">
          <span className="truncate font-medium text-zinc-200">{scenario.name}</span>
          {scenario.active && <Badge tone="amber">Active</Badge>}
        </div>
        <p className="mt-0.5 truncate font-mono text-xs text-zinc-500">
          {named.length === 0
            ? 'names no rules'
            : named.map((rule) => `${rule.name}${rule.enabled ? '' : ' (off)'}`).join(', ')}
        </p>
      </div>

      <Button
        variant={scenario.active ? 'secondary' : 'primary'}
        icon={scenario.active ? Square : Play}
        onClick={onToggle}
        busy={busy}
        className="w-24"
        title={
          scenario.active
            ? `Turn ${scenario.name} off, switching its rules off`
            : `Turn ${scenario.name} on, switching its rules on and starting their behavior over`
        }
      >
        {scenario.active ? 'Turn off' : 'Turn on'}
      </Button>
    </li>
  );
}

interface NewScenarioProps {
  rules: Rule[];
  busy: boolean;
  onCancel: () => void;
  onCreate: (name: string, ids: string[]) => void;
}

/** The form opens on the rules that are on, which is the case it exists for:
 * somebody has arranged a situation by hand and wants it named, committed, and
 * rehearsed again later. */
function NewScenario({ rules, busy, onCancel, onCreate }: NewScenarioProps) {
  const [name, setName] = useState('');
  const [selected, setSelected] = useState<string[]>(() => enabledRuleIDs(rules));

  const toggle = (id: string) =>
    setSelected((ids) => (ids.includes(id) ? ids.filter((other) => other !== id) : [...ids, id]));

  const blocked = scenarioSaveBlocked(name, selected);

  return (
    <form
      onSubmit={(e) => {
        e.preventDefault();
        // The order the rules are written in is the store's, not the order
        // they happened to be ticked in: order is precedence, so a scenario
        // should name them the way they will be applied.
        onCreate(
          name.trim(),
          rules.filter((rule) => selected.includes(rule.id)).map((rule) => rule.id),
        );
      }}
      className="border-b border-zinc-800 bg-zinc-950/60 px-4 py-3"
    >
      <div className="flex items-center gap-2">
        <label htmlFor="scenario-name" className="text-xs text-zinc-400">
          Name
        </label>
        <input
          id="scenario-name"
          value={name}
          onChange={(e) => setName(e.target.value)}
          placeholder="payments-down"
          autoFocus
          className="w-64 rounded-md border border-zinc-700 bg-zinc-900 px-2 py-1 font-mono text-sm text-zinc-200 placeholder:text-zinc-600 focus:border-amber-500/60"
        />
        {blocked && name.trim() !== '' && <span className="text-xs text-zinc-500">{blocked}</span>}
        <div className="flex-1" />
        <Button
          type="submit"
          variant="primary"
          icon={Plus}
          busy={busy}
          disabled={blocked !== null}
          title={blocked ?? 'Write this scenario to the config file'}
        >
          Save scenario
        </Button>
        <Button variant="ghost" icon={X} aria-label="Cancel" title="Cancel" onClick={onCancel} />
      </div>

      <p className="mt-2 text-xs text-zinc-500">
        The rules this scenario turns on, at least one. It starts out ticked for whatever is on now, and
        creating it changes nothing until you turn it on.
      </p>

      {rules.length === 0 ? (
        <p className="mt-2 text-xs text-zinc-500">
          There are no rules to name yet. A scenario needs at least one: add rules first, then come back.
        </p>
      ) : (
        <ul className="mt-2 flex flex-wrap gap-2">
          {rules.map((rule) => (
            <li key={rule.id}>
              <label
                className={cn(
                  'flex cursor-pointer items-center gap-2 rounded-md border px-2 py-1 text-xs transition-colors',
                  selected.includes(rule.id)
                    ? 'border-amber-500/40 bg-amber-500/10 text-zinc-200'
                    : 'border-zinc-700 bg-zinc-900 text-zinc-400 hover:border-zinc-600',
                )}
              >
                <input
                  type="checkbox"
                  checked={selected.includes(rule.id)}
                  onChange={() => toggle(rule.id)}
                  className="cursor-pointer accent-amber-400"
                />
                <span className="max-w-48 truncate">{rule.name}</span>
                <span className="font-mono text-zinc-600">{rule.id}</span>
              </label>
            </li>
          ))}
        </ul>
      )}
    </form>
  );
}
