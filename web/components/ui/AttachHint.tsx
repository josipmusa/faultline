import { attachOptions } from '@/lib/attachHint';
import type { ConfigInfo } from '@/types';

interface AttachHintProps {
  config: ConfigInfo | null;
  /** What arrives once traffic flows: "its outbound calls appear here". */
  then: string;
}

const code = 'rounded bg-zinc-800 px-1 py-0.5 font-mono text-xs text-zinc-300';

/** The empty-state sentence that says how to get traffic into Faultline:
 * wrap the start command, or point the application at an address this
 * instance is listening on. The addresses come from `GET /api/config`, so a
 * route or a proxy on a non-default port is named as it really is. */
export function AttachHint({ config, then }: AttachHintProps) {
  const options = attachOptions(config);
  return (
    <>
      Start your application with <code className={code}>faultline run -- &lt;your start command&gt;</code>
      {options.length > 0 && (
        <>
          {' '}
          or point it at{' '}
          {options.map((option, i) => (
            <span key={option.label}>
              {i > 0 && ', '}
              <code className={code}>{option.value}</code> <span className="text-zinc-600">({option.label})</span>
            </span>
          ))}
        </>
      )}
      , and {then}
    </>
  );
}
