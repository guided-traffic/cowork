import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { signal, WritableSignal } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { provideRouter, Router } from '@angular/router';
import { MessageService } from 'primeng/api';
import type { MockInstance } from 'vitest';
import { provideApiConfiguration } from '../api/api-configuration';
import { ChatAvailability, ChatMessage, ChatTurn, Me, Problem } from '../api/models';
import {
  CallEntry,
  CHAT_FETCH,
  ChatService,
  navigable,
  pageContext,
  TurnRecord,
} from './chat.service';
import { SessionService } from './session.service';

const uuid = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/;

const ada: Me = {
  id: 'p1',
  display_name: 'Ada Lovelace',
  memberships: [],
  global_admin: false,
  local: true,
  password_change_required: false,
};

const open: ChatAvailability = {
  available: true,
  providers: [
    { id: 'lmstudio', name: 'LM Studio', kind: 'openai', model: 'qwen/qwen3-30b-a3b-2507' },
    { id: 'claude', name: 'Claude', kind: 'anthropic', model: 'claude-sonnet-4-5' },
  ],
  reason: null,
};

/** A response body that hands out the pieces a test gives it, as a network would. */
class Body {
  private controller!: ReadableStreamDefaultController<Uint8Array>;
  readonly stream = new ReadableStream<Uint8Array>({
    start: (controller) => {
      this.controller = controller;
    },
  });

  event(name: string, data: unknown): void {
    this.controller.enqueue(
      new TextEncoder().encode(`event: ${name}\ndata: ${JSON.stringify(data)}\n\n`),
    );
  }

  end(): void {
    this.controller.close();
  }

  fail(error: unknown): void {
    this.controller.error(error);
  }
}

/** One request of a turn, as the fake `fetch` got it, with the ways a test answers it. */
interface Sent {
  url: string;
  init: RequestInit;
  turn: ChatTurn;
  body: Body;
  /** Answers with the stream of `body`: 200, the event stream, unless a status is given. */
  stream(status?: number): void;
  /** Answers with a status and a body, as a refusal before the stream. */
  refuse(status: number, body: unknown): void;
  /** No answer comes: the connection failed. */
  fail(): void;
}

function problem(status: number, code: Problem['code'], detail: string): Problem {
  return { type: 'about:blank', title: `Refused ${code}`, status, code, detail };
}

