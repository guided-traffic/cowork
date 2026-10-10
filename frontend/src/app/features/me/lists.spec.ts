import { HttpErrorResponse, HttpHeaders } from '@angular/common/http';
import { Component, signal, Type } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideRouter } from '@angular/router';
import { MessageService } from 'primeng/api';
import { Subject } from 'rxjs';
import { Api } from '../../api/api';
import { listMyAssigned } from '../../api/fn/me/list-my-assigned';
import { listMyDecisions } from '../../api/fn/me/list-my-decisions';
import { listMyNext } from '../../api/fn/me/list-my-next';
import { Decision, DecisionList, Me, MyTicketList, Ticket } from '../../api/models';
import { EventStreamService, StreamEvent } from '../../core/event-stream.service';
import { SessionService } from '../../core/session.service';
import { askedOf, Decisions } from './decisions';
import { MyList, MyTickets } from './my-tickets';
import { shortKey, ticketRoute } from './person-list';

/**
 * The Api of the page's specs: `invoke` answers the body, and `invoke$Response`, which the
 * conditional loads use (docs/adr/0054 D7), the same body with no headers.
 */
function apiOf(invoke: ReturnType<typeof vi.fn>) {
  return {
    invoke,
    invoke$Response: async (fn: unknown, params: unknown) => ({
      body: await (invoke as (fn: unknown, params: unknown) => Promise<unknown>)(fn, params),
      headers: new HttpHeaders(),
    }),
  };
}

@Component({ template: '' })
class Page {}

const ada = { id: 'p1', display_name: 'Ada Lovelace', username: 'ada' };
const sam = { id: 'p2', display_name: 'Sam Rivera', username: 'sam' };
const me: Me = {
  ...ada,
  global_admin: false,
  local: true,
  password_change_required: false,
  memberships: [],
};

function ticket(key: string, overrides: Partial<Ticket> = {}): Ticket {
  const [, short] = key.split('/');
  const [project, number] = short.split('-');
  return {
    id: `t-${key}`,
    key,
    number: Number(number),
    project,
    title: `Title of ${key}`,
    body: '',
    type: 'task',
    state: 'in-progress',
    severity: 'high',
    security: 'none',
    confidential: false,
    assignee: ada,
    reporter: sam,
    reporter_agent: null,
    reporter_token: null,
    block: null,
    threat: null,
    parent: null,
    effort: 'M',
    progress: 0,
    progress_derived: false,
    progress_refinement: 0,
    progress_review: 0,
    horizon: 'later',
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
    version: 1,
    ...overrides,
  };
}

function decision(id: string, key: string, askedOfPerson: typeof ada | null): Decision {
  return {
    team: { slug: key.split('/')[0], name: 'Acme Corp' },
    // The deprecated name of team, which the page never shows (docs/adr/0005 D1).
    tenant: { slug: key.split('/')[0], name: 'not shown' },
    ticket: { key, title: `Title of ${key}`, state: 'analysed' },
    question: {
      id,
      number: 1,
      question: 'Which way?',
      options: '',
      options_html: '',
      recommendation: '',
      answer: null,
      answer_html: null,
      status: 'open',
      asked_by: sam,
      asked_by_agent: null,
      asked_by_token: null,
      asked_of: askedOfPerson,
      answered_by: null,
      answered_at: null,
      recorded_by_agent: false,
      answered_by_token: null,
      withdrawn_at: null,
      version: 1,
      created_at: '2026-10-04T09:00:00Z',
      updated_at: '2026-10-04T09:00:00Z',
    },
  };
}

describe('the helpers of the person-level pages', () => {
  it("routes a canonical key to its tenant's ticket page and shortens it", () => {
    expect(ticketRoute('acme/COW-12')).toEqual(['/t', 'acme', 'tickets', 'COW-12']);
    expect(shortKey('acme/COW-12')).toBe('COW-12');
  });

  it('says whom a decision waits for', () => {
    expect(askedOf(decision('q1', 'acme/COW-1', ada), 'p1')).toBe('asked of you');
    expect(askedOf(decision('q1', 'acme/COW-1', null), 'p1')).toBe('open in the team');
    expect(askedOf(decision('q1', 'acme/COW-1', null), undefined)).toBe('open in the team');
  });
});

