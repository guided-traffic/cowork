import { HttpErrorResponse } from '@angular/common/http';
import { signal, WritableSignal } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { provideRouter } from '@angular/router';
import { MessageService } from 'primeng/api';
import { Membership, Problem, Project, Ticket, TicketState } from '../../api/models';
import { EntityCache } from '../../core/entity-cache';
import { ProjectsService } from '../../core/projects.service';
import { SessionService } from '../../core/session.service';
import { TicketPage, TicketsService } from '../../core/tickets.service';
import { Clock } from '../../shared/time';
import { openStates, pageSize, summarise, TenantOverview } from './overview';

const now = Date.parse('2026-10-03T12:00:00Z');
const ada = { id: 'p1', display_name: 'Ada Lovelace', username: 'local:ada' };

function ticket(project: string, number: number, overrides: Partial<Ticket> = {}): Ticket {
  return {
    id: `t-${project}-${number}`,
    key: `acme/${project}-${number}`,
    number,
    project,
    title: `Ticket ${project}-${number}`,
    body: '',
    type: 'task',
    state: 'filed',
    severity: 'medium',
    security: 'none',
    confidential: false,
    assignee: null,
    reporter: ada,
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
    urgency_rule: 'v1:default',
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

function project(key: string, name: string, description = ''): Project {
  return {
    id: `id-${key}`,
    key,
    name,
    description,
    restricted: false,
    wip_limits: {},
    version: 1,
    created_at: '2026-10-01T09:00:00Z',
    updated_at: '2026-10-01T09:00:00Z',
  };
}

let counter = 0;
/** A ticket of the project COW in the given state, numbered on from the last. */
const withState = (state: TicketState) => ticket('COW', ++counter, { state });

describe('summarise', () => {
  const cow = { key: 'COW', name: 'Cowork', description: 'The tool itself' };
  const ops = { key: 'OPS', name: 'Operations', description: '' };

  it('counts the open tickets of each project and keeps the order of the projects', () => {
    const tickets = [ticket('OPS', 1), ticket('COW', 1), ticket('COW', 2)];

    expect(summarise([cow, ops], tickets).map(({ key, open }) => [key, open])).toEqual([
      ['COW', 2],
      ['OPS', 1],
    ]);
  });

  it('carries the name and the description of the project', () => {
    expect(summarise([cow], [])[0]).toMatchObject({
      key: 'COW',
      name: 'Cowork',
      description: 'The tool itself',
    });
  });

  it('counts zero for a project without tickets', () => {
    expect(summarise([cow, ops], [ticket('COW', 1)])[1]).toEqual({
      key: 'OPS',
      name: 'Operations',
      description: '',
      open: 0,
      byState: [],
    });
  });

  it('lists the states in the order of docs/adr/0009 D1 whatever the order of the tickets', () => {
    const tickets = [
      withState('blocked'),
      withState('review'),
      withState('in-progress'),
      withState('filed'),
      withState('decided'),
      withState('analysed'),
    ];

    expect(summarise([cow], tickets)[0].byState.map((part) => part.state)).toEqual(openStates);
    expect(openStates).toEqual([
      'filed',
      'analysed',
      'decided',
      'in-progress',
      'review',
      'blocked',
    ]);
  });

  it('counts the tickets of each state and drops the states without tickets', () => {
    const tickets = [
      withState('decided'),
      withState('filed'),
      withState('decided'),
      withState('decided'),
    ];

    expect(summarise([cow], tickets)[0].byState).toEqual([
      { state: 'filed', count: 1 },
      { state: 'decided', count: 3 },
    ]);
  });

  it('leaves the tickets of other projects out', () => {
    const tickets = [ticket('OPS', 1), ticket('OPS', 2), ticket('COW', 1)];

    const [summary] = summarise([cow], tickets);

    expect(summary.open).toBe(1);
    expect(summary.byState).toEqual([{ state: 'filed', count: 1 }]);
  });

  it('has nothing to say without projects', () => {
    expect(summarise([], [ticket('COW', 1)])).toEqual([]);
  });
});

describe('TenantOverview', () => {
  let tenant: WritableSignal<string | null>;
  let membership: WritableSignal<Membership | undefined>;
  let projects: {
    list: WritableSignal<Project[]>;
    projects: { isLoading: WritableSignal<boolean> };
  };
  let cache: EntityCache<Ticket>;
  let open: {
    value: WritableSignal<TicketPage | undefined>;
    hasValue: () => boolean;
    error: WritableSignal<unknown>;
  };
  let listParams: () => unknown;

  beforeEach(() => {
    tenant = signal<string | null>('acme');
    membership = signal<Membership | undefined>({
      role: 'admin',
      tenant: { name: 'Acme Corp', slug: 'acme' },
      origins: [{ source: 'grant', role: 'admin' }],
    });
    projects = { list: signal<Project[]>([]), projects: { isLoading: signal(false) } };
    cache = new EntityCache<Ticket>();
    const value = signal<TicketPage | undefined>(undefined);
    open = { value, hasValue: () => value() !== undefined, error: signal<unknown>(undefined) };
    TestBed.configureTestingModule({
      providers: [
        provideRouter([]),
        MessageService,
        { provide: SessionService, useValue: { tenant, membership } },
        { provide: ProjectsService, useValue: projects },
        {
          provide: TicketsService,
          useValue: {
            cache,
            tenantTickets: (params: () => unknown) => {
              listParams = params;
              return open;
            },
          },
        },
        { provide: Clock, useValue: { now: signal(now) } },
      ],
    });
  });

  /** Puts the tickets into the cache and shows them as the page of open tickets the server counted. */
  function loadPage(tickets: Ticket[], total: number | undefined) {
    for (const each of tickets) {
      cache.put(each.key, each);
    }
    open.value.set({ keys: tickets.map((each) => each.key), total, nextCursor: null });
  }

  /** The tickets are all the open ones there are. */
  function load(...tickets: Ticket[]) {
    loadPage(tickets, tickets.length);
  }

  async function render() {
    const fixture = TestBed.createComponent(TenantOverview);
    await fixture.whenStable();
    return { fixture, page: fixture.nativeElement as HTMLElement };
  }

  const text = (page: HTMLElement, selector: string) =>
    page.querySelector(selector)?.textContent?.replace(/\s+/g, ' ').trim();

  describe('loading', () => {
    it('asks for the first numbered page of open tickets of the tenant of the page', async () => {
      await render();

      expect(listParams()).toEqual({ tenant: 'acme', page: 1, per_page: 100 });
      expect(pageSize).toBe(100);
    });

    it('asks for nothing outside a tenant', async () => {
      tenant.set(null);

      await render();

      expect(listParams()).toBeUndefined();
    });
  });

  describe('the heading', () => {
    it('shows the name of the tenant', async () => {
      const { page } = await render();

      expect(text(page, 'h1')).toBe('Acme Corp');
    });

    it('shows the slug while the membership is not known yet', async () => {
      membership.set(undefined);

      const { page } = await render();

      expect(text(page, 'h1')).toBe('acme');
    });

    it('counts the open tickets and the projects once they are loaded', async () => {
      projects.list.set([project('COW', 'Cowork'), project('OPS', 'Operations')]);
      load(ticket('COW', 1), ticket('COW', 2), ticket('OPS', 1));

      const { page } = await render();

      expect(text(page, '[data-testid="open-count"]')).toBe('3 open tickets across 2 projects');
    });

    it('speaks of one open ticket across one project in the singular', async () => {
      projects.list.set([project('COW', 'Cowork')]);
      load(ticket('COW', 1));

      const { page } = await render();

      expect(text(page, '[data-testid="open-count"]')).toBe('1 open ticket across 1 project');
    });

    it('speaks of no open ticket across no project in the plural', async () => {
      load();

      const { page } = await render();

      expect(text(page, '[data-testid="open-count"]')).toBe('0 open tickets across 0 projects');
    });

    it('counts what the server counted, not the tickets of the page', async () => {
      projects.list.set([project('COW', 'Cowork')]);
      loadPage([ticket('COW', 1), ticket('COW', 2)], 130);

      const { page } = await render();

      expect(text(page, '[data-testid="open-count"]')).toBe('130 open tickets across 1 project');
    });

    it('counts none when the answer carries no total', async () => {
      loadPage([ticket('COW', 1)], undefined);

      const { page } = await render();

      expect(text(page, '[data-testid="open-count"]')).toBe('0 open tickets across 0 projects');
    });

    it('says nothing about counts before the tickets are loaded', async () => {
      projects.list.set([project('COW', 'Cowork')]);

      const { page } = await render();

      expect(page.querySelector('[data-testid="open-count"]')).toBeNull();
    });
  });

  describe('a page that holds fewer tickets than are open', () => {
    it('says that the cards count the newest tickets only', async () => {
      projects.list.set([project('COW', 'Cowork')]);
      loadPage([ticket('COW', 1), ticket('COW', 2)], 130);

      const { page } = await render();

      expect(text(page, '[data-testid="partial"]')).toBe(
        'The project cards count the 2 newest of them; the dashboard will count all.',
      );
    });

    it('says nothing of it when the page holds every open ticket', async () => {
      load(ticket('COW', 1), ticket('COW', 2));

      const { page } = await render();

      expect(page.querySelector('[data-testid="partial"]')).toBeNull();
    });

    it('says nothing of it before the tickets are loaded', async () => {
      const { page } = await render();

      expect(page.querySelector('[data-testid="partial"]')).toBeNull();
    });
  });

  describe('a list that could not be loaded', () => {
    function failed(status: number, title: string, detail: string) {
      const body: Problem = { type: 'about:blank', title, status, detail, code: 'internal' };
      return new HttpErrorResponse({ status, statusText: title, error: body });
    }

    it('says why, with the detail of the problem', async () => {
      open.error.set(failed(503, 'The service is not ready', 'The database is starting.'));

      const { page } = await render();

      expect(text(page, '[data-testid="overview-failed"]')).toBe(
        'The open tickets could not be loaded: The database is starting.',
      );
    });

    it('says why with the title when the problem has no detail', async () => {
      open.error.set(new HttpErrorResponse({ status: 0, statusText: 'Unknown Error' }));

      const { page } = await render();

      expect(text(page, '[data-testid="overview-failed"]')).toBe(
        'The open tickets could not be loaded: The connection failed; cowork tries again on its own.',
      );
    });

    it('says nothing when the list loads', async () => {
      load(ticket('COW', 1));

      const { page } = await render();

      expect(page.querySelector('[data-testid="overview-failed"]')).toBeNull();
    });
  });

  describe('the projects', () => {
    it('shows a card per project with its count, its name and its description', async () => {
      projects.list.set([
        project('COW', 'Cowork', 'The tool itself'),
        project('OPS', 'Operations'),
      ]);
      load(ticket('COW', 1), ticket('COW', 2));

      const { page } = await render();

      const cow = page.querySelector('[data-testid="project-COW"]');
      expect(cow?.querySelector('.key')?.textContent).toBe('COW');
      expect(cow?.querySelector('.name')?.textContent).toBe('Cowork');
      expect(cow?.querySelector('.count')?.textContent).toBe('2');
      expect(cow?.querySelector('.description')?.textContent).toBe('The tool itself');
      const ops = page.querySelector('[data-testid="project-OPS"]');
      expect(ops?.querySelector('.count')?.textContent).toBe('0');
      expect(ops?.querySelector('.description')).toBeNull();
    });

    it('links each card to the backlog of its project', async () => {
      projects.list.set([project('COW', 'Cowork')]);

      const { page } = await render();

      expect(page.querySelector('[data-testid="project-COW"]')?.getAttribute('href')).toBe(
        '/t/acme/p/COW/backlog',
      );
    });

    it('shows each state with its count in the order of the states', async () => {
      projects.list.set([project('COW', 'Cowork')]);
      load(
        ticket('COW', 1, { state: 'blocked' }),
        ticket('COW', 2, { state: 'filed' }),
        ticket('COW', 3, { state: 'blocked' }),
      );

      const { page } = await render();

      const legend = [...page.querySelectorAll('[data-testid="project-COW"] .legend > span')].map(
        (part) => part.textContent?.replace(/\s+/g, ' ').trim(),
      );
      expect(legend).toEqual(['filed 1', 'blocked 2']);
    });

    it('sizes the segments of the bar by the share of each state and colours them by state', async () => {
      projects.list.set([project('COW', 'Cowork')]);
      load(
        ticket('COW', 1, { state: 'filed' }),
        ticket('COW', 2, { state: 'decided' }),
        ticket('COW', 3, { state: 'decided' }),
        ticket('COW', 4, { state: 'decided' }),
      );

      const { page } = await render();

      const segments = [
        ...page.querySelectorAll<HTMLElement>('[data-testid="project-COW"] .segment'),
      ];
      expect(segments.map((segment) => segment.style.width)).toEqual(['25%', '75%']);
      expect(segments.map((segment) => segment.style.background)).toEqual([
        'var(--p-state-filed)',
        'var(--p-state-decided)',
      ]);
    });

    it('says nothing is open for a project without open tickets', async () => {
      projects.list.set([project('COW', 'Cowork')]);
      load();

      const { page } = await render();

      expect(text(page, '[data-testid="project-COW"] .legend')).toBe('Nothing open');
      expect(page.querySelector('[data-testid="project-COW"] .segment')).toBeNull();
    });

    it('shows skeletons while the projects load', async () => {
      projects.projects.isLoading.set(true);

      const { page } = await render();

      expect(page.querySelectorAll('.projects p-skeleton')).toHaveLength(3);
      expect(page.querySelector('.projects .muted')).toBeNull();
    });

    it('says the tenant has no projects yet once they are loaded', async () => {
      const { page } = await render();

      expect(text(page, '.projects')).toBe('This tenant has no projects yet.');
      expect(page.querySelector('.projects p-skeleton')).toBeNull();
    });

    it('follows the projects when they arrive', async () => {
      const { fixture, page } = await render();

      projects.list.set([project('COW', 'Cowork')]);
      await fixture.whenStable();

      expect(page.querySelector('[data-testid="project-COW"]')).not.toBeNull();
    });
  });

  describe('the recently updated tickets', () => {
    const day = (n: number) => `2026-09-${String(10 + n).padStart(2, '0')}T12:00:00Z`;

    it('shows the most recently updated first and at most eight', async () => {
      const tickets = Array.from({ length: 10 }, (_, index) =>
        ticket('COW', index + 1, { updated_at: day(index + 1) }),
      );
      load(...tickets);

      const { page } = await render();

      const shown = [...page.querySelectorAll('.recent [data-testid^="recent-"]')].map((row) =>
        row.getAttribute('data-testid'),
      );
      expect(shown).toEqual([10, 9, 8, 7, 6, 5, 4, 3].map((number) => `recent-acme/COW-${number}`));
    });

    it('shows what each ticket is: type, key, title, state and age', async () => {
      load(ticket('COW', 7, { title: 'The board flickers', type: 'bug', state: 'blocked' }));

      const { page } = await render();

      const row = page.querySelector('[data-testid="recent-acme/COW-7"]');
      expect(row?.querySelector('app-type')?.getAttribute('title')).toContain('bug');
      expect(row?.querySelector('.ticket-key')?.textContent).toBe('COW-7');
      expect(row?.querySelector('.ticket-title')?.textContent).toBe('The board flickers');
      expect(row?.querySelector('app-state [data-state]')?.getAttribute('data-state')).toBe(
        'blocked',
      );
      expect(row?.querySelector('.when')?.textContent).toBe('5 minutes ago');
    });

    it('links each ticket to its page under the tenant', async () => {
      load(ticket('COW', 7));

      const { page } = await render();

      expect(page.querySelector('[data-testid="recent-acme/COW-7"]')?.getAttribute('href')).toBe(
        '/t/acme/tickets/COW-7',
      );
    });

    it('shows a skeleton until the tickets are loaded', async () => {
      const { page } = await render();

      expect(page.querySelectorAll('.recent p-skeleton')).toHaveLength(1);
      expect(page.querySelector('.recent .empty')).toBeNull();
    });

    it('says no ticket is open when the tenant has none', async () => {
      load();

      const { page } = await render();

      expect(text(page, '.recent .empty')).toBe('No open tickets.');
      expect(page.querySelector('.recent p-skeleton')).toBeNull();
    });

    it('reads the tickets through the cache, so a refetch shows without a new list', async () => {
      load(ticket('COW', 1, { title: 'Before' }));
      const { fixture, page } = await render();

      cache.put('acme/COW-1', ticket('COW', 1, { title: 'After', version: 2 }));
      await fixture.whenStable();

      expect(text(page, '[data-testid="recent-acme/COW-1"] .ticket-title')).toBe('After');
    });

    it('leaves out a ticket that the cache no longer holds', async () => {
      projects.list.set([project('COW', 'Cowork'), project('OPS', 'Operations')]);
      load(ticket('COW', 1), ticket('COW', 2), ticket('OPS', 1));
      const { fixture, page } = await render();
      expect(text(page, '[data-testid="open-count"]')).toBe('3 open tickets across 2 projects');

      cache.delete('acme/COW-1');
      await fixture.whenStable();

      expect(page.querySelector('[data-testid="recent-acme/COW-1"]')).toBeNull();
      expect(page.querySelector('[data-testid="recent-acme/COW-2"]')).not.toBeNull();
      expect(page.querySelector('[data-testid="project-COW"] .count')?.textContent).toBe('1');
      // The headline is the server's count; the page now holds one ticket fewer than that.
      expect(text(page, '[data-testid="open-count"]')).toBe('3 open tickets across 2 projects');
      expect(text(page, '[data-testid="partial"]')).toContain('count the 2 newest');
    });
  });
});
