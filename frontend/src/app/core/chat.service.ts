import { DOCUMENT } from '@angular/common';
import { HttpErrorResponse } from '@angular/common/http';
import {
  computed,
  effect,
  inject,
  Injectable,
  InjectionToken,
  Injector,
  linkedSignal,
  resource,
  signal,
  untracked,
} from '@angular/core';
import { Router } from '@angular/router';
import { Api } from '../api/api';
import { getChatAvailability, runChatTurn } from '../api/functions';
import {
  ChatConfirmation,
  ChatMessage,
  ChatPageContext,
  ChatToolCall,
  ChatTurn,
  ChatTurnEnd,
} from '../api/models';
import { chatEvents, ChatStreamEvent } from './chat-stream';
import { requestedWithHeader, toSignIn } from './http';
import { ProblemService, ProblemView } from './problem.service';
import { refresh } from './refresh';
import { SessionService } from './session.service';

/**
 * How a turn is sent: `fetch`, because the answer is a stream to a POST, which neither the
 * HttpClient (it waits for the whole body) nor an `EventSource` (GET only) reads as it arrives.
 * Tests hand in their own.
 */
export const CHAT_FETCH = new InjectionToken<typeof fetch>('CHAT_FETCH', {
  providedIn: 'root',
  factory: () => (input, init) => fetch(input, init),
});

/** What the person wrote, or what the assistant answered, as the panel shows it: text. */
export interface MessageEntry {
  id: number;
  kind: 'user' | 'assistant';
  text: string;
  /**
   * An answer whose turn called no tool: nothing it says about cowork was looked up for it, and the
   * panel says so beneath it — a model may answer from what it assumes.
   */
  noTools?: true;
}

/**
 * Where a call stands: `running` until its result came, then `ok` or `failed`; `waiting` for the
 * person's Run or Skip; `skipped` by Skip, or by a new message written instead; `unanswered` when
 * its turn ended before its result — it may have run.
 */
export type CallState = 'running' | 'ok' | 'failed' | 'waiting' | 'skipped' | 'unanswered';

/** A tool the assistant called, with what came of it. */
export interface CallEntry {
  id: number;
  kind: 'call';
  call: ChatToolCall;
  state: CallState;
  /** The start of what the tool answered. */
  summary?: string;
  /** A proposal's act in words, for the person who decides it. */
  description?: string;
  /** A page the call asked to open that is not one of this tenant, which the panel did not open. */
  refused?: string;
}

/** Why a turn failed. */
export interface ProblemEntry {
  id: number;
  kind: 'problem';
  problem: ProblemView;
}

/**
 * How a turn ended, or why it did not begin, where that needs saying: stopped, cut off, out of
 * steps, no answer; the person's turns running elsewhere; a conversation too long for a turn, where
 * the notice offers a new one.
 */
export interface NoticeEntry {
  id: number;
  kind: 'notice';
  text: string;
  /** The conversation cannot go on: the notice offers a new conversation. */
  restart?: true;
}

/** The conversation as the panel shows it, oldest first. */
export type ChatEntry = MessageEntry | CallEntry | ProblemEntry | NoticeEntry;

/** What a failed connection says: no answer came, so nothing of the turn is known. */
const unreachable: ProblemView = {
  status: 0,
  code: 'backend_unreachable',
  title: 'The backend cannot be reached',
  detail: 'The connection failed before an answer came.',
  fields: {},
  current: {},
};

/** As long as a `tool_result` summary gets. */
const summaryLength = 2000;

/** The longest text of a message (the API document's `ChatMessage.text`). */
const maxText = 100000;

/** What the conversation says when it outgrew a turn; the panel offers a new one with it. */
const tooLongText = 'This conversation is too long for another turn. A new one starts empty.';

/** What the conversation says when the person's turns run elsewhere (`chat_busy`). */
const busyText = 'A turn of yours is running elsewhere: wait for it, or stop it there.';

/** What the conversation says when the model answered nothing but white space. */
const silentText = 'The assistant gave no answer.';

/**
 * Whether a refusal says the conversation outgrew what a turn carries: a body larger than the
 * backend takes (`payload_too_large`, or a proxy's own 413 before it), or messages beyond a bound
 * of the API document — more than 400 of them, say. Every later turn sends the same conversation
 * and would be refused alike; it is not trimmed on its own.
 */
