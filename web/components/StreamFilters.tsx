'use client';

import { X } from 'lucide-react';

import { emptyFilters, statusClasses, type Filters } from '@/lib/filters';
import { cn } from '@/lib/utils';

import { Button } from './ui/Button';

interface StreamFiltersProps {
  filters: Filters;
  onChange: (filters: Filters) => void;
  /** The hosts and methods actually seen, so the lists only offer what exists. */
  hosts: string[];
  methods: string[];
  /** How many events the filters let through, out of how many there are. */
  showing: number;
  total: number;
}

export function StreamFilters({ filters, onChange, hosts, methods, showing, total }: StreamFiltersProps) {
  const filtering = showing !== total;

  return (
    <div className="flex min-h-11 flex-wrap items-center gap-2 border-b border-zinc-800 bg-zinc-950/50 px-4 py-2">
      <Select
        label="Host"
        value={filters.host}
        options={hosts}
        anyLabel="Any host"
        onChange={(host) => onChange({ ...filters, host })}
      />
      <Select
        label="Method"
        value={filters.method}
        options={methods}
        anyLabel="Any method"
        onChange={(method) => onChange({ ...filters, method })}
      />
      <Select
        label="Status"
        value={filters.statusClass}
        options={statusClasses}
        anyLabel="Any status"
        onChange={(statusClass) => onChange({ ...filters, statusClass: statusClass as Filters['statusClass'] })}
      />

      <Button
        variant={filters.faultedOnly ? 'primary' : 'secondary'}
        aria-pressed={filters.faultedOnly}
        onClick={() => onChange({ ...filters, faultedOnly: !filters.faultedOnly })}
      >
        Faulted only
      </Button>

      {filtering && (
        <Button variant="ghost" icon={X} onClick={() => onChange(emptyFilters)}>
          Clear filters
        </Button>
      )}

      <span className="ml-auto text-xs text-zinc-500">
        {filtering ? (
          <>
            <span className="font-mono text-zinc-300">{showing}</span> of{' '}
            <span className="font-mono text-zinc-400">{total}</span> shown
          </>
        ) : (
          <>
            <span className="font-mono text-zinc-300">{total}</span> shown
          </>
        )}
      </span>
    </div>
  );
}

interface SelectProps {
  label: string;
  value: string;
  options: string[];
  anyLabel: string;
  onChange: (value: string) => void;
}

function Select({ label, value, options, anyLabel, onChange }: SelectProps) {
  return (
    <label className="flex items-center gap-1.5 text-xs text-zinc-500">
      <span className="sr-only">{label}</span>
      <select
        value={value}
        onChange={(e) => onChange(e.target.value)}
        className={cn(
          'cursor-pointer rounded-md border border-zinc-700 bg-zinc-900 px-2 py-1 text-xs',
          value === '' ? 'text-zinc-400' : 'text-zinc-100',
        )}
      >
        <option value="">{anyLabel}</option>
        {options.map((option) => (
          <option key={option} value={option}>
            {option}
          </option>
        ))}
      </select>
    </label>
  );
}
