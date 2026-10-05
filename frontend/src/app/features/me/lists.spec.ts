import { Component, signal, Type } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideRouter } from '@angular/router';
import { MessageService } from 'primeng/api';
import { Subject } from 'rxjs';
import { Api } from '../../api/api';
import { listMyAssigned, listMyDecisions } from '../../api/functions';
import { Decision, DecisionList, Me, MyTicketList, Ticket } from '../../api/models';
import { EventStreamService, StreamEvent } from '../../core/event-stream.service';
import { SessionService } from '../../core/session.service';
import { Assigned } from './assigned';
import { askedOf, Decisions } from './decisions';
import { shortKey, ticketRoute } from './person-list';

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
    urgency: 'later',
    urgency_derived: 'later',
    urgency_override: null,
    urgency_rule: 'v2:default',
    opened_at: '2026-10-01T09:00:00Z',
    created_at: '2026-10-01T09:00:00Z',
    updated_at: '2026-10-03T11:55:00Z',
    decided_at: null,
    done_at: null,
    done_from: null,
    done_by_hand: false,
    open_prerequisites: 0,
    version: 1,
    ...overrides,
  };
}

function decision(id: string, key: string, askedOfPerson: typeof ada | null): Decision {
  return {
    tenant: { slug: key.split('/')[0], name: 'Acme Corp' },
    ticket: { key, title: `Title of ${key}`, state: 'analysed' },
    question: {
      id,
      number: 1,
      question: 'Which way?',
      options: '',
      recommendation: '',
      answer: null,
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
    expect(askedOf(decision('q1', 'acme/COW-1', null), 'p1')).toBe('open in the tenant');
    expect(askedOf(decision('q1', 'acme/COW-1', null), undefined)).toBe('open in the tenant');
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
        { provide: Api, useValue: { invoke } },
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

  describe('Assigned', () => {
    const first: MyTicketList = {
      items: [
        { tenant: { slug: 'acme', name: 'Acme Corp' }, ticket: ticket('acme/COW-2') },
        { tenant: { slug: 'globex', name: 'Globex' }, ticket: ticket('globex/OPS-1') },
      ],
      next_cursor: null,
    };

    it('lists the tickets assigned to the person in the order given, each beside its tenant (docs/adr/0018 D3)', async () => {
      configure(() => first);

      const { page } = await render(Assigned);

      const rows = [...page.querySelectorAll('.row')];
      expect(rows.map((row) => row.getAttribute('data-testid'))).toEqual([
        'assigned-acme/COW-2',
        'assigned-globex/OPS-1',
      ]);
      expect(
        rows.map((row) => row.querySelector('[data-testid="tenant"]')?.textContent?.trim()),
      ).toEqual(['Acme Corp', 'Globex']);
      expect(rows[1].querySelector('a')?.getAttribute('href')).toBe('/t/globex/tickets/OPS-1');
      expect(byTestId(page, 'assigned-count')?.textContent?.trim()).toBe('2 open tickets');
      expect(invoke).toHaveBeenCalledWith(listMyAssigned, { cursor: undefined, limit: 50 });
    });

    it('loads again on an inbox change and a ticket change, not on a comment', async () => {
      configure(() => first);
      const { fixture } = await render(Assigned);
      invoke.mockClear();

      stream.next({ name: 'inbox.changed', unread: 1 });
      await fixture.whenStable();
      stream.next({
        name: 'ticket.changed',
        id: 'e',
        key: 'acme/COW-2',
        version: 2,
        kind: 'assigned',
      });
      await fixture.whenStable();
      stream.next({
        name: 'comment.changed',
        id: 'e',
        key: 'acme/COW-2',
        version: 2,
        kind: 'commented',
      });
      await fixture.whenStable();

      expect(invoke).toHaveBeenCalledTimes(2);
    });

    it('loads one page more on request', async () => {
      configure((_fn, cursor) =>
        cursor === 'c1'
          ? {
              items: [
                { tenant: { slug: 'globex', name: 'Globex' }, ticket: ticket('globex/OPS-7') },
              ],
              next_cursor: null,
            }
          : { ...first, next_cursor: 'c1' },
      );
      const { fixture, page } = await render(Assigned);
      expect(byTestId(page, 'assigned-count')?.textContent?.trim()).toBe(
        '2 open tickets shown, more to load',
      );

      (byTestId(page, 'load-more') as HTMLButtonElement).click();
      await fixture.whenStable();

      expect(byTestId(page, 'assigned-globex/OPS-7')).not.toBeNull();
      expect(byTestId(page, 'load-more')).toBeNull();
    });

    it('says when nothing is assigned', async () => {
      configure(() => ({ items: [], next_cursor: null }));

      const { page } = await render(Assigned);

      expect(byTestId(page, 'assigned-empty')).not.toBeNull();
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
        'open in the tenant',
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
  });
});
