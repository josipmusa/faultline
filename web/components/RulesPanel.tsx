'use client';

import { useState } from 'react';
import { Loader2, Plus, Trash2 } from 'lucide-react';

import { deleteRule, disableRule, enableRule } from '@/lib/api';
import { describeFault, describeMatch } from '@/lib/ruleForm';
import { useCatalogue } from '@/lib/useCatalogue';
import { cn } from '@/lib/utils';
import type { Rule } from '@/types';

import { RuleForm } from './RuleForm';

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
        {error && (
          <p className="border-b border-red-500/20 bg-red-500/10 px-6 py-2 text-sm text-red-400">
            The fault catalogue could not be read, so rules cannot be edited: {error}
          </p>
        )}

        <div className="flex items-center justify-between border-b border-zinc-800 px-4 py-2">
          <p className="text-xs text-zinc-500">
            {rules.length === 0
              ? 'No rules yet'
              : `${rules.length} rule${rules.length === 1 ? '' : 's'}, applied in this order`}
          </p>
          <button
            type="button"
            onClick={() => onEditingChange({ kind: 'new' })}
            disabled={!catalogue}
            className={cn(
              'inline-flex cursor-pointer items-center gap-1 rounded border border-amber-500/40 bg-amber-500/15 px-2 py-1 text-xs font-medium text-amber-300 transition-colors hover:bg-amber-500/25',
              !catalogue && 'cursor-not-allowed opacity-50',
            )}
          >
            <Plus className="h-3 w-3" />
            New rule
          </button>
        </div>

        <div className="flex-1 overflow-auto">
          <table className="w-full text-sm">
            <thead className="sticky top-0 border-b border-zinc-800 bg-zinc-900 text-xs text-zinc-400">
              <tr>
                <th scope="col" className="w-20 px-4 py-2 text-left font-medium">On</th>
                <th scope="col" className="px-4 py-2 text-left font-medium">Rule</th>
                <th scope="col" className="px-4 py-2 text-left font-medium">Match</th>
                <th scope="col" className="px-4 py-2 text-left font-medium">Fault</th>
                <th scope="col" className="w-40 px-4 py-2 text-left font-medium">Behavior</th>
                <th scope="col" className="w-12 px-4 py-2" />
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
            <p className="flex h-64 items-center justify-center text-sm text-zinc-500">
              No rules yet. Add one to start degrading traffic on purpose.
            </p>
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
          onSaved={(id) => onEditingChange({ kind: 'rule', id })}
        />
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
        className={cn(
          'cursor-pointer transition-colors hover:bg-zinc-800/40',
          !notice && 'border-b border-zinc-800/50',
          selected && 'bg-zinc-800/60',
          !rule.enabled && 'text-zinc-500',
        )}
      >
        {/* The toggle sits inside a clickable row, so it stops the click that
            would otherwise open the editor behind it. */}
        <td className="px-4 py-2" onClick={(e) => e.stopPropagation()}>
          <button
            type="button"
            onClick={onToggle}
            disabled={busy !== undefined}
            aria-pressed={rule.enabled}
            title={rule.enabled ? `Switch ${rule.id} off, keeping it` : `Switch ${rule.id} on`}
            className={cn(
              'inline-flex h-5 w-9 cursor-pointer items-center rounded-full border px-0.5 transition-colors',
              rule.enabled ? 'justify-end border-amber-500/40 bg-amber-500/25' : 'justify-start border-zinc-700 bg-zinc-800',
              busy !== undefined && 'cursor-not-allowed opacity-50',
            )}
          >
            {busy === 'enabled' ? (
              <Loader2 className="h-3.5 w-3.5 animate-spin text-zinc-400" />
            ) : (
              <span className={cn('h-3.5 w-3.5 rounded-full', rule.enabled ? 'bg-amber-400' : 'bg-zinc-500')} />
            )}
          </button>
        </td>
        <td className="max-w-xs truncate px-4 py-2 text-zinc-300">
          {rule.name}
          <span className="ml-2 font-mono text-xs text-zinc-600">{rule.id}</span>
        </td>
        <td className="max-w-xs truncate px-4 py-2 font-mono text-xs text-zinc-400">
          {describeMatch(rule.match)}
        </td>
        <td className="max-w-xs truncate px-4 py-2 font-mono text-xs text-amber-300/90">
          {describeFault(rule.fault)}
        </td>
        <td className="truncate px-4 py-2 font-mono text-xs text-zinc-400">
          {rule.behavior ? describeFault(rule.behavior) : 'always'}
        </td>
        <td className="px-4 py-2" onClick={(e) => e.stopPropagation()}>
          <button
            type="button"
            onClick={onDelete}
            disabled={busy !== undefined}
            aria-label={`Delete ${rule.id}`}
            title={`Delete ${rule.id}`}
            className={cn(
              'cursor-pointer rounded p-1 text-zinc-500 transition-colors hover:bg-zinc-800 hover:text-red-400',
              busy !== undefined && 'cursor-not-allowed opacity-50',
            )}
          >
            {busy === 'delete' ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Trash2 className="h-3.5 w-3.5" />}
          </button>
        </td>
      </tr>

      {notice && (
        <tr className="border-b border-zinc-800/50">
          <td colSpan={6} className="px-4 pb-2">
            <p className="rounded-md border border-red-500/20 bg-red-500/10 px-3 py-2 text-xs text-red-400">
              {notice}
            </p>
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
