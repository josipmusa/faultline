'use client';

import { Plus, X } from 'lucide-react';

import { boundsHint, placeholderFor, type Pair, type Params, type ParamValue } from '@/lib/ruleForm';
import { cn } from '@/lib/utils';
import type { CatalogueEntry, CatalogueField } from '@/types';

import { Button } from './ui/Button';

/** The controls for one fault's or one behavior's parameters, rendered from
 * what the binary said it takes.
 *
 * This is the whole reason a fault the UI was never told about still appears:
 * nothing here names a fault, only the four kinds a parameter can have. A new
 * fault built from those kinds renders itself, and only a new kind would need
 * work in this file. */
interface ParamFieldsProps {
  entry: CatalogueEntry | undefined;
  params: Params;
  /** `fault` or `behavior`: the first half of the field path the API names in
   * an error, so a rejected parameter can be marked where it was typed. */
  scope: string;
  /** The field path the API rejected, when it rejected one. */
  badField?: string;
  badMessage?: string;
  onChange: (name: string, value: ParamValue) => void;
}

export function ParamFields({ entry, params, scope, badField, badMessage, onChange }: ParamFieldsProps) {
  if (!entry) {
    return null;
  }
  if (entry.fields.length === 0) {
    return <p className="text-xs text-zinc-500">Nothing to configure.</p>;
  }

  return (
    <div className="space-y-3">
      {entry.fields.map((field) => {
        const path = `${scope}.${field.name}`;
        return (
          <Labelled
            key={field.name}
            label={field.name}
            path={path}
            hint={hintFor(field, entry)}
            required={field.required}
            error={badField === path ? badMessage : undefined}
          >
            <Control
              field={field}
              value={params[field.name]}
              invalid={badField === path}
              onChange={(value) => onChange(field.name, value)}
            />
          </Labelled>
        );
      })}
    </div>
  );
}

/** What a parameter's description says, plus what it is written with and the
 * bounds it is held to. The pair a parameter belongs to is said once, on the
 * field that declares it, in the words that say whether both are allowed. */
function hintFor(field: CatalogueField, entry: CatalogueEntry): string {
  // The catalogue's descriptions are written as phrases; what follows them
  // here is written as sentences, so the phrase gets its full stop.
  const parts = [sentence(field.description)];
  if (field.partner) {
    parts.push(
      field.exclusive
        ? `Give this or ${field.partner}, not both.`
        : `Give this, ${field.partner}, or both.`,
    );
  }
  if (field.chars) {
    parts.push(`Written with ${field.chars.split('').join(' and ')}.`);
  }
  if (entry.name === 'pattern') {
    parts.push('FFP faults twice, passes once, and repeats.');
  }
  const bounds = boundsHint(field);
  if (bounds) {
    parts.push(bounds);
  }
  return parts.join(' ');
}

function sentence(text: string): string {
  const trimmed = text.trim();
  return trimmed === '' || /[.!?]$/.test(trimmed) ? trimmed : `${trimmed}.`;
}

interface ControlProps {
  field: CatalogueField;
  value: ParamValue | undefined;
  invalid: boolean;
  onChange: (value: ParamValue) => void;
}

function Control({ field, value, invalid, onChange }: ControlProps) {
  switch (field.kind) {
    case 'string map':
      return (
        <PairRows
          rows={(value as Pair[] | undefined) ?? []}
          invalid={invalid}
          nameLabel={`${field.name} name`}
          addLabel={`Add ${singular(field.name)}`}
          onChange={onChange}
        />
      );
    case 'string list':
      return (
        <TextRows
          rows={(value as string[] | undefined) ?? []}
          invalid={invalid}
          label={field.name}
          addLabel={`Add ${singular(field.name)}`}
          onChange={onChange}
        />
      );
    case 'integer':
      // A number is short, so its input is too: a value like 2000 stretched
      // across the panel reads as a field that wants a sentence.
      return (
        <input
          type="number"
          inputMode="numeric"
          value={(value as string) ?? ''}
          min={field.min}
          max={field.max}
          placeholder={placeholderFor(field)}
          onChange={(e) => onChange(e.target.value)}
          className={cn(inputClass(invalid), 'w-32')}
        />
      );
    default:
      return (
        <input
          type="text"
          value={(value as string) ?? ''}
          onChange={(e) => onChange(e.target.value)}
          className={inputClass(invalid)}
        />
      );
  }
}

