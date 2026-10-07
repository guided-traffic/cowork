import { HttpErrorResponse, provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { computed, signal, Type, WritableSignal } from '@angular/core';
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
  PullRequest,
  PullRequestList,
  Question,
  PrerequisiteTree,
  QuestionList,
  Ticket,
  TicketBody as RenderedBody,
  TimeEntryList,
} from '../../api/models';
import { Conversation } from '../../core/conversation.service';
import { EntityCache } from '../../core/entity-cache';
import { EventStreamService, StreamEvent } from '../../core/event-stream.service';
import { MembersService } from '../../core/members.service';
import { SessionService } from '../../core/session.service';
import { StaleWrite, TicketActions } from '../../core/ticket-actions.service';
import { TicketRecords } from '../../core/ticket-records.service';
import { TicketsService } from '../../core/tickets.service';
import { Clock } from '../../shared/time';
import { CommentItem } from './comment-item';
import {
  AnswerQuestion,
  AskQuestion,
  CommentComposer,
  EditQuestion,
  LinkAdder,
} from './conversation-forms';
import { InterestControl } from './interest-control';
import { PullRequestsCard } from './pull-requests-card';
import { AttachmentsCard, TimeCard } from './records-cards';
import { describe as describeActivity, TicketDetail } from './ticket-detail';
import { TicketFields } from './ticket-fields';
import { TicketMoves } from './ticket-moves';

const ada = { id: 'p1', display_name: 'Ada Lovelace', username: 'local:ada' };
const sam = { id: 'p2', display_name: 'Sam Rivera', username: 'local:sam' };
const script = { id: 'tok-1', name: 'ci-script' };
const laptop = { id: 'tok-2', name: 'claude-laptop' };
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
    reporter_agent: null,
    reporter_token: null,
    block: null,
    threat: null,
    parent: null,
    effort: 'M',
    progress: 40,
    progress_derived: false,
    progress_refinement: 0,
    progress_review: 0,
    horizon: 'next',
    horizon_set: null,
    opened_at: '2026-10-01T09:00:00Z',
    created_at: '2026-10-01T09:00:00Z',
    updated_at: '2026-10-03T11:55:00Z',
    decided_at: null,
    done_at: null,
    done_from: null,
    done_by_hand: false,
    open_prerequisites: 0,
    score: null,
    score_version: null,
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
    options_html: '',
    recommendation: '',
    status: 'open',
    answer: null,
    answer_html: null,
    answered_at: null,
    answered_by: null,
    asked_by: ada,
    asked_by_agent: null,
    asked_by_token: null,
    asked_of: null,
    recorded_by_agent: false,
    answered_by_token: null,
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
    token: null,
    body: 'Reproduced on the second board.',
    body_html: null,
    edited: false,
    explains: [],
    mentions: [],
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
    token: null,
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
    agent: null,
    token: null,
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

