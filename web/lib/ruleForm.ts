import type { Behavior, Catalogue, CatalogueEntry, Fault, Match, Rule } from '@/types';

/** A rule as the form holds it while it is being edited.
 *
 * Everything a control owns is text, including numbers: an input's value is a
 * string, and an empty one has to stay distinguishable from a zero somebody
 * typed. Nothing is coerced or dropped until the rule is written back, so what
 * was typed is what travels, and the server's answer names the field it came
 * from. */
export interface RuleForm {
  id: string;
  name: string;
  enabled: boolean;
  match: {
    host: string;
    method: string;
    path: string;
    header: Pair[];
  };
  faultType: string;
  faultParams: Params;
  /** Empty means the rule has no behavior, so its fault applies to everything
   * it matches. */
  behaviorType: string;
  behaviorParams: Params;
}

/** One row of a mapping: a header name and the value it takes. */
export interface Pair {
  name: string;
  value: string;
}

/** One parameter's value in the form. Which of the three it is follows from
 * the kind the catalogue declared, so nothing has to guess. */
export type ParamValue = string | string[] | Pair[];

export type Params = Record<string, ParamValue>;

const emptyPair: Pair = { name: '', value: '' };

/** The entry in a catalogue for one fault or behavior name. */
export function entryFor(entries: CatalogueEntry[], name: string): CatalogueEntry | undefined {
  return entries.find((entry) => entry.name === name);
}

/** An empty value for every parameter the entry declares. A map or a list
 * starts with one blank row, so there is something to type into without
 * clicking add first. */
export function blankParams(entry: CatalogueEntry | undefined): Params {
  const params: Params = {};
  for (const field of entry?.fields ?? []) {
    params[field.name] = blankValue(field.kind);
  }
  return params;
}

function blankValue(kind: string): ParamValue {
  switch (kind) {
    case 'string map':
      return [{ ...emptyPair }];
    case 'string list':
      return [''];
    default:
      return '';
  }
}

/** A new rule, on the first fault the binary offers. */
export function blankForm(catalogue: Catalogue): RuleForm {
  const fault = catalogue.faults[0];
  return {
    id: '',
    name: '',
    enabled: true,
    match: { host: '', method: '', path: '', header: [{ ...emptyPair }] },
    faultType: fault?.name ?? '',
    faultParams: blankParams(fault),
    behaviorType: '',
    behaviorParams: {},
  };
}

/** Reads a saved fault or behavior into the form's controls. The entry decides
 * what shape each parameter takes; a parameter the saved value leaves out
 * comes back empty, which is how it will be written again if untouched. */
export function paramsFromValues(
  entry: CatalogueEntry | undefined,
  source: Record<string, unknown>,
): Params {
  const params: Params = {};
  for (const field of entry?.fields ?? []) {
    const value = source[field.name];
    switch (field.kind) {
      case 'string map':
        params[field.name] = pairsFrom(value);
        break;
      case 'string list':
        params[field.name] = listFrom(value);
        break;
      default:
        params[field.name] = value === undefined || value === null ? '' : String(value);
    }
  }
  return params;
}

function pairsFrom(value: unknown): Pair[] {
  if (value && typeof value === 'object' && !Array.isArray(value)) {
    const rows = Object.entries(value as Record<string, unknown>).map(([name, v]) => ({
      name,
      value: String(v),
    }));
    if (rows.length > 0) {
      return rows;
    }
  }
  return [{ ...emptyPair }];
}

function listFrom(value: unknown): string[] {
  if (Array.isArray(value) && value.length > 0) {
    return value.map((entry) => String(entry));
  }
  return [''];
}

/** Writes the form's controls back to the parameters of a fault or a behavior.
 *
 * An empty control means the parameter was left out, and is dropped rather
 * than sent as a zero or an empty string: `reset` with `after_bytes` 0 breaks
 * the connection before anything is delivered, which is a different rule from
 * one that never said. Text in a number field travels as it was typed, so the
 * server answers naming the field instead of the form inventing a value. */
