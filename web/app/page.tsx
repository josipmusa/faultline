'use client';

import { useMemo, useState } from 'react';

import { EventInspector } from '@/components/EventInspector';
import { Header } from '@/components/Header';
import { MetricsStrip } from '@/components/MetricsStrip';
import { RequestStream } from '@/components/RequestStream';
import { RulesPanel, type Editing } from '@/components/RulesPanel';
import { ScenariosPanel } from '@/components/ScenariosPanel';
import { Sidebar } from '@/components/Sidebar';
import { StreamFilters } from '@/components/StreamFilters';
import { UpstreamsPanel } from '@/components/UpstreamsPanel';
import { emptyFilters, filterEvents, hostsOf, methodsOf } from '@/lib/filters';
import { useConfig } from '@/lib/useConfig';
import { useEventStream } from '@/lib/useEventStream';
import { useMetrics } from '@/lib/useMetrics';
import { useScenarios } from '@/lib/useScenarios';
import { useUpstreams } from '@/lib/useUpstreams';

export default function Home() {
  const [activeView, setActiveView] = useState('live');
  const [filters, setFilters] = useState(emptyFilters);
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [editing, setEditing] = useState<Editing>(null);
  const { events, rules, connected, atCap, error, clear } = useEventStream();
  const upstreams = useUpstreams(activeView === 'upstreams');
  const session = useScenarios(activeView === 'scenarios');
  const config = useConfig();

  const shown = useMemo(() => filterEvents(events, filters), [events, filters]);
  const hosts = useMemo(() => hostsOf(events), [events]);
  const methods = useMemo(() => methodsOf(events), [events]);
  // The strip plots the rows beneath it, the way "showing 40 of 500" reads:
  // filtering to one host is how its shape gets isolated. Whether the browser
  // is at its cap is about the whole list, not about the filtered slice.
  const points = useMetrics(shown, atCap);
  const byId = useMemo(() => new Map(rules.map((rule) => [rule.id, rule])), [rules]);

  // The selection follows the event, not the row: an event filtered out or
  // cleared away closes the panel rather than leaving it showing something
  // that is no longer on screen.
  const selected = shown.find((event) => event.id === selectedId);

  /** Opens a rule in the editor from wherever it was named, which today is the
   * fault badge on an event. The panel has to be showing for the editor to be
   * on screen, so the view follows the rule rather than the click quietly
   * doing nothing. */
  const openRule = (id: string) => {
    setEditing({ kind: 'rule', id });
    setActiveView('rules');
  };

  return (
    <div className="flex h-screen bg-zinc-900 text-zinc-100">
      <Sidebar activeView={activeView} onViewChange={setActiveView} />

      <div className="flex flex-1 flex-col overflow-hidden">
        <Header
          connected={connected}
          eventCount={events.length}
          ruleCount={rules.length}
          config={config}
          onReset={() => void clear()}
        />

        {error && (
          <p className="border-b border-red-500/20 bg-red-500/10 px-6 py-2 text-sm text-red-400">{error}</p>
        )}

        {activeView === 'rules' ? (
          <RulesPanel rules={rules} editing={editing} onEditingChange={setEditing} />
        ) : activeView === 'scenarios' ? (
          <ScenariosPanel
            scenarios={session.scenarios}
            rules={rules}
            report={session.report}
            error={session.error}
            // Turning a scenario on is a rule change, so the rule list
            // refreshes itself off the stream's rules_changed; which scenario
            // is active is per run state the socket never carries, so the
            // panel's own reads are asked for again here.
            onChanged={session.refresh}
          />
        ) : activeView === 'upstreams' ? (
          <UpstreamsPanel
            upstreams={upstreams.upstreams}
            rules={rules}
            error={upstreams.error}
            // A quick action changes a rule, and the rule list refreshes
            // itself off the stream's rules_changed; the upstream rows are
            // read again here, because a bypass shows up only in them.
            onChanged={upstreams.refresh}
          />
        ) : (
          <>
            <StreamFilters
              filters={filters}
              onChange={setFilters}
              hosts={hosts}
              methods={methods}
              showing={shown.length}
              total={events.length}
            />

            <MetricsStrip points={points} filtering={shown.length !== events.length} />

            <div className="flex flex-1 overflow-hidden">
              <RequestStream
                events={shown}
                rules={byId}
                selectedId={selected?.id}
                onSelect={(event) => setSelectedId(event.id === selectedId ? null : event.id)}
                filtering={shown.length !== events.length}
              />

              {selected && (
                <EventInspector
                  // Keyed by the event, so opening another row starts the
                  // panel over rather than showing the last one's capture
                  // while the new one loads.
                  key={selected.id}
                  event={selected}
                  rule={selected.rule_id ? byId.get(selected.rule_id) : undefined}
                  onOpenRule={openRule}
                  onClose={() => setSelectedId(null)}
                />
              )}
            </div>
          </>
        )}
      </div>
    </div>
  );
}
