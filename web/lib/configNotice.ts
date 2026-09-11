import type { ConfigInfo } from '@/types';

export interface ConfigNotice {
  /** Whether a change made here outlives the process. */
  persisted: boolean;
  /** What the chip reads: the file's name, or where the rules live instead. */
  label: string;
  /** The whole of it, for the hover: which file, or what to run to get one. */
  title: string;
}

/** The file's name without its directory. The chip has room for one of the
 * two and the name is the half that identifies it; the path is in the title. */
function baseName(path: string): string {
  const cut = Math.max(path.lastIndexOf('/'), path.lastIndexOf('\\'));
  return cut === -1 ? path : path.slice(cut + 1);
}

/** What the header should say about where rule changes go, or null while the
 * answer is still unknown - before the read finishes, or after it failed.
 * Saying nothing is the only honest thing then: either chip would be a claim
 * about persistence that nothing has confirmed. */
export function configNotice(config: ConfigInfo | null): ConfigNotice | null {
  if (config === null) {
    return null;
  }
  if (!config.persisted) {
    return {
      persisted: false,
      label: 'in-memory',
      title:
        'rules live in memory and are gone when Faultline stops; run faultline init to write a faultline.yaml',
    };
  }
  return {
    persisted: true,
    label: config.path ? baseName(config.path) : 'config file',
    title: config.path ? `changes are saved to ${config.path}` : 'changes are saved to the config file',
  };
}
