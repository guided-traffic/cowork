import { CdkDrag, CdkDragDrop, CdkDropList } from '@angular/cdk/drag-drop';
import { HttpErrorResponse } from '@angular/common/http';
import { signal, WritableSignal } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { By } from '@angular/platform-browser';
import { provideRouter, Router, UrlTree } from '@angular/router';
import { MenuItem, MessageService } from 'primeng/api';
import { Menu } from 'primeng/menu';
import { Select } from 'primeng/select';
import { ToggleSwitch } from 'primeng/toggleswitch';
import { Tooltip } from 'primeng/tooltip';
import type { Mock, MockInstance } from 'vitest';
import {
  Horizon,
  Problem,
  Project,
  SavedFilter,
  Ticket,
  TicketHead,
  TicketState,
} from '../../api/models';
import { EntityCache } from '../../core/entity-cache';
import { ImportsService } from '../../core/imports.service';
import { MembersService } from '../../core/members.service';
import { ProjectsService } from '../../core/projects.service';
import { SavedFiltersService } from '../../core/saved-filters.service';
import { SessionService } from '../../core/session.service';
import { TenantService } from '../../core/tenant.service';
import { StaleWrite, TicketActions } from '../../core/ticket-actions.service';
import { ProjectTicketPagesParams, TicketPage, TicketsService } from '../../core/tickets.service';
import { Clock, dateTime } from '../../shared/time';
import { Backlog } from './backlog';
import { Group } from './backlog-model';
import { ProjectHeader } from './project-header';
import { backlogLeftOut } from './saved-filter-model';
import { SavedFilters } from './saved-filters';

const now = Date.parse('2026-10-03T12:00:00Z');
const ada = { id: 'p1', display_name: 'Ada Lovelace', username: 'local:ada' };
const sam = { id: 'p2', display_name: 'Sam Rivera', username: 'local:sam' };

/** The head of a parent the reader reads, by its canonical key (docs/adr/0005 D3). */
function headOf(key: string, overrides: Partial<TicketHead> = {}): TicketHead {
  return {
    team: {
      slug: key.slice(0, key.indexOf('/')),
      name: key.startsWith('acme/') ? 'Acme' : 'Globex',
    },
    key,
    title: `Ticket ${key}`,
    type: 'task',
    state: 'filed',
    placeholder: false,
    readable: true,
    ...overrides,
  };
}

/** A ticket of the backlog; a parent comes with its head, as the API sends it. */
function ticket(number: number, overrides: Partial<Ticket> = {}): Ticket {
  const parent = overrides.parent ?? null;
  return {
    parent_head: parent === null ? null : headOf(parent),
    id: `t-${number}`,
    key: `acme/COW-${number}`,
    number,
    project: 'COW',
    title: `Ticket COW-${number}`,
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
    parent,
    effort: 'M',
    progress: 0,
    progress_refinement: 0,
    progress_review: 0,
    progress_derived: false,
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
  } as Ticket;
}

const cowork: Project = {
  id: 'id-COW',
  key: 'COW',
  name: 'Cowork',
  description: 'The tool itself',
  restricted: false,
  wip_limits: {},
  version: 1,
  created_at: '2026-10-01T09:00:00Z',
  updated_at: '2026-10-01T09:00:00Z',
};

/** What a list of the service is as the page sees it, made of signals the test sets. */
interface FakeList {
  value: WritableSignal<TicketPage | undefined>;
  hasValue: () => boolean;
  isLoading: WritableSignal<boolean>;
  error: WritableSignal<unknown>;
  reload: Mock<() => boolean>;
}

function fakeList(): FakeList {
  const value = signal<TicketPage | undefined>(undefined);
  return {
    value,
    hasValue: () => value() !== undefined,
    isLoading: signal(false),
    error: signal<unknown>(undefined),
    reload: vi.fn<() => boolean>(() => true),
  };
}

/** A promise that the test answers when it is ready to. */
function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((done) => (resolve = done));
  return { promise, resolve };
}

/** One turn of the event loop, in which what a click or a drop started runs on. */
const tick = () => new Promise((resolve) => setTimeout(resolve));

const refusal = (status: number, code: Problem['code'], title: string, detail?: string) => {
  const body: Problem = { type: 'about:blank', title, status, code, ...(detail ? { detail } : {}) };
  return new HttpErrorResponse({ status, statusText: title, error: body });
};

