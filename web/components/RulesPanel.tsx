'use client';

import { useCallback, useState } from 'react';
import { Plus, Trash2 } from 'lucide-react';

import { deleteRule, disableRule, enableRule } from '@/lib/api';
import { describeFault, describeMatch } from '@/lib/ruleForm';
import { useCatalogue } from '@/lib/useCatalogue';
import { cn } from '@/lib/utils';
import type { Rule } from '@/types';

import { RuleForm } from './RuleForm';
import { Button } from './ui/Button';
import { EmptyState } from './ui/EmptyState';
import { Notice } from './ui/Notice';
import { Switch } from './ui/Switch';
import { Toast } from './ui/Toast';
import { Toolbar } from './ui/Toolbar';

/** What the editor has open: nothing, a new rule, or one that exists. */
export type Editing = { kind: 'new' } | { kind: 'rule'; id: string } | null;

interface RulesPanelProps {
  /** The rules as the stream keeps them. A write makes the server send
   * rules_changed, which re-reads this list, so nothing here holds its own
   * copy to keep in step. */
  rules: Rule[];
  editing: Editing;
  onEditingChange: (editing: Editing) => void;
}

export function RulesPanel({ rules, editing, onEditingChange }: RulesPanelProps) {
  const { catalogue, error } = useCatalogue(true);
  const [pending, setPending] = useState<Record<string, string>>({});
  const [notices, setNotices] = useState<Record<string, string>>({});
  /** The rule the editor just wrote, while its toast shows. The panel holds
   * this rather than the form: a new rule is re-keyed to its id once written,
   * which mounts a fresh form, and the toast has to outlive that. */
  const [saved, setSaved] = useState<{ id: string; at: number } | null>(null);
  const dismissSaved = useCallback(() => setSaved(null), []);

  const act = async (id: string, what: string, run: () => Promise<unknown>) => {
    setPending((p) => ({ ...p, [id]: what }));
    setNotices((n) => without(n, id));
    try {
      await run();
    } catch (err: unknown) {
      setNotices((n) => ({ ...n, [id]: err instanceof Error ? err.message : String(err) }));
    } finally {
      setPending((p) => without(p, id));
    }
  };

  const remove = (rule: Rule) =>
    act(rule.id, 'delete', async () => {
      await deleteRule(rule.id);
      if (editing?.kind === 'rule' && editing.id === rule.id) {
        onEditingChange(null);
      }
    });

  const toggle = (rule: Rule) =>
    act(rule.id, 'enabled', () => (rule.enabled ? disableRule(rule.id) : enableRule(rule.id)));

  // A rule deleted underneath an open editor, by another client or by the
  // config file, closes it rather than editing something that is gone.
  const open = editing?.kind === 'rule' ? rules.find((rule) => rule.id === editing.id) : undefined;
  const showForm = catalogue && (editing?.kind === 'new' || open);

  return (
    <div className="flex flex-1 overflow-hidden">
      <div className="flex flex-1 flex-col overflow-hidden">
        <Toolbar
          summary={
            rules.length === 0
              ? 'No rules yet'
              : `${rules.length} rule${rules.length === 1 ? '' : 's'}, applied in this order`
          }
        >
          <Button
            variant="primary"
            icon={Plus}
            onClick={() => onEditingChange({ kind: 'new' })}
            disabled={!catalogue}
          >
            New rule
          </Button>
        </Toolbar>

        {error && (
          <Notice tone="error" className="mx-4 mt-3">
            The fault catalogue could not be read, so rules cannot be edited: {error}
          </Notice>
        )}

        {/* Below 56rem, which is the editor open at a laptop width, the
            Behavior column goes: the editor shows it, and Match needs the
            room more. */}
        <div className="@container flex flex-1 flex-col overflow-auto">
          <table className="w-full table-fixed text-sm">
            <thead className="sticky top-0 z-10 border-b border-zinc-800 bg-zinc-900 text-xs text-zinc-400">
              <tr>
                <th scope="col" className="w-16 px-4 py-2 text-left font-medium">On</th>
                <th scope="col" className="w-[30%] px-4 py-2 text-left font-medium">Rule</th>
                <th scope="col" className="px-4 py-2 text-left font-medium">Match</th>
                <th scope="col" className="w-[22%] px-4 py-2 text-left font-medium">Fault</th>
                <th scope="col" className="w-36 px-4 py-2 text-left font-medium @max-4xl:hidden">Behavior</th>
                <th scope="col" className="w-14 px-4 py-2">
                  <span className="sr-only">Delete</span>
                </th>
              </tr>
            </thead>
            <tbody>
              {rules.map((rule) => (
                <Row
                  key={rule.id}
                  rule={rule}
                  selected={editing?.kind === 'rule' && editing.id === rule.id}
                  busy={pending[rule.id]}
                  notice={notices[rule.id]}
                  onOpen={() => onEditingChange({ kind: 'rule', id: rule.id })}
                  onToggle={() => void toggle(rule)}
                  onDelete={() => void remove(rule)}
                />
              ))}
            </tbody>
          </table>

          {rules.length === 0 && !error && (
            <EmptyState>
              No rules yet. Add one with New rule, or add delay or 503 to an upstream from the Upstreams panel.
            </EmptyState>
          )}
        </div>
      </div>

      {showForm && catalogue && (
        <RuleForm
          // Keyed by what is open, so switching rules starts the form over
          // rather than leaving the last one's values in its inputs.
          key={editing?.kind === 'new' ? 'new' : open?.id}
          catalogue={catalogue}
          rule={open ?? null}
          onClose={() => onEditingChange(null)}
          onSaved={(id) => {
            setSaved({ id, at: Date.now() });
            onEditingChange({ kind: 'rule', id });
          }}
        />
      )}

      {/* The editor looks the same after a save as before it, so the save
          says so from the corner of the window rather than from a footer
          that may be scrolled away from. */}
      {saved && (
        // Keyed by the time of the save, so saving again while the last toast
        // is still up starts it over rather than leaving it to run out.
        <Toast key={saved.at} message={`Rule ${saved.id} saved`} onDone={dismissSaved} />
      )}
    </div>
  );
}

