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
    reporter_agent: null,
    reporter_token: null,
    block: null,
    confidential: false,
    parent: null,
    progress: 0,
    progress_derived: false,
    progress_refinement: 0,
    progress_review: 0,
    threat: null,
    created_at: '2026-10-01T10:00:00Z',
    updated_at: '2026-10-01T10:00:00Z',
    opened_at: '2026-10-01T10:00:00Z',
    decided_at: null,
    done_at: null,
    done_from: null,
    done_by_hand: false,
    open_prerequisites: 0,
    version,
    ...overrides,
  };
}

const key = 'acme/VKO-12';
const createUrl = '/api/v1/tenants/acme/projects/VKO/tickets';
const route = '/api/v1/tenants/acme/projects/VKO/tickets/12';
const transitionUrl = `${route}/transitions`;
const rankUrl = `${route}/rank`;
const overrideUrl = `${route}/urgency-override`;
const readUrl = '/api/v1/tickets/acme/VKO-12';
/** The key a form holds for its content (docs/adr/0045 D3). */
const formKey = '0199aaaa-0000-7000-8000-00000000f0f0';

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
      const done = actions.create('acme', 'VKO', body, formKey);

      const sent = request(createUrl);
      expect(sent.request.method).toBe('POST');
      expect(sent.request.body).toEqual(body);
      sent.flush(ticket(key, 1, { title: 'New' }));

      expect((await done).key).toBe(key);
      expect(tickets.cache.value(key)?.title).toBe('New');
      expect(tickets.cache.etag(key)).toBe('"1"');
    });

    it("sends the form's Idempotency-Key, one for each content it holds (docs/adr/0045 D3)", async () => {
      const done = actions.create('acme', 'VKO', body, formKey);
      const sent = request(createUrl);
      expect(sent.request.headers.get('Idempotency-Key')).toBe(formKey);
      sent.flush(ticket('acme/VKO-1'));
      await done;
    });

    it('rejects with the HTTP error and leaves the cache alone', async () => {
      const outcome = rejection(actions.create('acme', 'VKO', body, formKey));

      request(createUrl).flush(problem(422), failed(422, 'Unprocessable Entity'));
      const error = await outcome;

      expect((error as HttpErrorResponse).status).toBe(422);
      expect(tickets.cache.ids()).toEqual([]);
    });

    it('is not followed by a refetch when its own event arrives', async () => {
      const done = actions.create('acme', 'VKO', body, formKey);
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
  describe('rank', () => {
    it('places the ticket directly after another, with no If-Match and no Idempotency-Key (docs/adr/0050 D4)', async () => {
      tickets.cache.put(key, ticket(key, 5));

      const done = actions.rank(key, { after: 3 });

      const sent = request(rankUrl);
      expect(sent.request.method).toBe('PUT');
      expect(sent.request.body).toEqual({ after: 3 });
      expect(sent.request.headers.has('If-Match')).toBe(false);
      expect(sent.request.headers.has('Idempotency-Key')).toBe(false);
      sent.flush(ticket(key, 6));

      expect((await done).version).toBe(6);
      expect(tickets.cache.value(key)?.version).toBe(6);
      expect(tickets.cache.etag(key)).toBe('"6"');
    });

    it('places the ticket directly before another', async () => {
      const done = actions.rank(key, { before: 4 });

      const sent = request(rankUrl);
      expect(sent.request.body).toEqual({ before: 4 });
      sent.flush(ticket(key, 6));
      await done;
    });

    it('needs no read of the ticket first: a move does not overwrite', async () => {
      const done = actions.rank(key, { after: 3 });

      none(readUrl);
      request(rankUrl).flush(ticket(key, 2));
      await done;
    });

    it('shows the answer of a move that changed nothing, with the version it had', async () => {
      tickets.cache.put(key, ticket(key, 5, { title: 'Held' }));

      const done = actions.rank(key, { after: 3 });
      request(rankUrl).flush(ticket(key, 5, { title: 'Held' }));

      expect((await done).version).toBe(5);
    });

    it.each([
      [400, 'validation_failed', 'Bad Request'],
      [409, 'state_conflict', 'Conflict'],
      [403, 'forbidden', 'Forbidden'],
    ])(
      'rejects with the HTTP error of a %i and leaves the cache as it was',
      async (status, code, text) => {
        tickets.cache.put(key, ticket(key, 5));
        const outcome = rejection(actions.rank(key, { after: 3 }));

        request(rankUrl).flush(problem(status, { code }), failed(status, text));
        const error = await outcome;

        expect((error as HttpErrorResponse).status).toBe(status);
        expect(tickets.cache.value(key)?.version).toBe(5);
        none(readUrl);
      },
    );

    it('is not followed by a refetch when its own event arrives', async () => {
      tickets.cache.put(key, ticket(key, 5));
      const done = actions.rank(key, { after: 3 });
      request(rankUrl).flush(ticket(key, 6));
      await done;

      stream.next({ name: 'ticket.changed', id: 'e1', key, version: 6, kind: 'ranked' });

      none(readUrl);
    });
  });

  describe('overrideUrgency', () => {
    it('sets the value with the cached version as If-Match, and shows the answer at once', async () => {
      tickets.cache.put(key, ticket(key, 5));

      const done = actions.overrideUrgency(key, 'next');

      const sent = request(overrideUrl);
      expect(sent.request.method).toBe('PUT');
      expect(sent.request.headers.get('If-Match')).toBe('"5"');
      sent.flush(ticket(key, 6, { urgency: 'next' }));

      expect((await done).urgency).toBe('next');
      expect(tickets.cache.value(key)?.urgency).toBe('next');
      expect(tickets.cache.etag(key)).toBe('"6"');
    });

    it('sends no reason when the person gave none, and does not make one up (docs/adr/0010 D3)', async () => {
      tickets.cache.put(key, ticket(key, 5));

      const done = actions.overrideUrgency(key, 'next');

      const sent = request(overrideUrl);
      expect(sent.request.body).toEqual({ value: 'next' });
      expect(Object.keys(sent.request.body as object)).toEqual(['value']);
      sent.flush(ticket(key, 6));
      await done;
    });

    it('sends no reason for an empty one', async () => {
      tickets.cache.put(key, ticket(key, 5));

      const done = actions.overrideUrgency(key, 'next', '');

      const sent = request(overrideUrl);
      expect(sent.request.body).toEqual({ value: 'next' });
      sent.flush(ticket(key, 6));
      await done;
    });

    it('sends the reason the person typed', async () => {
      tickets.cache.put(key, ticket(key, 5));

      const done = actions.overrideUrgency(key, 'now', 'The client escalated it');

      const sent = request(overrideUrl);
      expect(sent.request.body).toEqual({ value: 'now', reason: 'The client escalated it' });
      sent.flush(ticket(key, 6, { urgency: 'now' }));
      await done;
    });

    it('sends the ETag that the cache holds rather than building one', async () => {
      tickets.cache.put(key, ticket(key, 5), '"held"');

      const done = actions.overrideUrgency(key, 'next');

      const sent = request(overrideUrl);
      expect(sent.request.headers.get('If-Match')).toBe('"held"');
      sent.flush(ticket(key, 6));
      await done;
    });

    it('reads the ticket first when no view has it cached, and writes over the version it read', async () => {
      const done = actions.overrideUrgency(key, 'next');

      request(readUrl).flush(ticket(key, 3));
      await settle();
      const sent = request(overrideUrl);
      expect(sent.request.headers.get('If-Match')).toBe('"3"');
      sent.flush(ticket(key, 4, { urgency: 'next' }));

      expect((await done).version).toBe(4);
    });

    describe('on a 412', () => {
      beforeEach(() => tickets.cache.put(key, ticket(key, 5)));

      it('reads the ticket again and writes once more over the new version while its urgency is what it was', async () => {
        const done = actions.overrideUrgency(key, 'next', 'Because');

        request(overrideUrl).flush(stale, failed(412, 'Precondition Failed'));
        await settle();
        request(readUrl).flush(ticket(key, 7, { title: 'Renamed by someone' }));
        await settle();
        const again = request(overrideUrl);
        expect(again.request.headers.get('If-Match')).toBe('"7"');
        expect(again.request.body).toEqual({ value: 'next', reason: 'Because' });
        again.flush(ticket(key, 8, { urgency: 'next', title: 'Renamed by someone' }));

        expect((await done).version).toBe(8);
        expect(tickets.cache.value(key)?.urgency).toBe('next');
        expect(tickets.cache.etag(key)).toBe('"8"');
      });

      it('rejects with a StaleWrite and writes nothing more when the urgency changed meanwhile', async () => {
        const outcome = rejection(actions.overrideUrgency(key, 'next'));

        request(overrideUrl).flush(stale, failed(412, 'Precondition Failed'));
        await settle();
        request(readUrl).flush(ticket(key, 7, { urgency: 'now' }));
        const error = await outcome;
        await settle();

        expect(error).toBeInstanceOf(StaleWrite);
        expect((error as StaleWrite).current.urgency).toBe('now');
        expect((error as StaleWrite).problem.code).toBe('precondition_failed');
        none(overrideUrl);
        expect(tickets.cache.value(key)?.version).toBe(7);
      });

      it("writes once more only once: a second 412 is the person's to settle", async () => {
        const outcome = rejection(actions.overrideUrgency(key, 'next'));

        request(overrideUrl).flush(stale, failed(412, 'Precondition Failed'));
        await settle();
        request(readUrl).flush(ticket(key, 7));
        await settle();
        request(overrideUrl).flush(stale, failed(412, 'Precondition Failed'));
        await settle();
        request(readUrl).flush(ticket(key, 9));
        const error = await outcome;
        await settle();

        expect(error).toBeInstanceOf(StaleWrite);
        expect((error as StaleWrite).current.version).toBe(9);
        none(overrideUrl);
      });

      it('rejects with the error of the refetch when the ticket cannot be read again', async () => {
        const outcome = rejection(actions.overrideUrgency(key, 'next'));

        request(overrideUrl).flush(stale, failed(412, 'Precondition Failed'));
        await settle();
        request(readUrl).flush(problem(404, { code: 'not_found' }), failed(404, 'Not Found'));
        const error = await outcome;

        expect(error).not.toBeInstanceOf(StaleWrite);
        expect((error as HttpErrorResponse).status).toBe(404);
      });
    });

    it.each([
      [400, 'validation_failed', 'Bad Request'],
      [403, 'forbidden', 'Forbidden'],
      [428, 'precondition_required', 'Precondition Required'],
      [500, 'internal', 'Internal Server Error'],
    ])('rethrows a %i as it is and does not read the ticket again', async (status, code, text) => {
      tickets.cache.put(key, ticket(key, 5));
      const outcome = rejection(actions.overrideUrgency(key, 'next'));

      request(overrideUrl).flush(problem(status, { code }), failed(status, text));
      const error = await outcome;
      await settle();

      expect(error).toBeInstanceOf(HttpErrorResponse);
      expect(error).not.toBeInstanceOf(StaleWrite);
      expect((error as HttpErrorResponse).status).toBe(status);
      none(readUrl);
      expect(tickets.cache.value(key)?.version).toBe(5);
    });

    it('is not followed by a refetch when its own event arrives', async () => {
      tickets.cache.put(key, ticket(key, 5));
      const done = actions.overrideUrgency(key, 'next');
      request(overrideUrl).flush(ticket(key, 6, { urgency: 'next' }));
      await done;

      stream.next({ name: 'ticket.changed', id: 'e1', key, version: 6, kind: 'overridden' });

      none(readUrl);
    });
  });

  describe('withdrawUrgency', () => {
    it('deletes the override with the cached version as If-Match and no body, and shows the answer', async () => {
      tickets.cache.put(
        key,
        ticket(key, 5, {
          urgency: 'now',
          urgency_override: { value: 'now', reason: 'x', at: 't' },
        }),
      );

      const done = actions.withdrawUrgency(key);

      const sent = request(overrideUrl);
      expect(sent.request.method).toBe('DELETE');
      expect(sent.request.headers.get('If-Match')).toBe('"5"');
      expect(sent.request.body).toBeNull();
      sent.flush(ticket(key, 6, { urgency: 'later' }));

      expect((await done).urgency).toBe('later');
      expect(tickets.cache.value(key)?.urgency).toBe('later');
      expect(tickets.cache.etag(key)).toBe('"6"');
    });

    it('reads the ticket first when no view has it cached', async () => {
      const done = actions.withdrawUrgency(key);

      request(readUrl).flush(ticket(key, 3));
      await settle();
      const sent = request(overrideUrl);
      expect(sent.request.headers.get('If-Match')).toBe('"3"');
      sent.flush(ticket(key, 4));

      expect((await done).version).toBe(4);
    });

    it('writes once more over the new version when only another field changed', async () => {
      tickets.cache.put(key, ticket(key, 5, { urgency: 'now' }));
      const done = actions.withdrawUrgency(key);

      request(overrideUrl).flush(stale, failed(412, 'Precondition Failed'));
      await settle();
      request(readUrl).flush(ticket(key, 7, { urgency: 'now' }));
      await settle();
      const again = request(overrideUrl);
      expect(again.request.headers.get('If-Match')).toBe('"7"');
      again.flush(ticket(key, 8, { urgency: 'later' }));

      expect((await done).urgency).toBe('later');
    });

    it('rejects with a StaleWrite when somebody else set another urgency meanwhile', async () => {
      tickets.cache.put(key, ticket(key, 5, { urgency: 'now' }));
      const outcome = rejection(actions.withdrawUrgency(key));

      request(overrideUrl).flush(stale, failed(412, 'Precondition Failed'));
      await settle();
      request(readUrl).flush(ticket(key, 7, { urgency: 'next' }));
      const error = await outcome;

      expect(error).toBeInstanceOf(StaleWrite);
      expect((error as StaleWrite).current.urgency).toBe('next');
    });

    it('rethrows other failures as they are', async () => {
      tickets.cache.put(key, ticket(key, 5));
      const outcome = rejection(actions.withdrawUrgency(key));

      request(overrideUrl).flush(problem(403, { code: 'forbidden' }), failed(403, 'Forbidden'));
      const error = await outcome;

      expect((error as HttpErrorResponse).status).toBe(403);
      none(readUrl);
    });
  });

  describe('update over the version an editor began with (docs/adr/0050 D3)', () => {
    it('sends that version as If-Match, never the newer one the cache took meanwhile', async () => {
      const since = ticket(key, 5);
      tickets.cache.put(key, ticket(key, 6, { severity: 'high' }));
      const done = actions.update(key, { title: 'Mine' }, since);

      const sent = request(route);
      expect(sent.request.headers.get('If-Match')).toBe('"5"');
      expect(sent.request.body).toEqual({ title: 'Mine' });
      sent.flush(ticket(key, 7, { title: 'Mine' }));

      expect((await done).title).toBe('Mine');
      expect(tickets.cache.value(key)?.title).toBe('Mine');
    });

    it('writes once more over the new version when only another field changed meanwhile', async () => {
      const since = ticket(key, 5, { title: 'Theirs' });
      const done = actions.update(key, { title: 'Mine', comment: 'Why' }, since);

      request(route).flush(stale, failed(412, 'Precondition Failed'));
      await settle();
      request(readUrl).flush(ticket(key, 6, { title: 'Theirs', severity: 'high' }));
      await settle();
      const again = request(route);
      expect(again.request.headers.get('If-Match')).toBe('"6"');
      again.flush(ticket(key, 7, { title: 'Mine' }));

      expect((await done).version).toBe(7);
    });

    it('rejects with a StaleWrite once somebody changed the field itself', async () => {
      const since = ticket(key, 5, { title: 'Theirs' });
      const outcome = rejection(actions.update(key, { title: 'Mine' }, since));

      request(route).flush(stale, failed(412, 'Precondition Failed'));
      await settle();
      request(readUrl).flush(ticket(key, 6, { title: 'Somebody else' }));
      const error = await outcome;
      await settle();

      expect(error).toBeInstanceOf(StaleWrite);
      expect((error as StaleWrite).current.title).toBe('Somebody else');
      none(route);
    });

    it('compares a parent by its key, and none as none', async () => {
      const since = ticket(key, 5);
      const done = actions.update(key, { parent: 'acme/VKO-3' }, since);

      request(route).flush(stale, failed(412, 'Precondition Failed'));
      await settle();
      request(readUrl).flush(ticket(key, 6, { parent: null }));
      await settle();
      request(route).flush(ticket(key, 7, { parent: 'acme/VKO-3' }));

      expect((await done).parent).toBe('acme/VKO-3');
    });
  });

  describe('replaceBody (docs/adr/0011 D1)', () => {
    const bodyUrl = `${route}/body`;

    it('replaces the body as a whole over the version the editor began with, and shows the answer', async () => {
      tickets.cache.put(key, ticket(key, 6));
      const done = actions.replaceBody(key, '## Current state\n\nNew.', ticket(key, 5));

      const sent = request(bodyUrl);
      expect(sent.request.method).toBe('PUT');
      expect(sent.request.headers.get('If-Match')).toBe('"5"');
      expect(sent.request.body).toEqual({ body: '## Current state\n\nNew.' });
      sent.flush(ticket(key, 7, { body: '## Current state\n\nNew.' }));

      expect((await done).version).toBe(7);
      expect(tickets.cache.value(key)?.body).toBe('## Current state\n\nNew.');
    });

    it('writes once more while the body is still the one the editor began with', async () => {
      const done = actions.replaceBody(key, 'Mine', ticket(key, 5, { body: 'Old' }));

      request(bodyUrl).flush(stale, failed(412, 'Precondition Failed'));
      await settle();
      request(readUrl).flush(ticket(key, 6, { body: 'Old', state: 'analysed' }));
      await settle();
      const again = request(bodyUrl);
      expect(again.request.headers.get('If-Match')).toBe('"6"');
      again.flush(ticket(key, 7, { body: 'Mine' }));

      expect((await done).body).toBe('Mine');
    });

    it('rejects with a StaleWrite that carries the body somebody else wrote', async () => {
      const outcome = rejection(actions.replaceBody(key, 'Mine', ticket(key, 5, { body: 'Old' })));

      request(bodyUrl).flush(stale, failed(412, 'Precondition Failed'));
      await settle();
      request(readUrl).flush(ticket(key, 6, { body: 'Theirs' }));
      const error = await outcome;
      await settle();

      expect(error).toBeInstanceOf(StaleWrite);
      expect((error as StaleWrite).current.body).toBe('Theirs');
      none(bodyUrl);
    });
  });

  describe('setConfidential (docs/adr/0065)', () => {
    const flagUrl = `${route}/confidential`;

    it('sets the flag with the cached version as If-Match and without a reason not given', async () => {
      tickets.cache.put(key, ticket(key, 5));
      const done = actions.setConfidential(key, true);

      await settle();
      const sent = request(flagUrl);
      expect(sent.request.method).toBe('PUT');
      expect(sent.request.headers.get('If-Match')).toBe('"5"');
      expect(sent.request.body).toEqual({ confidential: true });
      sent.flush(ticket(key, 6, { confidential: true }));

      expect((await done).confidential).toBe(true);
      expect(tickets.cache.value(key)?.confidential).toBe(true);
    });

    it('lifts it with the reason', async () => {
      tickets.cache.put(key, ticket(key, 5, { confidential: true }));
      const done = actions.setConfidential(key, false, 'Fixed and released');

      await settle();
      const sent = request(flagUrl);
      expect(sent.request.body).toEqual({ confidential: false, reason: 'Fixed and released' });
      sent.flush(ticket(key, 6));

      expect((await done).confidential).toBe(false);
    });

    it('writes once more while the flag is as it was, and is a StaleWrite once somebody changed it', async () => {
      tickets.cache.put(key, ticket(key, 5));
      const first = actions.setConfidential(key, true);
      await settle();
      request(flagUrl).flush(stale, failed(412, 'Precondition Failed'));
      await settle();
      request(readUrl).flush(ticket(key, 6, { title: 'Renamed' }));
      await settle();
      request(flagUrl).flush(ticket(key, 7, { confidential: true }));
      expect((await first).version).toBe(7);

      const second = rejection(actions.setConfidential(key, false, 'Why'));
      await settle();
      request(flagUrl).flush(stale, failed(412, 'Precondition Failed'));
      await settle();
      request(readUrl).flush(ticket(key, 8, { confidential: false }));
      const error = await second;
      await settle();

      expect(error).toBeInstanceOf(StaleWrite);
      none(flagUrl);
    });
  });
});
