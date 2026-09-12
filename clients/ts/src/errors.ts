/** A refusal from the admin API: the message it wrote for a person, and the field it blamed. */
export class ApiError extends Error {
  readonly status: number;
  /** The field the message names, such as `fault.ms`, when the problem was one field's. */
  readonly field?: string;

  constructor(status: number, message: string, field?: string) {
    super(field ? `${message} (${field})` : message);
    this.name = 'ApiError';
    this.status = status;
    this.field = field;
  }
}

/**
 * Nothing answered at the address. It is the common mistake - no instance
 * running - and deserves a sentence rather than a connect error.
 */
export class UnreachableError extends Error {
  readonly addr: string;

  constructor(addr: string, cause: unknown) {
    super(`no Faultline is listening at ${addr}; start one with \`faultline serve\``, { cause });
    this.name = 'UnreachableError';
    this.addr = addr;
  }
}

/**
 * Nothing matching arrived before the wait ran out. It is an answer rather than
 * a failure: usually it means the application never made the call being watched
 * for.
 */
export class WaitTimeoutError extends Error {
  constructor(timeoutMs: number) {
    super(`no matching event arrived within ${timeoutMs}ms`);
    this.name = 'WaitTimeoutError';
  }
}
