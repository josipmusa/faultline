'use client';

import { Zap } from 'lucide-react';

import { cn } from '@/lib/utils';
import type { Event, Rule, Tier } from '@/types';

import { Badge, badgeClass, type BadgeTone } from './ui/Badge';

const tierTones: Record<Tier, BadgeTone> = {
  plain: 'neutral',
  intercepted: 'sky',
  encrypted: 'violet',
};

/** What each tier means for the faults that can apply, in SPEC's words. */
const tierTitles: Record<Tier, string> = {
  plain: 'plain: unencrypted, so every fault applies',
  intercepted: 'intercepted: HTTPS terminated with the Faultline CA, so every fault applies',
  encrypted: 'encrypted, connection faults only',
};

export function TierBadge({ tier }: { tier: Tier }) {
  return (
    <Badge tone={tierTones[tier]} title={tierTitles[tier]}>
      {tier}
    </Badge>
  );
}

const methodTones: Record<string, BadgeTone> = {
  GET: 'blue',
  POST: 'emerald',
  PUT: 'yellow',
  PATCH: 'orange',
  DELETE: 'red',
  CONNECT: 'violet',
};

export function MethodBadge({ method }: { method: Event['method'] }) {
  return <Badge tone={methodTones[method] ?? 'neutral'}>{method}</Badge>;
}

/** The rule that faulted a request, named where the UI knows the name. The
 * bolt is what makes a faulted row readable at a glance: a rule name alone is
 * just more text in a dense table, and rule names get long.
 *
 * With somewhere to go it opens the rule in the editor. Without a rule id
 * there is nothing to open - the request was faulted by a rule that has since
 * been deleted, or by one this list never saw - so the badge stays a label
 * rather than becoming a button that does nothing. */
export function FaultBadge({
  ruleId,
  rule,
  onOpen,
}: {
  ruleId?: string;
  rule?: Rule;
  onOpen?: (id: string) => void;
}) {
  const name = rule?.name ?? ruleId ?? 'faulted';

  if (!ruleId || !onOpen) {
    return (
      <Badge tone="amber" icon={Zap} iconFilled title={ruleId ? `Faulted by rule ${ruleId}` : 'Faulted'}>
        {name}
      </Badge>
    );
  }

  return (
    <button
      type="button"
      title={`Faulted by rule ${ruleId}. Open it in the editor.`}
      onClick={() => onOpen(ruleId)}
      className={cn(badgeClass('amber'), 'cursor-pointer transition-colors hover:bg-amber-500/25')}
    >
      <Zap className="h-3 w-3 shrink-0" fill="currentColor" aria-hidden />
      <span className="truncate">{name}</span>
    </button>
  );
}
