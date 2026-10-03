import { HttpClient, HttpErrorResponse, provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { resource } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { MessageService } from 'primeng/api';
import { firstValueFrom } from 'rxjs';
import { Problem } from '../api/models';
import { fieldOf, ProblemService } from './problem.service';

function problem(overrides: Partial<Problem> = {}): Problem {
  return {
    type: 'https://cowork.example/problems/validation_failed',
    title: 'Validation failed',
    status: 422,
    code: 'validation_failed',
    ...overrides,
  };
}

function failure(error: unknown, status: number, statusText = ''): HttpErrorResponse {
  return new HttpErrorResponse({ error, status, statusText, url: '/api/v1/anything' });
}

describe('fieldOf', () => {
  it.each([
    ['/title', 'title'],
    ['/block/kind', 'block.kind'],
    ['/items/0/name', 'items.0.name'],
    ['query:assignee', 'assignee'],
    ['header:If-Match', 'If-Match'],
    ['title', 'title'],
    ['', ''],
  ])('turns the pointer %j into the field %j', (pointer, field) => {
    expect(fieldOf(pointer)).toBe(field);
  });
});

describe('ProblemService', () => {
  let service: ProblemService;
  let messages: MessageService;

  beforeEach(() => {
    TestBed.configureTestingModule({
      providers: [provideHttpClient(), provideHttpClientTesting(), MessageService],
    });
    service = TestBed.inject(ProblemService);
    messages = TestBed.inject(MessageService);
    vi.spyOn(messages, 'add');
  });

  describe('read', () => {
    it('takes the problem details of the API and keeps each invalid field under its path', () => {
      const body = problem({
        detail: 'The ticket is invalid.',
        request_id: 'req-7',
        errors: [
          { pointer: '/title', message: 'must not be empty' },
          { pointer: '/block/kind', message: 'is not a known kind' },
          { pointer: 'query:assignee', message: 'names nobody' },
          { pointer: 'header:If-Match', message: 'is required' },
        ],
      });

      expect(service.read(failure(body, 422))).toEqual({
        status: 422,
        code: 'validation_failed',
        title: 'Validation failed',
        detail: 'The ticket is invalid.',
        requestId: 'req-7',
        fields: {
          title: 'must not be empty',
          'block.kind': 'is not a known kind',
          assignee: 'names nobody',
          'If-Match': 'is required',
        },
        current: {},
      });
    });

    it('hands on the server value of each field of a 412, for the merge (docs/adr/0050 D5)', () => {
      const body = problem({
        status: 412,
        code: 'precondition_failed',
        title: 'The ticket changed',
        errors: [
          { pointer: '/title', message: 'changed meanwhile', current: 'Their title' },
          { pointer: '/block/reason', message: 'changed meanwhile', current: null },
          { pointer: '/progress', message: 'changed meanwhile', current: 0 },
          { pointer: '/confidential', message: 'changed meanwhile', current: false },
          { pointer: '/body', message: 'changed meanwhile' },
        ],
      });

      const view = service.read(failure(body, 412));

      expect(view.status).toBe(412);
      expect(view.code).toBe('precondition_failed');
      expect(view.current).toEqual({
        title: 'Their title',
        'block.reason': null,
        progress: 0,
        confidential: false,
      });
      expect(Object.keys(view.fields)).toEqual([
        'title',
        'block.reason',
        'progress',
        'confidential',
        'body',
      ]);
    });

    it('shows a problem without optional members as empty, not as missing', () => {
      const view = service.read(failure(problem({ status: 404, code: 'not_found' }), 404));

      expect(view.detail).toBe('');
      expect(view.requestId).toBeUndefined();
      expect(view.fields).toEqual({});
      expect(view.current).toEqual({});
    });

    it('reports status 0 as the backend being unreachable', () => {
      const view = service.read(failure(new ProgressEvent('error'), 0));

      expect(view).toEqual({
        status: 0,
        code: 'backend_unreachable',
        title: 'The backend cannot be reached',
        detail: 'The connection failed; cowork tries again on its own.',
        fields: {},
        current: {},
      });
    });

    it('calls an answer without a problem body unexpected and names its status', () => {
      const view = service.read(failure('<html>Bad Gateway</html>', 502, 'Bad Gateway'));

      expect(view).toEqual({
        status: 502,
        code: 'unexpected',
        title: 'Unexpected answer',
        detail: 'The server answered 502 without a problem body.',
        fields: {},
        current: {},
      });
    });

    it.each([
      ['a status text', 'Bad Gateway'],
      ['an empty status text, which Angular turns into Unknown Error', ''],
      ['no helpful status text', 'Unknown Error'],
    ])(
      'titles that answer Unexpected answer whatever it comes with: %s',
      (_description, statusText) => {
        expect(service.read(failure(null, 503, statusText)).title).toBe('Unexpected answer');
        expect(service.read(failure('plain text', 500, statusText)).title).toBe(
          'Unexpected answer',
        );
      },
    );

    it.each([
      ['an object without a code', { message: 'oops', status: 500 }],
      ['an object without a status', { code: 'internal' }],
      ['a code that is not a string', { code: 500, status: 500 }],
      ['a status that is not a number', { code: 'internal', status: '500' }],
      ['an array', [{ code: 'internal', status: 500 }]],
    ])('does not take %s for a problem', (_name, body) => {
      const view = service.read(failure(body, 500, 'Internal Server Error'));

      expect(view.code).toBe('unexpected');
      expect(view.title).toBe('Unexpected answer');
      expect(view.detail).toBe('The server answered 500 without a problem body.');
    });

    it('reports an error of the page itself with its message', () => {
      expect(service.read(new Error('boom'))).toEqual({
        status: 0,
        code: 'client_error',
        title: 'Something went wrong in the page',
        detail: 'boom',
        fields: {},
        current: {},
      });
    });

    it.each([
      ['a string', 'plain text', 'plain text'],
      ['a number', 42, '42'],
      ['null', null, 'null'],
      ['undefined', undefined, 'undefined'],
    ])('reports %s that was thrown as it is written', (_name, thrown, detail) => {
      const view = service.read(thrown);

      expect(view.code).toBe('client_error');
      expect(view.detail).toBe(detail);
    });

    it('reads the HTTP error that an Error carries as its cause, as a resource wraps it', () => {
      const body = problem({
        status: 409,
        code: 'state_conflict',
        title: 'Conflict',
        detail: 'Moved on.',
      });
      const wrapped = new Error('Resource returned an error', { cause: failure(body, 409) });

      const view = service.read(wrapped);

      expect(view.status).toBe(409);
      expect(view.code).toBe('state_conflict');
      expect(view.detail).toBe('Moved on.');
    });

    it('follows a chain of causes down to the original', () => {
      const original = failure(null, 0);
      const wrapped = new Error('outer', { cause: new Error('middle', { cause: original }) });

      expect(service.read(wrapped).code).toBe('backend_unreachable');
    });

    it('reports the message of the cause when the cause is a plain error', () => {
      const view = service.read(new Error('outer', { cause: new Error('inner') }));

      expect(view.code).toBe('client_error');
      expect(view.detail).toBe('inner');
    });

    describe('with the errors of a resource', () => {
      let http: HttpTestingController;

      /**
       * Starts the loads that are due, lets the promise chains of answered requests finish, and runs
       * the effects they feed. Fake timers yield the macrotask for that.
       */
      const settle = async () => {
        TestBed.tick();
        await vi.advanceTimersByTimeAsync(0);
        TestBed.tick();
      };

      beforeEach(() => {
        vi.useFakeTimers();
        http = TestBed.inject(HttpTestingController);
      });

      afterEach(() => {
        try {
          http.verify();
        } finally {
          vi.useRealTimers();
        }
      });

      it('reads the HTTP error a resource hands on as it is', async () => {
        const client = TestBed.inject(HttpClient);
        const ref = TestBed.runInInjectionContext(() =>
          resource({ loader: () => firstValueFrom(client.get('/api/v1/tickets/acme/VKO-1')) }),
        );
        TestBed.tick();

        http
          .expectOne('/api/v1/tickets/acme/VKO-1')
          .flush(problem({ status: 412, code: 'precondition_failed', request_id: 'req-9' }), {
            status: 412,
            statusText: 'Precondition Failed',
          });
        await settle();

        expect(ref.status()).toBe('error');
        expect(ref.error()).toBeInstanceOf(HttpErrorResponse);
        const view = service.read(ref.error());
        expect(view.status).toBe(412);
        expect(view.code).toBe('precondition_failed');
        expect(view.requestId).toBe('req-9');
      });

      it('reads the original of a value that is not an Error, which a resource wraps', async () => {
        const ref = TestBed.runInInjectionContext(() =>
          resource({ loader: () => Promise.reject('not an error') }),
        );
        await settle();

        expect(ref.status()).toBe('error');
        expect(ref.error()).not.toBe('not an error');
        expect(service.read(ref.error())).toMatchObject({
          code: 'client_error',
          detail: 'not an error',
        });
      });
    });
  });

  describe('report', () => {
    it('shows the problem as a toast and hands it back', () => {
      const body = problem({
        status: 404,
        code: 'not_found',
        title: 'Not found',
        detail: 'No such ticket.',
      });

      const view = service.report(failure(body, 404));

      expect(messages.add).toHaveBeenCalledTimes(1);
      expect(messages.add).toHaveBeenCalledWith({
        severity: 'warn',
        summary: 'Not found',
        detail: 'No such ticket.',
        life: 6000,
      });
      expect(view).toEqual(service.read(failure(body, 404)));
    });

    it('appends the request id to the detail, so that it can be quoted', () => {
      service.report(failure(problem({ detail: 'It broke.', request_id: 'req-3' }), 422));

      expect(messages.add).toHaveBeenCalledWith(
        expect.objectContaining({ detail: 'It broke. (request req-3)' }),
      );
    });

    it.each([
      [400, 'warn'],
      [404, 'warn'],
      [409, 'warn'],
      [412, 'warn'],
      [499, 'warn'],
      [500, 'error'],
      [503, 'error'],
    ] as const)('uses the severity of a toast for status %i: %s', (status, severity) => {
      service.report(failure(problem({ status }), status));

      expect(messages.add).toHaveBeenCalledWith(expect.objectContaining({ severity }));
    });

    it('shows an answer without a problem body as an unexpected one, an error for a 5xx', () => {
      service.report(failure('<html>Bad Gateway</html>', 502, 'Bad Gateway'));

      expect(messages.add).toHaveBeenCalledWith({
        severity: 'error',
        summary: 'Unexpected answer',
        detail: 'The server answered 502 without a problem body.',
        life: 6000,
      });
    });

    it('is an error toast when the backend cannot be reached', () => {
      service.report(failure(new ProgressEvent('error'), 0));

      expect(messages.add).toHaveBeenCalledWith(
        expect.objectContaining({ severity: 'error', summary: 'The backend cannot be reached' }),
      );
    });

    it('is an error toast for an error of the page', () => {
      service.report(new Error('boom'));

      expect(messages.add).toHaveBeenCalledWith(
        expect.objectContaining({ severity: 'error', detail: 'boom' }),
      );
    });

    describe('for a form that shows the field errors itself', () => {
      const invalid = problem({ errors: [{ pointer: '/title', message: 'must not be empty' }] });

      it('leaves the toast out when there are field errors, and still returns them', () => {
        const view = service.report(failure(invalid, 422), { fields: true });

        expect(messages.add).not.toHaveBeenCalled();
        expect(view.fields).toEqual({ title: 'must not be empty' });
      });

      it('shows the toast when the problem has no field errors', () => {
        service.report(failure(problem({ status: 409, code: 'state_conflict' }), 409), {
          fields: true,
        });

        expect(messages.add).toHaveBeenCalledTimes(1);
      });

      it('shows the toast for an error that is not a problem at all', () => {
        service.report(new Error('boom'), { fields: true });

        expect(messages.add).toHaveBeenCalledTimes(1);
      });

      it('shows the toast when the caller did not ask for the field errors', () => {
        service.report(failure(invalid, 422), { fields: false });
        service.report(failure(invalid, 422));

        expect(messages.add).toHaveBeenCalledTimes(2);
      });
    });
  });
});