describe('Backlog', () => {
  let tenant: WritableSignal<string | null>;
  let projects: WritableSignal<Project[]>;
  let cache: EntityCache<Ticket>;
  /** The list of the open tickets, and the one of the closed ones: the page asks for them in this order. */
  let open: FakeList;
  let closed: FakeList;
  let openParams: () => ProjectTicketPagesParams | undefined;
  let closedParams: () => ProjectTicketPagesParams | undefined;
  let rank: Mock<TicketActions['rank']>;
  let setHorizon: Mock<TicketActions['setHorizon']>;
  let sortByScore: Mock<TicketActions['sortByScore']>;
  let membership: WritableSignal<{ role: string } | undefined>;
  let navigate: MockInstance<Router['navigate']>;
  let navigateByUrl: MockInstance<Router['navigateByUrl']>;
  let toast: MockInstance<MessageService['add']>;
  let warn: MockInstance<typeof console.warn>;
  /** How many lists the pages of this test have asked for. */
  let created: number;
  let savedFilters: WritableSignal<SavedFilter[]>;

  beforeEach(() => {
    created = 0;
    savedFilters = signal<SavedFilter[]>([]);
    warn = vi.spyOn(console, 'warn');
    tenant = signal<string | null>('acme');
    projects = signal<Project[]>([cowork]);
    membership = signal<{ role: string } | undefined>({ role: 'member' });
    sortByScore = vi.fn<TicketActions['sortByScore']>(async () => 2);
    cache = new EntityCache<Ticket>();
    open = fakeList();
    closed = fakeList();
    // The writes put their answers into the cache, as the real ones do; a move raises the version.
    rank = vi.fn<TicketActions['rank']>(async (key) => {
      const held = cache.value(key) as Ticket;
      const answer = { ...held, version: held.version + 1 } as Ticket;
      cache.put(key, answer);
      return answer;
    });
    // later clears the horizon set, as the API does (docs/adr/0010 D3).
    setHorizon = vi.fn<TicketActions['setHorizon']>(async (key, value) => {
      const held = cache.value(key) as Ticket;
      const answer = {
        ...held,
        horizon: value,
        horizon_set: value === 'later' ? null : { value, at: 'then', reason: 'because' },
        version: held.version + 1,
      } as Ticket;
      cache.put(key, answer);
      return answer;
    });
    TestBed.configureTestingModule({
      providers: [
        provideRouter([]),
        MessageService,
        {
          provide: TicketActions,
          useValue: { create: vi.fn(), rank, setHorizon, sortByScore },
        },
        { provide: MembersService, useValue: { list: signal([]) } },
        // The header's export and its way to the import (docs/adr/0051).
        { provide: ImportsService, useValue: {} },
        { provide: TenantService, useValue: { isAdmin: signal(false), canWrite: signal(false) } },
        {
          provide: SessionService,
          useValue: { tenant, membership, person: signal({ id: 'p-ada' }) },
        },
        {
          provide: SavedFiltersService,
          useValue: {
            list: savedFilters,
            reload: vi.fn(),
            create: vi.fn(),
            update: vi.fn(),
            remove: vi.fn(),
          },
        },
        {
          provide: ProjectsService,
          useValue: { byKey: (key: string) => projects().find((p) => p.key === key) },
        },
        {
          provide: TicketsService,
          useValue: {
            cache,
            // Each page asks for its open list first and its closed list second.
            projectTicketPages: (params: () => ProjectTicketPagesParams | undefined) => {
              if (created++ % 2 === 0) {
                openParams = params;
                return open;
              }
              closedParams = params;
              return closed;
            },
          },
        },
        { provide: Clock, useValue: { now: signal(now) } },
      ],
    });
    const router = TestBed.inject(Router);
    navigate = vi.spyOn(router, 'navigate').mockResolvedValue(true);
    navigateByUrl = vi.spyOn(router, 'navigateByUrl').mockResolvedValue(true);
    toast = vi.spyOn(TestBed.inject(MessageService), 'add');
  });

  afterEach(() => {
    // The dialog for a new ticket is part of the page; it must not warn (NG01354).
    const warnings = warn.mock.calls.filter((call) => String(call[0]).includes('NG01354'));
    warn.mockRestore();
    expect(warnings).toEqual([]);
  });

  /**
   * Puts the tickets into the cache and shows their keys, in this rank, as the list the server
   * answered, with the version each had in the answer.
   */
  function load(tickets: Ticket[], nextCursor: string | null = null, list = open) {
    for (const each of tickets) {
      cache.put(each.key, each);
    }
    list.value.set({
      keys: tickets.map((each) => each.key),
      nextCursor,
      versions: new Map(tickets.map((each) => [each.key, each.version])),
    });
  }

  async function render(project = 'COW') {
    const fixture = TestBed.createComponent(Backlog);
    fixture.componentRef.setInput('project', project);
    await fixture.whenStable();
    return { fixture, page: fixture.nativeElement as HTMLElement };
  }

  /** Lets what a drop or a click started finish, and shows it. */
  async function settle(fixture: ComponentFixture<Backlog>) {
    for (let round = 0; round < 3; round++) {
      await new Promise((resolve) => setTimeout(resolve));
      fixture.detectChanges();
    }
    await fixture.whenStable();
  }

  const text = (page: HTMLElement, selector: string) =>
    page.querySelector(selector)?.textContent?.replace(/\s+/g, ' ').trim();

  const numberOf = (row: Element) => Number(row.getAttribute('data-testid')?.split('-').pop());

  /** The numbers of the tickets in the rows of a group, in the order shown. */
  const rowsIn = (page: HTMLElement, horizon: string) =>
    [...page.querySelectorAll(`[data-testid="group-${horizon}"] tr.row`)].map(numberOf);

  const groupsShown = (page: HTMLElement) =>
    [...page.querySelectorAll('tbody.group')].map((group) => group.getAttribute('data-horizon'));

  const rowOf = (page: HTMLElement, number: number) =>
    page.querySelector<HTMLElement>(`[data-testid="row-acme/COW-${number}"]`);

  function type(page: HTMLElement, value: string) {
    const input = page.querySelector<HTMLInputElement>('[data-testid="search"]');
    if (!input) {
      throw new Error('the search field is missing');
    }
    input.value = value;
    input.dispatchEvent(new Event('input'));
  }

  /** What choosing states in the filter, or clearing it (which gives null), tells its model. */
  const chooseStates = (fixture: ComponentFixture<Backlog>, states: TicketState[] | null) =>
    fixture.debugElement
      .query(By.css('[data-testid="state-filter"]'))
      .triggerEventHandler('ngModelChange', states);

  describe('the groups', () => {
    it('are a section of one table each, now, next and later, even while empty', async () => {
      load([]);

      const { page } = await render();

      expect(page.querySelectorAll('table')).toHaveLength(1);
      expect(groupsShown(page)).toEqual(['now', 'next', 'later']);
    });

    it('put each ticket into the group of its horizon, in the order of the list', async () => {
      load([
        ticket(1, { horizon: 'later' }),
        ticket(2, { horizon: 'now' }),
        ticket(3, { horizon: 'later' }),
        ticket(4, { horizon: 'next' }),
        ticket(5, { horizon: 'now' }),
      ]);

      const { page } = await render();

      expect(rowsIn(page, 'now')).toEqual([2, 5]);
      expect(rowsIn(page, 'next')).toEqual([4]);
      expect(rowsIn(page, 'later')).toEqual([1, 3]);
    });

    it('show release and icebox only while they hold a ticket, in the order now, release, next, later, icebox', async () => {
      load([
        ticket(1, { horizon: 'icebox' }),
        ticket(2, { horizon: 'later' }),
        ticket(3, { horizon: 'release' }),
        ticket(4, { horizon: 'now' }),
        ticket(5, { horizon: 'next' }),
      ]);

      const { page } = await render();

      expect(groupsShown(page)).toEqual(['now', 'release', 'next', 'later', 'icebox']);
    });

    it('leave out release when only icebox holds a ticket, and the other way round', async () => {
      load([ticket(1, { horizon: 'icebox' })]);
      const first = await render();
      expect(groupsShown(first.page)).toEqual(['now', 'next', 'later', 'icebox']);
      first.fixture.destroy();

      load([ticket(2, { horizon: 'release' })]);
      const second = await render();
      expect(groupsShown(second.page)).toEqual(['now', 'release', 'next', 'later']);
    });

    it('have a header row with the name, the count and the meaning as a tooltip', async () => {
      load([ticket(1, { horizon: 'now' }), ticket(2, { horizon: 'now' }), ticket(3)]);

      const { fixture, page } = await render();

      expect(text(page, '[data-testid="group-now"] .group-head .group-name')).toBe('now');
      expect(text(page, '[data-testid="count-now"]')).toBe('2');
      expect(text(page, '[data-testid="count-next"]')).toBe('0');
      expect(text(page, '[data-testid="count-later"]')).toBe('1');
      const tooltip = (horizon: string) =>
        fixture.debugElement
          .query(By.css(`[data-testid="group-${horizon}"] .group-name`))
          .injector.get(Tooltip)
          .content();
      expect(tooltip('now')).toBe(
        'Now: to be worked on now — maybe still to be refined, but it matters to the project now, or it is a low-hanging fruit',
      );
      expect(tooltip('later')).toBe(
        'Later: worth less at the moment — maybe some day, maybe never; kept so it is not forgotten',
      );
    });

    it('are named in the table as the horizons of the open tickets', async () => {
      const { page } = await render();

      expect(page.querySelector('[data-testid="backlog-table"]')?.getAttribute('aria-label')).toBe(
        'Open tickets, grouped by horizon',
      );
    });

    it('count the children among the rows of the group', async () => {
      load([ticket(1), ticket(2, { parent: 'acme/COW-1' })]);

      const { page } = await render();

      expect(text(page, '[data-testid="count-later"]')).toBe('2');
    });

    it('say that they have no ticket while they have none', async () => {
      load([ticket(1, { horizon: 'later' })]);

      const { page } = await render();

      expect(text(page, '[data-testid="empty-now"]')).toBe('No tickets');
      expect(text(page, '[data-testid="empty-next"]')).toBe('No tickets');
      expect(page.querySelector('[data-testid="empty-later"]')).toBeNull();
    });

    it('name the key and the title in the header row of each section, which is a header for the rows that follow', async () => {
      load([]);

      const { page } = await render();

      expect(
        page.querySelector('[data-testid="group-now"] .group-head th')?.getAttribute('scope'),
      ).toBe('rowgroup');
    });
  });

  describe('a row', () => {
    it('shows the key, the type, the title, the size, the state, the severity, the assignee, the progress and the age', async () => {
      load([
        ticket(1, {
          type: 'bug',
          title: 'The board flickers',
          state: 'blocked',
          severity: 'high',
          security: 'boundary',
          effort: 'L',
          progress: 60,
          block: { kind: 'external', from: 'in-progress', reason: 'The provider' },
          assignee: sam,
        }),
      ]);

      const { page } = await render();

      const row = rowOf(page, 1);
      expect(row?.querySelector('.col-key a')?.textContent).toBe('COW-1');
      expect(row?.querySelector('.col-key a')?.getAttribute('href')).toBe('/t/acme/tickets/COW-1');
      expect(row?.querySelector('app-type')?.getAttribute('title')).toContain('bug');
      expect(row?.querySelector('.col-title .text')?.textContent).toBe('The board flickers');
      expect(row?.querySelector('[data-security]')?.getAttribute('data-security')).toBe('boundary');
      expect(row?.querySelector('app-size')?.getAttribute('aria-label')).toBe('Effort L');
      expect(row?.querySelector('[data-state]')?.getAttribute('data-state')).toBe('blocked');
      expect(row?.querySelector('[data-severity]')?.getAttribute('data-severity')).toBe('high');
      expect(row?.querySelector('.col-assignee')?.textContent?.trim()).toBe('Sam Rivera');
      expect(row?.querySelector('.col-progress app-stage-bar')?.getAttribute('aria-label')).toBe(
        'Implementation 60%',
      );
      expect(row?.querySelector('.col-when')?.textContent).toBe('5 minutes ago');
    });

    it('has the columns Key, Title, Size, State, Severity, Assignee, Progress and Updated, and no Horizon, which the group says', async () => {
      load([]);

      const { page } = await render();

      const headers = [...page.querySelectorAll('thead th')].map((th) => th.textContent?.trim());
      expect(headers).toEqual([
        'Drag',
        'Key',
        'Title',
        'Size',
        'State',
        'Severity',
        'Assignee',
        'Progress',
        'Updated',
        'Move',
      ]);
    });

    it('shows the bar of the stage the ticket works on, and says where a parent has it from', async () => {
      load([
        ticket(1, { state: 'in-progress', progress: 60, progress_derived: true }),
        ticket(2, { state: 'analysed', progress_refinement: 40 }),
        ticket(3, { state: 'review', progress_review: 25 }),
        ticket(4, {
          state: 'blocked',
          progress_refinement: 75,
          block: { kind: 'human', from: 'filed', reason: 'Waiting' },
        }),
      ]);

      const { page } = await render();

      const bar = (number: number) =>
        rowOf(page, number)
          ?.querySelector('.col-progress app-stage-bar')
          ?.getAttribute('aria-label');
      expect(bar(1)).toBe('Implementation 60%, from its children');
      expect(bar(2)).toBe('Refinement 40%');
      expect(bar(3)).toBe('Review 25%');
      expect(bar(4)).toBe('Refinement 75%');
    });

    it('shows a dash for a decided ticket, which waits and has no stage of its own', async () => {
      load([ticket(1, { state: 'decided', progress: 30 })]);

      const { page } = await render();

      expect(rowOf(page, 1)?.querySelector('.col-progress app-stage-bar')).toBeNull();
      expect(text(page, '[data-testid="row-acme/COW-1"] .col-progress')).toBe('—');
    });

    it('shows a dash for a ticket nobody is assigned to and no security badge for none', async () => {
      load([ticket(1, { assignee: null, security: 'none' })]);

      const { page } = await render();

      expect(rowOf(page, 1)?.querySelector('.col-assignee')?.textContent?.trim()).toBe('—');
      expect(rowOf(page, 1)?.querySelector('[data-security]')).toBeNull();
    });

    it('shows what a refetch of a ticket put into the cache', async () => {
      load([ticket(1, { title: 'Before' })]);
      const { fixture, page } = await render();

      cache.put('acme/COW-1', ticket(1, { title: 'After', version: 2 }));
      await fixture.whenStable();

      expect(text(page, '[data-testid="row-acme/COW-1"] .text')).toBe('After');
    });

    it('moves to the group that a refetched ticket belongs to now', async () => {
      load([ticket(1, { horizon: 'later' }), ticket(2, { horizon: 'later' })]);
      const { fixture, page } = await render();

      cache.put('acme/COW-1', ticket(1, { horizon: 'now', version: 2 }));
      await fixture.whenStable();

      expect(rowsIn(page, 'now')).toEqual([1]);
      expect(rowsIn(page, 'later')).toEqual([2]);
    });

    it('leaves out a ticket that the cache no longer holds', async () => {
      load([ticket(1), ticket(2)]);
      const { fixture, page } = await render();

      cache.delete('acme/COW-1');
      await fixture.whenStable();

      expect(rowOf(page, 1)).toBeNull();
      expect(rowOf(page, 2)).not.toBeNull();
    });

    it('goes to the page of the ticket under the tenant when it is clicked', async () => {
      load([ticket(1), ticket(2)]);
      const { page } = await render();

      rowOf(page, 2)?.click();

      expect(navigate).toHaveBeenCalledExactlyOnceWith(['/t', 'acme', 'tickets', 'COW-2']);
    });

    it('opens nothing when the handle is clicked, which is a drag that did not happen', async () => {
      load([ticket(1)]);
      const { page } = await render();

      rowOf(page, 1)?.querySelector<HTMLElement>('[data-testid="grip"]')?.click();
      rowOf(page, 1)?.querySelector<HTMLElement>('.col-handle')?.click();

      expect(navigate).not.toHaveBeenCalled();
      expect(navigateByUrl).not.toHaveBeenCalled();
    });

    it('leaves the navigation to the link when the key itself is clicked', async () => {
      load([ticket(1)]);
      const { page } = await render();

      rowOf(page, 1)?.querySelector<HTMLElement>('.col-key a')?.click();

      expect(navigate).not.toHaveBeenCalled();
      expect(navigateByUrl).toHaveBeenCalledOnce();
      const target = navigateByUrl.mock.calls[0][0] as UrlTree;
      expect(TestBed.inject(Router).serializeUrl(target)).toBe('/t/acme/tickets/COW-1');
    });

    it('keeps its key on one line, and ends a key longer than the column in an ellipsis', async () => {
      load([ticket(1234)]);
      const { page } = await render();

      const cell = getComputedStyle(rowOf(page, 1234)!.querySelector('td.col-key')!);
      expect(cell.whiteSpace).toBe('nowrap');
      expect(cell.overflow).toBe('hidden');
      expect(cell.textOverflow).toBe('ellipsis');
    });
  });

  describe('a family', () => {
    it('shows a child under its parent where both are in the group, indented', async () => {
      load([
        ticket(1),
        ticket(2, { parent: 'acme/COW-3' }),
        ticket(3),
        ticket(4, { parent: 'acme/COW-3' }),
      ]);

      const { page } = await render();

      expect(rowsIn(page, 'later')).toEqual([1, 3, 2, 4]);
      const indent = (number: number) =>
        rowOf(page, number)?.querySelector<HTMLElement>('.cell-title')?.style.paddingLeft;
      expect(indent(3)).toBe('0rem');
      expect(indent(2)).toBe('1.75rem');
      expect(indent(4)).toBe('1.75rem');
      expect(rowOf(page, 2)?.querySelector('.chip')).toBeNull();
    });

    it('indents a grandchild twice', async () => {
      load([ticket(1), ticket(2, { parent: 'acme/COW-1' }), ticket(3, { parent: 'acme/COW-2' })]);

      const { page } = await render();

      expect(rowOf(page, 3)?.querySelector<HTMLElement>('.cell-title')?.style.paddingLeft).toBe(
        '3.5rem',
      );
    });

    it('names the parent in a small chip where the parent is in another group, and indents nothing', async () => {
      load([ticket(1, { horizon: 'now' }), ticket(2, { horizon: 'later', parent: 'acme/COW-1' })]);

      const { page } = await render();

      const row = rowOf(page, 2);
      expect(row?.querySelector<HTMLElement>('.cell-title')?.style.paddingLeft).toBe('0rem');
      const chip = row?.querySelector('.chip');
      expect(chip?.textContent?.trim()).toBe('COW-1');
      expect(chip?.getAttribute('href')).toBe('/t/acme/tickets/COW-1');
      expect(chip?.getAttribute('aria-label')).toBe('Parent COW-1');
    });

    it('names the parent that a filter leaves out as well', async () => {
      load([ticket(2, { parent: 'acme/COW-1' })]);

      const { page } = await render();

      expect(rowOf(page, 2)?.querySelector('.chip')?.textContent?.trim()).toBe('COW-1');
    });

    it('leaves the navigation to the chip, which is a link to the parent', async () => {
      load([ticket(2, { parent: 'acme/COW-1' })]);
      const { page } = await render();

      rowOf(page, 2)?.querySelector<HTMLElement>('.chip')?.click();

      expect(navigate).not.toHaveBeenCalled();
      const target = navigateByUrl.mock.calls[0][0] as UrlTree;
      expect(TestBed.inject(Router).serializeUrl(target)).toBe('/t/acme/tickets/COW-1');
    });

    it('has no chip for a ticket without a parent', async () => {
      load([ticket(1)]);

      const { page } = await render();

      expect(rowOf(page, 1)?.querySelector('.chip')).toBeNull();
    });

    it('says the title of the parent in the tooltip of its chip', async () => {
      load([ticket(2, { parent: 'acme/COW-1' })]);

      const { fixture } = await render();

      const chip = fixture.debugElement.query(By.css('[data-testid="parent-acme/COW-2"]'));
      expect(chip.injector.get(Tooltip).content()).toBe('Its parent: Ticket acme/COW-1');
    });

    // docs/adr/0008 D2, docs/adr/0005 D3 as amended 2026-10-10.
    it('names a parent of another team by its team and key, linked in that team where the reader opens it', async () => {
      load([ticket(2, { parent: 'globex/API-7', parent_head: headOf('globex/API-7') })]);

      const { page } = await render();

      const chip = rowOf(page, 2)?.querySelector('.chip');
      expect(chip?.textContent?.trim()).toBe('Globex · API-7');
      expect(chip?.getAttribute('href')).toBe('/t/globex/tickets/API-7');
      expect(chip?.getAttribute('aria-label')).toBe('Parent Globex · API-7');
    });

    it('names a parent the reader may not open by its head, unlinked, and says so in the tooltip', async () => {
      load([
        ticket(2, {
          parent: 'globex/API-7',
          parent_head: headOf('globex/API-7', { readable: false }),
        }),
      ]);

      const { fixture, page } = await render();

      const chip = rowOf(page, 2)?.querySelector('.chip');
      expect(chip?.tagName).toBe('SPAN');
      expect(chip?.textContent?.replace(/\s+/g, ' ').trim()).toBe('Parent Globex · API-7');
      expect(
        fixture.debugElement
          .query(By.css('[data-testid="parent-acme/COW-2"]'))
          .injector.get(Tooltip)
          .content(),
      ).toBe('Its parent, which you cannot open: Ticket globex/API-7');
    });

    // docs/adr/0065 D5 as amended 2026-10-10: in another team or in the reader's own.
    it.each([
      ['another team', 'globex', 'Globex'],
      ['the reader’s own team', 'acme', 'Acme'],
    ])(
      'names a parent the reader may not see in %s as `<team> [Confidential]`, unlinked',
      async (_where, slug, name) => {
        const placeholder: TicketHead = {
          team: { slug, name },
          key: null,
          title: null,
          type: null,
          state: null,
          placeholder: true,
          readable: false,
        };
        load([ticket(2, { parent: null, parent_head: placeholder })]);

        const { page } = await render();

        const chip = rowOf(page, 2)?.querySelector('.chip');
        expect(chip?.tagName).toBe('SPAN');
        expect(chip?.textContent?.replace(/\s+/g, ' ').trim()).toBe(
          `Parent ${name} [Confidential]`,
        );
        expect(chip?.querySelector('a')).toBeNull();
      },
    );
  });

  describe('loading the open tickets', () => {
    it('asks for the first page of the open tickets of the project, whatever their state, and for no text', async () => {
      await render();

      expect(openParams()).toEqual({
        team: 'acme',
        project: 'COW',
        pages: 1,
        state: undefined,
        q: undefined,
      });
    });

    it('asks for nothing outside a tenant', async () => {
      tenant.set(null);

      await render();

      expect(openParams()).toBeUndefined();
      expect(closedParams()).toBeUndefined();
    });

    it('asks for the project of the path', async () => {
      const { fixture } = await render();

      fixture.componentRef.setInput('project', 'OPS');
      await fixture.whenStable();

      expect(openParams()?.project).toBe('OPS');
    });

    it('goes back to the first page when the project changes: the router keeps this page alive', async () => {
      load([ticket(1)], 'c1');
      const { fixture, page } = await render();
      page.querySelector<HTMLButtonElement>('[data-testid="load-more"]')?.click();
      expect(openParams()?.pages).toBe(2);

      fixture.componentRef.setInput('project', 'OPS');
      await fixture.whenStable();

      expect(openParams()).toMatchObject({ project: 'OPS', pages: 1 });
    });

    describe('the filter of the state', () => {
      it('offers the open states only, in the order of the vocabulary, each with its meaning', async () => {
        const { fixture } = await render();

        const select = fixture.debugElement.query(By.css('[data-testid="state-filter"]'))
          .componentInstance as Select;
        expect(select.multiple()).toBe(true);
        expect(select.options()?.map((option) => option.state)).toEqual([
          'filed',
          'analysed',
          'decided',
          'in-progress',
          'review',
          'blocked',
        ]);
        expect(select.options()?.[0].meaning).toBe('Recorded, not analysed yet');
      });

      it('asks for the chosen states only', async () => {
        const { fixture } = await render();

        chooseStates(fixture, ['blocked', 'in-progress']);

        expect(openParams()?.state).toEqual(['blocked', 'in-progress']);
      });

      it('goes back to the first page when the states change', async () => {
        load([ticket(1)], 'c1');
        const { fixture, page } = await render();
        page.querySelector<HTMLButtonElement>('[data-testid="load-more"]')?.click();
        expect(openParams()?.pages).toBe(2);

        chooseStates(fixture, ['filed']);

        expect(openParams()?.pages).toBe(1);
      });

      it.each([
        ['cleared, which hands on null', null],
        ['emptied', []],
      ])('asks for every open state again when the filter is %s', async (_, value) => {
        const { fixture } = await render();
        chooseStates(fixture, ['blocked']);

        chooseStates(fixture, value);

        expect(openParams()?.state).toBeUndefined();
      });
    });

    describe('a saved filter (docs/adr/0018 D5)', () => {
      const saved = (parameters: SavedFilter['parameters']): SavedFilter => ({
        id: 'f-1',
        name: 'Bugs',
        owner: { id: 'p-ada', display_name: 'Ada', username: 'ada' },
        shared: false,
        parameters,
        redacted: false,
        warnings: [],
        version: 1,
        created_at: '2026-10-01T00:00:00Z',
        updated_at: '2026-10-01T00:00:00Z',
      });
      const bar = (fixture: ComponentFixture<Backlog>) =>
        fixture.debugElement.query(By.directive(SavedFilters)).componentInstance as SavedFilters;
      const apply = async (fixture: ComponentFixture<Backlog>, filter: SavedFilter | null) => {
        bar(fixture).chosen.emit(filter);
        fixture.detectChanges();
        await fixture.whenStable();
      };

      it('puts its text and open states into the bar and hands the rest to the lists as it is', async () => {
        const { fixture, page } = await render();
        fixture.debugElement
          .query(By.css('[data-testid="show-closed"]'))
          .triggerEventHandler('ngModelChange', true);

        await apply(
          fixture,
          saved({
            q: 'flicker',
            state: ['blocked', '!review'],
            severity: ['high'],
            assignee: ['me'],
            project: ['OPS'],
          }),
        );

        expect(openParams()).toEqual({
          team: 'acme',
          project: 'COW',
          pages: 1,
          q: 'flicker',
          state: ['blocked', '!review'],
          severity: ['high'],
          assignee: ['me'],
        });
        expect(closedParams()).toMatchObject({
          severity: ['high'],
          assignee: ['me'],
          q: 'flicker',
        });
        expect(closedParams()?.state).toEqual(['done', 'dropped']);
        expect(page.querySelector<HTMLInputElement>('[data-testid="search"]')?.value).toBe(
          'flicker',
        );
        const select = fixture.debugElement.query(By.css('[data-testid="state-filter"]'))
          .componentInstance as Select;
        expect(select.value).toEqual(['blocked']);
      });

      it('offers the conditions the bar applies now, to save', async () => {
        const { fixture } = await render();
        await apply(fixture, saved({ severity: ['high'] }));
        chooseStates(fixture, ['filed']);
        fixture.detectChanges();

        expect(bar(fixture).current()).toEqual({ severity: ['high'], state: ['filed'] });
      });

      it('clears every condition when none is chosen', async () => {
        const { fixture } = await render();
        await apply(fixture, saved({ q: 'flicker', state: ['blocked'], severity: ['high'] }));

        await apply(fixture, null);

        expect(openParams()).toEqual({ team: 'acme', project: 'COW', pages: 1 });
        expect(bar(fixture).applied()).toBeNull();
      });

      it('says under the bar that a backlog leaves the filter’s project out', async () => {
        const { fixture, page } = await render();

        await apply(fixture, saved({ project: ['OPS'], severity: ['high'] }));

        expect(bar(fixture).leftOut()).toEqual(backlogLeftOut);
        expect(page.querySelector('[data-testid="filter-notes"]')?.textContent).toBe(
          'project: a backlog is one project; the team’s ticket list applies this condition',
        );
      });
    });

    describe('the search', () => {
      beforeEach(() => {
        vi.useFakeTimers();
      });

      afterEach(() => {
        vi.useRealTimers();
      });

      /** Renders without waiting for Angular, whose scheduler would wait for the faked timers. */
      function renderNow() {
        const fixture = TestBed.createComponent(Backlog);
        fixture.componentRef.setInput('project', 'COW');
        fixture.detectChanges();
        return { fixture, page: fixture.nativeElement as HTMLElement };
      }

      it('asks for the text only after 250 ms without typing', () => {
        const { fixture, page } = renderNow();

        type(page, 'flicker');
        fixture.detectChanges();
        vi.advanceTimersByTime(249);
        expect(openParams()?.q).toBeUndefined();

        vi.advanceTimersByTime(1);
        expect(openParams()?.q).toBe('flicker');
      });

      it('starts the wait again with every keystroke', () => {
        const { fixture, page } = renderNow();

        type(page, 'flick');
        fixture.detectChanges();
        vi.advanceTimersByTime(200);
        type(page, 'flicker');
        fixture.detectChanges();
        vi.advanceTimersByTime(200);
        expect(openParams()?.q).toBeUndefined();

        vi.advanceTimersByTime(50);
        expect(openParams()?.q).toBe('flicker');
      });

      it('goes back to the first page once the text is asked for, for the open and the closed tickets', () => {
        load([ticket(1)], 'c1');
        const { fixture, page } = renderNow();
        fixture.detectChanges();
        page.querySelector<HTMLButtonElement>('[data-testid="load-more"]')?.click();
        fixture.detectChanges();
        expect(openParams()?.pages).toBe(2);

        type(page, 'flicker');
        fixture.detectChanges();
        expect(openParams()?.pages).toBe(2);
        vi.advanceTimersByTime(250);

        expect(openParams()).toMatchObject({ pages: 1, q: 'flicker' });
      });

      it('lets a text typed and taken back within the wait change nothing', () => {
        const { fixture, page } = renderNow();

        type(page, 'flicker');
        fixture.detectChanges();
        vi.advanceTimersByTime(100);
        type(page, '');
        fixture.detectChanges();
        vi.advanceTimersByTime(300);

        expect(openParams()?.q).toBeUndefined();
      });

      it('leaves out spaces around the text and asks for no text that is only spaces', () => {
        const { fixture, page } = renderNow();

        type(page, '  flicker  ');
        fixture.detectChanges();
        vi.advanceTimersByTime(250);
        expect(openParams()?.q).toBe('flicker');

        type(page, '   ');
        fixture.detectChanges();
        vi.advanceTimersByTime(250);
        expect(openParams()?.q).toBeUndefined();
      });

      it('combines the text with the chosen states', () => {
        const { fixture, page } = renderNow();

        chooseStates(fixture, ['blocked']);
        type(page, 'flicker');
        fixture.detectChanges();
        vi.advanceTimersByTime(250);

        expect(openParams()).toMatchObject({ state: ['blocked'], q: 'flicker' });
      });

      it('asks for the closed tickets with the same text', () => {
        const { fixture, page } = renderNow();
        fixture.debugElement
          .query(By.css('[data-testid="show-closed"]'))
          .triggerEventHandler('ngModelChange', true);
        fixture.detectChanges();

        type(page, 'flicker');
        fixture.detectChanges();
        vi.advanceTimersByTime(250);

        expect(closedParams()).toMatchObject({ q: 'flicker', pages: 1 });
      });
    });

    describe('more than one page', () => {
      it('offers to load more at the end when the list has a next page, and nothing otherwise', async () => {
        load([ticket(1)], null);
        const first = await render();
        expect(first.page.querySelector('[data-testid="load-more"]')).toBeNull();
        first.fixture.destroy();

        load([ticket(1)], 'c1');
        const second = await render();
        expect(second.page.querySelector('[data-testid="load-more"]')?.textContent?.trim()).toBe(
          'Load more',
        );
      });

      it('asks for one page more each time it is clicked', async () => {
        load([ticket(1)], 'c1');
        const { page } = await render();

        page.querySelector<HTMLButtonElement>('[data-testid="load-more"]')?.click();
        expect(openParams()?.pages).toBe(2);
        page.querySelector<HTMLButtonElement>('[data-testid="load-more"]')?.click();
        expect(openParams()?.pages).toBe(3);
      });

      it('is disabled while the list loads', async () => {
        load([ticket(1)], 'c1');
        open.isLoading.set(true);

        const { page } = await render();

        expect(page.querySelector<HTMLButtonElement>('[data-testid="load-more"]')?.disabled).toBe(
          true,
        );
      });

      it('says that the tickets shown are those loaded so far', async () => {
        load([ticket(1), ticket(2)], 'c1');

        const { page } = await render();

        expect(text(page, '[data-testid="total"]')).toBe('2 open tickets loaded');
      });
    });

    describe('while it loads, and when it fails', () => {
      it('counts the open tickets in the list', async () => {
        load([ticket(1), ticket(2), ticket(3)]);

        const { page } = await render();

        expect(text(page, '[data-testid="total"]')).toBe('3 open tickets');
      });

      it('counts one open ticket in the singular, and none before the list is loaded', async () => {
        const { fixture, page } = await render();
        expect(text(page, '[data-testid="total"]')).toBe('0 open tickets');

        load([ticket(1)]);
        await fixture.whenStable();

        expect(text(page, '[data-testid="total"]')).toBe('1 open ticket');
      });

      it('keeps the rows it has while the list loads again, and shows the table as busy', async () => {
        load([ticket(1), ticket(2)]);
        const { fixture, page } = await render();

        open.value.set(undefined);
        open.isLoading.set(true);
        await fixture.whenStable();

        expect(rowsIn(page, 'later')).toEqual([1, 2]);
        expect(page.querySelector('.panel')?.classList).toContain('loading');
        expect(page.querySelector('.panel')?.getAttribute('aria-busy')).toBe('true');
      });

      it('does not show the rows of one project in the next one while it loads', async () => {
        load([ticket(1), ticket(2)]);
        const { fixture, page } = await render();

        open.value.set(undefined);
        fixture.componentRef.setInput('project', 'OPS');
        await fixture.whenStable();

        expect(rowsIn(page, 'later')).toEqual([]);
      });

      it('says why the tickets could not be loaded, with the detail of the problem', async () => {
        open.error.set(
          refusal(503, 'not_ready', 'The service is not ready', 'The database is starting.'),
        );

        const { page } = await render();

        expect(text(page, '[data-testid="failure"]')).toBe(
          'The tickets could not be loaded: The database is starting.',
        );
      });

      it('says why with the title when the problem has no detail', async () => {
        open.error.set(refusal(403, 'forbidden', 'Forbidden'));

        const { page } = await render();

        expect(text(page, '[data-testid="failure"]')).toBe(
          'The tickets could not be loaded: Forbidden',
        );
      });

      it('says why when the backend cannot be reached', async () => {
        open.error.set(new HttpErrorResponse({ status: 0, statusText: 'Unknown Error' }));

        const { page } = await render();

        expect(text(page, '[data-testid="failure"]')).toBe(
          'The tickets could not be loaded: The connection failed; cowork tries again on its own.',
        );
      });

      it('shows the tickets it has beside the failure', async () => {
        load([ticket(1)]);
        const { fixture, page } = await render();

        open.value.set(undefined);
        open.error.set(new HttpErrorResponse({ status: 0, statusText: 'Unknown Error' }));
        await fixture.whenStable();

        expect(rowsIn(page, 'later')).toEqual([1]);
        expect(page.querySelector('[data-testid="failure"]')).not.toBeNull();
      });

      it('says nothing about a failure while there is none', async () => {
        const { page } = await render();

        expect(page.querySelector('[data-testid="failure"]')).toBeNull();
      });
    });
  });

  describe('the closed tickets', () => {
    const toggle = async (fixture: ComponentFixture<Backlog>, on = true) => {
      fixture.debugElement
        .query(By.css('[data-testid="show-closed"]'))
        .triggerEventHandler('ngModelChange', on);
      await fixture.whenStable();
    };
    const closedRows = (page: HTMLElement) =>
      [...page.querySelectorAll('[data-testid="group-closed"] tr.row')].map(numberOf);

    it('are not asked for, and not shown, until the toggle is on', async () => {
      const { fixture, page } = await render();

      expect(closedParams()).toBeUndefined();
      expect(page.querySelector('[data-testid="group-closed"]')).toBeNull();
      const toggleSwitch = fixture.debugElement.query(By.directive(ToggleSwitch));
      expect(toggleSwitch.nativeElement.getAttribute('data-testid')).toBe('show-closed');
      expect(text(page, '.closed-toggle')).toBe('Show closed');
    });

    it('are shown from the start when the address asks for them, as the board links them', async () => {
      const fixture = TestBed.createComponent(Backlog);
      fixture.componentRef.setInput('project', 'COW');
      fixture.componentRef.setInput('closed', 'true');
      await fixture.whenStable();

      expect(closedParams()).toMatchObject({ state: ['done', 'dropped'] });
      expect(
        (fixture.nativeElement as HTMLElement).querySelector('[data-testid="group-closed"]'),
      ).not.toBeNull();

      await toggle(fixture, false);
      expect(closedParams()).toBeUndefined();
    });

    it('are not shown for an address that asks for anything else', async () => {
      const fixture = TestBed.createComponent(Backlog);
      fixture.componentRef.setInput('project', 'COW');
      fixture.componentRef.setInput('closed', 'no');
      await fixture.whenStable();

      expect(closedParams()).toBeUndefined();
    });

    describe('narrowed to a time, as the board counts the tickets done since then (docs/adr/0018 D1)', () => {
      const since = '2026-09-19T12:00:00.000Z';

      async function opened(doneAfter: string) {
        const fixture = TestBed.createComponent(Backlog);
        fixture.componentRef.setInput('project', 'COW');
        fixture.componentRef.setInput('closed', 'true');
        fixture.componentRef.setInput('done_after', doneAfter);
        await fixture.whenStable();
        return { fixture, page: fixture.nativeElement as HTMLElement };
      }

      it('are the tickets done after the time of the address, and say so', async () => {
        const { fixture, page } = await opened(since);

        expect(closedParams()).toEqual({
          team: 'acme',
          project: 'COW',
          pages: 1,
          state: ['done'],
          done_after: since,
          q: undefined,
        });
        expect(text(page, '[data-testid="closed-name"]')).toBe('done');
        expect(text(page, '[data-testid="closed-since"]')).toBe(`since ${dateTime(since)}`);

        load([], null, closed);
        await fixture.whenStable();
        expect(text(page, '[data-testid="empty-closed"]')).toBe('No ticket done since then');
      });

      it('give way to every closed ticket from the button, from the first page again', async () => {
        const { fixture, page } = await opened(since);
        load([ticket(8, { state: 'done' })], 'c1', closed);
        await fixture.whenStable();
        page.querySelector<HTMLButtonElement>('[data-testid="load-more-closed"]')?.click();
        expect(closedParams()?.pages).toBe(2);

        page.querySelector<HTMLButtonElement>('[data-testid="all-closed"]')?.click();
        await fixture.whenStable();

        expect(closedParams()).toEqual({
          team: 'acme',
          project: 'COW',
          pages: 1,
          state: ['done', 'dropped'],
          q: undefined,
        });
        expect(text(page, '[data-testid="closed-name"]')).toBe('closed');
        expect(page.querySelector('[data-testid="closed-since"]')).toBeNull();
        expect(page.querySelector('[data-testid="all-closed"]')).toBeNull();
      });

      it('are every closed ticket where the address gives no time', async () => {
        await opened('fourteen days ago');

        expect(closedParams()).toMatchObject({ state: ['done', 'dropped'] });
        expect(closedParams()).not.toHaveProperty('done_after');
      });
    });

    it('show no stage bar: a closed ticket works on none', async () => {
      const { fixture, page } = await render();
      await toggle(fixture);

      load([ticket(8, { state: 'done', progress: 100 })], null, closed);
      await fixture.whenStable();

      expect(rowOf(page, 8)?.querySelector('app-stage-bar')).toBeNull();
    });

    it('are asked for, done and dropped, when the toggle is switched on, and for no more once it is off again', async () => {
      const { fixture } = await render();

      await toggle(fixture);
      expect(closedParams()).toEqual({
        team: 'acme',
        project: 'COW',
        pages: 1,
        state: ['done', 'dropped'],
        q: undefined,
      });

      await toggle(fixture, false);
      expect(closedParams()).toBeUndefined();
    });

    it('show below the groups, as rows that cannot be dragged, moved or reordered', async () => {
      load([ticket(1)]);
      const { fixture, page } = await render();
      await toggle(fixture);

      load([ticket(8, { state: 'done' }), ticket(9, { state: 'dropped' })], null, closed);
      await fixture.whenStable();

      expect(closedRows(page)).toEqual([8, 9]);
      expect(text(page, '[data-testid="count-closed"]')).toBe('2');
      const tbodies = [...page.querySelectorAll('tbody')].map((each) =>
        each.getAttribute('data-testid'),
      );
      expect(tbodies.at(-1)).toBe('group-closed');
      const row = rowOf(page, 8);
      expect(row?.querySelector('[data-testid="grip"]')).toBeNull();
      expect(row?.querySelector('.col-menu button')).toBeNull();
      expect(
        fixture.debugElement.queryAll(By.directive(CdkDrag)).map((each) => each.nativeElement),
      ).not.toContain(row);
      expect(page.querySelector('[data-testid="group-closed"]')?.hasAttribute('cdkdroplist')).toBe(
        false,
      );
    });

    it('open the ticket when they are clicked, and show the same columns', async () => {
      const { fixture, page } = await render();
      await toggle(fixture);
      load(
        [ticket(8, { state: 'done', title: 'Finished work', parent: 'acme/COW-2' })],
        null,
        closed,
      );
      await fixture.whenStable();

      expect(text(page, '[data-testid="row-acme/COW-8"] .text')).toBe('Finished work');
      expect(rowOf(page, 8)?.querySelector('[data-state]')?.getAttribute('data-state')).toBe(
        'done',
      );
      expect(rowOf(page, 8)?.querySelector('.chip')?.textContent?.trim()).toBe('COW-2');
      rowOf(page, 8)?.click();
      expect(navigate).toHaveBeenCalledExactlyOnceWith(['/t', 'acme', 'tickets', 'COW-8']);
    });

    it('say that there are none once the list has answered, and not while it loads', async () => {
      const { fixture, page } = await render();
      closed.isLoading.set(true);
      await toggle(fixture);
      expect(page.querySelector('[data-testid="empty-closed"]')).toBeNull();

      closed.isLoading.set(false);
      load([], null, closed);
      await fixture.whenStable();

      expect(text(page, '[data-testid="empty-closed"]')).toBe('No closed tickets');
    });

    it('load more when the list has a next page, one page at a time', async () => {
      const { fixture, page } = await render();
      await toggle(fixture);
      load([ticket(8, { state: 'done' })], 'c1', closed);
      await fixture.whenStable();

      const more = page.querySelector<HTMLButtonElement>('[data-testid="load-more-closed"]');
      expect(more?.textContent?.trim()).toBe('Load more');
      more?.click();
      expect(closedParams()?.pages).toBe(2);
    });

    it('offer no more once the last page is there, and disable the button while loading', async () => {
      const { fixture, page } = await render();
      await toggle(fixture);
      load([ticket(8, { state: 'done' })], 'c1', closed);
      closed.isLoading.set(true);
      await fixture.whenStable();
      expect(
        page.querySelector<HTMLButtonElement>('[data-testid="load-more-closed"]')?.disabled,
      ).toBe(true);

      load([ticket(8, { state: 'done' })], null, closed);
      closed.isLoading.set(false);
      await fixture.whenStable();

      expect(page.querySelector('[data-testid="load-more-closed"]')).toBeNull();
    });

    it('keep the rows they have while the list loads again', async () => {
      const { fixture, page } = await render();
      await toggle(fixture);
      load([ticket(8, { state: 'done' })], null, closed);
      await fixture.whenStable();

      closed.value.set(undefined);
      await fixture.whenStable();

      expect(closedRows(page)).toEqual([8]);
    });

    it('go back to the first page when the project changes', async () => {
      const { fixture, page } = await render();
      await toggle(fixture);
      load([ticket(8, { state: 'done' })], 'c1', closed);
      await fixture.whenStable();
      page.querySelector<HTMLButtonElement>('[data-testid="load-more-closed"]')?.click();
      expect(closedParams()?.pages).toBe(2);

      fixture.componentRef.setInput('project', 'OPS');
      await fixture.whenStable();

      expect(closedParams()).toMatchObject({ project: 'OPS', pages: 1 });
    });
  });

  describe('the header', () => {
    it('is the header of the project, with its tabs', async () => {
      const { fixture } = await render();

      const header = fixture.debugElement.query(By.directive(ProjectHeader));
      expect((header.componentInstance as ProjectHeader).project()).toBe('COW');
      expect(
        (fixture.nativeElement as HTMLElement)
          .querySelector('[data-testid="tab-backlog"]')
          ?.getAttribute('href'),
      ).toBe('/t/acme/p/COW/backlog');
    });

    it('follows the project of the path', async () => {
      const { fixture } = await render();

      fixture.componentRef.setInput('project', 'OPS');
      await fixture.whenStable();

      const header = fixture.debugElement.query(By.directive(ProjectHeader));
      expect((header.componentInstance as ProjectHeader).project()).toBe('OPS');
    });

    it('loads the list again when a ticket was filed, so that it shows at once', async () => {
      const { fixture } = await render();
      expect(open.reload).not.toHaveBeenCalled();

      fixture.debugElement
        .query(By.directive(ProjectHeader))
        .triggerEventHandler('filed', ticket(7));

      expect(open.reload).toHaveBeenCalledOnce();
    });
  });

  // What a drop asks for. The CDK reports a drop as the list it ended in, the index in it, and the
  // list the row came from; jsdom has no pointer to drag with, so a test hands the page that event.

  const dropLists = (fixture: ComponentFixture<Backlog>) =>
    fixture.debugElement.queryAll(By.directive(CdkDropList));
  const listFor = (fixture: ComponentFixture<Backlog>, horizon: Horizon) =>
    dropLists(fixture).find(
      (each) => (each.injector.get(CdkDropList).data as Group).horizon === horizon,
    );

  /** Drops the row of a ticket at an index of a group, the way the CDK reports it. */
  function drop(
    fixture: ComponentFixture<Backlog>,
    number: number,
    to: Horizon,
    at: number,
    over = true,
  ) {
    const key = `acme/COW-${number}`;
    const source = dropLists(fixture).find((each) =>
      (each.injector.get(CdkDropList).data as Group).rows.some((row) => row.ticket.key === key),
    );
    const target = listFor(fixture, to);
    // The row carries the ticket as it was rendered, whatever the cache holds by now.
    const row = (source?.injector.get(CdkDropList).data as Group).rows.find(
      (each) => each.ticket.key === key,
    );
    const event = {
      item: { data: row?.ticket },
      previousContainer: source?.injector.get(CdkDropList),
      container: target?.injector.get(CdkDropList),
      previousIndex: 0,
      currentIndex: at,
      isPointerOverContainer: over,
      distance: { x: 0, y: 0 },
      dropPoint: { x: 0, y: 0 },
      event: new MouseEvent('mouseup'),
    } as unknown as CdkDragDrop<Group, Group, Ticket>;
    target?.triggerEventHandler('cdkDropListDropped', event);
  }

  /** A row picked up: the CDK announces it, and then measures its lists. */
  function pickUp(fixture: ComponentFixture<Backlog>, number: number) {
    fixture.debugElement
      .query(By.css(`[data-testid="row-acme/COW-${number}"]`))
      .triggerEventHandler('cdkDragStarted', {});
  }

  function putDown(fixture: ComponentFixture<Backlog>, number: number) {
    fixture.debugElement
      .query(By.css(`[data-testid="row-acme/COW-${number}"]`))
      .triggerEventHandler('cdkDragEnded', {});
  }

  describe('a drop within a group', () => {
    beforeEach(() => {
      load([ticket(1), ticket(2), ticket(3), ticket(4)]);
    });

    it('places a row that lands at the top of the group before the row below it', async () => {
      const { fixture } = await render();

      drop(fixture, 3, 'later', 0);
      await settle(fixture);

      expect(rank).toHaveBeenCalledExactlyOnceWith('acme/COW-3', { before: 1 });
      expect(setHorizon).not.toHaveBeenCalled();
    });

    it('places a row that lands in the middle after the row above it', async () => {
      const { fixture } = await render();

      drop(fixture, 1, 'later', 2);
      await settle(fixture);

      expect(rank).toHaveBeenCalledExactlyOnceWith('acme/COW-1', { after: 3 });
    });

    it('places a row that lands at the bottom after the last row', async () => {
      const { fixture } = await render();

      drop(fixture, 1, 'later', 3);
      await settle(fixture);

      expect(rank).toHaveBeenCalledExactlyOnceWith('acme/COW-1', { after: 4 });
    });

    it('asks for nothing when a row is dropped where it was', async () => {
      const { fixture, page } = await render();

      drop(fixture, 2, 'later', 1);
      await settle(fixture);

      expect(rank).not.toHaveBeenCalled();
      expect(rowsIn(page, 'later')).toEqual([1, 2, 3, 4]);
    });

    it('shows the move at once, before the write is answered, and keeps it when it is', async () => {
      const pending = deferred<Ticket>();
      rank.mockImplementation(() => pending.promise);
      const { fixture, page } = await render();

      drop(fixture, 3, 'later', 0);
      fixture.detectChanges();
      await tick();

      expect(rank).toHaveBeenCalledOnce();
      expect(rowsIn(page, 'later')).toEqual([3, 1, 2, 4]);
      pending.resolve(cache.value('acme/COW-3') as Ticket);
      await settle(fixture);
      expect(rowsIn(page, 'later')).toEqual([3, 1, 2, 4]);
    });

    it('changes nothing visible when the list answers after the write, with the order that the move made', async () => {
      const { fixture, page } = await render();
      drop(fixture, 3, 'later', 0);
      await settle(fixture);
      const row = rowOf(page, 3);

      load([ticket(3, { version: 2 }), ticket(1), ticket(2), ticket(4)]);
      await settle(fixture);

      expect(rowsIn(page, 'later')).toEqual([3, 1, 2, 4]);
      expect(rowOf(page, 3)).toBe(row);
    });

    it('keeps the move when an answer that left the server before it arrives while the write is on its way', async () => {
      const pending = deferred<Ticket>();
      rank.mockImplementation(() => pending.promise);
      const { fixture, page } = await render();
      drop(fixture, 3, 'later', 0);
      await settle(fixture);

      load([ticket(1), ticket(2), ticket(3), ticket(4)]);
      await settle(fixture);

      expect(rowsIn(page, 'later')).toEqual([3, 1, 2, 4]);
      pending.resolve(ticket(3, { version: 2 }));
      await settle(fixture);
      expect(rowsIn(page, 'later')).toEqual([3, 1, 2, 4]);
    });

    it('keeps the move when such an answer arrives after the write was answered', async () => {
      const { fixture, page } = await render();
      drop(fixture, 3, 'later', 0);
      await settle(fixture);

      load([ticket(1), ticket(2), ticket(3), ticket(4)]);
      await settle(fixture);

      expect(rowsIn(page, 'later')).toEqual([3, 1, 2, 4]);
    });

    it('shows what an answer has once it has the version the move wrote, or a later one: a later move of somebody else wins', async () => {
      const { fixture, page } = await render();
      drop(fixture, 3, 'later', 0);
      await settle(fixture);

      load([ticket(1), ticket(2), ticket(3, { version: 3 }), ticket(4)]);
      await settle(fixture);
      expect(rowsIn(page, 'later')).toEqual([1, 2, 3, 4]);

      // The move is settled: an older answer after that shows as it is.
      load([ticket(1), ticket(4), ticket(3, { version: 3 }), ticket(2)]);
      await settle(fixture);
      expect(rowsIn(page, 'later')).toEqual([1, 4, 3, 2]);
    });

    it('lets a moved ticket go with the answer that no longer has it, closed by somebody else meanwhile', async () => {
      const pending = deferred<Ticket>();
      rank.mockImplementation(() => pending.promise);
      const { fixture, page } = await render();
      drop(fixture, 3, 'later', 0);
      await settle(fixture);

      load([ticket(1), ticket(2), ticket(4)]);
      await settle(fixture);
      expect(rowsIn(page, 'later')).toEqual([1, 2, 4]);

      pending.resolve(ticket(3, { version: 2 }));
      await settle(fixture);
      expect(rowsIn(page, 'later')).toEqual([1, 2, 4]);
    });

    it('puts the row back and says why when the write fails', async () => {
      rank.mockRejectedValue(
        refusal(400, 'validation_failed', 'Validation failed', 'No such ticket in the project.'),
      );
      const { fixture, page } = await render();

      drop(fixture, 3, 'later', 0);
      await settle(fixture);

      expect(rowsIn(page, 'later')).toEqual([1, 2, 3, 4]);
      expect(toast).toHaveBeenCalledWith(
        expect.objectContaining({ severity: 'warn', summary: 'Validation failed' }),
      );
    });

    it('loads the list again after a failure, to show what the server has', async () => {
      rank.mockRejectedValue(refusal(409, 'state_conflict', 'Conflict'));
      const { fixture } = await render();

      drop(fixture, 3, 'later', 0);
      await settle(fixture);

      expect(open.reload).toHaveBeenCalledOnce();
    });

    it('asks for nothing for a row that is dropped beside the table, and lets it go back', async () => {
      const { fixture, page } = await render();

      drop(fixture, 3, 'later', 0, false);
      await settle(fixture);

      expect(rank).not.toHaveBeenCalled();
      expect(rowsIn(page, 'later')).toEqual([1, 2, 3, 4]);
    });

    it('does the writes of two moves one after the other', async () => {
      const started: string[] = [];
      const answers: (() => void)[] = [];
      rank.mockImplementation(
        (key) =>
          new Promise<Ticket>((resolve) => {
            started.push(key);
            answers.push(() => resolve(cache.value(key) as Ticket));
          }),
      );
      const { fixture, page } = await render();

      drop(fixture, 3, 'later', 0);
      fixture.detectChanges();
      drop(fixture, 4, 'later', 0);
      fixture.detectChanges();
      await settle(fixture);

      expect(started).toEqual(['acme/COW-3']);
      expect(rowsIn(page, 'later')).toEqual([4, 3, 1, 2]);
      answers[0]();
      await settle(fixture);
      expect(started).toEqual(['acme/COW-3', 'acme/COW-4']);
      answers[1]();
      await settle(fixture);
      expect(rowsIn(page, 'later')).toEqual([4, 3, 1, 2]);
    });

    it('puts back the row whose write failed and not the one that followed it', async () => {
      rank.mockRejectedValueOnce(refusal(400, 'validation_failed', 'Validation failed'));
      const { fixture, page } = await render();

      drop(fixture, 3, 'later', 0);
      fixture.detectChanges();
      drop(fixture, 4, 'later', 0);
      await settle(fixture);

      // 3 is back behind 2. 4 went directly before 3, and the server put it there: before 3, where
      // 3 is.
      expect(rank).toHaveBeenNthCalledWith(2, 'acme/COW-4', { before: 3 });
      expect(rowsIn(page, 'later')).toEqual([1, 2, 4, 3]);
      expect(rank).toHaveBeenCalledTimes(2);
    });
  });

  describe('a drop into another group', () => {
    beforeEach(() => {
      load([
        ticket(1, { horizon: 'now' }),
        ticket(2, { horizon: 'now' }),
        ticket(3, { horizon: 'later' }),
        ticket(4, { horizon: 'later' }),
        ticket(5, { horizon: 'later' }),
      ]);
    });

    it('sets the horizon to the value of the group with the override, and no reason', async () => {
      const { fixture } = await render();

      drop(fixture, 3, 'now', 1);
      await settle(fixture);

      expect(setHorizon).toHaveBeenCalledExactlyOnceWith('acme/COW-3', 'now');
    });

    it('then places the row after the row above it where it lands in the middle', async () => {
      const { fixture } = await render();

      drop(fixture, 3, 'now', 1);
      await settle(fixture);

      expect(rank).toHaveBeenCalledExactlyOnceWith('acme/COW-3', { after: 1 });
    });

    it('places a row that lands at the top of the group before the first row', async () => {
      const { fixture } = await render();

      drop(fixture, 3, 'now', 0);
      await settle(fixture);

      expect(rank).toHaveBeenCalledExactlyOnceWith('acme/COW-3', { before: 1 });
    });

    it('places a row that lands at the bottom after the last row', async () => {
      const { fixture } = await render();

      drop(fixture, 3, 'now', 2);
      await settle(fixture);

      expect(rank).toHaveBeenCalledExactlyOnceWith('acme/COW-3', { after: 2 });
    });

    it('sets the horizon and places nothing where the group was empty', async () => {
      const { fixture, page } = await render();

      drop(fixture, 3, 'next', 0);
      await settle(fixture);

      expect(setHorizon).toHaveBeenCalledExactlyOnceWith('acme/COW-3', 'next');
      expect(rank).not.toHaveBeenCalled();
      expect(rowsIn(page, 'next')).toEqual([3]);
    });

    it('writes the horizon first and the place after it', async () => {
      const calls: string[] = [];
      setHorizon.mockImplementation(async (key) => {
        calls.push('horizon');
        return cache.value(key) as Ticket;
      });
      rank.mockImplementation(async (key) => {
        calls.push('rank');
        return cache.value(key) as Ticket;
      });
      const { fixture } = await render();

      drop(fixture, 3, 'now', 1);
      await settle(fixture);

      expect(calls).toEqual(['horizon', 'rank']);
    });

    it('shows the row in its new group and place at once, and keeps it there when the writes are answered', async () => {
      const pending = deferred<Ticket>();
      setHorizon.mockImplementation(() => pending.promise);
      const { fixture, page } = await render();

      drop(fixture, 3, 'now', 1);
      fixture.detectChanges();
      await tick();

      expect(setHorizon).toHaveBeenCalledOnce();
      expect(rowsIn(page, 'now')).toEqual([1, 3, 2]);
      expect(rowsIn(page, 'later')).toEqual([4, 5]);
      expect(text(page, '[data-testid="count-now"]')).toBe('3');
      pending.resolve({ ...(cache.value('acme/COW-3') as Ticket), horizon: 'now', version: 2 });
      cache.put('acme/COW-3', {
        ...(cache.value('acme/COW-3') as Ticket),
        horizon: 'now',
        version: 2,
      });
      await settle(fixture);
      expect(rowsIn(page, 'now')).toEqual([1, 3, 2]);
      expect(rowsIn(page, 'later')).toEqual([4, 5]);
    });

    it('keeps the row in its new group and place once the writes are answered', async () => {
      const { fixture, page } = await render();

      drop(fixture, 3, 'now', 1);
      await settle(fixture);

      expect(rowsIn(page, 'now')).toEqual([1, 3, 2]);
      expect(rowsIn(page, 'later')).toEqual([4, 5]);
    });

    it('sets later through the same route, and asks for no reason: later keeps none', async () => {
      load([
        ticket(1, { horizon: 'now' }),
        ticket(3, { horizon: 'now', horizon_set: { value: 'now', at: 'then', reason: 'x' } }),
        ticket(4, { horizon: 'later' }),
      ]);
      const { fixture, page } = await render();

      drop(fixture, 3, 'later', 1);
      await settle(fixture);

      expect(setHorizon).toHaveBeenCalledExactlyOnceWith('acme/COW-3', 'later');
      expect(rank).toHaveBeenCalledExactlyOnceWith('acme/COW-3', { after: 4 });
      expect(rowsIn(page, 'later')).toEqual([4, 3]);
      expect(page.querySelector('[data-testid="reason"]')).toBeNull();
    });

    it('puts the row back, writes no place and says why when the horizon cannot be set', async () => {
      setHorizon.mockRejectedValue(
        refusal(400, 'validation_failed', 'Validation failed', 'The reason is required.'),
      );
      const { fixture, page } = await render();

      drop(fixture, 3, 'now', 1);
      await settle(fixture);

      expect(rowsIn(page, 'now')).toEqual([1, 2]);
      expect(rowsIn(page, 'later')).toEqual([3, 4, 5]);
      expect(rank).not.toHaveBeenCalled();
      expect(toast).toHaveBeenCalledWith(
        expect.objectContaining({
          summary: 'Validation failed',
          detail: 'The reason is required.',
        }),
      );
      expect(open.reload).toHaveBeenCalledOnce();
      expect(page.querySelector('[data-testid="reason"]')).toBeNull();
    });

    it('puts the row back in its place when the place cannot be set, but not out of the group the server has put it in', async () => {
      rank.mockRejectedValue(refusal(409, 'state_conflict', 'Conflict'));
      const { fixture, page } = await render();

      drop(fixture, 3, 'now', 0);
      await settle(fixture);

      // The override is written, so the cache says now; the place is where the list has it.
      expect(rowsIn(page, 'now')).toEqual([1, 2, 3]);
      expect(rowsIn(page, 'later')).toEqual([4, 5]);
      expect(toast).toHaveBeenCalledWith(expect.objectContaining({ summary: 'Conflict' }));
      expect(open.reload).toHaveBeenCalledOnce();
    });

    it('tells the person that somebody else changed the horizon meanwhile, and puts the row back', async () => {
      setHorizon.mockRejectedValue(
        new StaleWrite(
          {
            status: 412,
            code: 'precondition_failed',
            title: 'x',
            detail: '',
            fields: {},
            current: {},
          },
          ticket(3, { horizon: 'next', version: 3 }),
        ),
      );
      const { fixture, page } = await render();

      drop(fixture, 3, 'now', 1);
      await settle(fixture);

      expect(toast).toHaveBeenCalledWith({
        severity: 'warn',
        summary: 'Changed meanwhile',
        detail: 'COW-3 was changed by someone else: its horizon is next now.',
        life: 6000,
      });
      expect(rowsIn(page, 'now')).toEqual([1, 2]);
      expect(rowsIn(page, 'later')).toEqual([3, 4, 5]);
    });

    it('asks for nothing for a row that is dropped beside the table', async () => {
      const { fixture, page } = await render();

      drop(fixture, 3, 'now', 1, false);
      await settle(fixture);

      expect(setHorizon).not.toHaveBeenCalled();
      expect(rowsIn(page, 'later')).toEqual([3, 4, 5]);
    });

    it('sets the horizon of a child without placing it where it cannot be shown, outside the family of its parent', async () => {
      load([
        ticket(1, { horizon: 'now' }),
        ticket(2, { horizon: 'now', parent: 'acme/COW-1' }),
        ticket(3, { horizon: 'now' }),
        ticket(4, { horizon: 'later', parent: 'acme/COW-1' }),
      ]);
      const { fixture, page } = await render();

      drop(fixture, 4, 'now', 3);
      await settle(fixture);

      expect(setHorizon).toHaveBeenCalledExactlyOnceWith('acme/COW-4', 'now');
      expect(rank).not.toHaveBeenCalled();
      // It stands under its parent, behind its sibling, as their rank has it.
      expect(rowsIn(page, 'now')).toEqual([1, 2, 4, 3]);
      expect(rowOf(page, 4)?.querySelector('.chip')).toBeNull();
    });
  });

  describe('a drop inside a family', () => {
    beforeEach(() => {
      load([
        ticket(1),
        ticket(2, { parent: 'acme/COW-1' }),
        ticket(3, { parent: 'acme/COW-1' }),
        ticket(4),
        ticket(5),
      ]);
    });

    it('places a root that lands between a parent and its children behind the whole family', async () => {
      const { fixture } = await render();

      drop(fixture, 5, 'later', 2);
      await settle(fixture);

      expect(rank).toHaveBeenCalledExactlyOnceWith('acme/COW-5', { after: 1 });
    });

    it('moves a child among its siblings: above the first one is before it', async () => {
      const { fixture } = await render();

      drop(fixture, 3, 'later', 1);
      await settle(fixture);

      expect(rank).toHaveBeenCalledExactlyOnceWith('acme/COW-3', { before: 2 });
    });

    it('refuses a child that lands outside the family of its parent, and lets it go back', async () => {
      const { fixture, page } = await render();

      drop(fixture, 3, 'later', 4);
      await settle(fixture);

      expect(rank).not.toHaveBeenCalled();
      expect(rowsIn(page, 'later')).toEqual([1, 2, 3, 4, 5]);
    });

    it('shows a parent with its children where it was dropped', async () => {
      const { fixture, page } = await render();

      drop(fixture, 1, 'later', 3);
      await settle(fixture);

      expect(rank).toHaveBeenCalledExactlyOnceWith('acme/COW-1', { after: 4 });
      expect(rowsIn(page, 'later')).toEqual([4, 1, 2, 3, 5]);
    });
  });

  describe('the reason of a horizon', () => {
    beforeEach(() => {
      load([ticket(1, { horizon: 'now' }), ticket(2), ticket(3)]);
    });

    const field = (page: HTMLElement) =>
      page.querySelector<HTMLInputElement>('[data-testid="reason"]');
    const typeReason = (fixture: ComponentFixture<Backlog>, value: string) => {
      const input = field(fixture.nativeElement as HTMLElement);
      if (!input) {
        throw new Error('the reason field is missing');
      }
      input.value = value;
      input.dispatchEvent(new Event('input'));
      fixture.detectChanges();
    };
    const press = (page: HTMLElement, key: string) =>
      field(page)?.dispatchEvent(new KeyboardEvent('keydown', { key, bubbles: true }));

    /** A drop into the group now, and the reason field it offers. */
    async function dropIntoNow() {
      const rendered = await render();
      drop(rendered.fixture, 2, 'now', 1);
      await settle(rendered.fixture);
      return rendered;
    }

    it('is offered in a small field at the row once an override is written, and has the focus', async () => {
      const { page } = await dropIntoNow();

      const input = field(page);
      expect(input).not.toBeNull();
      expect(rowOf(page, 2)?.contains(input)).toBe(true);
      expect(document.activeElement).toBe(input);
      expect(input?.getAttribute('aria-label')).toBe('Why COW-2 is now, optional');
      expect(input?.getAttribute('placeholder')).toBe('Why now? Enter saves, Esc skips');
    });

    it('is offered for one row only', async () => {
      const { page } = await dropIntoNow();

      expect(page.querySelectorAll('[data-testid="reason"]')).toHaveLength(1);
    });

    it('is not offered before the override is written', async () => {
      const pending = deferred<Ticket>();
      setHorizon.mockImplementation(() => pending.promise);
      const { fixture, page } = await render();

      drop(fixture, 2, 'now', 1);
      fixture.detectChanges();
      await tick();
      expect(setHorizon).toHaveBeenCalledOnce();
      expect(field(page)).toBeNull();

      pending.resolve(cache.value('acme/COW-2') as Ticket);
      await settle(fixture);
      expect(field(page)).not.toBeNull();
    });

    it('is not offered within a group, where no override is written', async () => {
      const { fixture, page } = await render();

      drop(fixture, 3, 'later', 0);
      await settle(fixture);

      expect(field(page)).toBeNull();
    });

    it('sends the override again with the same value, the reason and no other change, when Enter is pressed', async () => {
      const { fixture, page } = await dropIntoNow();
      setHorizon.mockClear();

      typeReason(fixture, '  The client escalated it  ');
      press(page, 'Enter');
      await settle(fixture);

      expect(setHorizon).toHaveBeenCalledExactlyOnceWith(
        'acme/COW-2',
        'now',
        'The client escalated it',
      );
      expect(field(page)).toBeNull();
    });

    it('keeps the field, read only, while the reason is on its way', async () => {
      const { fixture, page } = await dropIntoNow();
      const pending = deferred<Ticket>();
      setHorizon.mockClear();
      setHorizon.mockImplementation(() => pending.promise);

      typeReason(fixture, 'Because');
      press(page, 'Enter');
      fixture.detectChanges();
      expect(field(page)?.readOnly).toBe(true);
      press(page, 'Enter');
      expect(setHorizon).toHaveBeenCalledTimes(1);

      pending.resolve(cache.value('acme/COW-2') as Ticket);
      await settle(fixture);
      expect(field(page)).toBeNull();
    });

    it('is dismissed by Enter on an empty field, without a write', async () => {
      const { fixture, page } = await dropIntoNow();
      setHorizon.mockClear();

      typeReason(fixture, '   ');
      press(page, 'Enter');
      await settle(fixture);

      expect(setHorizon).not.toHaveBeenCalled();
      expect(field(page)).toBeNull();
    });

    it('is dismissed by Escape, without a write', async () => {
      const { fixture, page } = await dropIntoNow();
      setHorizon.mockClear();

      typeReason(fixture, 'Because');
      press(page, 'Escape');
      await settle(fixture);

      expect(setHorizon).not.toHaveBeenCalled();
      expect(field(page)).toBeNull();
    });

    it('is dismissed when the person leaves the field, without a write', async () => {
      const { fixture, page } = await dropIntoNow();
      setHorizon.mockClear();

      typeReason(fixture, 'Because');
      field(page)?.dispatchEvent(new Event('blur'));
      await settle(fixture);

      expect(setHorizon).not.toHaveBeenCalled();
      expect(field(page)).toBeNull();
    });

    it('does not open the ticket when the field is clicked', async () => {
      const { page } = await dropIntoNow();

      field(page)?.click();

      expect(navigate).not.toHaveBeenCalled();
    });

    it('says why when the reason cannot be written, and goes away', async () => {
      const { fixture, page } = await dropIntoNow();
      setHorizon.mockRejectedValue(refusal(403, 'forbidden', 'Forbidden', 'Not allowed.'));

      typeReason(fixture, 'Because');
      press(page, 'Enter');
      await settle(fixture);

      expect(toast).toHaveBeenCalledWith(expect.objectContaining({ summary: 'Forbidden' }));
      expect(field(page)).toBeNull();
    });

    it('goes to the next row that is dropped into a group, empty: what was typed for the first is not sent', async () => {
      const { fixture, page } = await dropIntoNow();
      typeReason(fixture, 'Half a thought');

      drop(fixture, 3, 'now', 2);
      await settle(fixture);

      expect(page.querySelectorAll('[data-testid="reason"]')).toHaveLength(1);
      expect(rowOf(page, 3)?.contains(field(page))).toBe(true);
      expect(field(page)?.value).toBe('');
      expect(setHorizon).not.toHaveBeenCalledWith('acme/COW-2', 'now', 'Half a thought');
    });

    it('does not take the field of the next row away when an earlier reason is answered', async () => {
      const { fixture, page } = await dropIntoNow();
      const pending = deferred<Ticket>();
      setHorizon.mockImplementationOnce(() => pending.promise);
      typeReason(fixture, 'Because');
      press(page, 'Enter');
      fixture.detectChanges();

      drop(fixture, 3, 'now', 2);
      await settle(fixture);
      expect(rowOf(page, 3)?.contains(field(page))).toBe(true);
      pending.resolve(cache.value('acme/COW-2') as Ticket);
      await settle(fixture);

      expect(rowOf(page, 3)?.contains(field(page))).toBe(true);
    });
  });

  describe('while a row is dragged', () => {
    beforeEach(() => {
      load([ticket(1, { horizon: 'now' }), ticket(2), ticket(3, { title: 'Before' })]);
    });

    it('holds back the list: a new order and a new group wait until the row is put down', async () => {
      const { fixture, page } = await render();
      pickUp(fixture, 2);

      load([ticket(3), ticket(2), ticket(1, { horizon: 'next' })]);
      await settle(fixture);
      expect(rowsIn(page, 'now')).toEqual([1]);
      expect(rowsIn(page, 'later')).toEqual([2, 3]);

      putDown(fixture, 2);
      await settle(fixture);
      expect(rowsIn(page, 'now')).toEqual([]);
      expect(rowsIn(page, 'next')).toEqual([1]);
      expect(rowsIn(page, 'later')).toEqual([3, 2]);
    });

    it('holds back a ticket that other people change, and a ticket that goes', async () => {
      const { fixture, page } = await render();
      pickUp(fixture, 2);

      cache.put('acme/COW-3', ticket(3, { title: 'After', version: 2 }));
      cache.delete('acme/COW-1');
      await settle(fixture);
      expect(text(page, '[data-testid="row-acme/COW-3"] .text')).toBe('Before');
      expect(rowOf(page, 1)).not.toBeNull();

      putDown(fixture, 2);
      await settle(fixture);
      expect(text(page, '[data-testid="row-acme/COW-3"] .text')).toBe('After');
      expect(rowOf(page, 1)).toBeNull();
    });

    it('applies what was held back after a drop, with the move on top of it', async () => {
      const { fixture, page } = await render();
      pickUp(fixture, 3);

      load([ticket(1, { horizon: 'now' }), ticket(2), ticket(3), ticket(4)]);
      await settle(fixture);
      expect(rowOf(page, 4)).toBeNull();

      drop(fixture, 3, 'later', 0);
      await settle(fixture);

      expect(rowsIn(page, 'later')).toEqual([3, 2, 4]);
    });

    const zones = (page: HTMLElement) =>
      [...page.querySelectorAll('[data-testid="zones"] .zone')].map((zone) =>
        zone.getAttribute('data-testid'),
      );

    it('offers the groups that are empty and not in the table, release and icebox, as drop zones in a bar of their own', async () => {
      const { fixture, page } = await render();
      expect(zones(page)).toEqual([]);

      pickUp(fixture, 2);

      expect(zones(page)).toEqual(['zone-release', 'zone-icebox']);
      expect(groupsShown(page)).toEqual(['now', 'next', 'later']);
      expect(page.querySelector('[data-testid="zones"]')?.getAttribute('aria-hidden')).toBe('true');
      expect(text(page, '[data-testid="zone-release"] .group-name')).toBe('release');
      expect(text(page, '[data-testid="zone-release"] .muted')).toBe(
        'Drop here to move a ticket to release',
      );
    });

    it('says in the table that an empty group takes the row', async () => {
      const { fixture, page } = await render();

      pickUp(fixture, 2);

      expect(text(page, '[data-testid="empty-next"]')).toBe('Drop here to move a ticket to next');
    });

    it('has the drop zones in the page when the CDK measures its lists, which is right after the announcement', async () => {
      const { fixture, page } = await render();

      // No change detection of the test between the announcement and the check: the page renders at once.
      fixture.debugElement
        .query(By.css('[data-testid="row-acme/COW-2"]'))
        .triggerEventHandler('cdkDragStarted', {});

      expect(page.querySelector('[data-testid="zone-release"]')).not.toBeNull();
      expect(listFor(fixture, 'release')).toBeDefined();
      expect(listFor(fixture, 'icebox')).toBeDefined();
    });

    it('offers no zone for a group that holds a ticket, which is in the table in its place', async () => {
      load([ticket(1, { horizon: 'release' }), ticket(2), ticket(3, { horizon: 'next' })]);
      const { fixture, page } = await render();
      expect(groupsShown(page)).toEqual(['now', 'release', 'next', 'later']);

      pickUp(fixture, 2);

      expect(zones(page)).toEqual(['zone-icebox']);
      expect(groupsShown(page)).toEqual(['now', 'release', 'next', 'later']);
    });

    it('offers no bar at all when no group is empty and out of the table', async () => {
      load([ticket(1, { horizon: 'release' }), ticket(2), ticket(3, { horizon: 'icebox' })]);
      const { fixture, page } = await render();

      pickUp(fixture, 2);

      expect(page.querySelector('[data-testid="zones"]')).toBeNull();
    });

    it('sets the horizon of a row dropped into a zone, places nothing there and offers the reason', async () => {
      const { fixture, page } = await render();
      pickUp(fixture, 2);

      drop(fixture, 2, 'release', 0);
      await settle(fixture);

      expect(setHorizon).toHaveBeenCalledExactlyOnceWith('acme/COW-2', 'release');
      expect(rank).not.toHaveBeenCalled();
      expect(zones(page)).toEqual([]);
      expect(groupsShown(page)).toEqual(['now', 'release', 'next', 'later']);
      expect(rowsIn(page, 'release')).toEqual([2]);
      expect(rowOf(page, 2)?.contains(page.querySelector('[data-testid="reason"]'))).toBe(true);
    });

    it('takes the drop zones away again when the row is put down', async () => {
      const { fixture, page } = await render();
      pickUp(fixture, 2);

      putDown(fixture, 2);
      await settle(fixture);

      expect(zones(page)).toEqual([]);
      expect(groupsShown(page)).toEqual(['now', 'next', 'later']);
    });

    it('lets go of the list after a drop, also where the drop asks for nothing', async () => {
      const { fixture, page } = await render();
      pickUp(fixture, 2);

      drop(fixture, 2, 'later', 0, false);
      await settle(fixture);
      load([ticket(3), ticket(2), ticket(1, { horizon: 'now' })]);
      await settle(fixture);

      expect(rowsIn(page, 'later')).toEqual([3, 2]);
      expect(groupsShown(page)).toEqual(['now', 'next', 'later']);
    });

    it('lets a row that is gone from the list meanwhile be dropped, and says only that it moved', async () => {
      const { fixture, page } = await render();
      pickUp(fixture, 3);
      cache.delete('acme/COW-3');
      await settle(fixture);

      drop(fixture, 3, 'later', 0);
      await settle(fixture);

      expect(text(page, '[data-testid="status"]')).toBe('COW-3 moved');
    });

    it('asks for the override of a row that the cache no longer holds, by what it was dragged as', async () => {
      const { fixture } = await render();
      pickUp(fixture, 3);
      const dragged = cache.value('acme/COW-3') as Ticket;
      cache.delete('acme/COW-3');
      setHorizon.mockResolvedValue({ ...dragged, horizon: 'now' });
      await settle(fixture);

      drop(fixture, 3, 'now', 1);
      await settle(fixture);

      expect(setHorizon).toHaveBeenCalledExactlyOnceWith('acme/COW-3', 'now');
    });

    it('lists the groups with the counts they had when the row was picked up', async () => {
      const { fixture, page } = await render();
      pickUp(fixture, 2);

      expect(text(page, '[data-testid="count-later"]')).toBe('2');
      expect(text(page, '[data-testid="count-now"]')).toBe('1');
    });
  });

  describe('the menu of a row', () => {
    beforeEach(() => {
      load([
        ticket(1, { horizon: 'now' }),
        ticket(2, { horizon: 'later' }),
        ticket(3, { horizon: 'later' }),
        ticket(4, { horizon: 'later' }),
        ticket(5, { horizon: 'next' }),
      ]);
    });

    const menuOf = (fixture: ComponentFixture<Backlog>) =>
      fixture.debugElement.query(By.directive(Menu)).componentInstance as Menu;

    /** Opens the menu of a ticket's row and says what it offers. */
    async function open$(fixture: ComponentFixture<Backlog>, page: HTMLElement, number: number) {
      const toggle = vi.spyOn(menuOf(fixture), 'toggle').mockImplementation(() => undefined);
      page.querySelector<HTMLButtonElement>(`[data-testid="row-menu-acme/COW-${number}"]`)?.click();
      await fixture.whenStable();
      return { toggle, items: menuOf(fixture).model() ?? [] };
    }

    const pick = (items: MenuItem[], label: string) => {
      const item = items.find((candidate) => candidate.label === label);
      if (!item?.command) {
        throw new Error(`no menu item ${label}`);
      }
      item.command({ item });
    };
    const labels = (items: MenuItem[]) =>
      items.filter((item) => !item.separator).map((item) => item.label);
    const disabled = (items: MenuItem[]) =>
      items.filter((item) => item.disabled).map((item) => item.label);

    it('is a button on every open row, named for its ticket, that opens one menu for it', async () => {
      const { fixture, page } = await render();

      const { toggle, items } = await open$(fixture, page, 3);

      const button = page.querySelector('[data-testid="row-menu-acme/COW-3"]');
      expect(button?.getAttribute('aria-label')).toBe('Move COW-3');
      expect(button?.getAttribute('aria-haspopup')).toBe('menu');
      expect(toggle).toHaveBeenCalledOnce();
      expect(page.querySelectorAll('p-menu')).toHaveLength(1);
      expect(items.length).toBeGreaterThan(0);
    });

    it('offers Move up, Move down, Move to top, Move to bottom and a move to each other group', async () => {
      const { fixture, page } = await render();

      const { items } = await open$(fixture, page, 3);

      expect(labels(items)).toEqual([
        'Move up',
        'Move down',
        'Move to top',
        'Move to bottom',
        'Move to now',
        'Move to release',
        'Move to next',
        'Move to icebox',
      ]);
      expect(disabled(items)).toEqual([]);
    });

    it('does not open the ticket when the button is clicked', async () => {
      const { fixture, page } = await render();

      await open$(fixture, page, 3);

      expect(navigate).not.toHaveBeenCalled();
    });

    it('cannot move the first row up or to the top', async () => {
      const { fixture, page } = await render();

      const { items } = await open$(fixture, page, 2);

      expect(disabled(items)).toEqual(['Move up', 'Move to top']);
    });

    it('cannot move the last row down or to the bottom', async () => {
      const { fixture, page } = await render();

      const { items } = await open$(fixture, page, 4);

      expect(disabled(items)).toEqual(['Move down', 'Move to bottom']);
    });

    it('cannot move a row that is alone in its group', async () => {
      const { fixture, page } = await render();

      const { items } = await open$(fixture, page, 1);

      expect(disabled(items)).toEqual(['Move up', 'Move down', 'Move to top', 'Move to bottom']);
    });

    it.each([
      ['Move up', 3, { before: 2 }, [3, 2, 4]],
      ['Move down', 3, { after: 4 }, [2, 4, 3]],
      ['Move to top', 4, { before: 2 }, [4, 2, 3]],
      ['Move to bottom', 2, { after: 4 }, [3, 4, 2]],
    ] as const)(
      '%s on COW-%i makes the call of a drop, %o, and shows the row there',
      async (label, number, call, order) => {
        const { fixture, page } = await render();
        const { items } = await open$(fixture, page, number);

        pick(items, label);
        await settle(fixture);

        expect(rank).toHaveBeenCalledExactlyOnceWith(`acme/COW-${number}`, call);
        expect(rowsIn(page, 'later')).toEqual(order);
        expect(setHorizon).not.toHaveBeenCalled();
      },
    );

    it('moves the row up by one, directly after the row that is two above it', async () => {
      const { fixture, page } = await render();
      const { items } = await open$(fixture, page, 4);

      pick(items, 'Move up');
      await settle(fixture);

      expect(rank).toHaveBeenCalledExactlyOnceWith('acme/COW-4', { after: 2 });
    });

    it('moves the row up to the top of its group, before the first row', async () => {
      const { fixture, page } = await render();
      const { items } = await open$(fixture, page, 4);

      pick(items, 'Move to top');
      await settle(fixture);

      expect(rank).toHaveBeenCalledExactlyOnceWith('acme/COW-4', { before: 2 });
    });

    it('moves the row down by one, and to the bottom, after the row it passes', async () => {
      const first = await render();
      const down = await open$(first.fixture, first.page, 2);
      pick(down.items, 'Move down');
      await settle(first.fixture);
      expect(rank).toHaveBeenLastCalledWith('acme/COW-2', { after: 3 });

      const bottom = await open$(first.fixture, first.page, 3);
      pick(bottom.items, 'Move to bottom');
      await settle(first.fixture);
      expect(rank).toHaveBeenLastCalledWith('acme/COW-3', { after: 4 });
    });

    it('moves a row to another group like a drop does: the horizon, then the end of that group', async () => {
      const { fixture, page } = await render();
      const { items } = await open$(fixture, page, 3);

      pick(items, 'Move to next');
      await settle(fixture);

      expect(setHorizon).toHaveBeenCalledExactlyOnceWith('acme/COW-3', 'next');
      expect(rank).toHaveBeenCalledExactlyOnceWith('acme/COW-3', { after: 5 });
      expect(rowsIn(page, 'next')).toEqual([5, 3]);
      expect(rowsIn(page, 'later')).toEqual([2, 4]);
    });

    it('sets the horizon and places nothing in a group that is empty', async () => {
      const { fixture, page } = await render();
      const { items } = await open$(fixture, page, 3);

      pick(items, 'Move to release');
      await settle(fixture);

      expect(setHorizon).toHaveBeenCalledExactlyOnceWith('acme/COW-3', 'release');
      expect(rank).not.toHaveBeenCalled();
      expect(groupsShown(page)).toContain('release');
      expect(rowsIn(page, 'release')).toEqual([3]);
    });

    it('moves a ticket to later through the same route', async () => {
      load([
        ticket(1, { horizon: 'now', horizon_set: { value: 'now', at: 'then', reason: 'x' } }),
        ticket(2, { horizon: 'later' }),
      ]);
      const { fixture, page } = await render();
      const { items } = await open$(fixture, page, 1);

      pick(items, 'Move to later');
      await settle(fixture);

      expect(setHorizon).toHaveBeenCalledExactlyOnceWith('acme/COW-1', 'later');
      expect(rank).toHaveBeenCalledExactlyOnceWith('acme/COW-1', { after: 2 });
    });

    it('offers the reason of a horizon it has set, like a drop does', async () => {
      const { fixture, page } = await render();
      const { items } = await open$(fixture, page, 3);

      pick(items, 'Move to now');
      await settle(fixture);

      expect(rowOf(page, 3)?.contains(page.querySelector('[data-testid="reason"]'))).toBe(true);
    });

    it('moves a child among its siblings only', async () => {
      load([
        ticket(1),
        ticket(2, { parent: 'acme/COW-1' }),
        ticket(3, { parent: 'acme/COW-1' }),
        ticket(4),
      ]);
      const { fixture, page } = await render();
      const { items } = await open$(fixture, page, 3);
      expect(disabled(items)).toEqual(['Move down', 'Move to bottom']);

      pick(items, 'Move up');
      await settle(fixture);

      expect(rank).toHaveBeenCalledExactlyOnceWith('acme/COW-3', { before: 2 });
      expect(rowsIn(page, 'later')).toEqual([1, 3, 2, 4]);
    });

    it('puts the keyboard back on the menu button of the row, where the row went', async () => {
      const { fixture, page } = await render();
      const { items } = await open$(fixture, page, 4);

      pick(items, 'Move to top');
      await settle(fixture);

      expect(document.activeElement).toBe(
        page.querySelector('[data-testid="row-menu-acme/COW-4"]'),
      );
    });

    it('puts the keyboard in the field of the reason when the move set a horizon, and on the button once that is done', async () => {
      const { fixture, page } = await render();
      const { items } = await open$(fixture, page, 3);

      pick(items, 'Move to next');
      await settle(fixture);
      const field = page.querySelector<HTMLInputElement>('[data-testid="reason"]');
      expect(document.activeElement).toBe(field);

      field?.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }));
      await settle(fixture);
      expect(document.activeElement).toBe(
        page.querySelector('[data-testid="row-menu-acme/COW-3"]'),
      );
    });

    it('tells a screen reader where the ticket went', async () => {
      const { fixture, page } = await render();
      const first = await open$(fixture, page, 3);

      pick(first.items, 'Move to next');
      await settle(fixture);
      expect(text(page, '[data-testid="status"]')).toBe('COW-3 moved to next, place 2 of 2');
      expect(page.querySelector('[data-testid="status"]')?.getAttribute('role')).toBe('status');

      const second = await open$(fixture, page, 4);
      pick(second.items, 'Move to top');
      await settle(fixture);
      expect(text(page, '[data-testid="status"]')).toBe('COW-4 moved to later, place 1 of 2');
    });

    it('does nothing for a choice that is not a move, such as a row that is alone moving up', async () => {
      const { fixture, page } = await render();
      const { items } = await open$(fixture, page, 1);

      pick(items, 'Move up');
      await settle(fixture);

      expect(rank).not.toHaveBeenCalled();
    });
  });

  // docs/adr/0014 D3: the score is a marker beside the rank, and the rank can adopt it in one act.
  describe('the score', () => {
    const dialog = () => document.body.querySelector('.p-confirmdialog');
    const press = (label: string) =>
      [...(dialog()?.querySelectorAll('button') ?? [])]
        .find((button) => button.textContent?.trim() === label)
        ?.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    const mark = (page: HTMLElement, number: number) =>
      page.querySelector(`[data-testid="score-mark-acme/COW-${number}"]`);

    it('marks the ticket the score would lift, and only that one, as a marker, not a figure', async () => {
      load([ticket(1, { score: 2 }), ticket(2, { score: 9.4 }), ticket(3, { score: 1 })]);

      const { page } = await render();

      expect(mark(page, 1)).toBeNull();
      expect(mark(page, 3)).toBeNull();
      expect(mark(page, 2)?.getAttribute('data-mark')).toBe('higher');
      expect(mark(page, 2)?.textContent).toContain('score');
      expect(mark(page, 2)?.querySelector('.sr-only')?.textContent).toContain(
        'The score, 9.4, says higher than the rank puts it',
      );
    });

    it('marks the ticket the score would lower, and nothing where the rank follows the score', async () => {
      load([ticket(1, { score: 1 }), ticket(2, { score: 6 }), ticket(3, { score: 5 })]);
      const first = await render();
      expect(mark(first.page, 1)?.getAttribute('data-mark')).toBe('lower');
      first.fixture.destroy();

      load([ticket(1, { score: 9 }), ticket(2, { score: 5 }), ticket(3, { score: null })]);
      const second = await render();
      expect(second.page.querySelector('.score-mark')).toBeNull();
    });

    it('sorts the backlog by the score once the person confirms, and says how many moved', async () => {
      load([ticket(1, { score: 2 }), ticket(2, { score: 9 })]);
      const { fixture, page } = await render();

      page.querySelector<HTMLButtonElement>('[data-testid="sort-by-score"]')?.click();
      await settle(fixture);
      expect(dialog()?.textContent).toContain('Sort COW by score?');
      expect(sortByScore).not.toHaveBeenCalled();
      press('Sort by score');
      await settle(fixture);

      expect(sortByScore).toHaveBeenCalledExactlyOnceWith('acme', 'COW');
      expect(toast).toHaveBeenCalledWith(
        expect.objectContaining({
          severity: 'success',
          summary: 'Sorted by score: 2 tickets moved',
        }),
      );
    });

    it('sorts nothing when the person keeps the order', async () => {
      load([ticket(1, { score: 2 }), ticket(2, { score: 9 })]);
      const { fixture, page } = await render();

      page.querySelector<HTMLButtonElement>('[data-testid="sort-by-score"]')?.click();
      await settle(fixture);
      press('Keep the order');
      await settle(fixture);

      expect(sortByScore).not.toHaveBeenCalled();
    });

    it('says so when the backlog follows the score already', async () => {
      sortByScore.mockResolvedValue(0);
      load([ticket(1, { score: 9 })]);
      const { fixture, page } = await render();

      page.querySelector<HTMLButtonElement>('[data-testid="sort-by-score"]')?.click();
      await settle(fixture);
      press('Sort by score');
      await settle(fixture);

      expect(toast).toHaveBeenCalledWith(
        expect.objectContaining({
          severity: 'info',
          summary: 'The backlog follows the score already',
        }),
      );
    });

    it('offers a viewer no sort', async () => {
      membership.set({ role: 'viewer' });
      load([ticket(1, { score: 2 })]);

      const { page } = await render();

      expect(page.querySelector('[data-testid="sort-by-score"]')).toBeNull();
    });
  });

  describe('the other people', () => {
    it('re-sort the rows when the list answers with a new order', async () => {
      load([ticket(1), ticket(2), ticket(3)]);
      const { fixture, page } = await render();

      load([ticket(3), ticket(1), ticket(2)]);
      await settle(fixture);

      expect(rowsIn(page, 'later')).toEqual([3, 1, 2]);
    });

    it('add a ticket that is new, and take one away that is closed', async () => {
      load([ticket(1), ticket(2)]);
      const { fixture, page } = await render();

      load([ticket(2), ticket(3)]);
      await settle(fixture);

      expect(rowsIn(page, 'later')).toEqual([2, 3]);
    });
  });
});