describe('ChatService', () => {
  let tenant: WritableSignal<string | null>;
  let person: WritableSignal<Me | undefined>;
  let sent: Sent[];
  let http: HttpTestingController;
  let router: Router;
  let url: string;
  let navigateByUrl: MockInstance<Router['navigateByUrl']>;
  let navigate: MockInstance<Router['navigate']>;
  let service: ChatService;

  /** Runs the effects, lets the answered requests and the streamed events finish, runs them again. */
  const settle = async () => {
    TestBed.tick();
    await new Promise((resolve) => setTimeout(resolve));
    TestBed.tick();
  };

  const availability = (slug: string) =>
    http.expectOne((request) => request.url === `/api/v1/tenants/${slug}/chat`);

  /** The request that stops the person's turns in a tenant, answered 204. */
  const stopped = (slug = 'acme') =>
    http
      .expectOne(
        (request) =>
          request.method === 'DELETE' && request.url === `/api/v1/tenants/${slug}/chat/turns`,
      )
      .flush(null, { status: 204, statusText: 'No Content' });

  /** The person's chat capabilities, read once the panel is open. */
  const capabilitiesRead = () =>
    http.expectOne((request) => request.method === 'GET' && request.url === '/api/v1/me/chat');

  /** The last turn's request, answered with its stream. */
  const streaming = () => {
    const last = sent[sent.length - 1];
    last.stream();
    return last;
  };

  const kinds = () => service.entries().map((entry) => entry.kind);
  const calls = () =>
    service.entries().filter((entry): entry is CallEntry => entry.kind === 'call');
  const lastEntry = () => service.entries()[service.entries().length - 1];

  function fakeFetch(input: RequestInfo | URL, init?: RequestInit): Promise<Response> {
    return new Promise<Response>((resolve, reject) => {
      const body = new Body();
      const request = init as RequestInit;
      sent.push({
        url: String(input),
        init: request,
        turn: JSON.parse(request.body as string) as ChatTurn,
        body,
        stream: (status = 200) =>
          resolve(
            new Response(body.stream, {
              status,
              headers: { 'Content-Type': 'text/event-stream' },
            }),
          ),
        refuse: (status, answer) =>
          resolve(
            new Response(typeof answer === 'string' ? answer : JSON.stringify(answer), {
              status,
            }),
          ),
        fail: () => reject(new TypeError('Failed to fetch')),
      });
      request.signal?.addEventListener('abort', () => {
        const aborted = new DOMException('The operation was aborted.', 'AbortError');
        reject(aborted);
        body.fail(aborted);
      });
    });
  }

  /** Sends a message and answers its request with a stream; resolves once the turn began. */
  async function begin(text = 'File a bug'): Promise<{ turn: Sent; begun: Promise<boolean> }> {
    const begun = service.send(text);
    const turn = streaming();
    await settle();
    return { turn, begun };
  }

  beforeEach(async () => {
    tenant = signal<string | null>('acme');
    person = signal<Me | undefined>(ada);
    sent = [];
    url = '/t/acme/p/COW/board';
    localStorage.clear();
    TestBed.configureTestingModule({
      providers: [
        provideHttpClient(),
        provideHttpClientTesting(),
        provideApiConfiguration(''),
        provideRouter([]),
        MessageService,
        { provide: SessionService, useValue: { tenant, workTenant: tenant, person } },
        { provide: CHAT_FETCH, useValue: fakeFetch },
      ],
    });
    http = TestBed.inject(HttpTestingController);
    router = TestBed.inject(Router);
    vi.spyOn(router, 'url', 'get').mockImplementation(() => url);
    navigateByUrl = vi.spyOn(router, 'navigateByUrl').mockResolvedValue(true);
    navigate = vi.spyOn(router, 'navigate').mockResolvedValue(true);
    service = TestBed.inject(ChatService);
    await settle();
    availability('acme').flush(open);
    await settle();
  });

  afterEach(() => {
    try {
      http.verify();
    } finally {
      localStorage.clear();
      vi.restoreAllMocks();
      TestBed.resetTestingModule();
    }
  });

  describe('the availability', () => {
    it('is asked of the tenant the pages show, and says the chat is there', () => {
      expect(service.availability.value()).toEqual(open);
      expect(service.available()).toBe(true);
    });

    it('is no chat while the tenant says so', async () => {
      tenant.set('globex');
      await settle();
      expect(service.available()).toBe(false);

      availability('globex').flush({ available: false, providers: [], reason: 'not_configured' });
      await settle();

      expect(service.available()).toBe(false);
      expect(service.availability.value()?.reason).toBe('not_configured');
      expect(service.providers()).toEqual([]);
      expect(service.provider()).toBeNull();
    });

    it('is no chat when it cannot be read', async () => {
      tenant.set('globex');
      await settle();

      availability('globex').flush(problem(500, 'internal', 'down'), {
        status: 500,
        statusText: 'Internal',
      });
      await settle();

      expect(service.available()).toBe(false);
    });

    it('asks nothing outside a tenant, where there is no chat', async () => {
      tenant.set(null);
      await settle();

      expect(service.available()).toBe(false);
      http.expectNone(() => true);
    });

    it('is asked again when asked', async () => {
      service.reloadAvailability();
      await settle();

      availability('acme').flush({ ...open, providers: open.providers.slice(1) });
      await settle();

      expect(service.providers().map((each) => each.id)).toEqual(['claude']);
    });
  });

  describe('a message', () => {
    it("is one POST to the tenant's chat with the session's CSRF header and the cookie, answered as a stream", async () => {
      void service.send('File a bug for the login');

      expect(sent).toHaveLength(1);
      const [request] = sent;
      expect(request.url).toBe('/api/v1/tenants/acme/chat');
      expect(request.init.method).toBe('POST');
      expect(request.init.headers).toEqual({
        'Content-Type': 'application/json',
        Accept: 'text/event-stream',
        'X-Requested-With': 'cowork',
      });
      expect(request.init.credentials).toBe('same-origin');
      expect(request.init.signal).toBeInstanceOf(AbortSignal);
      streaming().body.end();
      await settle();
    });

    it('sends the conversation, the message and the page the person is on', async () => {
      void service.send('File a bug for the login');

      expect(sent[0].turn).toEqual({
        conversation: expect.stringMatching(uuid),
        messages: [{ role: 'user', text: 'File a bug for the login' }],
        provider: 'lmstudio',
        context: { path: '/t/acme/p/COW/board', project: 'COW' },
      });
      expect(Object.keys(sent[0].turn)).not.toContain('confirmations');
      streaming().body.end();
      await settle();
    });

    it('writes the tenant into the path as one segment', async () => {
      tenant.set('a b');
      await settle();
      http.match(() => true).forEach((request) => request.flush(open));

      void service.send('Hi');

      expect(sent[0].url).toBe('/api/v1/tenants/a%20b/chat');
      streaming().body.end();
      await settle();
    });

    it('is sent without the space around it, and an empty one is not sent', async () => {
      expect(await service.send('   \n ')).toBe(false);
      expect(sent).toHaveLength(0);

      void service.send('  two lines\nof text  ');

      expect(sent[0].turn.messages).toEqual([{ role: 'user', text: 'two lines\nof text' }]);
      streaming().body.end();
      await settle();
    });

    it('is not sent outside a tenant', async () => {
      tenant.set(null);
      await settle();

      expect(await service.send('Hi')).toBe(false);
      expect(sent).toHaveLength(0);
    });

    it('shows at once, and the service is busy until the turn ends', async () => {
      const { turn, begun } = await begin('File a bug');

      expect(service.entries()).toEqual([
        { id: expect.any(Number), kind: 'user', text: 'File a bug' },
      ]);
      expect(service.busy()).toBe(true);

      turn.body.event('done', { messages: [], reason: 'answered' });
      await settle();

      expect(await begun).toBe(true);
      expect(service.busy()).toBe(false);
    });

    it("shows the assistant's text in one entry as its pieces arrive", async () => {
      const { turn } = await begin();

      turn.body.event('text', { delta: 'Filed ' });
      await settle();
      expect(lastEntry()).toEqual({ id: expect.any(Number), kind: 'assistant', text: 'Filed ' });

      turn.body.event('text', { delta: 'COW-12.\n<b>Done</b>' });
      await settle();

      expect(kinds()).toEqual(['user', 'assistant']);
      expect(lastEntry()).toEqual({
        id: expect.any(Number),
        kind: 'assistant',
        text: 'Filed COW-12.\n<b>Done</b>',
      });
      turn.body.end();
      await settle();
    });

    it('marks the answer of a turn that called no tool: nothing in it was looked up', async () => {
      const { turn } = await begin('Which tickets are in WEB?');

      turn.body.event('text', { delta: 'WEB-1, WEB-2 and WEB-3.' });
      turn.body.event('done', {
        messages: [{ role: 'assistant', text: 'WEB-1, WEB-2 and WEB-3.' }],
        reason: 'answered',
      });
      await settle();

      expect(kinds()).toEqual(['user', 'assistant']);
      expect(lastEntry()).toEqual({
        id: expect.any(Number),
        kind: 'assistant',
        text: 'WEB-1, WEB-2 and WEB-3.',
        noTools: true,
      });
    });

    it('does not mark the answer of a turn that called a tool', async () => {
      const { turn } = await begin('Which tickets are in WEB?');

      turn.body.event('tool_call', { id: 'c1', name: 'search', arguments: { project: 'WEB' } });
      turn.body.event('tool_result', { id: 'c1', ok: true, summary: 'WEB-1' });
      turn.body.event('text', { delta: 'WEB-1 only.' });
      turn.body.event('done', {
        messages: [
          {
            role: 'assistant',
            tool_calls: [{ id: 'c1', name: 'search', arguments: { project: 'WEB' } }],
          },
          { role: 'tool', tool_call_id: 'c1', ok: true, text: 'WEB-1' },
          { role: 'assistant', text: 'WEB-1 only.' },
        ],
        reason: 'answered',
      });
      await settle();

      expect(lastEntry()).toEqual({
        id: expect.any(Number),
        kind: 'assistant',
        text: 'WEB-1 only.',
      });
    });

    it('does not mark text a turn did not finish as its answer', async () => {
      const { turn } = await begin();

      turn.body.event('text', { delta: 'I will look' });
      turn.body.end();
      await settle();

      expect(kinds()).toEqual(['user', 'assistant', 'notice']);
      expect(service.entries()[1]).toEqual({
        id: expect.any(Number),
        kind: 'assistant',
        text: 'I will look',
      });
    });

    it('runs one turn at a time', async () => {
      const { turn } = await begin();

      expect(await service.send('Another')).toBe(false);
      expect(sent).toHaveLength(1);
      expect(kinds()).toEqual(['user']);

      turn.body.end();
      await settle();
    });
  });

  describe('the conversation', () => {
    const filed: ChatMessage[] = [
      {
        role: 'assistant',
        tool_calls: [{ id: 'c1', name: 'file_ticket', arguments: { title: 'Login fails' } }],
      },
      { role: 'tool', tool_call_id: 'c1', ok: true, text: 'Filed COW-12' },
      { role: 'assistant', text: 'Filed COW-12.' },
    ];

    it('gets the messages of done as they are, and sends them with the next message', async () => {
      const { turn } = await begin('File a bug');
      turn.body.event('done', { messages: filed, reason: 'answered' });
      await settle();

      void service.send('Rank it to now');

      expect(sent[1].turn.messages).toEqual([
        { role: 'user', text: 'File a bug' },
        ...filed,
        { role: 'user', text: 'Rank it to now' },
      ]);
      expect(sent[1].turn.conversation).toBe(sent[0].turn.conversation);
      streaming().body.end();
      await settle();
    });

    it('shows a call as a card that is running until its result came', async () => {
      const { turn } = await begin();

      turn.body.event('tool_call', { id: 'c1', name: 'file_ticket', arguments: { title: 'A' } });
      await settle();
      expect(calls()).toEqual([
        {
          id: expect.any(Number),
          kind: 'call',
          call: { id: 'c1', name: 'file_ticket', arguments: { title: 'A' } },
          state: 'running',
        },
      ]);

      turn.body.event('tool_result', { id: 'c1', ok: true, summary: 'Filed COW-12' });
      await settle();

      expect(calls()[0]).toEqual(expect.objectContaining({ state: 'ok', summary: 'Filed COW-12' }));
      turn.body.end();
      await settle();
    });

    it('shows a refused call as failed, with what the API answered', async () => {
      const { turn } = await begin();

      turn.body.event('tool_call', { id: 'c1', name: 'transition', arguments: {} });
      turn.body.event('tool_result', { id: 'c1', ok: false, summary: 'state_conflict' });
      await settle();

      expect(calls()[0]).toEqual(
        expect.objectContaining({ state: 'failed', summary: 'state_conflict' }),
      );
      turn.body.end();
      await settle();
    });

    it('gives a call a card of its own when the model uses an id an earlier call had', async () => {
      const { turn } = await begin();
      turn.body.event('tool_call', { id: 'c1', name: 'search', arguments: { q: 'a' } });
      turn.body.event('tool_result', { id: 'c1', ok: true, summary: 'one' });

      turn.body.event('tool_call', { id: 'c1', name: 'search', arguments: { q: 'b' } });
      turn.body.event('tool_result', { id: 'c1', ok: true, summary: 'two' });
      turn.body.event('done', { messages: [], reason: 'answered' });
      await settle();

      expect(calls().map((card) => [card.call.arguments, card.state, card.summary])).toEqual([
        [{ q: 'a' }, 'ok', 'one'],
        [{ q: 'b' }, 'ok', 'two'],
      ]);
    });

    it('reads past a result of no call it shows', async () => {
      const { turn } = await begin();

      turn.body.event('tool_result', { id: 'nobody', ok: true, summary: 'x' });
      turn.body.event('done', { messages: [], reason: 'answered' });
      await settle();

      expect(kinds()).toEqual(['user']);
    });

    it("splits the assistant's text where a call comes between", async () => {
      const { turn } = await begin();

      turn.body.event('text', { delta: 'Let me look.' });
      turn.body.event('tool_call', { id: 'c1', name: 'search', arguments: {} });
      turn.body.event('tool_result', { id: 'c1', ok: true, summary: 'none' });
      turn.body.event('text', { delta: 'Nothing.' });
      turn.body.event('done', { messages: [], reason: 'answered' });
      await settle();

      expect(service.entries().map((entry) => ('text' in entry ? entry.text : entry.kind))).toEqual(
        ['File a bug', 'Let me look.', 'call', 'Nothing.'],
      );
    });

    it("shows a running call's answer from done where its result came as no event it could read", async () => {
      const { turn } = await begin();
      turn.body.event('tool_call', { id: 'c1', name: 'watch', arguments: {} });
      turn.body.event('tool_result', { id: 'c1', ok: 'yes', summary: 'unreadable' });
      turn.body.event('tool_call', { id: 'c2', name: 'watch', arguments: {} });
      turn.body.event('done', {
        messages: [
          {
            role: 'assistant',
            tool_calls: [
              { id: 'c1', name: 'watch', arguments: {} },
              { id: 'c2', name: 'watch', arguments: {} },
            ],
          },
          { role: 'tool', tool_call_id: 'c1', ok: false, text: 'x'.repeat(2500) },
          { role: 'tool', tool_call_id: 'c2' },
        ],
        reason: 'answered',
      });
      await settle();

      expect(calls().map((card) => [card.state, card.summary?.length])).toEqual([
        ['failed', 2000],
        ['ok', 0],
      ]);
    });

    it('marks a call whose result never came as unanswered when the turn ends', async () => {
      const { turn } = await begin();

      turn.body.event('tool_call', { id: 'c1', name: 'search', arguments: {} });
      turn.body.event('done', { messages: [], reason: 'answered' });
      await settle();

      expect(calls()[0].state).toBe('unanswered');
    });

    it('shows the problem of an error event, and the turn ends with its done', async () => {
      const { turn, begun } = await begin();

      turn.body.event('error', problem(502, 'chat_provider_failed', 'the provider answered 500'));
      turn.body.event('done', { messages: [], reason: 'error' });
      await settle();

      expect(lastEntry()).toEqual({
        id: expect.any(Number),
        kind: 'problem',
        problem: expect.objectContaining({
          status: 502,
          code: 'chat_provider_failed',
          detail: 'the provider answered 500',
        }),
      });
      expect(await begun).toBe(true);
      expect(service.busy()).toBe(false);
    });

    it('shows an error event without a problem as a failed turn', async () => {
      const { turn } = await begin();

      turn.body.event('error', 'nonsense');
      turn.body.event('done', { messages: [], reason: 'error' });
      await settle();

      expect(lastEntry()).toEqual({
        id: expect.any(Number),
        kind: 'problem',
        problem: expect.objectContaining({ title: 'The turn failed', code: 'internal' }),
      });
    });

    it('says when the turn took as many steps as one turn allows', async () => {
      const { turn } = await begin();

      turn.body.event('done', { messages: [], reason: 'step_limit' });
      await settle();

      expect(lastEntry()).toEqual({
        id: expect.any(Number),
        kind: 'notice',
        text: 'The assistant took as many steps as one turn allows. Write to let it go on.',
      });
    });

    it('reads nothing after done', async () => {
      const { turn } = await begin();

      turn.body.event('text', { delta: 'Done.' });
      turn.body.event('done', {
        messages: [{ role: 'assistant', text: 'Done.' }],
        reason: 'answered',
      });
      turn.body.event('text', { delta: 'late' });
      await settle();

      expect(service.entries().map((entry) => ('text' in entry ? entry.text : entry.kind))).toEqual(
        ['File a bug', 'Done.'],
      );
    });

    it('says the assistant gave no answer where it answered white space only, which adds nothing', async () => {
      const { turn, begun } = await begin('File a bug');

      turn.body.event('done', { messages: [], reason: 'answered' });
      await settle();

      expect(await begun).toBe(true);
      expect(service.busy()).toBe(false);
      expect(service.entries()).toEqual([
        { id: expect.any(Number), kind: 'user', text: 'File a bug' },
        { id: expect.any(Number), kind: 'notice', text: 'The assistant gave no answer.' },
      ]);

      void service.send('Are you there?');
      expect(sent[1].turn.messages).toEqual([
        { role: 'user', text: 'File a bug' },
        { role: 'user', text: 'Are you there?' },
      ]);
      streaming().body.end();
      await settle();
    });

    it('says nothing of the kind where the events showed what the turn did', async () => {
      const { turn } = await begin();

      turn.body.event('tool_call', { id: 'c1', name: 'search', arguments: {} });
      turn.body.event('tool_result', { id: 'c1', ok: true, summary: 'none' });
      turn.body.event('done', { messages: [], reason: 'answered' });
      await settle();

      expect(kinds()).toEqual(['user', 'call']);
    });
  });

  describe('a ui event', () => {
    async function opened(path: string): Promise<CallEntry> {
      const { turn } = await begin('Open it');
      turn.body.event('tool_call', { id: 'c1', name: 'open_ticket', arguments: {} });
      turn.body.event('ui', { action: 'navigate', path });
      turn.body.event('tool_result', { id: 'c1', ok: true, summary: 'opened' });
      turn.body.event('done', { messages: [], reason: 'answered' });
      await settle();
      return calls()[0];
    }

    it.each(['/t/acme/tickets/COW-12', '/t/acme/p/COW/backlog', '/t/acme/p/OPS2/board'])(
      'opens %s with the router',
      async (path) => {
        const card = await opened(path);

        expect(navigateByUrl).toHaveBeenCalledExactlyOnceWith(path);
        expect(card.refused).toBeUndefined();
      },
    );

    it.each([
      '/me/tokens',
      '/t/acme/settings',
      '/t/acme/members',
      '/t/globex/p/COW/board',
      '/t/acme/p/COW/board/../../members',
      '/t/acme/tickets/COW-12?x=1',
      '/t/acme/tickets/cow-12',
      '/t/acme//p/COW/board',
      'https://evil.example/t/acme/p/COW/board',
      '//evil.example/t/acme/p/COW/board',
      'javascript:alert(1)',
      '',
    ])('opens nothing for %j, and the card says so', async (path) => {
      const card = await opened(path);

      expect(navigateByUrl).not.toHaveBeenCalled();
      expect(card.refused).toBe(path);
    });

    it('marks no card when no call runs', async () => {
      const { turn } = await begin('Open it');

      turn.body.event('ui', { action: 'navigate', path: '/me/tokens' });
      turn.body.event('done', { messages: [{ role: 'assistant', text: 'x' }], reason: 'answered' });
      await settle();

      expect(navigateByUrl).not.toHaveBeenCalled();
      expect(kinds()).toEqual(['user']);
    });
  });

  describe('a refusal before the stream', () => {
    it("shows the problem and takes the person's message back out of the conversation", async () => {
      const before = service.send('File a bug');
      sent[0].refuse(400, problem(400, 'validation_failed', 'the conversation is too long'));
      await settle();

      expect(await before).toBe(false);
      expect(service.entries()).toEqual([
        {
          id: expect.any(Number),
          kind: 'problem',
          problem: expect.objectContaining({
            status: 400,
            code: 'validation_failed',
            detail: 'the conversation is too long',
          }),
        },
      ]);
      expect(service.busy()).toBe(false);

      void service.send('Again');
      expect(sent[1].turn.messages).toEqual([{ role: 'user', text: 'Again' }]);
      streaming().body.end();
      await settle();
    });

    it('sends the browser to the login on a 401, which comes back here', async () => {
      const before = service.send('Hi');
      sent[0].refuse(401, problem(401, 'unauthenticated', 'no session'));
      await settle();

      expect(await before).toBe(false);
      expect(navigate).toHaveBeenCalledExactlyOnceWith(['/login'], {
        queryParams: { return: '/t/acme/p/COW/board' },
      });
    });

    it('says why the chat went away and asks for the availability again', async () => {
      const toasts = vi.spyOn(TestBed.inject(MessageService), 'add');
      void service.send('Hi');
      sent[0].refuse(409, problem(409, 'chat_unavailable', 'no chat provider'));
      await settle();

      expect(toasts).toHaveBeenCalledExactlyOnceWith(
        expect.objectContaining({ detail: 'no chat provider' }),
      );
      availability('acme').flush({ available: false, providers: [], reason: 'not_configured' });
      await settle();
      expect(service.available()).toBe(false);
    });

    it('reads a refusal that is no problem body', async () => {
      void service.send('Hi');
      sent[0].refuse(502, '<html>Bad gateway</html>');
      await settle();

      expect(lastEntry()).toEqual(
        expect.objectContaining({
          kind: 'problem',
          problem: expect.objectContaining({
            status: 502,
            detail: 'The server answered 502 without a problem body.',
          }),
        }),
      );
    });

    describe('of a conversation that outgrew a turn', () => {
      const tooLong = {
        id: expect.any(Number),
        kind: 'notice',
        text: 'This conversation is too long for another turn. A new one starts empty.',
        restart: true,
      };
      const invalid = (pointer: string): Problem => ({
        ...problem(400, 'validation_failed', 'the request does not match the API document'),
        errors: [{ pointer, message: 'too many' }],
      });

      it.each([
        [
          'the body is larger than the backend takes',
          413,
          problem(413, 'payload_too_large', 'the body is larger than 1048576 bytes'),
        ],
        ['a proxy refuses the body before the backend', 413, '<html>Too Large</html>'],
        ['there are more messages than a turn takes', 400, invalid('/messages')],
        ['a message is longer than a turn takes', 400, invalid('/messages/3/text')],
      ])(
        'says so when %s, and offers a new conversation instead of the problem',
        async (_why, status, body) => {
          const before = service.send('File a bug');
          sent[0].refuse(status, body);
          await settle();

          expect(await before).toBe(false);
          expect(service.entries()).toEqual([tooLong]);
          expect(service.busy()).toBe(false);
        },
      );

      it('says so again on every message, which the conversation cannot take', async () => {
        void service.send('File a bug');
        sent[0].refuse(400, invalid('/messages'));
        await settle();

        void service.send('File a bug');
        sent[1].refuse(400, invalid('/messages'));
        await settle();

        expect(service.entries()).toEqual([tooLong, tooLong]);
      });

      it('shows the problem of a refusal of another part of the turn', async () => {
        void service.send('File a bug');
        sent[0].refuse(400, invalid('/context/path'));
        await settle();

        expect(kinds()).toEqual(['problem']);
      });
    });

    it('says to wait where turns of the person run elsewhere, and the message stays out of the conversation', async () => {
      const before = service.send('File a bug');
      sent[0].refuse(
        429,
        problem(
          429,
          'chat_busy',
          '2 turns of yours are running already; one ends, or is stopped, first',
        ),
      );
      await settle();

      expect(await before).toBe(false);
      expect(service.entries()).toEqual([
        {
          id: expect.any(Number),
          kind: 'notice',
          text: 'A turn of yours is running elsewhere: wait for it, or stop it.',
          stop: true,
        },
      ]);
      expect(service.busy()).toBe(false);

      void service.send('File a bug');
      expect(sent[1].turn.messages).toEqual([{ role: 'user', text: 'File a bug' }]);
      streaming().body.end();
      await settle();
    });

    it('says the backend cannot be reached when no answer comes, and tries nothing again', async () => {
      const before = service.send('Hi');
      sent[0].fail();
      await settle();

      expect(await before).toBe(false);
      expect(service.entries()).toEqual([
        {
          id: expect.any(Number),
          kind: 'problem',
          problem: expect.objectContaining({
            code: 'backend_unreachable',
            detail: 'The connection failed before an answer came.',
          }),
        },
      ]);
      expect(sent).toHaveLength(1);
    });
  });

  describe('Stop', () => {
    it('before the answer came takes the message back and shows nothing more', async () => {
      const before = service.send('File a bug');

      service.stop();
      stopped();
      await settle();

      expect(await before).toBe(false);
      expect(service.entries()).toEqual([]);
      expect(service.busy()).toBe(false);
    });

    it('during the stream ends the turn; what the events reported stays in the conversation', async () => {
      const { turn, begun } = await begin('File a bug');
      turn.body.event('text', { delta: 'Filing.' });
      turn.body.event('tool_call', { id: 'c1', name: 'file_ticket', arguments: { title: 'A' } });
      turn.body.event('tool_result', { id: 'c1', ok: true, summary: 'Filed COW-12' });
      turn.body.event('tool_call', { id: 'c2', name: 'transition', arguments: { to: 'now' } });
      await settle();

      service.stop();
      expect(turn.init.signal?.aborted).toBe(true);
      stopped();
      await settle();

      expect(await begun).toBe(true);
      expect(service.busy()).toBe(false);
      expect(calls().map((card) => card.state)).toEqual(['ok', 'unanswered']);
      expect(lastEntry()).toEqual({
        id: expect.any(Number),
        kind: 'notice',
        text: 'Stopped. What the calls above report has happened.',
      });

      void service.send('Why did you stop?');
      expect(sent[1].turn.messages).toEqual([
        { role: 'user', text: 'File a bug' },
        {
          role: 'assistant',
          text: 'Filing.',
          tool_calls: [{ id: 'c1', name: 'file_ticket', arguments: { title: 'A' } }],
        },
        { role: 'tool', tool_call_id: 'c1', ok: true, text: 'Filed COW-12' },
        { role: 'user', text: 'Why did you stop?' },
      ]);
      streaming().body.end();
      await settle();
    });

    it('keeps nothing of a step of blank text, which the next turn could not carry', async () => {
      const { turn } = await begin('File a bug');
      turn.body.event('text', { delta: '\n \n' });
      await settle();

      service.stop();
      stopped();
      await settle();
      void service.send('Go on');

      expect(sent[1].turn.messages).toEqual([
        { role: 'user', text: 'File a bug' },
        { role: 'user', text: 'Go on' },
      ]);
      streaming().body.end();
      await settle();
    });

    it('with nothing reported says only that it stopped', async () => {
      const { turn } = await begin();
      turn.body.event('text', { delta: 'Thinking' });
      await settle();

      service.stop();
      stopped();
      await settle();

      expect(lastEntry()).toEqual({ id: expect.any(Number), kind: 'notice', text: 'Stopped.' });
    });

    it('asks the backend to stop the turns of the tenant the turn runs in, so no proxy keeps it alive', async () => {
      await begin();

      service.stop();

      const request = http.expectOne('/api/v1/tenants/acme/chat/turns');
      expect(request.request.method).toBe('DELETE');
      request.flush(null, { status: 204, statusText: 'No Content' });
      await settle();
    });

    it('reports a stop the backend refused; the turn is stopped here all the same', async () => {
      const toasts = vi.spyOn(TestBed.inject(MessageService), 'add');
      await begin();

      service.stop();
      http
        .expectOne('/api/v1/tenants/acme/chat/turns')
        .flush(problem(503, 'not_ready', 'down'), { status: 503, statusText: 'Unavailable' });
      await settle();

      expect(toasts).toHaveBeenCalledOnce();
      expect(service.busy()).toBe(false);
      expect(lastEntry()).toEqual({ id: expect.any(Number), kind: 'notice', text: 'Stopped.' });
    });

    it('does nothing when no turn runs', () => {
      service.stop();

      expect(service.busy()).toBe(false);
      expect(service.entries()).toEqual([]);
      http.expectNone('/api/v1/tenants/acme/chat/turns');
    });

    it('from elsewhere ends the turn with done: its messages stay, and it says it stopped', async () => {
      const { turn } = await begin('File a bug');
      turn.body.event('tool_call', { id: 'c1', name: 'watch', arguments: {} });
      turn.body.event('tool_result', { id: 'c1', ok: true, summary: 'Watching COW-12' });
      turn.body.event('done', {
        messages: [
          { role: 'assistant', tool_calls: [{ id: 'c1', name: 'watch', arguments: {} }] },
          { role: 'tool', tool_call_id: 'c1', ok: true, text: 'Watching COW-12' },
        ],
        reason: 'stopped',
      });
      await settle();

      expect(service.busy()).toBe(false);
      expect(lastEntry()).toEqual({
        id: expect.any(Number),
        kind: 'notice',
        text: 'Stopped. What the calls above report has happened.',
      });
      void service.send('Go on');
      expect(sent[1].turn.messages).toEqual([
        { role: 'user', text: 'File a bug' },
        { role: 'assistant', tool_calls: [{ id: 'c1', name: 'watch', arguments: {} }] },
        { role: 'tool', tool_call_id: 'c1', ok: true, text: 'Watching COW-12' },
        { role: 'user', text: 'Go on' },
      ]);
      streaming().body.end();
      await settle();
    });

    it("stops the person's turns elsewhere from the busy notice, which then says so", async () => {
      const { turn } = await begin('Hello');
      turn.body.event('done', {
        messages: [{ role: 'assistant', text: 'Hi.' }],
        reason: 'answered',
      });
      await settle();
      void service.send('File a bug');
      sent[1].refuse(429, problem(429, 'chat_busy', 'one runs'));
      await settle();

      const done = service.stopElsewhere();
      stopped();
      await done;

      expect(kinds()).toEqual(['user', 'notice', 'notice']);
      expect(service.entries().slice(1)).toEqual([
        {
          id: expect.any(Number),
          kind: 'notice',
          text: 'A turn of yours is running elsewhere: wait for it, or stop it.',
        },
        {
          id: expect.any(Number),
          kind: 'notice',
          text: 'Your turns in this tenant are stopped. Send your message again.',
        },
      ]);
    });

    it('keeps the busy notice and its Stop when the stop elsewhere is refused', async () => {
      void service.send('File a bug');
      sent[0].refuse(429, problem(429, 'chat_busy', 'one runs'));
      await settle();

      const done = service.stopElsewhere();
      http
        .expectOne('/api/v1/tenants/acme/chat/turns')
        .flush(problem(500, 'internal', 'down'), { status: 500, statusText: 'Internal' });
      await done;

      expect(service.entries()).toEqual([expect.objectContaining({ kind: 'notice', stop: true })]);
    });
  });

  describe('a stream cut before done', () => {
    it('says the answer was cut off, and keeps what its events reported', async () => {
      const { turn } = await begin('File a bug');
      turn.body.event('tool_call', { id: 'c1', name: 'file_ticket', arguments: {} });
      turn.body.event('tool_result', { id: 'c1', ok: false, summary: 'refused' });
      turn.body.end();
      await settle();

      expect(lastEntry()).toEqual({
        id: expect.any(Number),
        kind: 'notice',
        text: 'The answer was cut off. What the calls above report has happened.',
      });
      void service.send('Again');
      expect(sent[1].turn.messages).toEqual([
        { role: 'user', text: 'File a bug' },
        { role: 'assistant', tool_calls: [{ id: 'c1', name: 'file_ticket', arguments: {} }] },
        { role: 'tool', tool_call_id: 'c1', ok: false, text: 'refused' },
        { role: 'user', text: 'Again' },
      ]);
      streaming().body.end();
      await settle();
    });

    it('takes a failed connection in the middle for a cut', async () => {
      const { turn } = await begin();

      turn.body.fail(new TypeError('network error'));
      await settle();

      expect(lastEntry()).toEqual({
        id: expect.any(Number),
        kind: 'notice',
        text: 'The answer was cut off.',
      });
      expect(service.busy()).toBe(false);
    });
  });

  describe('the tenant', () => {
    it("empties the conversation when another tenant's pages open, which begins a new one", async () => {
      const { turn } = await begin('File a bug');
      turn.body.event('done', {
        messages: [{ role: 'assistant', text: 'Done.' }],
        reason: 'answered',
      });
      await settle();

      tenant.set('globex');
      await settle();
      availability('globex').flush(open);
      await settle();

      expect(service.entries()).toEqual([]);
      void service.send('Hello');
      expect(sent[1].url).toBe('/api/v1/tenants/globex/chat');
      expect(sent[1].turn.messages).toEqual([{ role: 'user', text: 'Hello' }]);
      expect(sent[1].turn.conversation).toMatch(uuid);
      expect(sent[1].turn.conversation).not.toBe(sent[0].turn.conversation);
      streaming().body.end();
      await settle();
    });

    it('stops a running turn, of which nothing reaches the next conversation', async () => {
      const { turn, begun } = await begin('File a bug');

      // The event is on its way as the tenant changes.
      turn.body.event('text', { delta: 'late' });
      tenant.set('globex');
      TestBed.tick();
      await settle();
      stopped('acme');
      availability('globex').flush(open);
      await settle();

      expect(await begun).toBe(true);
      expect(service.busy()).toBe(false);
      expect(service.entries()).toEqual([]);
    });

    it('drops an answer that comes after the tenant changed', async () => {
      const before = service.send('File a bug');

      tenant.set('globex');
      await settle();
      stopped('acme');
      availability('globex').flush(open);
      sent[0].refuse(409, problem(409, 'chat_unavailable', 'gone'));
      await settle();

      expect(await before).toBe(false);
      expect(service.entries()).toEqual([]);
    });

    it('drops a refusal whose body is still on its way when the tenant changes', async () => {
      const toasts = vi.spyOn(TestBed.inject(MessageService), 'add');
      const before = service.send('File a bug');
      sent[0].stream(409);
      await settle();

      tenant.set('globex');
      await settle();
      stopped('acme');
      availability('globex').flush(open);
      await settle();

      expect(await before).toBe(false);
      expect(service.entries()).toEqual([]);
      expect(toasts).not.toHaveBeenCalled();
    });

    it('keeps the conversation while the person is on a page of no tenant, and stops its turn', async () => {
      const { turn } = await begin('File a bug');
      turn.body.event('tool_call', { id: 'c1', name: 'search', arguments: {} });
      turn.body.event('tool_result', { id: 'c1', ok: true, summary: 'none' });
      await settle();

      tenant.set(null);
      await settle();
      stopped('acme');
      expect(service.busy()).toBe(false);

      tenant.set('acme');
      await settle();
      availability('acme').flush(open);
      await settle();

      expect(kinds()).toEqual(['user', 'call', 'notice']);
    });
  });

  describe('the chat going away while a turn runs', () => {
    it('stops the turn once the installation configures no provider, whose panel and Stop go with it', async () => {
      const { turn, begun } = await begin('File a bug');
      turn.body.event('text', { delta: 'Filing' });
      await settle();

      service.reloadAvailability();
      await settle();
      expect(turn.init.signal?.aborted).toBe(false);
      availability('acme').flush({ available: false, providers: [], reason: 'not_configured' });
      await settle();
      stopped();

      expect(turn.init.signal?.aborted).toBe(true);
      expect(await begun).toBe(true);
      expect(service.busy()).toBe(false);
      expect(lastEntry()).toEqual({ id: expect.any(Number), kind: 'notice', text: 'Stopped.' });
    });

    it('stops the turn when the availability cannot be read again, which takes the panel too', async () => {
      const { turn, begun } = await begin();

      service.reloadAvailability();
      await settle();
      availability('acme').flush(problem(500, 'internal', 'down'), {
        status: 500,
        statusText: 'Internal',
      });
      await settle();
      stopped();

      expect(turn.init.signal?.aborted).toBe(true);
      expect(await begun).toBe(true);
      expect(service.busy()).toBe(false);
    });

    it('lets the turn run on while the chat stays available', async () => {
      const { turn } = await begin();

      service.reloadAvailability();
      await settle();
      availability('acme').flush({ ...open, providers: open.providers.slice(1) });
      await settle();

      expect(turn.init.signal?.aborted).toBe(false);
      expect(service.busy()).toBe(true);
      turn.body.end();
      await settle();
    });
  });

  describe('a new conversation', () => {
    it('begins with nothing and a new id', async () => {
      const { turn } = await begin('File a bug');
      turn.body.event('done', { messages: [], reason: 'answered' });
      await settle();

      service.restart();

      expect(service.entries()).toEqual([]);
      void service.send('Hello');
      expect(sent[1].turn.messages).toEqual([{ role: 'user', text: 'Hello' }]);
      expect(sent[1].turn.conversation).not.toBe(sent[0].turn.conversation);
      streaming().body.end();
      await settle();
    });

    it('does not begin while a turn runs', async () => {
      const { turn } = await begin('File a bug');

      service.restart();

      expect(kinds()).toEqual(['user']);
      expect(service.busy()).toBe(true);
      turn.body.end();
      await settle();
    });
  });

  describe('whether the panel is open', () => {
    const key = 'cowork.chat.p1';

    it('is closed when the person chose nothing yet', () => {
      expect(service.open()).toBe(false);
    });

    it("is the person's choice, kept in browser storage under the person", async () => {
      service.setOpen(true);
      await settle();
      capabilitiesRead().flush({ capabilities: [], chosen: true });

      expect(service.open()).toBe(true);
      expect(localStorage.getItem(key)).toBe('open');

      service.setOpen(false);

      expect(service.open()).toBe(false);
      expect(localStorage.getItem(key)).toBe('closed');
    });

    it('is read from browser storage for the person who signs in', async () => {
      localStorage.setItem('cowork.chat.p2', 'open');

      person.set({ ...ada, id: 'p2' });
      await settle();
      capabilitiesRead().flush({ capabilities: [], chosen: true });

      expect(service.open()).toBe(true);
    });

    it('is closed and stores nothing while the person is not known', async () => {
      person.set(undefined);
      await settle();

      service.setOpen(true);

      expect(service.open()).toBe(true);
      expect(localStorage.length).toBe(0);
    });

    it('holds the choice for the page when storage refuses it', async () => {
      vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
        throw new Error('denied');
      });
      vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
        throw new Error('quota');
      });
      person.set({ ...ada, id: 'p3' });
      await settle();
      expect(service.open()).toBe(false);

      service.setOpen(true);
      await settle();
      capabilitiesRead().flush({ capabilities: [], chosen: true });

      expect(service.open()).toBe(true);
    });
  });

  describe('the provider', () => {
    const key = 'cowork.chat.provider.p1';

    it('is the first configured while the person picked none, and a turn names it', async () => {
      expect(service.providers().map((each) => each.id)).toEqual(['lmstudio', 'claude']);
      expect(service.provider()?.id).toBe('lmstudio');

      void service.send('Hi');

      expect(sent[0].turn.provider).toBe('lmstudio');
      streaming().body.end();
      await settle();
    });

    it("is the person's pick, kept in browser storage under the person, for the next turn", async () => {
      service.setProvider('claude');

      expect(service.provider()?.id).toBe('claude');
      expect(localStorage.getItem(key)).toBe('claude');
      void service.send('Hi');
      expect(sent[0].turn.provider).toBe('claude');
      streaming().body.end();
      await settle();
    });

    it('is read from browser storage for the person who signs in', async () => {
      localStorage.setItem('cowork.chat.provider.p2', 'claude');

      person.set({ ...ada, id: 'p2' });
      await settle();

      expect(service.provider()?.id).toBe('claude');
    });

    it('holds the pick without storing it while the person is not known', async () => {
      person.set(undefined);
      await settle();

      service.setProvider('claude');

      expect(service.provider()?.id).toBe('claude');
      expect(localStorage.length).toBe(0);
    });

    it('is the first again when the installation no longer configures the pick', async () => {
      service.setProvider('gone');

      expect(service.provider()?.id).toBe('lmstudio');
    });

    it('holds the pick for the page when storage refuses it', async () => {
      vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
        throw new Error('quota');
      });

      service.setProvider('claude');

      expect(service.provider()?.id).toBe('claude');
    });
  });

  describe("the chat's capabilities", () => {
    it('are not read while the panel is closed', () => {
      expect(service.capabilities.value()).toBeUndefined();
      http.expectNone('/api/v1/me/chat');
    });

    it('are read once the panel is open', async () => {
      service.setOpen(true);
      await settle();

      capabilitiesRead().flush({ capabilities: ['rank'], chosen: false });
      await settle();

      expect(service.capabilities.value()).toEqual({ capabilities: ['rank'], chosen: false });
    });

    it("are the person's choice, which the backend answers with the set it took", async () => {
      const done = service.setCapabilities(['close', 'rank']);

      const request = http.expectOne('/api/v1/me/chat');
      expect(request.request.method).toBe('PUT');
      expect(request.request.body).toEqual({ capabilities: ['close', 'rank'] });
      request.flush({ capabilities: ['close', 'rank'], chosen: true });

      expect(await done).toBe(true);
      expect(service.capabilities.value()).toEqual({
        capabilities: ['close', 'rank'],
        chosen: true,
      });
    });

    it('stay as read when the choice is refused, which is reported', async () => {
      const toasts = vi.spyOn(TestBed.inject(MessageService), 'add');
      const done = service.setCapabilities(['close']);

      http.expectOne('/api/v1/me/chat').flush(problem(403, 'session_required', 'a session'), {
        status: 403,
        statusText: 'Forbidden',
      });

      expect(await done).toBe(false);
      expect(toasts).toHaveBeenCalledOnce();
      expect(service.capabilities.value()).toBeUndefined();
    });

    it('changed in a conversation under way say so once and offer a new conversation', async () => {
      const { turn } = await begin('Move WEB-1 to decided');
      turn.body.event('text', { delta: 'That needs decide, which I lack.' });
      turn.body.event('done', {
        messages: [{ role: 'assistant', text: 'That needs decide, which I lack.' }],
        reason: 'answered',
      });
      await settle();

      for (const set of [['decide'], ['decide', 'rank']] as const) {
        const done = service.setCapabilities([...set]);
        http.expectOne('/api/v1/me/chat').flush({ capabilities: [...set], chosen: true });
        expect(await done).toBe(true);
      }

      expect(kinds()).toEqual(['user', 'assistant', 'notice']);
      expect(lastEntry()).toEqual({
        id: expect.any(Number),
        kind: 'notice',
        text: expect.stringContaining('A new one starts clean.'),
        restart: true,
      });
    });

    it('changed before any conversation say nothing', async () => {
      const done = service.setCapabilities(['decide']);
      http.expectOne('/api/v1/me/chat').flush({ capabilities: ['decide'], chosen: true });

      expect(await done).toBe(true);
      expect(service.entries()).toEqual([]);
    });
  });
});