/** "set" and "remove" name lists of headers; the add button names one. */
function singular(name: string): string {
  return name === 'set' || name === 'remove' ? 'header' : 'entry';
}

/** Rows of names mapped to values: the headers a `headers` fault sets, and the
 * headers a match requires. */
export function PairRows({
  rows,
  invalid,
  nameLabel,
  addLabel,
  onChange,
}: {
  rows: Pair[];
  invalid?: boolean;
  nameLabel: string;
  addLabel: string;
  onChange: (rows: Pair[]) => void;
}) {
  const shown = rows.length > 0 ? rows : [{ name: '', value: '' }];

  return (
    <div className="space-y-1.5">
      {shown.map((row, i) => (
        <div key={i} className="flex items-center gap-1.5">
          <input
            type="text"
            value={row.name}
            aria-label={nameLabel}
            placeholder="name"
            onChange={(e) => onChange(replace(shown, i, { ...row, name: e.target.value }))}
            className={cn(inputClass(invalid), 'flex-1')}
          />
          <input
            type="text"
            value={row.value}
            aria-label={`${nameLabel} value`}
            placeholder="value"
            onChange={(e) => onChange(replace(shown, i, { ...row, value: e.target.value }))}
            className={cn(inputClass(invalid), 'flex-1')}
          />
          <Button
            icon={X}
            aria-label="Remove this row"
            onClick={() => onChange(drop(shown, i))}
            disabled={shown.length === 1}
          />
        </div>
      ))}
      <Button variant="ghost" icon={Plus} onClick={() => onChange([...shown, { name: '', value: '' }])}>
        {addLabel}
      </Button>
    </div>
  );
}

/** Rows of plain names: the headers a `headers` fault strips. */
function TextRows({
  rows,
  invalid,
  label,
  addLabel,
  onChange,
}: {
  rows: string[];
  invalid?: boolean;
  label: string;
  addLabel: string;
  onChange: (rows: string[]) => void;
}) {
  const shown = rows.length > 0 ? rows : [''];

  return (
    <div className="space-y-1.5">
      {shown.map((row, i) => (
        <div key={i} className="flex items-center gap-1.5">
          <input
            type="text"
            value={row}
            aria-label={label}
            placeholder="name"
            onChange={(e) => onChange(replace(shown, i, e.target.value))}
            className={cn(inputClass(invalid), 'flex-1')}
          />
          <Button
            icon={X}
            aria-label="Remove this row"
            onClick={() => onChange(drop(shown, i))}
            disabled={shown.length === 1}
          />
        </div>
      ))}
      <Button variant="ghost" icon={Plus} onClick={() => onChange([...shown, ''])}>
        {addLabel}
      </Button>
    </div>
  );
}

/** One labelled control, with the catalogue's own words under it and the
 * server's complaint under those when there is one. `path` is the field as
 * the API names it, so a rejected field can be found and focused. */
export function Labelled({
  label,
  path,
  hint,
  required,
  error,
  children,
}: {
  label: string;
  path: string;
  hint?: string;
  required?: boolean;
  error?: string;
  children: React.ReactNode;
}) {
  return (
    <label className="block" data-field={path}>
      <span className="mb-1 flex items-center gap-1 font-mono text-xs text-zinc-300">
        {label}
        {required && (
          <span className="text-amber-400" title="Required" aria-label="required">
            *
          </span>
        )}
      </span>
      {children}
      {hint && <span className="mt-1 block text-xs text-zinc-500">{hint}</span>}
      {error && (
        <span role="alert" className="mt-1 block text-xs text-red-400">
          {error}
        </span>
      )}
    </label>
  );
}

export function inputClass(invalid?: boolean): string {
  return cn(
    'w-full rounded-md border bg-zinc-900 px-2 py-1.5 text-sm text-zinc-100 transition-colors placeholder:text-zinc-600 focus:border-amber-500/60',
    invalid ? 'border-red-500/60' : 'border-zinc-700',
  );
}

function replace<T>(rows: T[], at: number, row: T): T[] {
  return rows.map((existing, i) => (i === at ? row : existing));
}

function drop<T>(rows: T[], at: number): T[] {
  return rows.filter((_, i) => i !== at);
}
