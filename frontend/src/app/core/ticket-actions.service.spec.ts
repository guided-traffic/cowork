import { HttpErrorResponse, provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { TestBed } from '@angular/core/testing';
import { MessageService } from 'primeng/api';
import { Subject } from 'rxjs';
import { provideApiConfiguration } from '../api/api-configuration';
import { Ticket, TicketCreate } from '../api/models';
import { EventStreamService, StreamEvent } from './event-stream.service';
import { routeOf, StaleWrite, TicketActions } from './ticket-actions.service';
import { TicketsService } from './tickets.service';

function ticket(key: string, version = 1, overrides: Partial<Ticket> = {}): Ticket {
  const [, short] = key.split('/');
  const [project, number] = short.split('-');
  return {
    id: `0199aaaa-0000-7000-8000-${number.padStart(12, '0')}`,
    key,
    number: Number(number),
    project,
    title: `Ticket ${key}`,
    body: '',
    type: 'task',
    state: 'filed',
    severity: 'medium',
    security: 'none',
    effort: 'M',
    urgency: 'later',
    urgency_derived: 'later',
    urgency_override: null,
    urgency_rule: 'v1:default',
    assignee: null,
    reporter: { id: 'p1', display_name: 'Hans' },
    block: null,
    confidential: false,
    parent: null,
    progress: 0,
    progress_derived: false,
    threat: null,
    created_at: '2026-10-01T10:00:00Z',
    updated_at: '2026-10-01T10:00:00Z',
    opened_at: '2026-10-01T10:00:00Z',
    decided_at: null,
    done_at: null,
    version,
    ...overrides,
  };
}

const key = 'acme/VKO-12';
const createUrl = '/api/v1/tenants/acme/projects/VKO/tickets';
const route = '/api/v1/tenants/acme/projects/VKO/tickets/12';
const transitionUrl = `${route}/transitions`;
const readUrl = '/api/v1/tickets/acme/VKO-12';
const uuid = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/;

const problem = (status: number, extra: object = {}) => ({
  type: 'about:blank',
  title: 'Problem',
  status,
  code: status === 412 ? 'precondition_failed' : 'internal',
  ...extra,
});
const stale = problem(412, {
  title: 'The ticket changed',
  detail: 'The ticket changed since you read it.',
  errors: [{ pointer: '/title', message: 'changed meanwhile', current: 'Their title' }],
});
const failed = (status: number, statusText: string) => ({ status, statusText });

describe('routeOf', () => {
  it('turns a canonical key into the address of the ticket routes', () => {
    expect(routeOf('acme/VKO-12')).toEqual({ tenant: 'acme', project: 'VKO', number: 12 });
  });

  it('takes the number after the last dash, so that a project key may hold dashes', () => {
    expect(routeOf('acme/MY-APP-7')).toEqual({ tenant: 'acme', project: 'MY-APP', number: 7 });
  });

  it('makes the number a number', () => {
    expect(typeof routeOf('acme/VKO-12').number).toBe('number');
  });
});

describe('StaleWrite', () => {
  it('is an error that carries the problem and the current ticket', () => {
    const problemView = {
      status: 412,
      code: 'precondition_failed',
      title: 'The ticket changed',
      detail: 'Somebody else wrote.',
      fields: {},
      current: {},
    };
    const current = ticket(key, 7);

    const error = new StaleWrite(problemView, current);

    expect(error).toBeInstanceOf(Error);
    expect(error.problem).toBe(problemView);
    expect(error.current).toBe(current);
    expect(error.message).toBe('Somebody else wrote.');
  });

  it('says that the ticket changed when the problem has no detail', () => {
    const error = new StaleWrite(
      { status: 412, code: 'precondition_failed', title: 'x', detail: '', fields: {}, current: {} },
      ticket(key, 7),
    );

    expect(error.message).toBe('The ticket changed since it was read');
  });
});

describe('TicketActions', () => {
  let actions: TicketActions;
  let tickets: TicketsService;
  let http: HttpTestingController;
  let stream: Subject<StreamEvent>;

  /**
   * Starts the loads that are due, lets the promise chains of answered requests finish, and runs the
   * effects they feed. Fake timers yield the macrotask for that, so this never waits for a request the
   * test has not answered yet.
   */
  const settle = async () => {
    TestBed.tick();
    await vi.advanceTimersByTimeAsync(0);
    TestBed.tick();
  };
  const request = (url: string) => http.expectOne((r) => r.url === url);
  const none = (url: string) => http.expectNone((r) => r.url === url);
  /** What a rejected promise rejected with. */
  const rejection = (promise: Promise<unknown>) =>
    promise.then(
      () => null,
      (error: unknown) => error,
    );

  beforeEach(async () => {
    vi.useFakeTimers();
    stream = new Subject<StreamEvent>();
    TestBed.configureTestingModule({
      providers: [
        provideHttpClient(),
        provideHttpClientTesting(),
        provideApiConfiguration(''),
        MessageService,
        { provide: EventStreamService, useValue: { events: stream.asObservable() } },
      ],
    });
    actions = TestBed.inject(TicketActions);
    tickets = TestBed.inject(TicketsService);
    http = TestBed.inject(HttpTestingController);
    await settle();
    request('/api/v1/me').flush({ id: 'p1', display_name: 'Hans', memberships: [] });
    await settle();
  });

  afterEach(() => {
    try {
      http.verify();
    } finally {
      vi.useRealTimers();
      TestBed.resetTestingModule();
    }
  });

  describe('create', () => {
    const body: TicketCreate = {
      title: 'New',
      type: 'bug',
      severity: 'high',
      security: 'none',
      effort: 'S',
    };

    it('posts the ticket to its project, puts the answer into the cache and hands it back', async () => {
      const done = actions.create('acme', 'VKO', body);

      const sent = request(createUrl);
      expect(sent.request.method).toBe('POST');
      expect(sent.request.body).toEqual(body);
      sent.flush(ticket(key, 1, { title: 'New' }));

      expect((await done).key).toBe(key);
      expect(tickets.cache.value(key)?.title).toBe('New');
      expect(tickets.cache.etag(key)).toBe('"1"');
    });

    it('sends an Idempotency-Key of its own for every act (docs/adr/0045 D3)', async () => {
      const first = actions.create('acme', 'VKO', body);
      const firstKey = request(createUrl);
      firstKey.flush(ticket('acme/VKO-1'));
      await first;
      const second = actions.create('acme', 'VKO', body);
      const secondKey = request(createUrl);
      secondKey.flush(ticket('acme/VKO-2'));
      await second;

      const keys = [firstKey, secondKey].map((r) => r.request.headers.get('Idempotency-Key'));
      expect(keys[0]).toMatch(uuid);
      expect(keys[1]).toMatch(uuid);
      expect(keys[0]).not.toBe(keys[1]);
    });

    it('rejects with the HTTP error and leaves the cache alone', async () => {
      const outcome = rejection(actions.create('acme', 'VKO', body));

      request(createUrl).flush(problem(422), failed(422, 'Unprocessable Entity'));
      const error = await outcome;

      expect((error as HttpErrorResponse).status).toBe(422);
      expect(tickets.cache.ids()).toEqual([]);
    });

    it('is not followed by a refetch when its own event arrives', async () => {
      const done = actions.create('acme', 'VKO', body);
      request(createUrl).flush(ticket(key, 1));
      await done;

      stream.next({ name: 'ticket.changed', id: 'e1', key, version: 1, kind: 'created' });

      none(readUrl);
    });
  });

  describe('update', () => {
    it('sends the changes with the cached version as If-Match, and shows the answer at once', async () => {
      tickets.cache.put(key, ticket(key, 5));

      const done = actions.update(key, { title: 'Renamed' });

      const sent = request(route);
      expect(sent.request.method).toBe('PATCH');
      expect(sent.request.headers.get('If-Match')).toBe('"5"');
      expect(sent.request.body).toEqual({ title: 'Renamed' });
      sent.flush(ticket(key, 6, { title: 'Renamed' }));

      expect((await done).version).toBe(6);
      expect(tickets.cache.value(key)?.title).toBe('Renamed');
      expect(tickets.cache.etag(key)).toBe('"6"');
    });

    it('sends the ETag that the cache holds rather than building one', async () => {
      tickets.cache.put(key, ticket(key, 5), '"held"');

      const done = actions.update(key, { progress: 50 });

      const sent = request(route);
      expect(sent.request.headers.get('If-Match')).toBe('"held"');
      sent.flush(ticket(key, 6));
      await done;
    });

    it('reads the ticket first when no view has it cached, and writes over the version it read', async () => {
      const done = actions.update(key, { title: 'Renamed' });

      request(readUrl).flush(ticket(key, 3));
      await settle();
      const sent = request(route);
      expect(sent.request.headers.get('If-Match')).toBe('"3"');
      sent.flush(ticket(key, 4, { title: 'Renamed' }));

      expect((await done).version).toBe(4);
      expect(tickets.cache.value(key)?.title).toBe('Renamed');
    });

    it('falls back to the version it read when the cache keeps the ticket under another key', async () => {
      const done = actions.update('acme/vko-12', { title: 'Renamed' });

      request('/api/v1/tickets/acme/vko-12').flush(ticket(key, 3));
      await settle();
      const sent = request('/api/v1/tenants/acme/projects/vko/tickets/12');
      expect(sent.request.headers.get('If-Match')).toBe('"3"');
      sent.flush(ticket(key, 4));
      await done;
    });

    it('rejects when the ticket it has to read first cannot be read, and writes nothing', async () => {
      const outcome = rejection(actions.update(key, { title: 'Renamed' }));

      request(readUrl).flush(problem(404, { code: 'not_found' }), failed(404, 'Not Found'));
      const error = await outcome;

      expect((error as HttpErrorResponse).status).toBe(404);
      none(route);
    });

    describe('on a 412, the ticket having changed since it was read (docs/adr/0050 D5)', () => {
      beforeEach(() => tickets.cache.put(key, ticket(key, 5)));

      it('rejects with a StaleWrite that carries the problem and the ticket as it is now', async () => {
        const outcome = rejection(actions.update(key, { title: 'Mine' }));

        request(route).flush(stale, failed(412, 'Precondition Failed'));
        await settle();
        request(readUrl).flush(ticket(key, 7, { title: 'Their title' }));
        const error = await outcome;

        expect(error).toBeInstanceOf(StaleWrite);
        const write = error as StaleWrite;
        expect(write.problem.code).toBe('precondition_failed');
        expect(write.problem.fields).toEqual({ title: 'changed meanwhile' });
        expect(write.problem.current).toEqual({ title: 'Their title' });
        expect(write.current.version).toBe(7);
        expect(write.current.title).toBe('Their title');
        expect(write.message).toBe('The ticket changed since you read it.');
      });

      it('shows the fresh ticket everywhere, so that the next write can go over it', async () => {
        const outcome = rejection(actions.update(key, { title: 'Mine' }));
        request(route).flush(stale, failed(412, 'Precondition Failed'));
        await settle();
        request(readUrl).flush(ticket(key, 7));
        await outcome;

        expect(tickets.cache.value(key)?.version).toBe(7);
        expect(tickets.cache.etag(key)).toBe('"7"');
      });

      it('rejects with the error of the refetch when the ticket cannot be read again', async () => {
        const outcome = rejection(actions.update(key, { title: 'Mine' }));

        request(route).flush(stale, failed(412, 'Precondition Failed'));
        await settle();
        request(readUrl).flush(problem(404, { code: 'not_found' }), failed(404, 'Not Found'));
        const error = await outcome;

        expect(error).not.toBeInstanceOf(StaleWrite);
        expect((error as HttpErrorResponse).status).toBe(404);
      });
    });

    it.each([
      [404, 'Not Found'],
      [422, 'Unprocessable Entity'],
      [428, 'Precondition Required'],
      [500, 'Internal Server Error'],
    ])('rethrows a %i as it is and does not read the ticket again', async (status, statusText) => {
      tickets.cache.put(key, ticket(key, 5));
      const outcome = rejection(actions.update(key, { title: 'Mine' }));

      request(route).flush(problem(status), failed(status, statusText));
      const error = await outcome;
      await settle();

      expect(error).toBeInstanceOf(HttpErrorResponse);
      expect(error).not.toBeInstanceOf(StaleWrite);
      expect((error as HttpErrorResponse).status).toBe(status);
      none(readUrl);
      expect(tickets.cache.value(key)?.version).toBe(5);
    });

    it('rethrows a failure of the connection as it is', async () => {
      tickets.cache.put(key, ticket(key, 5));
      const outcome = rejection(actions.update(key, { title: 'Mine' }));

      request(route).error(new ProgressEvent('error'));

      expect((await outcome) as HttpErrorResponse).toMatchObject({ status: 0 });
    });

    it('is not followed by a refetch when its own event arrives', async () => {
      tickets.cache.put(key, ticket(key, 5));
      const done = actions.update(key, { title: 'Renamed' });
      request(route).flush(ticket(key, 6));
      await done;

      stream.next({ name: 'ticket.changed', id: 'e1', key, version: 6, kind: 'edited' });

      none(readUrl);
    });
  });

  describe('transition', () => {
    it('moves the ticket from the state the cache holds, with no key and no version (docs/adr/0045 D2)', async () => {
      tickets.cache.put(key, ticket(key, 5, { state: 'analysed' }));

      const done = actions.transition(key, { to: 'decided' });

      const sent = request(transitionUrl);
      expect(sent.request.method).toBe('POST');
      expect(sent.request.body).toEqual({ to: 'decided', from: 'analysed' });
      expect(sent.request.headers.has('Idempotency-Key')).toBe(false);
      expect(sent.request.headers.has('If-Match')).toBe(false);
      sent.flush(ticket(key, 6, { state: 'decided' }));

      expect((await done).state).toBe('decided');
      expect(tickets.cache.value(key)?.state).toBe('decided');
      expect(tickets.cache.etag(key)).toBe('"6"');
    });

    it('reads the ticket first when no view has it cached, and moves it from the state it read', async () => {
      const done = actions.transition(key, { to: 'in-progress' });

      request(readUrl).flush(ticket(key, 3, { state: 'decided' }));
      await settle();
      const sent = request(transitionUrl);
      expect(sent.request.body).toEqual({ to: 'in-progress', from: 'decided' });
      sent.flush(ticket(key, 4, { state: 'in-progress' }));

      expect((await done).state).toBe('in-progress');
    });

    it('carries the reason, the note and the block of the move along', async () => {
      tickets.cache.put(key, ticket(key, 5, { state: 'in-progress' }));
      const block = { kind: 'human', reason: 'Waits for the owner' } as const;

      const done = actions.transition(key, {
        to: 'blocked',
        reason: 'Waits for the owner',
        comment: 'Asked in the thread.',
        note: 'n',
        block,
        override_prerequisites: false,
      });

      const sent = request(transitionUrl);
      expect(sent.request.body).toEqual({
        to: 'blocked',
        reason: 'Waits for the owner',
        comment: 'Asked in the thread.',
        note: 'n',
        block,
        override_prerequisites: false,
        from: 'in-progress',
      });
      sent.flush(ticket(key, 6, { state: 'blocked' }));
      await done;
    });

    it('sends the state the cache holds as from, whatever the move says', async () => {
      tickets.cache.put(key, ticket(key, 5, { state: 'filed' }));

      const done = actions.transition(key, { to: 'analysed', from: 'done' } as { to: 'analysed' });

      const sent = request(transitionUrl);
      expect((sent.request.body as { from: string }).from).toBe('filed');
      sent.flush(ticket(key, 6, { state: 'analysed' }));
      await done;
    });

    it('rejects with the HTTP error of a 409 and leaves the cache as it was', async () => {
      tickets.cache.put(key, ticket(key, 5, { state: 'filed' }));
      const outcome = rejection(actions.transition(key, { to: 'analysed' }));

      request(transitionUrl).flush(
        problem(409, { code: 'state_conflict' }),
        failed(409, 'Conflict'),
      );
      const error = await outcome;

      expect((error as HttpErrorResponse).status).toBe(409);
      expect(tickets.cache.value(key)?.state).toBe('filed');
    });

    it('is not followed by a refetch when its own event arrives', async () => {
      tickets.cache.put(key, ticket(key, 5, { state: 'filed' }));
      const done = actions.transition(key, { to: 'analysed' });
      request(transitionUrl).flush(ticket(key, 6, { state: 'analysed' }));
      await done;

      stream.next({ name: 'ticket.changed', id: 'e1', key, version: 6, kind: 'transitioned' });

      none(readUrl);
    });
  });
});