interface RowProps {
  rule: Rule;
  selected: boolean;
  busy?: string;
  notice?: string;
  onOpen: () => void;
  onToggle: () => void;
  onDelete: () => void;
}

function Row({ rule, selected, busy, notice, onOpen, onToggle, onDelete }: RowProps) {
  return (
    <>
      <tr
        onClick={onOpen}
        aria-selected={selected}
        className={cn(
          'cursor-pointer transition-colors hover:bg-zinc-800/40',
          !notice && 'border-b border-zinc-800/50',
          // The same selection mark as a row in the stream.
          selected && 'bg-zinc-800 shadow-[inset_2px_0_0_0_theme(--color-zinc-200)]',
          !rule.enabled && 'text-zinc-500',
        )}
      >
        {/* The toggle sits inside a clickable row, so it stops the click that
            would otherwise open the editor behind it. */}
        <td className="px-4 py-2" onClick={(e) => e.stopPropagation()}>
          <Switch
            on={rule.enabled}
            onToggle={onToggle}
            busy={busy === 'enabled'}
            disabled={busy !== undefined}
            aria-label={`${rule.name} on`}
            title={rule.enabled ? `Switch ${rule.id} off, keeping it` : `Switch ${rule.id} on`}
          />
        </td>
        <td className="px-4 py-2">
          {/* The opener. The row takes a click too, but a row is not something
              a keyboard can reach; this is. */}
          <button
            type="button"
            aria-expanded={selected}
            title={`${rule.name} (${rule.id})`}
            onClick={(e) => {
              e.stopPropagation();
              onOpen();
            }}
            className={cn(
              'block w-full cursor-pointer truncate rounded-sm text-left',
              rule.enabled ? 'text-zinc-300' : 'text-zinc-500',
            )}
          >
            {rule.name}
            <span className="ml-2 font-mono text-xs text-zinc-600">{rule.id}</span>
          </button>
        </td>
        <td className="truncate px-4 py-2 font-mono text-xs text-zinc-400" title={describeMatch(rule.match)}>
          {describeMatch(rule.match)}
        </td>
        <td className="truncate px-4 py-2 font-mono text-xs text-amber-300/90" title={describeFault(rule.fault)}>
          {describeFault(rule.fault)}
        </td>
        <td className="truncate px-4 py-2 font-mono text-xs text-zinc-400 @max-4xl:hidden">
          {rule.behavior ? describeFault(rule.behavior) : 'always'}
        </td>
        <td className="px-4 py-2" onClick={(e) => e.stopPropagation()}>
          <Button
            variant="danger"
            icon={Trash2}
            aria-label={`Delete ${rule.id}`}
            title={`Delete ${rule.id}`}
            busy={busy === 'delete'}
            disabled={busy !== undefined}
            onClick={onDelete}
          />
        </td>
      </tr>

      {notice && (
        <tr className="border-b border-zinc-800/50">
          <td colSpan={6} className="px-4 pb-2">
            <Notice tone="error">{notice}</Notice>
          </td>
        </tr>
      )}
    </>
  );
}

function without(map: Record<string, string>, id: string): Record<string, string> {
  const rest = { ...map };
  delete rest[id];
  return rest;
}