function tooLong(problem: ProblemView): boolean {
  return (
    problem.code === 'payload_too_large' ||
    problem.status === 413 ||
    (problem.code === 'validation_failed' &&
      Object.keys(problem.fields).some(
        (field) => field === 'messages' || field.startsWith('messages.'),
      ))
  );
}

/**
 * A step's text within the API document's bound, which counts characters: a string's length counts
 * UTF-16 units, at least as many, so the cut always passes — and it does not halve a character.
 */
function clipped(text: string): string {
  if (text.length <= maxText) {
    return text;
  }
  return text.slice(0, /[\uD800-\uDBFF]/.test(text.charAt(maxText - 1)) ? maxText - 1 : maxText);
}

/** An `error` event whose data is no problem. */
const failed: ProblemView = {
  status: 500,
  code: 'internal',
  title: 'The turn failed',
  detail: '',
  fields: {},
  current: {},
};

const projectKey = '[A-Z][A-Z0-9]{1,9}';
const ticketKey = `${projectKey}-[1-9][0-9]{0,9}`;

/**
 * The page the person is on, as a turn names it: the path without its query, the project's key
 * and the ticket's short key where the path has them — a ticket's project is the key's prefix.
 * Each only in the shape the API document allows, so the context never fails a turn.
 */
export function pageContext(url: string): ChatPageContext {
  const path = url.split(/[?#]/)[0];
  const context: ChatPageContext = {};
  if (path.length <= 256 && /^\/[A-Za-z0-9/._~-]*$/.test(path)) {
    context.path = path;
  }
  const ticket = new RegExp(`^/t/[^/]+/tickets/((${projectKey})-[1-9][0-9]{0,9})$`).exec(path);
  const project = new RegExp(`^/t/[^/]+/p/(${projectKey})(?:/|$)`).exec(path);
  if (ticket) {
    context.ticket = ticket[1];
    context.project = ticket[2];
  } else if (project) {
    context.project = project[1];
  }
  return context;
}

/**
 * Whether a `ui` event's path is a page the panel opens: a ticket, a backlog or a board of the
 * turn's tenant — nothing else of this application, and no other tenant, whose page would end
 * the conversation.
 */
export function navigable(path: string, tenant: string): boolean {
  const prefix = `/t/${tenant}/`;
  return (
    path.startsWith(prefix) &&
    new RegExp(`^(?:tickets/${ticketKey}|p/${projectKey}/(?:backlog|board))$`).test(
      path.slice(prefix.length),
    )
  );
}

/**
 * What a turn's events reported, for a turn that ends without `done` — stopped by the person, or
 * cut — whose messages the backend never sent. Its messages are the turn as far as the events
 * tell it, so the model knows next time what it did: each step's text and the calls that came to
 * a result, each call followed by its tool message with the result's summary as its text, and a
 * proposal's call left waiting, as `done` leaves it. A call without its result is left out —
 * whether it ran is unknown. The calls the conversation leaves open — the proposal a decision
 * runs, and the calls after it in the same message — get their tool messages first, right behind
 * the message that holds them; any other call the conversation holds is not the turn's to answer.
 * The messages hold to the API document and the backend's check, so that the next turn is not
 * refused for them: a step of blank text and no call is left out, a step's text is cut to the
 * bound, which streamed text is not held to.
 */
export class TurnRecord {
  private readonly steps: { text: string; calls: ChatToolCall[] }[] = [];
  private readonly earlier: string[] = [];
  private readonly results = new Map<string, { ok: boolean; summary: string }>();
  private readonly known = new Set<string>();
  /** The calls of the conversation's last assistant message that no tool message answers. */
  private open = new Set<string>();
  private waiting: string | undefined;
  /** A step ends with a result, or a proposal; what comes next is the model's next step. */
  private ended = true;
  /** Whether the turn called a tool, proposed one or reported a result. */
  private acted = false;

  /** The conversation the turn was sent with. */
  constructor(conversation: ChatMessage[]) {
    for (const message of conversation) {
      if (message.role === 'tool') {
        this.open.delete(message.tool_call_id ?? '');
      } else {
        // A message of the person passes a proposal over; the backend answers it as skipped.
        this.open = new Set((message.tool_calls ?? []).map((call) => call.id));
        this.open.forEach((id) => this.known.add(id));
      }
    }
  }

  /** Whether a call reported its result. */
  get reported(): boolean {
    return this.results.size > 0;
  }

  /** Whether a tool was called, proposed or answered in the turn. */
  get called(): boolean {
    return this.acted;
  }

  /** Whether the events reported nothing: no text, no call, no result. */
  get empty(): boolean {
    return this.steps.length === 0 && this.results.size === 0;
  }

  text(delta: string): void {
    this.step().text += delta;
  }

  call(call: ChatToolCall): void {
    this.acted = true;
    if (this.open.has(call.id)) {
      this.remember(call.id);
    } else if (!this.known.has(call.id) && !this.held(call.id)) {
      this.step().calls.push(call);
    }
  }

  result(id: string, ok: boolean, summary: string): void {
    this.acted = true;
    this.results.set(id, { ok, summary });
    if (this.open.has(id)) {
      this.remember(id);
    }
    this.ended = true;
  }

  propose(call: ChatToolCall): void {
    this.call(call);
    this.waiting = call.id;
    this.ended = true;
  }

  messages(): ChatMessage[] {
    const answer = (id: string): ChatMessage[] => {
      const result = this.results.get(id);
      return result
        ? [{ role: 'tool', tool_call_id: id, ok: result.ok, text: result.summary }]
        : [];
    };
    const messages = this.earlier.flatMap(answer);
    for (const step of this.steps) {
      const calls = step.calls.filter(
        (call) => this.results.has(call.id) || call.id === this.waiting,
      );
      if (step.text.trim() === '' && calls.length === 0) {
        continue;
      }
      const text = clipped(step.text);
      messages.push({
        role: 'assistant',
        ...(text !== '' ? { text } : {}),
        ...(calls.length > 0 ? { tool_calls: calls } : {}),
      });
      messages.push(...calls.flatMap((call) => answer(call.id)));
    }
    return messages;
  }

  private step(): { text: string; calls: ChatToolCall[] } {
    if (this.ended) {
      this.steps.push({ text: '', calls: [] });
      this.ended = false;
    }
    return this.steps[this.steps.length - 1];
  }

  private held(id: string): boolean {
    return this.steps.some((step) => step.calls.some((call) => call.id === id));
  }

  private remember(id: string): void {
    if (!this.earlier.includes(id)) {
      this.earlier.push(id);
    }
  }
}

/**
 * How a turn's stream ended: its `done`'s reason, `silent` for an answer that said and added
 * nothing, or without `done`.
 */
type Ending = ChatTurnEnd | 'silent' | 'stopped' | 'cut';

/**
 * The chat of the tenant the pages show (docs/adr/0076): whether it is available, and one
 * conversation, which lives in the browser — the backend keeps nothing between turns, so each
 * turn sends the conversation so far. One turn runs at a time; a turn is never repeated by the
 * client, because a repeated turn repeats its acts. The conversation belongs to its tenant: it is
 * gone when another tenant's pages open (docs/adr/0053 D4), and a turn stops when the person leaves
 * the tenant's pages or the chat is no longer available there. Whether the panel is open is the
 * person's preference in browser storage (D6).
 */
@Injectable({ providedIn: 'root' })
export class ChatService {
  private readonly api = inject(Api);
  private readonly session = inject(SessionService);
  private readonly problems = inject(ProblemService);
  private readonly router = inject(Router);
  private readonly injector = inject(Injector);
  private readonly document = inject(DOCUMENT);
  private readonly fetch = inject(CHAT_FETCH);

  /**
   * Whether the tenant's members may hold a conversation, and with which model; not asked of a
   * tenant the person holds no role in (docs/adr/0034 D2).
   */
  readonly availability = resource({
    params: () => this.session.workTenant() ?? undefined,
    loader: ({ params: tenant }) => this.api.invoke(getChatAvailability, { tenant }),
  });
  readonly available = computed(
    () => this.availability.hasValue() && this.availability.value().available,
  );

  private readonly shown = signal<ChatEntry[]>([]);
  readonly entries = this.shown.asReadonly();
  private readonly running = signal(false);
  /** A turn runs; another waits until it ends. */
  readonly busy = this.running.asReadonly();

  private readonly storageKey = computed(() => {
    const person = this.session.person()?.id;
    return person ? `cowork.chat.${person}` : null;
  });
  /** Whether the panel is open: the person's last choice, closed when there is none. */
  readonly open = linkedSignal(() => this.stored(this.storageKey()));

  /** The conversation as the backend reads it: the person's messages and what `done` added. */
  private messages: ChatMessage[] = [];
  private conversation = crypto.randomUUID();
  /** The tenant the conversation belongs to. */
  private owner: string | null = null;
  private controller: AbortController | null = null;
  /** Counts the conversations, so that a turn of an earlier one changes nothing of the next. */
  private generation = 0;
  private lastId = 0;

  constructor() {
    effect(() => {
      const tenant = this.session.tenant();
      untracked(() => this.enter(tenant));
    });
    // The panel and its Stop go when the chat does — an administrator withdrew the consent, or the
    // availability cannot be read: a turn nobody can see or stop does not run on.
    effect(() => {
      if (!this.available()) {
        untracked(() => this.stop());
      }
    });
  }

  setOpen(open: boolean): void {
    this.open.set(open);
    const key = this.storageKey();
    if (!key) {
      return;
    }
    try {
      this.document.defaultView?.localStorage.setItem(key, open ? 'open' : 'closed');
    } catch {
      // Storage refused (private mode, quota): the choice holds for this page only.
    }
  }

  /** Asks again, after the tenant's consent to an outside provider changed. */
  reloadAvailability(): void {
    refresh(this.availability, this.injector);
  }

  /**
   * Sends the person's message with the conversation so far. True once the turn began; false
   * when it did not — another turn runs, the message is empty, or the backend refused it, whose
   * problem the conversation shows — and the message is then not part of the conversation.
   */
  async send(text: string): Promise<boolean> {
    const tenant = this.session.tenant();
    const said = text.trim();
    if (this.running() || !tenant || said === '') {
      return false;
    }
    const before = this.shown();
    // A proposal the person writes past is answered as skipped by every later turn.
    this.shown.set([
      ...before.map((entry): ChatEntry =>
        entry.kind === 'call' && entry.state === 'waiting' ? { ...entry, state: 'skipped' } : entry,
      ),
      { id: this.id(), kind: 'user', text: said },
    ]);
    return this.turn(tenant, [...this.messages, { role: 'user', text: said }], undefined, before);
  }

  /**
   * The person's decision on a proposal: the next turn, with the conversation exactly as the last
   * `done` left it. True once the turn began; a refused decision leaves the proposal waiting.
   */
  async decide(callId: string, run: boolean): Promise<boolean> {
    const tenant = this.session.tenant();
    const card = this.lastCall(callId);
    if (this.running() || !tenant || card?.state !== 'waiting') {
      return false;
    }
    const before = this.shown();
    this.changeCall(card.id, (entry) => ({ ...entry, state: run ? 'running' : 'skipped' }));
    return this.turn(tenant, this.messages, [{ tool_call_id: callId, run }], before);
  }

  /** Stops the running turn; what its calls reported has happened. */
  stop(): void {
    this.controller?.abort();
  }

  /** Begins a new conversation; not while a turn runs. */
  restart(): void {
    if (!this.running()) {
      this.reset();
    }
  }

  private enter(tenant: string | null): void {
    if (tenant === this.owner) {
      return;
    }
    this.stop();
    if (tenant !== null) {
      this.reset();
      this.owner = tenant;
    }
  }

  private reset(): void {
    this.generation++;
    this.controller?.abort();
    this.controller = null;
    this.running.set(false);
    this.shown.set([]);
    this.messages = [];
    this.conversation = crypto.randomUUID();
  }

  private async turn(
    tenant: string,
    messages: ChatMessage[],
    confirmations: ChatConfirmation[] | undefined,
    before: ChatEntry[],
  ): Promise<boolean> {
    const generation = this.generation;
    const current = () => generation === this.generation;
    const controller = new AbortController();
    this.controller = controller;
    this.running.set(true);
    try {
      const response = await this.post(tenant, messages, confirmations, controller.signal);
      if (!current()) {
        return false;
      }
      if (!response?.ok || !response.body) {
        await this.refused(response, before, controller.signal.aborted, current);
        return false;
      }
      this.messages = messages;
      const record = new TurnRecord(messages);
      const ending = await this.read(response.body, record, tenant, controller.signal, current);
      if (current()) {
        this.end(ending, record);
      }
      return true;
    } finally {
      if (this.controller === controller) {
        this.controller = null;
        this.running.set(false);
      }
    }
  }

  /** The turn's request; undefined when no answer came. */
  private async post(
    tenant: string,
    messages: ChatMessage[],
    confirmations: ChatConfirmation[] | undefined,
    abort: AbortSignal,
  ): Promise<Response | undefined> {
    const turn: ChatTurn = {
      conversation: this.conversation,
      messages,
      context: pageContext(this.router.url),
      ...(confirmations ? { confirmations } : {}),
    };
    try {
      return await this.fetch(
        this.api.rootUrl + runChatTurn.PATH.replace('{tenant}', encodeURIComponent(tenant)),
        {
          method: 'POST',
          // The session's CSRF pair (docs/adr/0037): the browser sets Origin, this the header.
          headers: {
            'Content-Type': 'application/json',
            Accept: 'text/event-stream',
            ...requestedWithHeader,
          },
          body: JSON.stringify(turn),
          credentials: 'same-origin',
          signal: abort,
        },
      );
    } catch {
      return undefined;
    }
  }

  /**
   * A turn that did not begin shows what was shown before it, and why ({@link refusal}): the
   * person's message goes, a decision waits again. Stopped before an answer came, it shows nothing
   * more.
   */
  private async refused(
    response: Response | undefined,
    before: ChatEntry[],
    stopped: boolean,
    current: () => boolean,
  ): Promise<void> {
    if (!response) {
      this.shown.set(
        stopped ? before : [...before, { id: this.id(), kind: 'problem', problem: unreachable }],
      );
      return;
    }
    const error = new HttpErrorResponse({
      status: response.status,
      statusText: response.statusText,
      url: response.url,
      error: await answer(response),
    });
    if (!current()) {
      return;
    }
    const problem = this.problems.read(error);
    this.shown.set([...before, this.refusal(problem)]);
    if (response.status === 401) {
      toSignIn(this.router);
    }
    // The chat went away in the tenant: the panel goes with it, so the toast says why.
    if (problem.code === 'chat_unavailable') {
      this.problems.report(error);
      this.reloadAvailability();
    }
  }

  /**
   * How a refusal shows: a conversation too long for a turn as a notice that offers a new one, the
   * person's turns running elsewhere as a notice to wait for them, anything else as its problem.
   */
  private refusal(problem: ProblemView): ChatEntry {
    const id = this.id();
    if (tooLong(problem)) {
      return { id, kind: 'notice', text: tooLongText, restart: true };
    }
    if (problem.code === 'chat_busy') {
      return { id, kind: 'notice', text: busyText };
    }
    return { id, kind: 'problem', problem };
  }

  private async read(
    body: ReadableStream<Uint8Array>,
    record: TurnRecord,
    tenant: string,
    abort: AbortSignal,
    current: () => boolean,
  ): Promise<Ending> {
    let text: number | null = null;
    try {
      for await (const event of chatEvents(body)) {
        if (!current()) {
          return 'stopped';
        }
        if (event.name === 'done') {
          const { messages, reason } = event.data;
          this.messages = [...this.messages, ...messages];
          this.answer(messages);
          // The model answered white space only, which the backend neither streams nor keeps.
          return reason === 'answered' && messages.length === 0 && record.empty ? 'silent' : reason;
        }
        text = this.apply(event, record, tenant, text);
      }
      return 'cut';
    } catch {
      return abort.aborted ? 'stopped' : 'cut';
    }
  }

  /** Shows an event; returns the entry the assistant's next text goes to, if it continues one. */
  private apply(
    event: Exclude<ChatStreamEvent, { name: 'done' }>,
    record: TurnRecord,
    tenant: string,
    text: number | null,
  ): number | null {
    switch (event.name) {
      case 'text': {
        record.text(event.data.delta);
        if (text === null) {
          const id = this.id();
          this.shown.update((list) => [...list, { id, kind: 'assistant', text: event.data.delta }]);
          return id;
        }
        this.appendText(text, event.data.delta);
        return text;
      }
      case 'tool_call':
        record.call(event.data);
        this.track(event.data, 'running');
        return null;
      case 'confirm': {
        const { description, ...call } = event.data;
        record.propose(call);
        this.track(call, 'waiting', description);
        return null;
      }
      case 'tool_result': {
        const { id, ok, summary } = event.data;
        record.result(id, ok, summary);
        const card = this.lastCall(id);
        if (card) {
          this.changeCall(card.id, (entry) => ({
            ...entry,
            state: card.state === 'skipped' ? 'skipped' : ok ? 'ok' : 'failed',
            summary,
          }));
        }
        return null;
      }
      case 'ui':
        this.navigate(event.data.path, tenant);
        return null;
      case 'error':
        this.shown.update((list) => [
          ...list,
          {
            id: this.id(),
            kind: 'problem',
            problem: event.data
              ? this.problems.read(
                  new HttpErrorResponse({ status: event.data.status, error: event.data }),
                )
              : failed,
          },
        ]);
        return null;
    }
  }

  /**
   * The card of a call: the one it has — a proposal decided to run, which a decision's turn calls
   * again — or a new one.
   */
  private track(call: ChatToolCall, state: CallState, description?: string): void {
    const card = this.lastCall(call.id);
    if (card && (card.state === 'running' || card.state === 'waiting')) {
      this.changeCall(card.id, (entry) => ({
        ...entry,
        call,
        state,
        ...(description !== undefined ? { description } : {}),
      }));
      return;
    }
    this.shown.update((list) => [
      ...list,
      {
        id: this.id(),
        kind: 'call',
        call,
        state,
        ...(description !== undefined ? { description } : {}),
      },
    ]);
  }

  /**
   * A call `done` answers that is still running on the screen — one whose result came as no event
   * — shows the answer `done` holds, as far as a summary goes.
   */
  private answer(messages: ChatMessage[]): void {
    for (const message of messages) {
      const card = message.role === 'tool' ? this.lastCall(message.tool_call_id ?? '') : undefined;
      if (card?.state === 'running') {
        this.changeCall(card.id, (entry) => ({
          ...entry,
          state: message.ok === false ? 'failed' : 'ok',
          summary: (message.text ?? '').slice(0, summaryLength),
        }));
      }
    }
  }

  /** Opens the page a `ui` event names, if it is one of the tenant's; else the call says so. */
  private navigate(path: string, tenant: string): void {
    if (navigable(path, tenant)) {
      void this.router.navigateByUrl(path);
      return;
    }
    const list = this.shown();
    for (let index = list.length - 1; index >= 0; index--) {
      const entry = list[index];
      if (entry.kind === 'call' && entry.state === 'running') {
        this.changeCall(entry.id, (card) => ({ ...card, refused: path }));
        return;
      }
    }
  }

  private end(ending: Ending, record: TurnRecord): void {
    this.shown.update((list) =>
      list.map((entry): ChatEntry =>
        entry.kind === 'call' && entry.state === 'running'
          ? { ...entry, state: 'unanswered' }
          : entry,
      ),
    );
    let notice: string | undefined;
    if (ending === 'stopped' || ending === 'cut') {
      this.messages = [...this.messages, ...record.messages()];
      notice =
        (ending === 'stopped' ? 'Stopped.' : 'The answer was cut off.') +
        (record.reported ? ' What the calls above report has happened.' : '');
    } else if (ending === 'step_limit') {
      notice = 'The assistant took as many steps as one turn allows. Write to let it go on.';
    } else if (ending === 'silent') {
      notice = silentText;
    } else if (ending === 'answered' && !record.called) {
      this.markNoTools();
    }
    if (notice) {
      const text = notice;
      this.shown.update((list) => [...list, { id: this.id(), kind: 'notice', text }]);
    }
  }

  /** The answer of a turn that called no tool, the turn's last entry, marked so. */
  private markNoTools(): void {
    this.shown.update((list) => {
      const last = list[list.length - 1];
      return last?.kind === 'assistant' ? [...list.slice(0, -1), { ...last, noTools: true }] : list;
    });
  }

  private lastCall(callId: string): CallEntry | undefined {
    const list = this.shown();
    for (let index = list.length - 1; index >= 0; index--) {
      const entry = list[index];
      if (entry.kind === 'call' && entry.call.id === callId) {
        return entry;
      }
    }
    return undefined;
  }

  private changeCall(id: number, change: (card: CallEntry) => CallEntry): void {
    this.shown.update((list) =>
      list.map((entry) => (entry.id === id && entry.kind === 'call' ? change(entry) : entry)),
    );
  }

  private appendText(id: number, delta: string): void {
    this.shown.update((list) =>
      list.map((entry) =>
        entry.id === id && entry.kind === 'assistant'
          ? { ...entry, text: entry.text + delta }
          : entry,
      ),
    );
  }

  private id(): number {
    return ++this.lastId;
  }

  private stored(key: string | null): boolean {
    if (!key) {
      return false;
    }
    try {
      return this.document.defaultView?.localStorage.getItem(key) === 'open';
    } catch {
      return false;
    }
  }
}

/** A refusal's body: its JSON where it is JSON, else its text. */
async function answer(response: Response): Promise<unknown> {
  const text = await response.text().catch(() => '');
  try {
    return JSON.parse(text);
  } catch {
    return text;
  }
}