describe('the person-level lists', () => {
  let stream: Subject<StreamEvent>;
  let invoke: ReturnType<typeof vi.fn>;

  function configure(answer: (fn: unknown, cursor?: string) => unknown) {
    stream = new Subject<StreamEvent>();
    invoke = vi.fn(async (fn: unknown, params: { cursor?: string }) => answer(fn, params.cursor));
    TestBed.configureTestingModule({
      providers: [
        provideRouter([{ path: '**', component: Page }]),
        MessageService,
        { provide: Api, useValue: apiOf(invoke) },
        { provide: SessionService, useValue: { person: signal(me) } },
        { provide: EventStreamService, useValue: { events: stream.asObservable() } },
      ],
    });
  }

  async function render<T>(component: Type<T>) {
    const fixture: ComponentFixture<T> = TestBed.createComponent(component);
    await fixture.whenStable();
    return { fixture, page: fixture.nativeElement as HTMLElement };
  }

  const byTestId = (page: HTMLElement, id: string) => page.querySelector(`[data-testid="${id}"]`);

  describe('MyTickets', () => {
    async function renderList(list: MyList) {
      const fixture: ComponentFixture<MyTickets> = TestBed.createComponent(MyTickets);
      fixture.componentRef.setInput('list', list);
      await fixture.whenStable();
      return { fixture, page: fixture.nativeElement as HTMLElement };
    }

    const first: MyTicketList = {
      items: [
        {
          team: { slug: 'acme', name: 'Acme Corp' },
          // The deprecated name of team, which the page never shows (docs/adr/0005 D1).
          tenant: { slug: 'acme', name: 'not shown' },
          ticket: ticket('acme/COW-2', { score: 9.4, score_version: 1, horizon: 'now' }),
          place: 2,
        },
        {
          team: { slug: 'globex', name: 'Globex' },
          tenant: { slug: 'globex', name: 'not shown' },
          ticket: ticket('globex/OPS-1', { assignee: null, score: 4, score_version: 1 }),
          place: 1,
        },
      ],
      next_cursor: null,
    };

    // docs/adr/0018 D3 as amended 2026-10-05, docs/adr/0014 D5.
    it('lists "next for me" in the order given, each beside its tenant and its place in the backlog', async () => {
      configure(() => first);

      const { page } = await renderList('next');

      expect(byTestId(page, 'my-title')?.textContent?.trim()).toBe('Next for me');
      const rows = [...page.querySelectorAll('.row')];
      expect(rows.map((row) => row.getAttribute('data-testid'))).toEqual([
        'next-acme/COW-2',
        'next-globex/OPS-1',
      ]);
      expect(
        rows.map((row) => row.querySelector('[data-testid="tenant"]')?.textContent?.trim()),
      ).toEqual(['Acme Corp', 'Globex']);
      const place = rows[0].querySelector('[data-testid="place"]');
      expect(place?.firstChild?.textContent?.trim()).toBe('now #2');
      expect(place?.querySelector('.sr-only')?.textContent).toContain(
        '#2 of now in the backlog of COW; score 9.4',
      );
      expect(
        rows.map((row) => row.querySelector('[data-testid="whose"]')?.textContent?.trim()),
      ).toEqual(['yours', 'unassigned']);
      expect(rows[1].querySelector('a')?.getAttribute('href')).toBe('/t/globex/tickets/OPS-1');
      expect(byTestId(page, 'next-count')?.textContent?.trim()).toBe('2 open tickets');
      expect(invoke).toHaveBeenCalledWith(listMyNext, { cursor: undefined, limit: 50 });
    });

    it('lists the tickets assigned to the person, without whose they are', async () => {
      configure(() => first);

      const { page } = await renderList('assigned');

      expect(byTestId(page, 'my-title')?.textContent?.trim()).toBe('Assigned to me');
      expect(byTestId(page, 'assigned-acme/COW-2')).not.toBeNull();
      expect(byTestId(page, 'whose')).toBeNull();
      expect(byTestId(page, 'assigned-count')?.textContent?.trim()).toBe('2 open tickets');
      expect(invoke).toHaveBeenCalledWith(listMyAssigned, { cursor: undefined, limit: 50 });
    });

    it('says a ticket without a score has none yet', async () => {
      configure(() => ({
        items: [
          {
            team: { slug: 'acme', name: 'Acme Corp' },
            tenant: { slug: 'acme', name: 'Acme Corp' },
            ticket: ticket('acme/COW-9'),
            place: 1,
          },
        ],
        next_cursor: null,
      }));

      const { page } = await renderList('next');

      expect(byTestId(page, 'place')?.querySelector('.sr-only')?.textContent).toContain(
        'no score yet',
      );
    });

    it.each<MyList>(['next', 'assigned'])(
      'sends the weak ETag of %s on a poll and keeps the tickets on a 304 (docs/adr/0054 D7)',
      async (list) => {
        configure(() => first);
        const asked: unknown[] = [];
        TestBed.overrideProvider(Api, {
          useValue: {
            invoke,
            invoke$Response: async (_fn: unknown, params: Record<string, unknown>) => {
              asked.push(params);
              if (params['If-None-Match'] === 'W/"one"') {
                throw new HttpErrorResponse({ status: 304, statusText: 'Not Modified' });
              }
              return { body: first, headers: new HttpHeaders({ ETag: 'W/"one"' }) };
            },
          },
        });
        const { fixture, page } = await renderList(list);

        stream.next({ name: 'poll' });
        await fixture.whenStable();

        expect(asked).toEqual([
          { cursor: undefined, limit: 50 },
          { cursor: undefined, limit: 50, 'If-None-Match': 'W/"one"' },
        ]);
        expect(page.querySelectorAll('.row')).toHaveLength(2);
        expect(byTestId(page, `${list}-count`)?.textContent?.trim()).toBe('2 open tickets');
      },
    );

    it("loads again on a ticket change of any of the person's tenants, a stake and a project sorted, not on a comment or the count (docs/adr/0054 D1)", async () => {
      configure(() => first);
      const { fixture } = await renderList('next');
      invoke.mockClear();

      stream.next({
        name: 'ticket.changed',
        id: 'e1',
        key: 'acme/COW-2',
        version: 2,
        kind: 'assigned',
      });
      await fixture.whenStable();
      stream.next({
        name: 'ticket.changed',
        id: 'e2',
        key: 'globex/OPS-1',
        version: 3,
        kind: 'unassigned',
      });
      await fixture.whenStable();
      stream.next({
        name: 'interest.changed',
        id: 'e4',
        key: 'acme/COW-2',
        version: 2,
        kind: 'interest',
      });
      await fixture.whenStable();
      stream.next({ name: 'project.changed', id: 'e5', key: 'globex/OPS', kind: 'ranked' });
      await fixture.whenStable();
      stream.next({
        name: 'comment.changed',
        id: 'e3',
        key: 'acme/COW-2',
        version: 2,
        kind: 'commented',
      });
      await fixture.whenStable();
      stream.next({ name: 'inbox.changed', unread: 1 });
      await fixture.whenStable();

      expect(invoke).toHaveBeenCalledTimes(4);
    });

    it.each<[string, StreamEvent, number]>([
      [
        'an act that names the person in any of their tenants, a tenant left among them',
        { name: 'membership.changed', id: 'e1', tenant: 'globex', personId: 'p1' },
        1,
      ],
      [
        'a project restricted or opened in any of their tenants',
        { name: 'membership.changed', id: 'e1', tenant: 'globex', projectId: 'j1' },
        1,
      ],
      [
        "somebody else's membership",
        { name: 'membership.changed', id: 'e1', tenant: 'globex', personId: 'p2' },
        0,
      ],
      [
        'a group mapping',
        { name: 'membership.changed', id: 'e1', tenant: 'globex', mappingId: 'm1' },
        0,
      ],
      ['a resync', { name: 'resync' }, 1],
      ["the fallback's poll", { name: 'poll' }, 1],
    ])('follows what every person-level page follows: %s', async (_what, event, loads) => {
      configure(() => first);
      const { fixture } = await renderList('next');
      invoke.mockClear();

      stream.next(event);
      await fixture.whenStable();

      expect(invoke).toHaveBeenCalledTimes(loads);
    });

    it('loads once more after a burst of events of several tenants, not once for each', async () => {
      let release: () => void = () => undefined;
      configure(() => first);
      const { fixture } = await renderList('next');
      invoke.mockClear();
      invoke.mockImplementationOnce(
        () => new Promise((resolve) => (release = () => resolve(first))),
      );

      for (const key of ['acme/COW-2', 'globex/OPS-1', 'initech/HR-4', 'acme/COW-3']) {
        stream.next({ name: 'ticket.changed', id: key, key, version: 2, kind: 'edited' });
      }
      await vi.waitFor(() => expect(invoke).toHaveBeenCalledOnce());
      release();
      await fixture.whenStable();

      expect(invoke).toHaveBeenCalledTimes(2);
    });

    it('loads one page more on request', async () => {
      configure((_fn, cursor) =>
        cursor === 'c1'
          ? {
              items: [
                {
                  team: { slug: 'globex', name: 'Globex' },
                  tenant: { slug: 'globex', name: 'Globex' },
                  ticket: ticket('globex/OPS-7'),
                  place: 3,
                },
              ],
              next_cursor: null,
            }
          : { ...first, next_cursor: 'c1' },
      );
      const { fixture, page } = await renderList('assigned');
      expect(byTestId(page, 'assigned-count')?.textContent?.trim()).toBe(
        '2 open tickets shown, more to load',
      );

      (byTestId(page, 'load-more') as HTMLButtonElement).click();
      await fixture.whenStable();

      expect(byTestId(page, 'assigned-globex/OPS-7')).not.toBeNull();
      expect(byTestId(page, 'load-more')).toBeNull();
    });

    it('says when nothing is there', async () => {
      configure(() => ({ items: [], next_cursor: null }));

      const { page } = await renderList('next');

      expect(byTestId(page, 'next-empty')?.textContent).toContain(
        'Nothing is open for you, and nothing is unassigned.',
      );
    });
  });

  describe('Decisions', () => {
    const list: DecisionList = {
      items: [decision('q1', 'acme/COW-1', ada), decision('q2', 'acme/COW-3', null)],
      next_cursor: null,
    };

    it('lists the open decisions beside their tenant and ticket, saying whom each waits for', async () => {
      configure((fn) => (fn === listMyDecisions ? list : undefined));

      const { page } = await render(Decisions);

      const rows = [...page.querySelectorAll('.row')];
      expect(rows.map((row) => row.getAttribute('data-testid'))).toEqual([
        'decision-q1',
        'decision-q2',
      ]);
      expect(rows[0].querySelector('[data-testid="asked-of"]')?.textContent).toContain(
        'asked of you',
      );
      expect(rows[1].querySelector('[data-testid="asked-of"]')?.textContent).toContain(
        'open in the team',
      );
      expect(rows[0].querySelector('a')?.getAttribute('href')).toBe('/t/acme/tickets/COW-1');
      expect(rows[0].querySelector('[data-testid="tenant"]')?.textContent?.trim()).toBe(
        'Acme Corp',
      );
    });

    it('loads again when a question changes, in any of the tenants', async () => {
      configure(() => list);
      const { fixture } = await render(Decisions);
      invoke.mockClear();

      stream.next({
        name: 'question.changed',
        id: '',
        key: 'globex/OPS-1',
        version: 1,
        kind: 'asked',
      });
      await fixture.whenStable();

      expect(invoke).toHaveBeenCalledOnce();
    });

    it('loads again on an import, whose questions come without an event of their own, and not on a sort', async () => {
      configure(() => list);
      const { fixture } = await render(Decisions);
      invoke.mockClear();

      stream.next({ name: 'project.changed', id: 'e1', key: 'globex/OPS', kind: 'ranked' });
      await fixture.whenStable();
      expect(invoke).not.toHaveBeenCalled();

      stream.next({ name: 'project.changed', id: 'e2', key: 'globex/OPS', kind: 'imported' });
      await fixture.whenStable();
      expect(invoke).toHaveBeenCalledOnce();
    });
  });
});
