'use client';

import { useEffect, useState } from 'react';
import { Loader2, TriangleAlert, X } from 'lucide-react';

import { ApiError, createRule, getRule, updateRule } from '@/lib/api';
import {
  blankForm,
  blankParams,
  entryFor,
  formToRule,
  ruleToForm,
  type Params,
  type ParamValue,
  type RuleForm as FormState,
} from '@/lib/ruleForm';
import { cn } from '@/lib/utils';
import type { Catalogue, Rule } from '@/types';

import { inputClass, Labelled, PairRows, ParamFields } from './RuleFields';

interface RuleFormProps {
  catalogue: Catalogue;
  /** The rule being edited, or null for a new one. */
  rule: Rule | null;
  onClose: () => void;
  /** A rule was written. The panel re-reads and follows the rule, whose id the
   * API may have derived from the name. */
  onSaved: (id: string) => void;
}

export function RuleForm({ catalogue, rule, onClose, onSaved }: RuleFormProps) {
  const [form, setForm] = useState<FormState>(() =>
    rule ? ruleToForm(rule, catalogue) : blankForm(catalogue),
  );
  const [saving, setSaving] = useState(false);
  const [failure, setFailure] = useState<ApiError | null>(null);
  const [warnings, setWarnings] = useState<string[]>([]);

  // The list has no warnings on it, only the single-rule endpoints do, so an
  // existing rule is read again to learn whether its fault can apply at all.
  useEffect(() => {
    if (!rule) {
      return;
    }
    let live = true;
    getRule(rule.id).then(
      (read) => {
        if (live) setWarnings(read.warnings ?? []);
      },
      () => {
        // The rule is on screen from the list either way; a failed read here
        // costs only the warning, which is not worth an error of its own.
      },
    );
    return () => {
      live = false;
    };
  }, [rule]);

  const faultEntry = entryFor(catalogue.faults, form.faultType);
  const behaviorEntry = entryFor(catalogue.behaviors, form.behaviorType);
  const unknownFault = form.faultType !== '' && !faultEntry;

  const save = async () => {
    setSaving(true);
    setFailure(null);
    try {
      const payload = formToRule(form, catalogue);
      const saved = rule
        ? await updateRule(rule.id, { ...payload, id: rule.id })
        : await createRule(payload);
      setWarnings(saved.warnings ?? []);
      onSaved(saved.id);
    } catch (err: unknown) {
      setFailure(err instanceof ApiError ? err : new ApiError(String(err), 0));
    } finally {
      setSaving(false);
    }
  };

  const set = (patch: Partial<FormState>) => setForm((f) => ({ ...f, ...patch }));
  const setParam = (which: 'faultParams' | 'behaviorParams') => (name: string, value: ParamValue) =>
    setForm((f) => ({ ...f, [which]: { ...f[which], [name]: value } as Params }));

  return (
    <aside className="flex w-[32rem] shrink-0 flex-col overflow-hidden border-l border-zinc-800 bg-zinc-950">
      <header className="flex items-start justify-between gap-3 border-b border-zinc-800 px-5 py-4">
        <div className="min-w-0">
          <p className="truncate text-sm text-zinc-100">{rule ? 'Edit rule' : 'New rule'}</p>
          {rule && <p className="mt-1 truncate font-mono text-xs text-zinc-500">{rule.id}</p>}
        </div>
        <button
          type="button"
          aria-label="Close"
          onClick={onClose}
          className="cursor-pointer rounded-md p-1 text-zinc-500 transition-colors hover:bg-zinc-800 hover:text-zinc-200"
        >
          <X className="h-4 w-4" />
        </button>
      </header>

      <form
        className="flex-1 space-y-5 overflow-auto px-5 py-4"
        onSubmit={(e) => {
          e.preventDefault();
          void save();
        }}
      >
        {/* An error the form cannot point at, because the API named a field
            that is not on screen or named none at all. */}
        {failure && !onScreen(failure.field, form) && (
          <Notice tone="error" text={failure.message} />
        )}
        {warnings.map((warning) => (
          <Notice key={warning} tone="warning" text={warning} />
        ))}
        {unknownFault && (
          <Notice
            tone="warning"
            text={`This rule uses the fault "${form.faultType}", which this build does not have. Saving it as it stands would drop its parameters; pick another fault or leave the rule alone.`}
          />
        )}

        <Section title="Rule">
          <Labelled label="name" required error={fieldError(failure, 'name')}>
            <input
              type="text"
              value={form.name}
              onChange={(e) => set({ name: e.target.value })}
              className={inputClass(failure?.field === 'name')}
            />
          </Labelled>
          <Labelled
            label="id"
            hint={rule ? 'The id never changes; everything else names the rule by it.' : 'Left empty, Faultline derives one from the name.'}
            error={fieldError(failure, 'id')}
          >
            <input
              type="text"
              value={form.id}
              disabled={Boolean(rule)}
              onChange={(e) => set({ id: e.target.value })}
              className={cn(inputClass(failure?.field === 'id'), rule && 'opacity-50')}
            />
          </Labelled>
        </Section>

        <Section title="Match" hint="Everything left empty matches everything.">
          <Labelled label="host" hint="A hostname like api.stripe.com, not a URL." error={fieldError(failure, 'match.host')}>
            <input
              type="text"
              value={form.match.host}
              onChange={(e) => set({ match: { ...form.match, host: e.target.value } })}
              className={inputClass(failure?.field === 'match.host')}
            />
          </Labelled>
          <Labelled label="method" error={fieldError(failure, 'match.method')}>
            <input
              type="text"
              value={form.match.method}
              placeholder="GET"
              onChange={(e) => set({ match: { ...form.match, method: e.target.value } })}
              className={inputClass(failure?.field === 'match.method')}
            />
          </Labelled>
          <Labelled
            label="path"
            hint="A path, with * standing in for the rest: /v1/charges/*."
            error={fieldError(failure, 'match.path')}
          >
            <input
              type="text"
              value={form.match.path}
              onChange={(e) => set({ match: { ...form.match, path: e.target.value } })}
              className={inputClass(failure?.field === 'match.path')}
            />
          </Labelled>
          <Labelled label="header" hint="Header names and the values a request must carry." error={fieldError(failure, 'match.header')}>
            <PairRows
              rows={form.match.header}
              nameLabel="match header"
              invalid={failure?.field === 'match.header'}
              onChange={(header) => set({ match: { ...form.match, header } })}
            />
          </Labelled>
        </Section>

        <Section title="Fault" hint="What happens to the traffic this rule matches.">
          <Labelled label="type" required error={fieldError(failure, 'fault.type')}>
            <select
              value={form.faultType}
              onChange={(e) => {
                const entry = entryFor(catalogue.faults, e.target.value);
                set({ faultType: e.target.value, faultParams: blankParams(entry) });
              }}
              className={inputClass(failure?.field === 'fault.type')}
            >
              {unknownFault && <option value={form.faultType}>{form.faultType} (not in this build)</option>}
              {catalogue.faults.map((entry) => (
                <option key={entry.name} value={entry.name}>
                  {entry.name} · {entry.tier}
                </option>
              ))}
            </select>
          </Labelled>
          {faultEntry?.tier === 'response' && (
            <p className="text-xs text-zinc-500">
              A response fault needs Faultline to see inside the request, so it cannot apply to traffic
              that stays encrypted.
            </p>
          )}
          <ParamFields
            entry={faultEntry}
            params={form.faultParams}
            scope="fault"
            badField={failure?.field}
            badMessage={failure?.message}
            onChange={setParam('faultParams')}
          />
        </Section>

        <Section title="Behavior" hint="When the fault applies. Without one it applies to everything the rule matches.">
          <Labelled label="type" error={fieldError(failure, 'behavior.type')}>
            <select
              value={form.behaviorType}
              onChange={(e) => {
                const entry = entryFor(catalogue.behaviors, e.target.value);
                set({ behaviorType: e.target.value, behaviorParams: blankParams(entry) });
              }}
              className={inputClass(failure?.field === 'behavior.type')}
            >
              <option value="">always</option>
              {catalogue.behaviors.map((entry) => (
                <option key={entry.name} value={entry.name}>
                  {entry.name}
                </option>
              ))}
            </select>
          </Labelled>
          <ParamFields
            entry={behaviorEntry}
            params={form.behaviorParams}
            scope="behavior"
            badField={failure?.field}
            badMessage={failure?.message}
            onChange={setParam('behaviorParams')}
          />
        </Section>

        <label className="flex cursor-pointer items-center gap-2 text-sm text-zinc-300">
          <input
            type="checkbox"
            checked={form.enabled}
            onChange={(e) => set({ enabled: e.target.checked })}
            className="h-4 w-4 cursor-pointer accent-amber-500"
          />
          Enabled
        </label>
      </form>

      <footer className="flex items-center justify-end gap-2 border-t border-zinc-800 px-5 py-3">
        <button
          type="button"
          onClick={onClose}
          className="cursor-pointer rounded-md border border-zinc-700 bg-zinc-800/60 px-3 py-1.5 text-xs font-medium text-zinc-300 transition-colors hover:bg-zinc-700"
        >
          Cancel
        </button>
        <button
          type="button"
          onClick={() => void save()}
          disabled={saving}
          className={cn(
            'inline-flex cursor-pointer items-center gap-1.5 rounded-md border border-amber-500/40 bg-amber-500/15 px-3 py-1.5 text-xs font-medium text-amber-300 transition-colors hover:bg-amber-500/25',
            saving && 'cursor-not-allowed opacity-50',
          )}
        >
          {saving && <Loader2 className="h-3 w-3 animate-spin" />}
          {rule ? 'Save' : 'Create'}
        </button>
      </footer>
    </aside>
  );
}

