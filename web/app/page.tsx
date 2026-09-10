'use client';

import { useState } from 'react';

import { Header } from '@/components/Header';
import { RequestStream } from '@/components/RequestStream';
import { Sidebar } from '@/components/Sidebar';
import { useEventStream } from '@/lib/useEventStream';

export default function Home() {
  const [activeView, setActiveView] = useState('live');
  const { events, rules, connected, error, clear } = useEventStream();

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

        <RequestStream events={events} />
      </div>
    </div>
  );
}