const noTree: PrerequisiteTree = { items: [], next_cursor: null, open: 0 };

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

  // docs/adr/0014 D3, docs/adr/0015 D1: the project's act shows on every ticket it moved.
  it('says that the backlog was sorted by the score, where the act is the project', () => {
    expect(
      describeActivity(
        activity({ action: 'ranked', entity_type: 'project', after: { by: 'score' } }),
      ),
    ).toBe('Ada Lovelace sorted the backlog by score');
    expect(describeActivity(activity({ action: 'ranked', entity_type: 'ticket' }))).toBe(
      'Ada Lovelace ranked',
    );
  });

  // docs/adr/0010 D1: the act on the horizon is recorded as overridden, its name before.
  it('says what an act on the horizon did, never overridden', () => {
    expect(
      describeActivity(
        activity({
          action: 'overridden',
          before: { urgency_override: null },
          after: { urgency_override: 'now' },
        }),
      ),
    ).toBe('Ada Lovelace set the horizon to now');
    expect(
      describeActivity(
        activity({
          action: 'overridden',
          before: { urgency_override: 'next' },
          after: { urgency_override: null },
        }),
      ),
    ).toBe('Ada Lovelace returned the ticket to later');
  });

  // docs/adr/0071 D6: what GitHub's webhook linked and reported reads by the number or the id,
  // the webhook's system actor as GitHub.
  it('says what GitHub reported of a pull request or a commit, and who removed a link', () => {
    const github = { actor: null, actor_system: 'system:github' };
    const pr = { number: 34, repository: 'github.com/acme/app', state: 'open' };
    expect(
      describeActivity(
        activity({ ...github, action: 'linked', entity_type: 'pull_request', after: pr }),
      ),
    ).toBe('GitHub linked pull request #34');
    expect(
      describeActivity(
        activity({
          ...github,
          action: 'merged',
          entity_type: 'pull_request',
          after: { ...pr, state: 'merged' },
        }),
      ),
    ).toBe('GitHub reported pull request #34 merged');
    expect(
      describeActivity(
        activity({
          ...github,
          action: 'linked',
          entity_type: 'commit',
          after: {
            sha: '0d1a26e67d8f5eaf1f6ba5c57fc3c7d91ac0fd1c',
            repository: 'github.com/acme/app',
          },
        }),
      ),
    ).toBe('GitHub linked commit 0d1a26e');
    expect(
      describeActivity(
        activity({ action: 'unlinked', entity_type: 'pull_request', before: pr, after: null }),
      ),
    ).toBe('Ada Lovelace removed the link of pull request #34');
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
    pullRequests: `${base}/pull-requests?limit=200`,
    tree: `${base}/prerequisites?direction=down&limit=200`,
    body: `${base}/body`,
  };

  /** What the API answers for the parts around a ticket; a part left out stays unanswered. */
  interface Answers {
    tree?: PrerequisiteTree;
    comments?: CommentList;
    activity?: ActivityList;
    questions?: QuestionList;
    links?: LinkList;
    interest?: InterestList;
    attachments?: AttachmentList;
    time?: TimeEntryList;
    pullRequests?: PullRequestList;
    body?: RenderedBody;
  }

  let tenant: WritableSignal<string | null>;
  let role: WritableSignal<'admin' | 'member'>;
  let person: WritableSignal<Me | undefined>;
  let cache: EntityCache<Ticket>;
  let loadError: WritableSignal<unknown>;
  let loaded: WritableSignal<string | undefined>;
  let shownKey: (() => string | undefined) | undefined;
  let http: HttpTestingController;
  let conversation: { unlink: MockInstance<Conversation['unlink']> };
  let update: MockInstance<TicketActions['update']>;

  beforeEach(() => {
    tenant = signal<string | null>('acme');
    role = signal<'admin' | 'member'>('member');
    person = signal<Me | undefined>({
      ...ada,
      memberships: [],
      global_admin: false,
      local: true,
      password_change_required: false,
    });
    cache = new EntityCache<Ticket>();
    loadError = signal<unknown>(undefined);
    loaded = signal<string | undefined>(undefined);
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
        {
          provide: SessionService,
          useValue: { tenant, person, membership: computed(() => ({ role: role() })) },
        },
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
              return { error: loadError, hasValue: () => loaded() !== undefined, value: loaded };
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
        '/api/v1/tenants/acme/projects/OPS/tickets/3/pull-requests',
        '/api/v1/tenants/acme/projects/OPS/tickets/3/prerequisites',
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
    it('links the project in the breadcrumbs to its board and names the key', async () => {
      show();

      const { page } = await render();

      const crumbs = page.querySelector('nav.crumbs');
      expect(crumbs?.querySelector('a')?.getAttribute('href')).toBe('/t/acme/p/COW/board');
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

    it('offers the deletion to a tenant administrator only (docs/adr/0024 D7)', async () => {
      show();
      const member = await render();
      expect(member.page.querySelector('[data-testid="delete-ticket"]')).toBeNull();

      role.set('admin');
      member.fixture.detectChanges();

      expect(member.page.querySelector('[data-testid="delete-ticket"]')).not.toBeNull();
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

    it('shows the body as the server rendered it, once it has the rendering', async () => {
      show();

      const { page } = await render('COW-12', {
        body: {
          body: 'It flickers on every event.',
          body_html: '<p>It <em>flickers</em> on every event.</p>',
          version: 3,
        },
      });

      expect(page.querySelector('[data-testid="body"] .rendered em')?.textContent).toBe('flickers');
    });

    it('loads the rendering again for a newer version, and for an upload, which moves none', async () => {
      show();
      const { fixture } = await render('COW-12', {
        body: { body: 'It flickers on every event.', body_html: '<p>It flickers.</p>', version: 3 },
      });

      show({ version: 4, body: 'It flickers less.' });
      fixture.detectChanges();
      await answer(fixture, {
        body: { body: 'It flickers less.', body_html: '<p>It flickers less.</p>', version: 4 },
      });
      expect(text(fixture.nativeElement, '[data-testid="body"]')).toBe('It flickers less.');

      (TestBed.inject(EventStreamService).events as Subject<StreamEvent>).next({
        name: 'ticket.changed',
        id: 'act-1',
        key: 'acme/COW-12',
        version: 4,
        kind: 'uploaded',
      });
      await answer(fixture, {});
      expect(http.match(urls.body)).toHaveLength(1);
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
            options_html: '<p>Repaint or reflow</p>',
            recommendation: 'Repaint',
            asked_by_agent: 'claude',
            asked_of: sam,
          }),
        ),
      });

      const open = page.querySelector('[data-testid="question-2"]');
      expect(open?.querySelector('.ask')?.textContent).toBe('Which flicker is it?');
      expect(text(page, '[data-testid="question-2"] .options strong')).toBe('Options:');
      expect(text(page, '[data-testid="question-2"] .options .rendered')).toBe('Repaint or reflow');
      expect(text(page, '[data-testid="question-2"] p:nth-of-type(2)')).toBe(
        'Recommended: Repaint',
      );
      expect(text(page, '[data-testid="question-2"] .meta')).toBe(
        'asked by Ada Lovelace by the agent claude · of Sam Rivera · 1 hour ago',
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

    it("shows the options and the answer as the server rendered them, through Angular's sanitiser", async () => {
      show();

      const { page } = await render('COW-12', {
        questions: list(
          question({
            number: 1,
            options: '- *repaint*',
            options_html: '<ul><li><em>repaint</em></li></ul><img src="x" onerror="alert(1)">',
          }),
          question({
            id: 'q-2',
            number: 2,
            status: 'answered',
            answer: 'see [the run](https://ci.example/7)',
            answer_html:
              '<p>see <a href="https://ci.example/7" rel="noopener noreferrer nofollow" target="_blank">the run</a></p>',
          }),
        ),
      });

      const options = page.querySelector('[data-testid="question-1"] .options .rendered');
      expect(options?.querySelector('em')?.textContent).toBe('repaint');
      expect(options?.querySelector('img')?.getAttribute('onerror')).toBeNull();
      const link = page.querySelector('[data-testid="question-answer"] a');
      expect(link?.getAttribute('href')).toBe('https://ci.example/7');
      expect(link?.getAttribute('rel')).toBe('noopener noreferrer nofollow');
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
            answer_html: '<p>Reflow.</p>',
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
      expect(text(answered, '.answer strong')).toBe('Answer:');
      expect(text(answered, '.answer .rendered')).toBe('Reflow.');
      expect(text(withdrawn, '.ask')).toBe('Which browser?');
      expect(text(withdrawn, 'p.small')).toBe('withdrawn');
      expect(text(page, 'h2 .count')).toBe('1 open');
    });

    it('says who answered, and marks an answer an agent recorded in their name', async () => {
      show();

      const { page } = await render('COW-12', {
        questions: list(
          question({
            id: 'q-1',
            number: 1,
            status: 'answered',
            answer: 'Reflow.',
            answered_by: sam,
            answered_at: '2026-10-03T11:00:00Z',
            recorded_by_agent: true,
          }),
          question({
            id: 'q-2',
            number: 2,
            status: 'answered',
            answer: 'Repaint.',
            answered_by: ada,
            answered_at: '2026-10-03T11:00:00Z',
          }),
        ),
      });

      const [recorded, own] = [...page.querySelectorAll('.question.settled')] as HTMLElement[];
      expect(text(recorded, '.meta')).toBe(
        'answered by Sam Rivera · recorded by an agent · 1 hour ago',
      );
      expect(recorded.querySelector('[data-testid="agent-mark"]')?.getAttribute('data-agent')).toBe(
        'agent',
      );
      expect(text(own, '.meta')).toBe('answered by Ada Lovelace · 1 hour ago');
      expect(own.querySelector('[data-testid="agent-mark"]')).toBeNull();
    });

    it('marks a question asked through a token, by the token, and one an agent asked through it, by the agent', async () => {
      show();

      const { page } = await render('COW-12', {
        questions: list(
          question({ id: 'q-1', number: 1, asked_by_token: script }),
          question({
            id: 'q-2',
            number: 2,
            asked_by_agent: 'claude-code/opus/s-1',
            asked_by_token: laptop,
          }),
        ),
      });

      expect(text(page, '[data-testid="question-1"] .meta')).toBe(
        'asked by Ada Lovelace through the token ci-script · 1 hour ago',
      );
      const plain = page.querySelector('[data-testid="question-1"] [data-testid="agent-mark"]');
      expect(plain?.getAttribute('data-token')).toBe('tok-1');
      expect(plain?.hasAttribute('data-agent')).toBe(false);
      expect(text(page, '[data-testid="question-2"] .meta')).toBe(
        'asked by Ada Lovelace by the agent claude-code (claude-code/opus/s-1), through the token claude-laptop · 1 hour ago',
      );
    });

    it("marks an answer recorded through a token, the person's own or an agent's", async () => {
      show();

      const { page } = await render('COW-12', {
        questions: list(
          question({
            id: 'q-1',
            number: 1,
            status: 'answered',
            answer: 'Reflow.',
            answered_by: sam,
            answered_at: '2026-10-03T11:00:00Z',
            answered_by_token: script,
          }),
          question({
            id: 'q-2',
            number: 2,
            status: 'answered',
            answer: 'Repaint.',
            answered_by: sam,
            answered_at: '2026-10-03T11:00:00Z',
            recorded_by_agent: true,
            answered_by_token: laptop,
          }),
        ),
      });

      const [byToken, byAgent] = [...page.querySelectorAll('.question.settled')] as HTMLElement[];
      expect(text(byToken, '.meta')).toBe(
        'answered by Sam Rivera · recorded through the token ci-script · 1 hour ago',
      );
      expect(byToken.querySelector('[data-testid="agent-mark"]')?.hasAttribute('data-agent')).toBe(
        false,
      );
      expect(text(byAgent, '.meta')).toBe(
        'answered by Sam Rivera · recorded by an agent, through the token claude-laptop · 1 hour ago',
      );
      expect(byAgent.querySelector('[data-testid="agent-mark"]')?.getAttribute('data-token')).toBe(
        'tok-2',
      );
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
        'Ada Lovelace by the agent claude · 2 hours ago · edited',
      );
    });

    it('marks a comment written through a token with the token, beside its author', async () => {
      show();

      const { page } = await render('COW-12', {
        comments: list(comment({ id: 'c-7', token: script }), comment({ id: 'c-8' })),
      });

      expect(text(page, '[data-testid="comment-c-7"] .meta')).toBe(
        'Ada Lovelace through the token ci-script · 2 hours ago',
      );
      expect(
        page.querySelector('[data-testid="comment-c-8"] [data-testid="agent-mark"]'),
      ).toBeNull();
    });

    it('shows a withdrawn comment as withdrawn instead of its text', async () => {
      show();

      const { page } = await render('COW-12', {
        comments: list(comment({ id: 'c-7', withdrawn: true, body: null })),
      });

      expect(page.querySelector('[data-testid="comment-c-7"] .text')).toBeNull();
      expect(text(page, '[data-testid="comment-c-7"] p.muted:not(.meta)')).toBe('withdrawn');
    });

    it('shows a comment as the server rendered it', async () => {
      show();

      const { page } = await render('COW-12', {
        comments: list(
          comment({ id: 'c-7', body: '**done**', body_html: '<p><strong>done</strong></p>' }),
        ),
      });

      expect(page.querySelector('[data-testid="comment-c-7"] .rendered strong')?.textContent).toBe(
        'done',
      );
      expect(page.querySelector('[data-testid="comment-c-7"]')?.id).toBe('comment-c-7');
    });

    it('says so when there are no comments', async () => {
      show();

      const { page } = await render('COW-12', { comments: list() });

      expect(page.textContent).toContain('No comments yet.');
    });

    it('shows a skeleton until the comments are loaded', async () => {
      show();
      const { fixture, page } = await render('COW-12', { tree: noTree });
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
      const mark = first.querySelector('[data-testid="agent-mark"]');
      expect(mark?.getAttribute('data-agent')).toBe('claude');
      expect(mark?.querySelector('.pi-microchip-ai')).not.toBeNull();
      expect(second.querySelector('[data-testid="agent-mark"]')).toBeNull();
    });

    it("marks an act made through a token, and an agent's act with its token as well", async () => {
      show();

      const { page } = await render('COW-12', {
        activity: list(
          activity({ id: 'a-1', token: script }),
          activity({ id: 'a-2', agent: 'claude', token: laptop }),
          activity({ id: 'a-3' }),
        ),
      });

      const [byToken, byAgent, own] = [...page.querySelectorAll('.activity li')] as HTMLElement[];
      const plain = byToken.querySelector('[data-testid="agent-mark"]');
      expect(plain?.getAttribute('data-token')).toBe('tok-1');
      expect(plain?.hasAttribute('data-agent')).toBe(false);
      expect(plain?.querySelector('.pi-microchip-ai')).not.toBeNull();
      expect(text(byToken, '.what')).toBe('Ada Lovelace transitioned');
      expect(text(byToken, '[data-testid="agent-mark"]')).toBe('through the token ci-script');
      const agent = byAgent.querySelector('[data-testid="agent-mark"]');
      expect(agent?.getAttribute('data-agent')).toBe('claude');
      expect(agent?.getAttribute('data-token')).toBe('tok-2');
      expect(own.querySelector('[data-testid="agent-mark"]')).toBeNull();
    });

    it('shows a skeleton until the activity is loaded', async () => {
      show();
      const { fixture, page } = await render('COW-12', { tree: noTree });
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

  // docs/adr/0071 D6: what GitHub's webhook linked, hidden while there is none, and the hint that
  // the work may be ready to move once something merged — which moves nothing.
  describe('the pull requests', () => {
    function pr(overrides: Partial<PullRequest> = {}): PullRequest {
      return {
        id: 'l-1',
        kind: 'pull_request',
        repository: 'github.com/acme/app',
        number: 34,
        sha: null,
        title: 'fix: the flicker (COW-12)',
        state: 'open',
        url: 'https://github.com/acme/app/pull/34',
        author: 'octocat',
        merged_at: null,
        found_in: 'subject',
        first_seen_at: '2026-10-05T08:00:00Z',
        last_seen_at: '2026-10-05T08:00:00Z',
        ...overrides,
      };
    }

    it('hides the card while the ticket has none', async () => {
      show();
      const { page } = await render('COW-12', { pullRequests: list() });

      expect(page.querySelector('[data-testid="pull-requests"]')).toBeNull();
      expect(page.querySelector('[data-testid="merge-hint"]')).toBeNull();
    });

    it('shows the card beside the links, and lets a member remove a wrong link', async () => {
      show();
      const { fixture, page } = await render('COW-12', { pullRequests: list(pr()) });

      expect(page.querySelector('[data-testid="pull-requests"]')).not.toBeNull();
      const card = fixture.debugElement.query(By.directive(PullRequestsCard))
        .componentInstance as PullRequestsCard;
      expect(card.ticketKey()).toBe('acme/COW-12');
      expect(card.items().map((each) => each.id)).toEqual(['l-1']);
      expect(card.mayRemove()).toBe(true);
      const headings = [...page.querySelectorAll('aside.side h2')].map((heading) =>
        heading.textContent?.replace(/\s+/g, ' ').trim(),
      );
      expect(headings).toEqual(['Interest', 'Links', 'Pull requests', 'Attachments', 'Time']);
    });

    it('offers a viewer no removal', async () => {
      role.set('viewer' as 'member');
      show();
      const { fixture } = await render('COW-12', { pullRequests: list(pr()) });

      const card = fixture.debugElement.query(By.directive(PullRequestsCard))
        .componentInstance as PullRequestsCard;
      expect(card.mayRemove()).toBe(false);
    });

    it('hints that an open ticket may be ready to move once a pull request merged', async () => {
      show({ state: 'in-progress' });
      const { page } = await render('COW-12', {
        pullRequests: list(pr({ state: 'merged', merged_at: '2026-10-06T09:00:00Z' })),
      });

      expect(text(page, '[data-testid="merge-hint"]')).toBe(
        'Pull request #34 was merged — the work may be ready to move. Nothing moved it: moving it stays yours.',
      );
      expect(text(page, '[data-testid="ticket-state"]')).toBe('in-progress');
    });

    it('names a commit that reached the default branch, and hints nothing on a closed ticket', async () => {
      const commit = pr({
        kind: 'commit',
        number: null,
        sha: 'a'.repeat(40),
        state: 'merged',
        merged_at: '2026-10-06T09:00:00Z',
      });
      show({ state: 'review' });
      const { page } = await render('COW-12', { pullRequests: list(commit) });
      expect(text(page, '[data-testid="merge-hint"]')).toContain(
        'A commit naming it reached the default branch',
      );

      show({ state: 'done' });
      const done = await render('COW-12', { pullRequests: list(commit) });
      expect(done.page.querySelector('[data-testid="merge-hint"]')).toBeNull();
    });

    it('says why in place of the card when they could not be loaded', async () => {
      show();
      const { fixture, page } = await render();
      for (const request of http.match(urls.pullRequests)) {
        request.flush(
          {
            type: 'about:blank',
            title: 'Not ready',
            status: 503,
            detail: 'The database is starting.',
            code: 'not_ready',
          },
          { status: 503, statusText: 'Not ready' },
        );
      }
      await new Promise((resolve) => setTimeout(resolve));
      fixture.detectChanges();

      expect(text(page, '[data-testid="pull-requests-failed"]')).toBe(
        'Could not load this: The database is starting.',
      );
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
      const { fixture, page } = await render('COW-12', { tree: noTree });

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

    it('quotes what changed as text, never as markup', async () => {
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
          ticket({ assignee: { id: 'p9', display_name: '<a href="x">y</a>' } }),
        ),
      );
      show();
      const { fixture } = await render();

      fixture.debugElement
        .query(By.css('[data-testid="field-assignee"]'))
        .triggerEventHandler('ngModelChange', 'p1');
      await new Promise((resolve) => setTimeout(resolve));
      fixture.detectChanges();

      const dialog = document.body.querySelector('.p-confirmdialog');
      expect(dialog?.querySelector('.p-confirmdialog-message')?.textContent).toContain(
        'assignee: now {"id":"p9","display_name":"<a href=\\"x\\">y</a>"}, yours p1.',
      );
      expect(dialog?.querySelector('a')).toBeNull();
    });

    // The page is reused when the path names another ticket or another tenant. Answered then,
    // the question would write the change onto the ticket shown now, where the fields stay, or
    // onto the ticket the page no longer shows.
    describe('when the page turns elsewhere before it is answered', () => {
      async function asked() {
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
        fixture.debugElement
          .query(By.css('[data-testid="field-severity"]'))
          .triggerEventHandler('ngModelChange', 'low');
        await new Promise((resolve) => setTimeout(resolve));
        fixture.detectChanges();
        expect(document.body.querySelector('.p-confirmdialog')?.textContent).toContain(
          'Changed meanwhile',
        );
        return fixture;
      }

      /** Answers the question, if it is still there, by writing over the newer version. */
      async function writeMine(fixture: ComponentFixture<TicketDetail>) {
        [...(document.body.querySelector('.p-confirmdialog')?.querySelectorAll('button') ?? [])]
          .find((button) => button.textContent?.trim() === 'Write mine')
          ?.dispatchEvent(new MouseEvent('click', { bubbles: true }));
        await new Promise((resolve) => setTimeout(resolve));
        fixture.detectChanges();
      }

      it('drops the question for another ticket, which would take the change', async () => {
        cache.put('acme/COW-13', ticket({ id: 't-13', key: 'acme/COW-13', number: 13 }));
        const fixture = await asked();

        fixture.componentRef.setInput('key', 'COW-13');
        fixture.detectChanges();
        await new Promise((resolve) => setTimeout(resolve));
        fixture.detectChanges();

        expect(document.body.querySelector('.p-confirmdialog')).toBeNull();
        await writeMine(fixture);
        expect(update).toHaveBeenCalledOnce();
      });

      it('drops the question for another tenant', async () => {
        const fixture = await asked();

        tenant.set('globex');
        fixture.detectChanges();
        await new Promise((resolve) => setTimeout(resolve));
        fixture.detectChanges();

        expect(document.body.querySelector('.p-confirmdialog')).toBeNull();
        await writeMine(fixture);
        expect(update).toHaveBeenCalledOnce();
      });
    });
  });

  describe('the parts that edit (docs/adr/0018 D2)', () => {
    it('gives the title, the body and the prerequisite tree a place, the title as the heading', async () => {
      show();

      const { page } = await render();

      expect(page.querySelector('.head app-ticket-title h1')?.textContent).toBe(
        'The board flickers',
      );
      expect(page.querySelector('app-ticket-body [data-testid="body"]')?.textContent).toBe(
        'It flickers on every event.',
      );
      expect(page.querySelector('app-prerequisite-tree')).not.toBeNull();
    });

    it('offers each open question its editor, which decides whom it offers itself to', async () => {
      show();

      const { fixture } = await render('COW-12', {
        questions: list(question(), question({ id: 'q-2', number: 2, status: 'withdrawn' })),
      });

      const editors = fixture.debugElement.queryAll(By.directive(EditQuestion));
      expect(editors.map((each) => (each.componentInstance as EditQuestion).question().id)).toEqual(
        ['q-1'],
      );
    });

    it('hands each comment its own files, the person and whether they administer the tenant', async () => {
      show();
      role.set('admin');
      const shot: AttachmentList['items'][number] = {
        id: 'a-1',
        comment: 'c-1',
        file_name: 'shot.png',
        content_type: 'image/png',
        content_url: '/api/v1/tenants/acme/projects/COW/tickets/12/attachments/a-1/content',
        sha256: 'ab12',
        size: 2048,
        uploaded_by: ada,
        agent: null,
        token: null,
        created_at: '2026-10-03T11:00:00Z',
      };
      const loose = { ...shot, id: 'a-2', comment: null };

      const { fixture } = await render('COW-12', {
        comments: list(comment(), comment({ id: 'c-2' })),
        attachments: list(shot, loose),
      });

      const items = fixture.debugElement
        .queryAll(By.directive(CommentItem))
        .map((each) => each.componentInstance as CommentItem);
      expect(items.map((item) => item.files().map((file) => file.id))).toEqual([['a-1'], []]);
      expect(items.map((item) => [item.me(), item.administers()])).toEqual([
        ['p1', true],
        ['p1', true],
      ]);
    });

    it('closes the title and the body editors when the path names another ticket', async () => {
      show();
      cache.put(
        'acme/COW-13',
        ticket({ id: 't-13', key: 'acme/COW-13', number: 13, title: 'Next' }),
      );
      const { fixture, page } = await render();
      (page.querySelector('[data-testid="title-edit"]') as HTMLButtonElement).click();
      (page.querySelector('[data-testid="body-edit"]') as HTMLButtonElement).click();
      await new Promise((resolve) => setTimeout(resolve));
      fixture.detectChanges();
      expect(page.querySelector('[data-testid="title-input"]')).not.toBeNull();
      expect(page.querySelector('[data-testid="body-input"]')).not.toBeNull();

      fixture.componentRef.setInput('key', 'COW-13');
      fixture.detectChanges();
      await new Promise((resolve) => setTimeout(resolve));
      fixture.detectChanges();

      expect(page.querySelector('[data-testid="title-input"]')).toBeNull();
      expect(page.querySelector('[data-testid="body-input"]')).toBeNull();
      expect(page.querySelector('[data-testid="ticket-title"]')?.textContent).toBe('Next');
      expect(update).not.toHaveBeenCalled();
    });

    it('closes the editors of a comment and of a question with the ticket they belong to', async () => {
      show();
      cache.put('acme/COW-13', ticket({ id: 't-13', key: 'acme/COW-13', number: 13 }));
      const { fixture, page } = await render('COW-12', {
        comments: list(comment()),
        questions: list(question()),
      });
      (page.querySelector('[data-testid="comment-edit-c-1"]') as HTMLButtonElement).click();
      (page.querySelector('[data-testid="edit-question-1"]') as HTMLButtonElement).click();
      await new Promise((resolve) => setTimeout(resolve));
      fixture.detectChanges();
      expect(page.querySelector('[data-testid="comment-input-c-1"]')).not.toBeNull();
      expect(page.querySelector('[data-testid="edit-question-text-1"]')).not.toBeNull();

      fixture.componentRef.setInput('key', 'COW-13');
      fixture.detectChanges();
      await new Promise((resolve) => setTimeout(resolve));
      fixture.detectChanges();

      expect(page.querySelector('[data-testid="comment-input-c-1"]')).toBeNull();
      expect(page.querySelector('[data-testid="edit-question-text-1"]')).toBeNull();
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

    it('says there is no such ticket when the ticket it showed leaves the cache, deleted or out of sight', async () => {
      show();
      loaded.set('acme/COW-12');
      const { fixture, page } = await render();
      expect(text(page, '[data-testid="ticket-title"]')).toBe('The board flickers');

      cache.delete('acme/COW-12');
      fixture.detectChanges();

      const missing = page.querySelector('[data-testid="ticket-missing"]');
      expect(missing?.querySelector('h1')?.textContent).toBe('No such ticket');
      expect(page.querySelector('p-skeleton')).toBeNull();
    });

    it('waits with skeletons while the load of another key has not answered yet', async () => {
      loaded.set('acme/COW-11');

      const { page } = await render('COW-12');

      expect(page.querySelector('[data-testid="ticket-missing"]')).toBeNull();
      expect(page.querySelectorAll('p-skeleton').length).toBeGreaterThan(0);
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