describe('CHAT_FETCH', () => {
  afterEach(() => {
    vi.restoreAllMocks();
    TestBed.resetTestingModule();
  });

  it("is the browser's fetch", async () => {
    const answer = new Response('');
    const fetch = vi.spyOn(globalThis, 'fetch').mockResolvedValue(answer);
    const init: RequestInit = { method: 'POST' };

    const response = await TestBed.inject(CHAT_FETCH)('/api/v1/tenants/acme/chat', init);

    expect(fetch).toHaveBeenCalledExactlyOnceWith('/api/v1/tenants/acme/chat', init);
    expect(response).toBe(answer);
  });
});

describe('pageContext', () => {
  it.each([
    ['/t/acme/p/COW/board', { path: '/t/acme/p/COW/board', project: 'COW' }],
    ['/t/acme/p/COW/backlog?closed=true', { path: '/t/acme/p/COW/backlog', project: 'COW' }],
    ['/t/acme/p/COW', { path: '/t/acme/p/COW', project: 'COW' }],
    ['/t/acme/p/COW/settings#x', { path: '/t/acme/p/COW/settings', project: 'COW' }],
    [
      '/t/acme/tickets/COW-12',
      { path: '/t/acme/tickets/COW-12', project: 'COW', ticket: 'COW-12' },
    ],
    ['/t/acme/tickets/cow-12', { path: '/t/acme/tickets/cow-12' }],
    ['/t/acme/tickets/COW-0', { path: '/t/acme/tickets/COW-0' }],
    ['/t/acme/p/cow/board', { path: '/t/acme/p/cow/board' }],
    ['/t/acme', { path: '/t/acme' }],
    ['/t/acme/members', { path: '/t/acme/members' }],
    ['/', { path: '/' }],
    ['/t/acme/p/COW%20X/board', {}],
    [`/t/acme/${'a'.repeat(250)}`, {}],
  ])('makes %j into %j', (url, context) => {
    expect(pageContext(url)).toEqual(context);
  });
});

