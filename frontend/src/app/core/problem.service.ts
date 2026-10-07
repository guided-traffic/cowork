import { HttpErrorResponse } from '@angular/common/http';
import { inject, Injectable } from '@angular/core';
import { MessageService } from 'primeng/api';
import { Problem } from '../api/models';

/** A problem as the UI shows it (docs/adr/0047): what a toast, a form or a merge prompt needs. */
export interface ProblemView {
  status: number;
  code: string;
  title: string;
  detail: string;
  requestId?: string;
  /** Each invalid field's message, by the field's path (`title`, `block.kind`, `assignee`). */
  fields: Record<string, string>;
  /** On a `412`, the server's current value per field (docs/adr/0050 D5). */
  current: Record<string, unknown>;
}

/** `/block/kind` → `block.kind`; `query:assignee` → `assignee`; `header:If-Match` → `If-Match`. */
export function fieldOf(pointer: string): string {
  return pointer
    .replace(/^(query|header):/, '')
    .replace(/^\//, '')
    .split('/')
    .join('.');
}

function isProblem(body: unknown): body is Problem {
  return (
    typeof body === 'object' &&
    body !== null &&
    typeof (body as Problem).code === 'string' &&
    typeof (body as Problem).status === 'number'
  );
}

/**
 * The one place the API's problem details are translated (docs/adr/0053 D5): into a toast, a
 * form's field errors or the values a `412` merge needs. Components never parse a problem body.
 */
/** The statuses with which a proxy in front of the backend says it reached none. */
const gatewayStatuses = new Set([502, 503, 504]);

@Injectable({ providedIn: 'root' })
export class ProblemService {
  private readonly messages = inject(MessageService);

  read(error: unknown): ProblemView {
    // A resource hands on an HttpErrorResponse as it is and wraps anything else, with the
    // original as the cause.
    if (
      !(error instanceof HttpErrorResponse) &&
      error instanceof Error &&
      error.cause !== undefined
    ) {
      return this.read(error.cause);
    }
    if (error instanceof HttpErrorResponse) {
      if (isProblem(error.error)) {
        return this.fromProblem(error.error);
      }
      if (error.status === 0) {
        return {
          status: 0,
          code: 'backend_unreachable',
          title: 'The backend cannot be reached',
          detail: 'The connection failed; cowork tries again on its own.',
          fields: {},
          current: {},
        };
      }
      // The backend answers every error with a problem body (docs/adr/0047); a gateway's status
      // without one is the Ingress in front of it, which reached no backend.
      if (gatewayStatuses.has(error.status)) {
        return {
          status: error.status,
          code: 'backend_unreachable',
          title: 'The backend cannot be reached',
          detail: `The Ingress answered ${error.status}: no backend took the request. cowork tries again on its own.`,
          fields: {},
          current: {},
        };
      }
      return {
        status: error.status,
        code: 'unexpected',
        title: 'Unexpected answer',
        detail: `The server answered ${error.status} without a problem body.`,
        fields: {},
        current: {},
      };
    }
    return {
      status: 0,
      code: 'client_error',
      title: 'Something went wrong in the page',
      detail: error instanceof Error ? error.message : String(error),
      fields: {},
      current: {},
    };
  }

  /**
   * The problem's `errors[]` as the API names them, in their order, each pointer as it came: for a
   * page whose pointers name no field of a form — an import's refusal names each file as
   * `file:<path>` (docs/adr/0051 D2), and a path's slashes are no field path. Empty where the error
   * carries no problem body.
   */
  entries(error: unknown): { pointer: string; message: string }[] {
    if (
      !(error instanceof HttpErrorResponse) &&
      error instanceof Error &&
      error.cause !== undefined
    ) {
      return this.entries(error.cause);
    }
    if (!(error instanceof HttpErrorResponse) || !isProblem(error.error)) {
      return [];
    }
    return (error.error.errors ?? []).map(({ pointer, message }) => ({ pointer, message }));
  }

  /**
   * Shows the problem as a toast and returns it. A form passes `fields: true` and shows the field
   * errors itself; the toast is then left out when there are any.
   */
  report(error: unknown, options: { fields?: boolean } = {}): ProblemView {
    const problem = this.read(error);
    if (!options.fields || Object.keys(problem.fields).length === 0) {
      this.messages.add({
        severity: problem.status === 0 || problem.status >= 500 ? 'error' : 'warn',
        summary: problem.title,
        detail: problem.requestId
          ? `${problem.detail} (request ${problem.requestId})`
          : problem.detail,
        life: 6000,
      });
    }
    return problem;
  }

  private fromProblem(problem: Problem): ProblemView {
    const fields: Record<string, string> = {};
    const current: Record<string, unknown> = {};
    for (const error of problem.errors ?? []) {
      const field = fieldOf(error.pointer);
      fields[field] = error.message;
      if (error.current !== undefined) {
        current[field] = error.current;
      }
    }
    return {
      status: problem.status,
      code: problem.code,
      title: problem.title,
      detail: problem.detail ?? '',
      requestId: problem.request_id,
      fields,
      current,
    };
  }
}
