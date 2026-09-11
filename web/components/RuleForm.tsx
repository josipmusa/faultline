'use client';

import { useEffect, useRef, useState } from 'react';

import { ApiError, createRule, getRule, updateRule } from '@/lib/api';
import {
  blankForm,
  blankParams,
  entryFor,
  formToRule,
  groupByTier,
  ruleToForm,
  type Params,
  type ParamValue,
  type RuleForm as FormState,
} from '@/lib/ruleForm';
import { cn } from '@/lib/utils';
import type { Catalogue, Rule } from '@/types';

import { inputClass, Labelled, PairRows, ParamFields } from './RuleFields';
import { Button } from './ui/Button';
import { Notice } from './ui/Notice';
import { SidePanel } from './ui/SidePanel';
import { Switch } from './ui/Switch';

interface RuleFormProps {
  catalogue: Catalogue;
  /** The rule being edited, or null for a new one. */
  rule: Rule | null;
  onClose: () => void;
  /** A rule was written. The panel re-reads and follows the rule, whose id the
   * API may have derived from the name. */
  onSaved: (id: string) => void;
}

const formId = 'rule-form';

export function RuleForm({ catalogue, rule, onClose, onSaved }: RuleFormProps) {
  const [form, setForm] = useState<FormState>(() =>
    rule ? ruleToForm(rule, catalogue) : blankForm(catalogue),
  );
  const [saving, setSaving] = useState(false);
  const [failure, setFailure] = useState<ApiError | null>(null);
  const [warnings, setWarnings] = useState<string[]>([]);
  const body = useRef<HTMLFormElement>(null);

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

  // A rejected field may be scrolled out of view in a long form, so the
  // complaint under it would go unseen. Bring it on screen and put the cursor
  // in it, which is where the fix has to be typed anyway.
  useEffect(() => {
    if (!failure?.field) {
      return;
    }
    const holder = body.current?.querySelector<HTMLElement>(`[data-field="${failure.field}"]`);
    if (!holder) {
      return;
    }
    holder.scrollIntoView({ block: 'center' });
    holder.querySelector<HTMLElement>('input, select')?.focus();
  }, [failure]);

  const faultEntry = entryFor(catalogue.faults, form.faultType);
  const behaviorEntry = entryFor(catalogue.behaviors, form.behaviorType);
  const unknownFault = form.faultType !== '' && !faultEntry;
  const groups = groupByTier(catalogue.faults);

  const save = async () => {
    setSaving(true);
    setFailure(null);
    try {
      const payload = formToRule(form, catalogue);
      const written = rule
        ? await updateRule(rule.id, { ...payload, id: rule.id })
        : await createRule(payload);
      setWarnings(written.warnings ?? []);
      onSaved(written.id);
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
    <SidePanel
      onClose={onClose}
      title={rule ? 'Edit rule' : 'New rule'}
      subtitle={rule && <span className="font-mono">{rule.id}</span>}
      footer={
        <>
          <Button onClick={onClose}>Cancel</Button>
          {/* A submit bound to the form by id: the footer sits outside the
              form so it stays put while the fields scroll, and Enter in a
              field and this button have to be the same action. */}
          <Button type="submit" form={formId} variant="primary" busy={saving}>
            {rule ? 'Save' : 'Create'}
          </Button>
        </>
      }
    >
      <form
        id={formId}
        ref={body}
        className="flex-1 space-y-5 overflow-auto px-4 py-4"
        onSubmit={(e) => {
          e.preventDefault();
          void save();
        }}
      >
        {/* An error the form cannot point at, because the API named a field
            that is not on screen or named none at all. */}
        {failure && !onScreen(failure.field, form) && <Notice tone="error">{failure.message}</Notice>}
        {warnings.map((warning) => (
          <Notice key={warning} tone="warning">
            {warning}
          </Notice>
        ))}
        {unknownFault && (
          <Notice tone="warning">
            This rule uses the fault &quot;{form.faultType}&quot;, which this build does not have. Saving it as it
            stands would drop its parameters; pick another fault or leave the rule alone.
          </Notice>
        )}

        <Section title="Rule">
          <div className="flex items-end gap-3">
            <div className="min-w-0 flex-1">
              <Labelled label="name" path="name" required error={fieldError(failure, 'name')}>
                <input
                  type="text"
                  value={form.name}
                  placeholder="Stripe is slow"
                  // The first field takes the cursor when the panel opens, so
                  // a new rule can be typed without a click first.
                  autoFocus
                  onChange={(e) => set({ name: e.target.value })}
                  className={inputClass(failure?.field === 'name')}
                />
              </Labelled>
            </div>
            <label className="flex shrink-0 cursor-pointer flex-col items-start gap-1 pb-px">
              <span className="font-mono text-xs text-zinc-300">enabled</span>
              <Switch
                on={form.enabled}
                onToggle={() => set({ enabled: !form.enabled })}
                aria-label="Enabled"
                title={form.enabled ? 'On as soon as it is saved' : 'Saved but off until switched on'}
              />
            </label>
          </div>
          <Labelled
            label="id"
            path="id"
            hint={rule ? 'The id never changes; everything else names the rule by it.' : 'Left empty, Faultline derives one from the name.'}
            error={fieldError(failure, 'id')}
          >
            <input
              type="text"
              value={form.id}
              disabled={Boolean(rule)}
              placeholder={rule ? undefined : 'stripe-slow'}
              onChange={(e) => set({ id: e.target.value })}
              className={cn(inputClass(failure?.field === 'id'), 'font-mono', rule && 'opacity-50')}
            />
          </Labelled>
        </Section>

        <Section title="Match" hint="Everything left empty matches everything.">
          <Labelled
            label="host"
            path="match.host"
            hint="A hostname like api.stripe.com, not a URL."
            error={fieldError(failure, 'match.host')}
          >
            <input
              type="text"
              value={form.match.host}
              placeholder="api.stripe.com"
              onChange={(e) => set({ match: { ...form.match, host: e.target.value } })}
              className={cn(inputClass(failure?.field === 'match.host'), 'font-mono')}
            />
          </Labelled>
          <Labelled label="method" path="match.method" error={fieldError(failure, 'match.method')}>
            <input
              type="text"
              value={form.match.method}
              placeholder="GET"
              onChange={(e) => set({ match: { ...form.match, method: e.target.value } })}
              className={cn(inputClass(failure?.field === 'match.method'), 'w-32 font-mono')}
            />
          </Labelled>
          <Labelled
            label="path"
            path="match.path"
            hint="A path, with * standing in for the rest: /v1/charges/*."
            error={fieldError(failure, 'match.path')}
          >
            <input
              type="text"
              value={form.match.path}
              placeholder="/v1/charges/*"
              onChange={(e) => set({ match: { ...form.match, path: e.target.value } })}
              className={cn(inputClass(failure?.field === 'match.path'), 'font-mono')}
            />
          </Labelled>
          <Labelled
            label="header"
            path="match.header"
            hint="Header names and the values a request must carry."
            error={fieldError(failure, 'match.header')}
          >
            <PairRows
              rows={form.match.header}
              nameLabel="match header"
              addLabel="Add header"
              invalid={failure?.field === 'match.header'}
              onChange={(header) => set({ match: { ...form.match, header } })}
            />
          </Labelled>
        </Section>

        <Section title="Fault" hint="What happens to the traffic this rule matches.">
          <Labelled label="type" path="fault.type" required error={fieldError(failure, 'fault.type')}>
            <select
              value={form.faultType}
              onChange={(e) => {
                const entry = entryFor(catalogue.faults, e.target.value);
                set({ faultType: e.target.value, faultParams: blankParams(entry) });
              }}
              className={cn(inputClass(failure?.field === 'fault.type'), 'cursor-pointer')}
            >
              {unknownFault && <option value={form.faultType}>{form.faultType} (not in this build)</option>}
              {groups.map((group) => (
                <optgroup key={group.tier} label={group.label}>
                  {group.entries.map((entry) => (
                    <option key={entry.name} value={entry.name}>
                      {entry.name}
                    </option>
                  ))}
                </optgroup>
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
          <Labelled label="type" path="behavior.type" error={fieldError(failure, 'behavior.type')}>
            <select
              value={form.behaviorType}
              onChange={(e) => {
                const entry = entryFor(catalogue.behaviors, e.target.value);
                set({ behaviorType: e.target.value, behaviorParams: blankParams(entry) });
              }}
              className={cn(inputClass(failure?.field === 'behavior.type'), 'cursor-pointer')}
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
      </form>
    </SidePanel>
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