function Section({ title, hint, children }: { title: string; hint?: string; children: React.ReactNode }) {
  return (
    <section className="space-y-3">
      <div>
        <h2 className="text-xs font-medium tracking-wide text-zinc-400 uppercase">{title}</h2>
        {hint && <p className="mt-0.5 text-xs text-zinc-500">{hint}</p>}
      </div>
      {children}
    </section>
  );
}

function Notice({ tone, text }: { tone: 'error' | 'warning'; text: string }) {
  return (
    <p
      className={cn(
        'flex items-start gap-2 rounded-md border px-3 py-2 text-xs',
        tone === 'error'
          ? 'border-red-500/20 bg-red-500/10 text-red-400'
          : 'border-amber-500/20 bg-amber-500/10 text-amber-300',
      )}
    >
      <TriangleAlert className="mt-0.5 h-3.5 w-3.5 shrink-0" />
      <span>{text}</span>
    </p>
  );
}

function fieldError(failure: ApiError | null, path: string): string | undefined {
  return failure?.field === path ? failure.message : undefined;
}

/** Whether the field the API rejected has a control on screen to be marked.
 * A parameter of a fault that is no longer selected, or a field this form does
 * not render, has none, and the message goes to the top instead of nowhere. */
function onScreen(field: string | undefined, form: FormState): boolean {
  if (!field) {
    return false;
  }
  const [scope, name] = field.split('.');
  if (!name) {
    return field === 'id' || field === 'name';
  }
  if (scope === 'match') {
    return ['host', 'method', 'path', 'header'].includes(name);
  }
  if (scope === 'fault') {
    return name === 'type' || name in form.faultParams;
  }
  if (scope === 'behavior') {
    return name === 'type' || name in form.behaviorParams;
  }
  return false;
}