export function paramsToValues(
  entry: CatalogueEntry | undefined,
  params: Params,
): Record<string, unknown> {
  const out: Record<string, unknown> = {};
  for (const field of entry?.fields ?? []) {
    const value = params[field.name];
    switch (field.kind) {
      case 'string map': {
        const rows = (value as Pair[] | undefined)?.filter((row) => row.name !== '') ?? [];
        if (rows.length > 0) {
          out[field.name] = Object.fromEntries(rows.map((row) => [row.name, row.value]));
        }
        break;
      }
      case 'string list': {
        const rows = (value as string[] | undefined)?.filter((row) => row !== '') ?? [];
        if (rows.length > 0) {
          out[field.name] = rows;
        }
        break;
      }
      case 'integer': {
        const text = (value as string) ?? '';
        if (text !== '') {
          const n = Number(text);
          out[field.name] = Number.isFinite(n) && text.trim() !== '' ? n : text;
        }
        break;
      }
      default: {
        const text = (value as string) ?? '';
        if (text !== '') {
          out[field.name] = text;
        }
      }
    }
  }
  return out;
}

/** The rule the form describes, in the shape the API takes. The id is left out
 * when it was not given, because the API derives one from the name and an
 * empty string is not the same as saying nothing. */
export function formToRule(form: RuleForm, catalogue: Catalogue): Omit<Rule, 'id'> & { id?: string } {
  const rule: Omit<Rule, 'id'> & { id?: string } = {
    name: form.name,
    enabled: form.enabled,
    match: matchOf(form),
    fault: {
      type: form.faultType,
      ...paramsToValues(entryFor(catalogue.faults, form.faultType), form.faultParams),
    } as Fault,
  };
  if (form.id !== '') {
    rule.id = form.id;
  }
  if (form.behaviorType !== '') {
    rule.behavior = {
      type: form.behaviorType,
      ...paramsToValues(entryFor(catalogue.behaviors, form.behaviorType), form.behaviorParams),
    } as Behavior;
  }
  return rule;
}

function matchOf(form: RuleForm): Match {
  const match: Match = {};
  if (form.match.host !== '') {
    match.host = form.match.host;
  }
  if (form.match.method !== '') {
    match.method = form.match.method;
  }
  if (form.match.path !== '') {
    match.path = form.match.path;
  }
  const header = form.match.header.filter((row) => row.name !== '');
  if (header.length > 0) {
    match.header = Object.fromEntries(header.map((row) => [row.name, row.value]));
  }
  return match;
}

/** Reads a saved rule into the form.
 *
 * A fault type the catalogue does not describe is kept as it is, with no
 * parameters: it can only have come from a newer binary or a hand-edited file,
 * and the panel says so rather than quietly rewriting somebody's rule into
 * something this UI happens to understand. */
export function ruleToForm(rule: Rule, catalogue: Catalogue): RuleForm {
  const { type: faultType, ...faultParams } = rule.fault;
  const behaviorType = rule.behavior?.type ?? '';
  const behaviorParams = { ...rule.behavior } as Record<string, unknown>;
  delete behaviorParams.type;

  return {
    id: rule.id,
    name: rule.name,
    enabled: rule.enabled,
    match: {
      host: rule.match.host ?? '',
      method: rule.match.method ?? '',
      path: rule.match.path ?? '',
      header: pairsFrom(rule.match.header),
    },
    faultType,
    faultParams: paramsFromValues(entryFor(catalogue.faults, faultType), faultParams),
    behaviorType,
    behaviorParams:
      behaviorType === ''
        ? {}
        : paramsFromValues(entryFor(catalogue.behaviors, behaviorType), behaviorParams),
  };
}

/** A fault as one line in a table: its type, then whatever it was given. It is
 * written off the fault itself rather than off the catalogue, so a fault this
 * UI does not know still reads as what it is. */
export function describeFault(fault: Fault): string {
  const { type, ...params } = fault;
  const parts = Object.entries(params).map(([name, value]) => `${name}=${format(value)}`);
  return [type, ...parts].join(' ');
}

function format(value: unknown): string {
  if (Array.isArray(value)) {
    return value.join(',');
  }
  if (value && typeof value === 'object') {
    return Object.entries(value as Record<string, unknown>)
      .map(([name, v]) => `${name}:${String(v)}`)
      .join(',');
  }
  return String(value);
}

/** What a rule matches, read left to right the way it is written. Required
 * headers are counted rather than listed: they are the rarest part of a match
 * and the longest, and the row is not where somebody reads them. */
export function describeMatch(match: Match): string {
  const parts = [match.host, match.method, match.path].filter((part) => part && part !== '');
  const headers = Object.keys(match.header ?? {}).length;
  if (headers > 0) {
    parts.push(`+${headers} header${headers === 1 ? '' : 's'}`);
  }
  return parts.length === 0 ? 'all traffic' : parts.join(' ');
}
