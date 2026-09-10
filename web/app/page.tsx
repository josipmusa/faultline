'use client';

import { useMemo, useState } from 'react';

import { EventInspector } from '@/components/EventInspector';
import { Header } from '@/components/Header';
import { RequestStream } from '@/components/RequestStream';
import { Sidebar } from '@/components/Sidebar';
import { StreamFilters } from '@/components/StreamFilters';
import { UpstreamsPanel } from '@/components/UpstreamsPanel';
import { emptyFilters, filterEvents, hostsOf, methodsOf } from '@/lib/filters';
import { useEventStream } from '@/lib/useEventStream';
import { useUpstreams } from '@/lib/useUpstreams';

export default function Home() {
  const [activeView, setActiveView] = useState('live');
  const [filters, setFilters] = useState(emptyFilters);
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const { events, rules, connected, error, clear } = useEventStream();
  const upstreams = useUpstreams(activeView === 'upstreams');

  const shown = useMemo(() => filterEvents(events, filters), [events, filters]);
  const hosts = useMemo(() => hostsOf(events), [events]);
  const methods = useMemo(() => methodsOf(events), [events]);
  const byId = useMemo(() => new Map(rules.map((rule) => [rule.id, rule])), [rules]);

  // The selection follows the event, not the row: an event filtered out or
  // cleared away closes the panel rather than leaving it showing something
  // that is no longer on screen.
  const selected = shown.find((event) => event.id === selectedId);

  return (
    <div className="flex h-screen bg-zinc-900 text-zinc-100">
      <Sidebar activeView={activeView} onViewChange={setActiveView} />

      <div className="flex flex-1 flex-col overflow-hidden">
        <Header
          connected={connected}
          eventCount={events.length}
          ruleCount={rules.length}
          onClear={() => void clear()}
        />

        {error && (
          <p className="border-b border-red-500/20 bg-red-500/10 px-6 py-2 text-sm text-red-400">{error}</p>
        )}

        {activeView === 'upstreams' ? (
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
