import { HttpErrorResponse, provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { signal, Type, WritableSignal } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { By } from '@angular/platform-browser';
import { provideRouter } from '@angular/router';
import { MessageService } from 'primeng/api';
import { ConfirmDialog } from 'primeng/confirmdialog';
import type { MockInstance } from 'vitest';
import { Subject } from 'rxjs';
import { provideApiConfiguration } from '../../api/api-configuration';
import {
  Activity,
  ActivityList,
  AttachmentList,
  AuditAction,
  Comment,
  CommentList,
  Interest,
  InterestList,
  Link,
  LinkList,
  Me,
  Problem,
  Question,
  QuestionList,
  Ticket,
  TimeEntryList,
} from '../../api/models';
import { Conversation } from '../../core/conversation.service';
import { EntityCache } from '../../core/entity-cache';
import { EventStreamService } from '../../core/event-stream.service';
import { MembersService } from '../../core/members.service';
import { SessionService } from '../../core/session.service';
import { StaleWrite, TicketActions } from '../../core/ticket-actions.service';
import { TicketRecords } from '../../core/ticket-records.service';
import { TicketsService } from '../../core/tickets.service';
import { Clock } from '../../shared/time';
import { AnswerQuestion, AskQuestion, CommentComposer, LinkAdder } from './conversation-forms';
import { InterestControl } from './interest-control';
import { AttachmentsCard, TimeCard } from './records-cards';
import { describe as describeActivity, TicketDetail } from './ticket-detail';
import { TicketFields } from './ticket-fields';
import { TicketMoves } from './ticket-moves';

const ada = { id: 'p1', display_name: 'Ada Lovelace', username: 'local:ada' };
const sam = { id: 'p2', display_name: 'Sam Rivera', username: 'local:sam' };
const now = Date.parse('2026-10-03T12:00:00Z');

function ticket(overrides: Partial<Ticket> = {}): Ticket {
  return {
    id: 't-12',
    key: 'acme/COW-12',
    number: 12,
    project: 'COW',
    title: 'The board flickers',
    body: 'It flickers on every event.',
    type: 'bug',
    state: 'in-progress',
    severity: 'high',
    security: 'none',
    confidential: false,
    assignee: sam,
    reporter: ada,
    block: null,
    threat: null,
    parent: null,
    effort: 'M',
    progress: 40,
    progress_derived: false,
    progress_refinement: 0,
    progress_review: 0,
    urgency: 'next',
    urgency_derived: 'next',
    urgency_override: null,
    urgency_rule: 'v1:default',
    opened_at: '2026-10-01T09:00:00Z',
    created_at: '2026-10-01T09:00:00Z',
    updated_at: '2026-10-03T11:55:00Z',
    decided_at: null,
    done_at: null,
    done_from: null,
    done_by_hand: false,
    open_prerequisites: 0,
    version: 3,
    ...overrides,
  };
}

function question(overrides: Partial<Question> = {}): Question {
  return {
    id: 'q-1',
    number: 1,
    question: 'Which flicker is it?',
    options: '',
    recommendation: '',
    status: 'open',
    answer: null,
    answered_at: null,
    answered_by: null,
    asked_by: ada,
    asked_by_agent: null,
    asked_of: null,
    recorded_by_agent: false,
    withdrawn_at: null,
    created_at: '2026-10-03T11:00:00Z',
    updated_at: '2026-10-03T11:00:00Z',
    version: 1,
    ...overrides,
  };
}

function comment(overrides: Partial<Comment> = {}): Comment {
  return {
    id: 'c-1',
    author: ada,
    agent: null,
    body: 'Reproduced on the second board.',
    edited: false,
    explains: [],
    withdrawn: false,
    withdrawn_at: null,
    created_at: '2026-10-03T10:00:00Z',
    updated_at: '2026-10-03T10:00:00Z',
    version: 1,
    ...overrides,
  };
}

function activity(overrides: Partial<Activity> = {}): Activity {
  return {
    id: 'a-1',
    action: 'transitioned',
    actor: ada,
    actor_system: null,
    agent: null,
    at: '2026-10-03T11:30:00Z',
    before: null,
    after: null,
    entity_id: null,
    entity_type: 'ticket',
    explained_by_comment: null,
    note: null,
    reason: null,
    redacted: false,
    ...overrides,
  };
}

function link(overrides: Partial<Link> = {}): Link {
  return {
    id: 'l-1',
    type: 'blocks',
    direction: 'outgoing',
    name: 'blocks',
    ticket: { key: 'acme/COW-3', state: 'filed', title: 'Rework the board' },
    created_by: ada,
    created_at: '2026-10-02T09:00:00Z',
    ...overrides,
  };
}

function interest(person: Interest['person'], weight: Interest['weight'], note = ''): Interest {
  return {
    person,
    weight,
    note,
    settled: false,
    since: '2026-10-02T09:00:00Z',
    updated_at: '2026-10-02T09:00:00Z',
  };
}

function problem(status: number, title: string, detail: string): HttpErrorResponse {
  const body: Problem = {
    type: 'about:blank',
    title,
    status,
    detail,
    code: status === 404 ? 'not_found' : 'internal',
  };
  return new HttpErrorResponse({ status, statusText: title, error: body });
}

function list<T>(...items: T[]) {
  return { items, next_cursor: null };
}

describe('describe', () => {
  it('names the person who acted', () => {
    expect(describeActivity(activity({ actor: ada, action: 'transitioned' }))).toBe(
      'Ada Lovelace transitioned',
    );
  });

  it('names the system when no person acted', () => {
    expect(
      describeActivity(activity({ actor: null, actor_system: 'importer', action: 'created' })),
    ).toBe('importer created');
  });

  it('prefers the person over the system', () => {
    expect(describeActivity(activity({ actor: ada, actor_system: 'importer' }))).toBe(
      'Ada Lovelace transitioned',
    );
  });

  it('names cowork when neither a person nor a system acted', () => {
    expect(describeActivity(activity({ actor: null, actor_system: null, action: 'expired' }))).toBe(
      'cowork expired',
    );
  });

  it('replaces the underscores of the action with spaces', () => {
    expect(describeActivity(activity({ action: 'confidential_set' }))).toBe(
      'Ada Lovelace confidential set',
    );
  });

  it('replaces every underscore, not only the first', () => {
    const action = 'confidential_set_again' as AuditAction;

    expect(describeActivity(activity({ action }))).toBe('Ada Lovelace confidential set again');
  });
});

describe('TicketDetail', () => {
  const base = '/api/v1/tenants/acme/projects/COW/tickets/12';
  const urls = {
    comments: `${base}/comments?limit=200`,
    activity: `${base}/activity?order=desc&limit=100`,
    questions: `${base}/questions?limit=200`,
    links: `${base}/links?limit=200`,
    interest: `${base}/interest?limit=200`,
    attachments: `${base}/attachments?limit=200`,
    time: `${base}/time-entries?limit=200`,
  };

  /** What the API answers for the parts around a ticket; a part left out stays unanswered. */
  interface Answers {
    comments?: CommentList;
    activity?: ActivityList;
    questions?: QuestionList;
    links?: LinkList;
    interest?: InterestList;
    attachments?: AttachmentList;
    time?: TimeEntryList;
  }

  let tenant: WritableSignal<string | null>;
  let person: WritableSignal<Me | undefined>;
  let cache: EntityCache<Ticket>;
  let loadError: WritableSignal<unknown>;
  let shownKey: (() => string | undefined) | undefined;
  let http: HttpTestingController;
  let conversation: { unlink: MockInstance<Conversation['unlink']> };
  let update: MockInstance<TicketActions['update']>;

  beforeEach(() => {
    tenant = signal<string | null>('acme');
    person = signal<Me | undefined>({
      ...ada,
      memberships: [],
      global_admin: false,
      local: true,
      password_change_required: false,
    });
    cache = new EntityCache<Ticket>();
    loadError = signal<unknown>(undefined);
    shownKey = undefined;
    conversation = { unlink: vi.fn<Conversation['unlink']>().mockResolvedValue(undefined) };
    update = vi.fn<TicketActions['update']>().mockResolvedValue(ticket());
    TestBed.configureTestingModule({
      providers: [
        provideRouter([]),
        provideHttpClient(),
        provideHttpClientTesting(),
        provideApiConfiguration(''),
        MessageService,
        { provide: SessionService, useValue: { tenant, person } },
        { provide: Conversation, useValue: conversation },
        { provide: TicketActions, useValue: { update, transition: vi.fn() } },
        { provide: TicketRecords, useValue: {} },
        { provide: MembersService, useValue: { list: signal([]) } },
        {
          provide: TicketsService,
          useValue: {
            cache,
            ticket: (key: () => string | undefined) => {
              shownKey = key;
              return { error: loadError };
            },
          },
        },
        { provide: EventStreamService, useValue: { events: new Subject() } },
        { provide: Clock, useValue: { now: signal(now) } },
      ],
    });
    http = TestBed.inject(HttpTestingController);
  });

  /**
   * Answers the requests for the parts around the ticket and lets the page show them. A resource keeps
   * the application unstable while it loads, so this waits for the promises itself, not for stability.
   */
  async function answer(fixture: ComponentFixture<TicketDetail>, answers: Answers) {
    for (const [part, body] of Object.entries(answers)) {
      for (const request of http.match(urls[part as keyof Answers])) {
        request.flush(body);
      }
    }
    await new Promise((resolve) => setTimeout(resolve));
    fixture.detectChanges();
  }

  async function render(key = 'COW-12', answers: Answers = {}) {
    const fixture = TestBed.createComponent(TicketDetail);
    fixture.componentRef.setInput('key', key);
    fixture.detectChanges();
    await answer(fixture, answers);
    return { fixture, page: fixture.nativeElement as HTMLElement };
  }

  const text = (page: HTMLElement, selector: string) =>
    page.querySelector(selector)?.textContent?.replace(/\s+/g, ' ').trim();

  function show(overrides: Partial<Ticket> = {}) {
    cache.put('acme/COW-12', ticket(overrides));
  }

  describe('loading', () => {
    it('loads the ticket by its canonical key', async () => {
      show();

      await render('COW-12');

      expect(shownKey?.()).toBe('acme/COW-12');
    });

    it('loads the comments, the activity, the questions, the links, the interest, the files and the time of the ticket', async () => {
      show();

      await render('COW-12');

      for (const url of Object.values(urls)) {
        expect(http.match(url), url).toHaveLength(1);
      }
    });

    it('loads nothing for a page that belongs to no tenant', async () => {
      tenant.set(null);

      await render('COW-12');

      expect(shownKey?.()).toBeUndefined();
      http.expectNone(() => true);
    });

    it('follows the ticket when the key in the path changes', async () => {
      show();
      const { fixture } = await render('COW-12');
      http.match(() => true);

      fixture.componentRef.setInput('key', 'OPS-3');
      fixture.detectChanges();

      expect(shownKey?.()).toBe('acme/OPS-3');
      expect(http.match(() => true).map((request) => request.request.url)).toEqual([
        '/api/v1/tenants/acme/projects/OPS/tickets/3/comments',
        '/api/v1/tenants/acme/projects/OPS/tickets/3/activity',
        '/api/v1/tenants/acme/projects/OPS/tickets/3/questions',
        '/api/v1/tenants/acme/projects/OPS/tickets/3/links',
        '/api/v1/tenants/acme/projects/OPS/tickets/3/interest',
        '/api/v1/tenants/acme/projects/OPS/tickets/3/attachments',
        '/api/v1/tenants/acme/projects/OPS/tickets/3/time-entries',
      ]);
    });

    it('shows skeletons until the ticket is in the cache', async () => {
      const { fixture, page } = await render();
      expect(page.querySelectorAll('p-skeleton')).toHaveLength(2);
      expect(page.querySelector('[data-testid="ticket-title"]')).toBeNull();

      show();
      fixture.detectChanges();

      expect(text(page, '[data-testid="ticket-title"]')).toBe('The board flickers');
      expect(page.querySelectorAll('p-skeleton').length).toBeGreaterThan(0);
    });

    it('shows what a refetch of the ticket put into the cache', async () => {
      show();
      const { fixture, page } = await render();

      cache.put('acme/COW-12', ticket({ title: 'The board no longer flickers', version: 4 }));
      fixture.detectChanges();

      expect(text(page, '[data-testid="ticket-title"]')).toBe('The board no longer flickers');
    });
  });

  describe('the header', () => {
    it('links the project in the breadcrumbs and names the key', async () => {
      show();

      const { page } = await render();

      const crumbs = page.querySelector('nav.crumbs');
      expect(crumbs?.querySelector('a')?.getAttribute('href')).toBe('/t/acme/p/COW/backlog');
      expect(crumbs?.querySelector('a')?.textContent).toBe('COW');
      expect(crumbs?.querySelector('.tabular')?.textContent).toBe('COW-12');
    });

    it('shows the type, the state and the severity as badges', async () => {
      show();

      const { page } = await render();

      expect(text(page, '.badges app-type')).toBe('bug');
      expect(page.querySelector('.badges app-state [data-state]')?.getAttribute('data-state')).toBe(
        'in-progress',
      );
      expect(
        page.querySelector('.badges app-severity [data-severity]')?.getAttribute('data-severity'),
      ).toBe('high');
    });

    it('shows a security badge for a security class that matters and none for the rest', async () => {
      show({ security: 'live' });
      const { fixture, page } = await render();
      expect(page.querySelector('.badges [data-security="live"]')).not.toBeNull();

      show({ security: 'none', version: 4 });
      fixture.detectChanges();

      expect(page.querySelector('.badges [data-security]')).toBeNull();
    });

    it('marks a confidential ticket', async () => {
      show({ confidential: true });

      const { page } = await render();

      expect(text(page, '.badges > .pill')).toBe('confidential');
    });

    it('marks no ticket as confidential that is not', async () => {
      show();

      const { page } = await render();

      expect(page.querySelector('.badges > .pill')).toBeNull();
    });

    it('shows why a ticket is blocked', async () => {
      show({
        state: 'blocked',
        block: { kind: 'human', from: 'in-progress', reason: 'Waiting for the owner to choose.' },
      });

      const { page } = await render();

      expect(text(page, '[data-testid="block"]')).toBe(
        'Blocked (human): Waiting for the owner to choose.',
      );
    });

    it('names the ticket that a block waits on', async () => {
      show({
        state: 'blocked',
        block: {
          kind: 'ticket',
          from: 'in-progress',
          reason: 'The board comes first.',
          ticket: 'acme/COW-3',
        },
      });

      const { page } = await render();

      expect(text(page, '[data-testid="block"]')).toBe(
        'Blocked (ticket, on acme/COW-3): The board comes first.',
      );
    });

    it('shows no block for a ticket that is not blocked', async () => {
      show();

      const { page } = await render();

      expect(page.querySelector('[data-testid="block"]')).toBeNull();
    });

    it('shows the threat of a security ticket', async () => {
      show({ security: 'boundary', threat: 'A member can read another tenant by guessing a key.' });

      const { page } = await render();

      expect(text(page, '.threat')).toBe('A member can read another tenant by guessing a key.');
    });

    it('shows no threat when the ticket names none', async () => {
      show();

      const { page } = await render();

      expect(page.querySelector('.threat')).toBeNull();
    });
  });

  describe('the description', () => {
    it('shows the body of the ticket', async () => {
      show();

      const { page } = await render();

      expect(text(page, '[data-testid="body"]')).toBe('It flickers on every event.');
    });

    it('says so when the ticket has no body', async () => {
      show({ body: '' });

      const { page } = await render();

      expect(page.querySelector('[data-testid="body"]')).toBeNull();
      expect(page.textContent).toContain('No description.');
    });
  });

  describe('the questions', () => {
    it('shows an open question with its options, its recommendation and who asked whom', async () => {
      show();

      const { page } = await render('COW-12', {
        questions: list(
          question({
            number: 2,
            question: 'Which flicker is it?',
            options: 'Repaint or reflow',
            recommendation: 'Repaint',
            asked_by_agent: 'claude',
            asked_of: sam,
          }),
        ),
      });

      const open = page.querySelector('[data-testid="question-2"]');
      expect(open?.querySelector('.ask')?.textContent).toBe('Which flicker is it?');
      expect(text(page, '[data-testid="question-2"] p:nth-of-type(2)')).toBe(
        'Options: Repaint or reflow',
      );
      expect(text(page, '[data-testid="question-2"] p:nth-of-type(3)')).toBe(
        'Recommended: Repaint',
      );
      expect(text(page, '[data-testid="question-2"] .meta')).toBe(
        'asked by Ada Lovelace via claude · of Sam Rivera · 1 hour ago',
      );
      expect(text(page, 'h2 .count')).toBe('1 open');
    });

    it('leaves out what a question does not have', async () => {
      show();

      const { page } = await render('COW-12', { questions: list(question()) });

      const open = page.querySelector('[data-testid="question-1"]');
      expect(open?.querySelectorAll('p')).toHaveLength(2);
      expect(text(page, '[data-testid="question-1"] .meta')).toBe(
        'asked by Ada Lovelace · 1 hour ago',
      );
    });

    it('says so when no question is open', async () => {
      show();

      const { page } = await render('COW-12', { questions: list() });

      expect(page.textContent).toContain('No open question.');
      expect(text(page, 'h2 .count')).toBe('0 open');
    });

    it('shows the answer of an answered question and marks a withdrawn one', async () => {
      show();

      const { page } = await render('COW-12', {
        questions: list(
          question({
            id: 'q-1',
            number: 1,
            question: 'Repaint or reflow?',
            status: 'answered',
            answer: 'Reflow.',
          }),
          question({ id: 'q-2', number: 2, question: 'Which browser?', status: 'withdrawn' }),
          question({ id: 'q-3', number: 3, question: 'Still open?' }),
        ),
      });

      expect(page.querySelectorAll('.question.open')).toHaveLength(1);
      const [answered, withdrawn] = [
        ...page.querySelectorAll('.question.settled'),
      ] as HTMLElement[];
      expect(text(answered, '.ask')).toBe('Repaint or reflow?');
      expect(text(answered, 'p.small')).toBe('Answer: Reflow.');
      expect(text(withdrawn, '.ask')).toBe('Which browser?');
      expect(text(withdrawn, 'p.small')).toBe('withdrawn');
      expect(text(page, 'h2 .count')).toBe('1 open');
    });
  });

  describe('the comments', () => {
    it('shows a comment with its author, its age and its text', async () => {
      show();

      const { page } = await render('COW-12', { comments: list(comment({ id: 'c-7' })) });

      expect(text(page, '[data-testid="comment-c-7"] .meta')).toBe('Ada Lovelace · 2 hours ago');
      expect(text(page, '[data-testid="comment-c-7"] .text')).toBe(
        'Reproduced on the second board.',
      );
    });

    it('names the agent that wrote a comment in the name of its author and marks an edit', async () => {
      show();

      const { page } = await render('COW-12', {
        comments: list(comment({ id: 'c-7', agent: 'claude', edited: true })),
      });

      expect(text(page, '[data-testid="comment-c-7"] .meta')).toBe(
        'Ada Lovelace via claude · 2 hours ago · edited',
      );
    });

    it('shows a withdrawn comment as withdrawn instead of its text', async () => {
      show();

      const { page } = await render('COW-12', {
        comments: list(comment({ id: 'c-7', withdrawn: true, body: null })),
      });

      expect(page.querySelector('[data-testid="comment-c-7"] .text')).toBeNull();
      expect(text(page, '[data-testid="comment-c-7"] p.muted:not(.meta)')).toBe('withdrawn');
    });

    it('says so when there are no comments', async () => {
      show();

      const { page } = await render('COW-12', { comments: list() });

      expect(page.textContent).toContain('No comments yet.');
    });

    it('shows a skeleton until the comments are loaded', async () => {
      show();
      const { fixture, page } = await render();
      expect(page.querySelectorAll('p-skeleton')).toHaveLength(2);
      expect(page.textContent).not.toContain('No comments yet.');

      await answer(fixture, { comments: list(comment({ id: 'c-7' })) });

      expect(page.querySelectorAll('p-skeleton')).toHaveLength(1);
      expect(page.querySelector('[data-testid="comment-c-7"]')).not.toBeNull();
    });
  });

  describe('the activity', () => {
    it('shows who did what, when, and why', async () => {
      show();

      const { page } = await render('COW-12', {
        activity: list(
          activity({ id: 'a-1', action: 'transitioned', reason: 'The fix is in.' }),
          activity({
            id: 'a-2',
            action: 'confidential_set',
            actor: null,
            actor_system: 'importer',
            note: 'Imported',
          }),
          activity({ id: 'a-3', action: 'created', actor: null, actor_system: null }),
        ),
      });

      const [first, second, third] = [...page.querySelectorAll('.activity li')] as HTMLElement[];
      expect(text(first, '.what')).toBe('Ada Lovelace transitioned');
      expect(text(first, '.when')).toBe('30 minutes ago');
      expect(text(first, '.why')).toBe('The fix is in.');
      expect(text(second, '.what')).toBe('importer confidential set');
      expect(text(second, '.why')).toBe('Imported');
      expect(text(third, '.what')).toBe('cowork created');
      expect(third.querySelector('.why')).toBeNull();
    });

    it('prefers the reason over the note', async () => {
      show();

      const { page } = await render('COW-12', {
        activity: list(activity({ reason: 'The fix is in.', note: 'Merged' })),
      });

      expect(text(page, '.activity .why')).toBe('The fix is in.');
    });

    it('shows the agent chip when an agent acted for the person', async () => {
      show();

      const { page } = await render('COW-12', {
        activity: list(activity({ id: 'a-1', agent: 'claude' }), activity({ id: 'a-2' })),
      });

      const [first, second] = [...page.querySelectorAll('.activity li')] as HTMLElement[];
      expect(text(first, '.agent')).toBe('claude');
      expect(second.querySelector('.agent')).toBeNull();
    });

    it('shows a skeleton until the activity is loaded', async () => {
      show();
      const { fixture, page } = await render();
      expect(page.querySelectorAll('p-skeleton')).toHaveLength(2);
      expect(page.querySelector('.activity')).toBeNull();

      await answer(fixture, { activity: list(activity()) });

      expect(page.querySelectorAll('p-skeleton')).toHaveLength(1);
      expect(page.querySelectorAll('.activity li')).toHaveLength(1);
    });
  });

  describe('the links', () => {
    it('shows each link by its name with the ticket it leads to', async () => {
      show();

      const { page } = await render('COW-12', {
        links: list(
          link({ id: 'l-1', name: 'blocks' }),
          link({
            id: 'l-2',
            name: 'duplicated by',
            ticket: { key: 'acme/COW-9', state: 'done', title: 'Flicker on the second board' },
          }),
        ),
      });

      const [first, second] = [...page.querySelectorAll('.link')] as HTMLElement[];
      expect(text(first, '.muted')).toBe('blocks');
      expect(text(first, '.link-target .tabular')).toBe('acme/COW-3');
      expect(
        first.querySelector('.link-target app-state [data-state]')?.getAttribute('data-state'),
      ).toBe('filed');
      expect(text(first, '.small:last-child')).toBe('Rework the board');
      expect(text(second, '.muted')).toBe('duplicated by');
      expect(text(second, '.link-target .tabular')).toBe('acme/COW-9');
      expect(
        second.querySelector('.link-target app-state [data-state]')?.getAttribute('data-state'),
      ).toBe('done');
    });

    it('says so when the ticket has no links', async () => {
      show();

      const { page } = await render('COW-12', { links: list() });

      expect(page.textContent).toContain('No links.');
    });

    it('shows nothing under the heading until the links are loaded', async () => {
      show();

      const { page } = await render();

      expect(page.textContent).not.toContain('No links.');
      expect(page.querySelector('.link')).toBeNull();
    });

    describe('removing a link', () => {
      const unlink = (page: HTMLElement, id: string) =>
        page.querySelector<HTMLButtonElement>(`[data-testid="unlink-${id}"]`);

      it('offers to remove each link', async () => {
        show();

        const { page } = await render('COW-12', {
          links: list(link({ id: 'l-1' }), link({ id: 'l-2' })),
        });

        expect(unlink(page, 'l-1')?.getAttribute('aria-label')).toBe('Remove this link');
        expect(unlink(page, 'l-2')).not.toBeNull();
      });

      it('removes an outgoing link from this ticket, as its source', async () => {
        show();
        const { page } = await render('COW-12', {
          links: list(
            link({
              id: 'l-1',
              type: 'blocks',
              direction: 'outgoing',
              ticket: { key: 'acme/COW-3', state: 'filed', title: 'Rework the board' },
            }),
          ),
        });

        unlink(page, 'l-1')?.click();

        expect(conversation.unlink).toHaveBeenCalledExactlyOnceWith(
          'acme/COW-12',
          'blocks',
          'acme/COW-3',
        );
      });

      it('removes an incoming link from the other ticket, which is its source', async () => {
        show();
        const { page } = await render('COW-12', {
          links: list(
            link({
              id: 'l-1',
              type: 'duplicates',
              direction: 'incoming',
              ticket: { key: 'acme/COW-9', state: 'done', title: 'Flicker on the second board' },
            }),
          ),
        });

        unlink(page, 'l-1')?.click();

        expect(conversation.unlink).toHaveBeenCalledExactlyOnceWith(
          'acme/COW-9',
          'duplicates',
          'acme/COW-12',
        );
      });

      it('toasts the problem when the link cannot be removed', async () => {
        conversation.unlink.mockRejectedValue(
          problem(409, 'The link is gone already', 'Somebody removed it a moment ago.'),
        );
        const add = vi.spyOn(TestBed.inject(MessageService), 'add');
        show();
        const { page } = await render('COW-12', { links: list(link({ id: 'l-1' })) });

        unlink(page, 'l-1')?.click();
        await new Promise((resolve) => setTimeout(resolve));

        expect(add).toHaveBeenCalledExactlyOnceWith(
          expect.objectContaining({
            summary: 'The link is gone already',
            detail: 'Somebody removed it a moment ago.',
          }),
        );
      });
    });
  });

  describe('the parts of the page', () => {
    const part = <T>(fixture: ComponentFixture<TicketDetail>, type: Type<T>) =>
      fixture.debugElement.query(By.directive(type)).componentInstance as T;
    const parts = <T>(fixture: ComponentFixture<TicketDetail>, type: Type<T>) =>
      fixture.debugElement.queryAll(By.directive(type)).map((each) => each.componentInstance as T);

    it('hands the ticket to its moves in the header and to its fields in the side panel', async () => {
      show();

      const { fixture, page } = await render();

      expect(part(fixture, TicketMoves).ticket().key).toBe('acme/COW-12');
      expect(page.querySelector('.head-top app-ticket-moves')).not.toBeNull();
      expect(part(fixture, TicketFields).ticket().title).toBe('The board flickers');
      expect(page.querySelector('aside.side app-ticket-fields')).not.toBeNull();
    });

    it('gives every part that writes or loads the canonical key of the ticket', async () => {
      show();

      const { fixture } = await render('COW-12', { questions: list(question()) });

      const keys = [
        part(fixture, AnswerQuestion).ticketKey(),
        part(fixture, AskQuestion).ticketKey(),
        part(fixture, CommentComposer).ticketKey(),
        part(fixture, InterestControl).ticketKey(),
        part(fixture, LinkAdder).ticketKey(),
        part(fixture, AttachmentsCard).ticketKey(),
        part(fixture, TimeCard).ticketKey(),
      ];
      expect(keys).toEqual(Array(7).fill('acme/COW-12'));
    });

    it('lets the person answer an open question and change an answer, but not a withdrawn question', async () => {
      show();

      const { fixture } = await render('COW-12', {
        questions: list(
          question({ id: 'q-1', number: 1, question: 'Open?' }),
          question({ id: 'q-2', number: 2, status: 'answered', answer: 'Yes.' }),
          question({ id: 'q-3', number: 3, status: 'withdrawn' }),
        ),
      });

      const answers = parts(fixture, AnswerQuestion).map((each) => each.question().number);
      expect(answers).toEqual([1, 2]);
      expect(parts(fixture, AskQuestion)).toHaveLength(1);
    });

    it('offers to ask a question and to write a comment, whether or not there are any', async () => {
      show();

      const { fixture } = await render('COW-12', { questions: list(), comments: list() });

      expect(parts(fixture, AnswerQuestion)).toHaveLength(0);
      expect(parts(fixture, AskQuestion)).toHaveLength(1);
      expect(parts(fixture, CommentComposer)).toHaveLength(1);
      expect(parts(fixture, LinkAdder)).toHaveLength(1);
    });

    it('hands the interest of the ticket and the person to the interest control', async () => {
      show();
      const holders = [interest(sam, 'watch'), interest(ada, 'need', 'Blocks me')];

      const { fixture } = await render('COW-12', { interest: list(...holders) });

      expect(part(fixture, InterestControl).interests()).toEqual(holders);
      expect(part(fixture, InterestControl).me()).toBe('p1');
    });

    it('hands over no interest before it is loaded, and no person while the person is not known', async () => {
      person.set(undefined);
      show();

      const { fixture } = await render();

      expect(part(fixture, InterestControl).interests()).toEqual([]);
      expect(part(fixture, InterestControl).me()).toBeUndefined();
    });

    it('shows the interest control and the cards for the files and the time beside the links', async () => {
      show();

      const { page } = await render();

      const headings = [...page.querySelectorAll('aside.side h2')].map((heading) =>
        heading.textContent?.replace(/\s+/g, ' ').trim(),
      );
      expect(headings).toEqual(['Interest', 'Links', 'Attachments', 'Time']);
    });
  });

  describe('a part that could not be loaded', () => {
    const parts = [
      'questions',
      'comments',
      'activity',
      'interest',
      'links',
      'attachments',
      'time',
    ] as const;

    /** Refuses the request of one part the way the API does. */
    async function fail(
      fixture: ComponentFixture<TicketDetail>,
      part: (typeof parts)[number],
      body: Problem,
    ) {
      for (const request of http.match(urls[part])) {
        request.flush(body, { status: body.status, statusText: body.title });
      }
      await new Promise((resolve) => setTimeout(resolve));
      fixture.detectChanges();
    }

    const unready: Problem = {
      type: 'about:blank',
      title: 'The service is not ready',
      status: 503,
      detail: 'The database is starting.',
      code: 'not_ready',
    };

    it.each(parts)('says why, with the detail of the problem, in place of the %s', async (name) => {
      show();
      const { fixture, page } = await render();

      await fail(fixture, name, unready);

      expect(text(page, `[data-testid="${name}-failed"]`)).toBe(
        'Could not load this: The database is starting.',
      );
      expect(page.querySelectorAll('[data-testid$="-failed"]')).toHaveLength(1);
    });

    it('says why with the title when the problem has no detail', async () => {
      show();
      const { fixture, page } = await render();

      await fail(fixture, 'comments', {
        ...unready,
        detail: undefined,
        title: 'Forbidden',
        status: 403,
      });

      expect(text(page, '[data-testid="comments-failed"]')).toBe('Could not load this: Forbidden');
    });

    it('says nothing of the parts that loaded', async () => {
      show();
      const { page } = await render('COW-12', {
        questions: list(),
        comments: list(),
        activity: list(),
        interest: list(),
        links: list(),
        attachments: list(),
        time: { items: [], next_cursor: null, total_minutes: 0 },
      });

      expect(page.querySelector('[data-testid$="-failed"]')).toBeNull();
    });

    it('takes the interest control away when the interest cannot be loaded', async () => {
      show();
      const { fixture } = await render();

      await fail(fixture, 'interest', unready);

      expect(fixture.debugElement.query(By.directive(InterestControl))).toBeNull();
    });

    it.each([
      ['attachments', AttachmentsCard, 'Attachments'],
      ['time', TimeCard, 'Time'],
    ] as const)('takes the %s card away and keeps its heading', async (name, card, heading) => {
      show();
      const { fixture, page } = await render();

      await fail(fixture, name, unready);

      expect(fixture.debugElement.query(By.directive(card))).toBeNull();
      const failed = page.querySelector(`[data-testid="${name}-failed"]`);
      expect(failed?.closest('section')?.querySelector('h2')?.textContent).toBe(heading);
    });

    it('keeps the forms next to a list that could not be loaded', async () => {
      show();
      const { fixture } = await render();

      await fail(fixture, 'comments', unready);
      await fail(fixture, 'links', unready);
      await fail(fixture, 'questions', unready);

      expect(fixture.debugElement.query(By.directive(CommentComposer))).not.toBeNull();
      expect(fixture.debugElement.query(By.directive(LinkAdder))).not.toBeNull();
      expect(fixture.debugElement.query(By.directive(AskQuestion))).not.toBeNull();
    });

    it('says that no question is open next to the failure of the questions', async () => {
      show();
      const { fixture, page } = await render();

      await fail(fixture, 'questions', unready);

      expect(page.textContent).toContain('No open question.');
      expect(text(page, 'h2 .count')).toBe('0 open');
    });

    it('shows neither the skeleton nor the list of a failed part', async () => {
      show();
      const { fixture, page } = await render();

      await fail(fixture, 'comments', unready);
      await fail(fixture, 'activity', unready);

      expect(page.querySelector('p-skeleton')).toBeNull();
      expect(page.querySelector('.comment')).toBeNull();
      expect(page.querySelector('.activity')).toBeNull();
    });
  });

  describe('a ticket that changed meanwhile', () => {
    it("is asked about in a dialog of the page, which writes the person's values over it on request", async () => {
      update.mockRejectedValueOnce(
        new StaleWrite(
          {
            status: 412,
            code: 'precondition_failed',
            title: 'The ticket changed',
            detail: 'The ticket changed since you read it.',
            fields: {},
            current: {},
          },
          ticket({ severity: 'critical' }),
        ),
      );
      show();
      const { fixture } = await render();
      expect(fixture.debugElement.query(By.directive(ConfirmDialog))).not.toBeNull();

      fixture.debugElement
        .query(By.css('[data-testid="field-severity"]'))
        .triggerEventHandler('ngModelChange', 'low');
      await new Promise((resolve) => setTimeout(resolve));
      fixture.detectChanges();

      // The dialog opens on the document body, above the page.
      const dialog = document.body.querySelector('.p-confirmdialog');
      expect(dialog?.textContent).toContain(
        'Someone changed this ticket while you edited it. severity: now critical, yours low.',
      );
      const writeMine = [...(dialog?.querySelectorAll('button') ?? [])].find(
        (button) => button.textContent?.trim() === 'Write mine',
      );
      writeMine?.dispatchEvent(new MouseEvent('click', { bubbles: true }));
      await new Promise((resolve) => setTimeout(resolve));

      expect(update).toHaveBeenCalledTimes(2);
      expect(update).toHaveBeenLastCalledWith('acme/COW-12', { severity: 'low' });
    });
  });

  describe('a ticket that cannot be shown', () => {
    it('says there is no such ticket when the API answers 404', async () => {
      loadError.set(problem(404, 'Not found', 'No such ticket.'));

      const { page } = await render();

      const missing = page.querySelector('[data-testid="ticket-missing"]');
      expect(missing?.querySelector('h1')?.textContent).toBe('No such ticket');
      expect(missing?.querySelector('p')?.textContent?.trim()).toBe(
        'It does not exist, or you cannot see it.',
      );
      expect(page.querySelector('p-skeleton')).toBeNull();
      expect(page.querySelector('[data-testid="ticket-title"]')).toBeNull();
    });

    it('shows the problem as the API states it for any other failure', async () => {
      loadError.set(problem(503, 'The service is not ready', 'The database is starting.'));

      const { page } = await render();

      const missing = page.querySelector('[data-testid="ticket-missing"]');
      expect(missing?.querySelector('h1')?.textContent).toBe('The service is not ready');
      expect(missing?.querySelector('p')?.textContent?.trim()).toBe('The database is starting.');
    });

    it('shows the ticket instead of the failure when it is in the cache', async () => {
      show();
      loadError.set(problem(503, 'The service is not ready', 'The database is starting.'));

      const { page } = await render();

      expect(page.querySelector('[data-testid="ticket-missing"]')).toBeNull();
      expect(text(page, '[data-testid="ticket-title"]')).toBe('The board flickers');
    });

    it.each(['cow-12', 'COW', 'COW-0', 'COW-012', 'foo', 'acme/COW-12'])(
      'says there is no such ticket for %j, which is no ticket key, and requests nothing',
      async (key) => {
        const { page } = await render(key);

        expect(shownKey?.()).toBeUndefined();
        http.expectNone(() => true);
        const missing = page.querySelector('[data-testid="ticket-missing"]');
        expect(missing?.querySelector('h1')?.textContent).toBe('No such ticket');
        expect(missing?.querySelector('p')?.textContent?.trim()).toBe(
          'It does not exist, or you cannot see it.',
        );
        expect(page.querySelector('p-skeleton')).toBeNull();
        expect(page.querySelector('nav.crumbs')?.children).toHaveLength(0);
      },
    );

    it('keeps waiting while the tenant of the page is not known, whatever the key', async () => {
      tenant.set(null);

      const { page } = await render('cow-12');

      expect(page.querySelector('[data-testid="ticket-missing"]')).toBeNull();
      expect(page.querySelectorAll('p-skeleton')).toHaveLength(2);
    });

    it('shows the ticket of a valid key as soon as it is in the cache, after a malformed one', async () => {
      const { fixture, page } = await render('cow-12');
      expect(page.querySelector('[data-testid="ticket-missing"]')).not.toBeNull();

      show();
      fixture.componentRef.setInput('key', 'COW-12');
      fixture.detectChanges();

      expect(page.querySelector('[data-testid="ticket-missing"]')).toBeNull();
      expect(text(page, '[data-testid="ticket-title"]')).toBe('The board flickers');
    });
  });
});