describe('navigable', () => {
  it.each([
    ['/t/acme/tickets/COW-12', true],
    ['/t/acme/tickets/OPS2-1234567890', true],
    ['/t/acme/p/COW/backlog', true],
    ['/t/acme/p/COW/board', true],
    ['/t/acme/p/COW/settings', false],
    ['/t/acme/p/COW', false],
    ['/t/acme/tickets/COW-01', false],
    ['/t/acme', false],
    ['/t/acme/', false],
    ['/t/acmex/p/COW/board', false],
    ['/t/globex/p/COW/board', false],
    ['/t/acme/p/COW/board/', false],
    ['/t/acme/p/COW/board#x', false],
  ])('takes %j for a page to open in acme: %s', (path, expected) => {
    expect(navigable(path, 'acme')).toBe(expected);
  });
});

describe('TurnRecord', () => {
  const call = (id: string, name = 'search') => ({ id, name, arguments: { q: id } });

  it('holds no message when nothing was reported', () => {
    expect(new TurnRecord().messages()).toEqual([]);
  });

  it("keeps the assistant's text", () => {
    const record = new TurnRecord();

    record.text('Hel');
    record.text('lo');

    expect(record.messages()).toEqual([{ role: 'assistant', text: 'Hello' }]);
    expect(record.reported).toBe(false);
  });

  // The backend reports each call of a step with its result before the next call: tool_call,
  // tool_result, tool_call, tool_result.
  it('keeps each call with its result: the call in an assistant message, the result as a tool message behind it', () => {
    const record = new TurnRecord();

    record.text('Looking.');
    record.call(call('c1'));
    record.result('c1', true, 'one');
    record.call(call('c2'));
    record.result('c2', false, 'two');

    expect(record.messages()).toEqual([
      { role: 'assistant', text: 'Looking.', tool_calls: [call('c1')] },
      { role: 'tool', tool_call_id: 'c1', ok: true, text: 'one' },
      { role: 'assistant', tool_calls: [call('c2')] },
      { role: 'tool', tool_call_id: 'c2', ok: false, text: 'two' },
    ]);
    expect(record.reported).toBe(true);
  });

  it('leaves out a step of blank text, which the backend refuses as a message of the model', () => {
    const record = new TurnRecord();

    record.text(' \n\t ');

    expect(record.messages()).toEqual([]);
  });

  it('leaves out a step of blank text whose call came to no result', () => {
    const record = new TurnRecord();

    record.call(call('c1'));
    record.result('c1', true, 'one');
    record.text('\n\n');
    record.call(call('c2'));

    expect(record.messages()).toEqual([
      { role: 'assistant', tool_calls: [call('c1')] },
      { role: 'tool', tool_call_id: 'c1', ok: true, text: 'one' },
    ]);
  });

  it("cuts a step's text to the bound of the API document, which streamed text is not held to", () => {
    const record = new TurnRecord();

    record.text('a'.repeat(60000));
    record.text('b'.repeat(60000));

    expect(record.messages()).toEqual([
      { role: 'assistant', text: 'a'.repeat(60000) + 'b'.repeat(40000) },
    ]);
  });

  it('does not halve a character of two UTF-16 units at the cut', () => {
    const record = new TurnRecord();

    record.text(`${'a'.repeat(99999)}😀b`);

    expect(record.messages()).toEqual([{ role: 'assistant', text: 'a'.repeat(99999) }]);
  });

  it('begins the next step after a result', () => {
    const record = new TurnRecord();

    record.call(call('c1'));
    record.result('c1', true, 'one');
    record.call(call('c2'));
    record.result('c2', true, 'two');
    record.text('Both done.');

    expect(record.messages()).toEqual([
      { role: 'assistant', tool_calls: [call('c1')] },
      { role: 'tool', tool_call_id: 'c1', ok: true, text: 'one' },
      { role: 'assistant', tool_calls: [call('c2')] },
      { role: 'tool', tool_call_id: 'c2', ok: true, text: 'two' },
      { role: 'assistant', text: 'Both done.' },
    ]);
  });

  it('leaves out a call without its result, and a step that has nothing else', () => {
    const record = new TurnRecord();

    record.call(call('c1'));
    record.result('c1', true, 'one');
    record.call(call('c2'));

    expect(record.messages()).toEqual([
      { role: 'assistant', tool_calls: [call('c1')] },
      { role: 'tool', tool_call_id: 'c1', ok: true, text: 'one' },
    ]);
  });

  it('holds a call announced twice once', () => {
    const record = new TurnRecord();

    record.call(call('c1'));
    record.call(call('c1'));
    record.result('c1', true, 'one');

    expect(record.messages()).toEqual([
      { role: 'assistant', tool_calls: [call('c1')] },
      { role: 'tool', tool_call_id: 'c1', ok: true, text: 'one' },
    ]);
  });

  it('answers nothing for a result whose call the turn never announced', () => {
    const record = new TurnRecord();

    record.result('c9', true, 'nine');
    record.text('Done.');

    expect(record.messages()).toEqual([{ role: 'assistant', text: 'Done.' }]);
    expect(record.called).toBe(true);
  });
});
