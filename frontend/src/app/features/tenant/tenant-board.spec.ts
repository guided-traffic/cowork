import { CdkDrag, CdkDragDrop, CdkDropList, CdkDropListGroup } from '@angular/cdk/drag-drop';
import { HttpErrorResponse } from '@angular/common/http';
import { signal, WritableSignal } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { By } from '@angular/platform-browser';
import { ActivatedRoute, provideRouter, Router } from '@angular/router';
import { MessageService } from 'primeng/api';
import { Select } from 'primeng/select';
import type { Mock, MockInstance } from 'vitest';
import { Problem, Project, SavedFilter, Ticket } from '../../api/models';
import { EntityCache } from '../../core/entity-cache';
import { MembersService } from '../../core/members.service';
import { ProjectsService } from '../../core/projects.service';
import { SavedFiltersService } from '../../core/saved-filters.service';
import { SessionService } from '../../core/session.service';
import { TicketActions } from '../../core/ticket-actions.service';
import { ProjectTicketPagesParams, TicketPage, TicketsService } from '../../core/tickets.service';
import { ColumnId } from '../project/board-model';
import { boardLeftOut } from '../project/saved-filter-model';
import { SavedFilters } from '../project/saved-filters';
import { MoveDialog } from '../ticket/move-dialog';
import { nearMargin } from './board-lane';
import { TenantBoard } from './tenant-board';

function project(key: string, fields: Partial<Project> = {}): Project {
  return {
    id: `id-${key}`,
    key,
    name: `Project ${key}`,
    description: '',
    restricted: false,
    wip_limits: {},
    version: 1,
    created_at: '2026-10-01T09:00:00Z',
    updated_at: '2026-10-01T09:00:00Z',
    ...fields,
  };
}

function ticket(key: string, number: number, fields: Partial<Ticket> = {}): Ticket {
  return {
    id: `id-${key}-${number}`,
    key: `acme/${key}-${number}`,
    number,
    project: key,
    title: `Ticket ${key}-${number}`,
    body: '',
    type: 'task',
    state: 'in-progress',
    severity: 'medium',
    security: 'none',
    confidential: false,
    assignee: null,
    reporter: { id: 'p1', display_name: 'Ada Lovelace', username: 'local:ada' },
    block: null,
    threat: null,
    parent: null,
    effort: 'M',
    progress: 0,
    progress_refinement: 0,
    progress_review: 0,
    progress_derived: false,
    horizon: 'now',
    horizon_set: null,
    opened_at: '2026-10-01T09:00:00Z',
    created_at: '2026-10-01T09:00:00Z',
    updated_at: '2026-10-03T11:55:00Z',
    decided_at: null,
    done_at: null,
    done_from: null,
    done_by_hand: false,
    open_prerequisites: 0,
    version: 1,
    ...fields,
  } as Ticket;
}

const refusal = (status: number, code: Problem['code'], title: string, detail?: string) => {
  const body: Problem = { type: 'about:blank', title, status, code, ...(detail ? { detail } : {}) };
  return new HttpErrorResponse({ status, statusText: title, error: body });
};

/** What a list of the service is as a swimlane sees it, made of signals the test sets. */
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

/** The browser's IntersectionObserver, as the test decides what is in view. */
class FakeObserver {
  static all: FakeObserver[] = [];
  target: Element | undefined;
  readonly disconnect = vi.fn();

  constructor(
    private readonly callback: IntersectionObserverCallback,
    readonly options: IntersectionObserverInit | undefined,
  ) {
    FakeObserver.all.push(this);
  }

  observe(target: Element): void {
    this.target = target;
  }

  unobserve(): void {
    this.target = undefined;
  }

  takeRecords(): IntersectionObserverEntry[] {
    return [];
  }

  intersect(isIntersecting: boolean): void {
    this.callback(
      [{ isIntersecting, target: this.target } as IntersectionObserverEntry],
      this as unknown as IntersectionObserver,
    );
  }
}

describe('TenantBoard (docs/adr/0018 D4)', () => {
  let tenant: WritableSignal<string | null>;
  let oversight: WritableSignal<boolean>;
  let projects: WritableSignal<Project[]>;
  let projectsLoading: WritableSignal<boolean>;
  let cache: EntityCache<Ticket>;
  /** Every list a swimlane asked for, in the order the swimlanes were made. */
  let lists: { params: () => ProjectTicketPagesParams | undefined; list: FakeList }[];
  let transition: Mock<TicketActions['transition']>;
  let navigate: MockInstance<Router['navigate']>;
  let toast: MockInstance<MessageService['add']>;
  let pointAt: Mock<(x: number, y: number) => Element | null>;

  beforeEach(() => {
    FakeObserver.all = [];
    vi.stubGlobal('IntersectionObserver', FakeObserver);
    pointAt = vi.fn<(x: number, y: number) => Element | null>(() => null);
    Object.defineProperty(document, 'elementFromPoint', { configurable: true, value: pointAt });

    tenant = signal<string | null>('acme');
    oversight = signal(false);
    projects = signal([project('COW'), project('OPS')]);
    projectsLoading = signal(false);
    cache = new EntityCache<Ticket>();
    lists = [];
    transition = vi.fn<TicketActions['transition']>(async (key, move) => {
      const held = cache.value(key) as Ticket;
      const answer = { ...held, state: move.to, version: held.version + 1 } as Ticket;
      cache.put(key, answer);
      return answer;
    });
    TestBed.configureTestingModule({
      providers: [
        provideRouter([]),
        MessageService,
        {
          provide: TicketActions,
          // No rank: a move on a board never changes the rank, and a call would fail.
          useValue: { transition, setHorizon: vi.fn() },
        },
        { provide: MembersService, useValue: { list: signal([]) } },
        {
          provide: SessionService,
          useValue: {
            tenant,
            oversight,
            person: signal({ id: 'p-ada' }),
            // The team's head names the team (team-header.ts).
            shown: signal({ slug: 'acme', name: 'Acme Corp', role: 'admin' }),
          },
        },
        {
          provide: ProjectsService,
          useValue: { list: projects, projects: { isLoading: projectsLoading } },
        },
        {
          provide: TicketsService,
          useValue: {
            cache,
            projectTicketPages: (params: () => ProjectTicketPagesParams | undefined) => {
              const list = fakeList();
              lists.push({ params, list });
              return list;
            },
          },
        },
        {
          provide: SavedFiltersService,
          useValue: {
            list: signal([]),
            reload: vi.fn(),
            create: vi.fn(),
            update: vi.fn(),
            remove: vi.fn(),
          },
        },
      ],
    });
    navigate = vi.spyOn(TestBed.inject(Router), 'navigate').mockResolvedValue(true);
    toast = vi.spyOn(TestBed.inject(MessageService), 'add');
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    delete (document as { elementFromPoint?: unknown }).elementFromPoint;
  });

  async function render(filter?: string | string[]) {
    const fixture = TestBed.createComponent(TenantBoard);
    if (filter !== undefined) {
      fixture.componentRef.setInput('project', filter);
    }
    await fixture.whenStable();
    return { fixture, page: fixture.nativeElement as HTMLElement };
  }

  const lane = (page: HTMLElement, key: string) =>
    page.querySelector<HTMLElement>(`[data-testid="lane-${key}"]`);

  const lanesShown = (page: HTMLElement) =>
    [...page.querySelectorAll('app-board-lane')].map((each) => each.getAttribute('data-lane'));

  const text = (root: Element | null, selector: string) =>
    root?.querySelector(selector)?.textContent?.replace(/\s+/g, ' ').trim();

  /** The swimlane comes into view, or near it — or leaves. */
  async function see(fixture: ComponentFixture<TenantBoard>, key: string, near = true) {
    const observer = FakeObserver.all.find(
      (each) =>
        each.target?.getAttribute('data-lane') === key && !each.disconnect.mock.calls.length,
    );
    observer?.intersect(near);
    await fixture.whenStable();
  }

  /** The list of the swimlane of the project, once it asked for it. */
  const listOf = (key: string) => lists.find((each) => each.params()?.project === key)?.list;

  /** What the swimlane of the project asks for, once it does. */
  const paramsOf = (key: string) => lists.find((each) => each.params()?.project === key)?.params();

  /** Puts the tickets into the cache and has the swimlane's list answer their keys, in this rank. */
  async function load(fixture: ComponentFixture<TenantBoard>, key: string, tickets: Ticket[]) {
    await see(fixture, key);
    for (const each of tickets) {
      cache.put(each.key, each);
    }
    listOf(key)?.value.set({
      keys: tickets.map((each) => each.key),
      nextCursor: null,
      versions: new Map(tickets.map((each) => [each.key, each.version])),
    });
    await fixture.whenStable();
  }

  const numberOf = (card: Element) => Number(card.getAttribute('data-testid')?.split('-').pop());

  /** The numbers of the cards of a column of a swimlane, in the order shown. */
  const cardsIn = (page: HTMLElement, key: string, column: ColumnId | 'next') =>
    [
      ...(lane(page, key)?.querySelectorAll(`[data-testid="column-${column}"] app-board-card`) ??
        []),
    ].map(numberOf);

  const laneDebug = (fixture: ComponentFixture<TenantBoard>, key: string) =>
    fixture.debugElement.query(By.css(`[data-lane="${key}"]`));

  const card = (fixture: ComponentFixture<TenantBoard>, key: string, number: number) =>
    fixture.debugElement.query(By.css(`[data-testid="card-acme/${key}-${number}"]`));

  /** A drop of a card on a column of its swimlane, as the CDK reports it. */
  function drop(fixture: ComponentFixture<TenantBoard>, key: string, number: number, to: ColumnId) {
    const dragged = card(fixture, key, number);
    const lists = laneDebug(fixture, key).queryAll(By.directive(CdkDropList));
    const source = lists.find((each) =>
      (each.nativeElement as HTMLElement).contains(dragged.nativeElement),
    );
    const target = lists.find((each) => each.injector.get(CdkDropList).data === to);
    const event = {
      item: { data: dragged.injector.get(CdkDrag).data },
      previousContainer: source?.injector.get(CdkDropList),
      container: target?.injector.get(CdkDropList),
      previousIndex: 0,
      currentIndex: 0,
      isPointerOverContainer: true,
      distance: { x: 0, y: 0 },
      dropPoint: { x: 0, y: 0 },
      event: new MouseEvent('mouseup'),
    } as unknown as CdkDragDrop<ColumnId, ColumnId, Ticket>;
    target?.triggerEventHandler('cdkDropListDropped', event);
  }

  /** Lets what a drop started finish, and shows it. */
  async function settle(fixture: ComponentFixture<TenantBoard>) {
    for (let round = 0; round < 3; round++) {
      await new Promise((resolve) => setTimeout(resolve));
      fixture.detectChanges();
    }
    await fixture.whenStable();
  }

  /** The pointer is over an element of the swimlane, or of none. */
  function pointerOver(page: HTMLElement, key: string | null) {
    const over = key ? lane(page, key)?.querySelector('[data-testid="column-ready"]') : page;
    pointAt.mockReturnValue(over ?? null);
  }

  describe('the swimlanes', () => {
    it('are the projects the person sees that are not archived, in the order of the list, each linked to its board', async () => {
      projects.set([
        project('COW', { name: 'Cowork' }),
        project('OLD', { archived_at: '2026-10-02T09:00:00Z' }),
        project('OPS', { name: 'Operations' }),
      ]);
      const { page } = await render();

      expect(lanesShown(page)).toEqual(['COW', 'OPS']);
      const link = lane(page, 'COW')?.querySelector('[data-testid="lane-project"]');
      expect(link?.getAttribute('href')).toBe('/t/acme/p/COW/board');
      expect(link?.textContent?.replace(/\s+/g, ' ').trim()).toBe('COWCowork');
      expect(lane(page, 'COW')?.getAttribute('role')).toBe('region');
      expect(lane(page, 'COW')?.getAttribute('aria-labelledby')).toBe('lane-COW');
    });

    it('have the columns of the project board, each of them: Next, Refinement, Ready, In Progress, Blocked and Review', async () => {
      const { page } = await render();

      for (const key of ['COW', 'OPS']) {
        expect(
          [...(lane(page, key)?.querySelectorAll('.column h2') ?? [])].map((h) =>
            h.textContent?.trim(),
          ),
        ).toEqual(['Next', 'Refinement', 'Ready', 'In Progress', 'Blocked', 'Review']);
      }
    });

    it('give every heading an id of its own, so that each column is named by its swimlane', async () => {
      const { page } = await render();

      const ids = [...page.querySelectorAll('[id]')].map((each) => each.id);
      expect(new Set(ids).size).toBe(ids.length);
      expect(
        lane(page, 'OPS')
          ?.querySelector('[data-testid="column-ready"]')
          ?.getAttribute('aria-labelledby'),
      ).toBe('lane-OPS-column-ready');
    });

    it('make the headings of their columns one level below their own', async () => {
      const { page } = await render();

      expect(lane(page, 'COW')?.querySelector('h2')?.getAttribute('aria-level')).toBeNull();
      const columns = [...(lane(page, 'COW')?.querySelectorAll('.column h2') ?? [])];
      expect(columns.map((h) => h.getAttribute('aria-level'))).toEqual(Array(6).fill('3'));
    });

    it('put a ticket in the column the project board puts it in', async () => {
      const { fixture, page } = await render();
      await load(fixture, 'COW', [
        ticket('COW', 1, { state: 'review' }),
        ticket('COW', 2, { state: 'analysed', horizon: 'release' }),
        ticket('COW', 3, { state: 'in-progress', horizon: 'next' }),
        ticket('COW', 4, { state: 'decided', horizon: 'later' }),
        ticket('COW', 5, { progress_derived: true }),
      ]);
      await load(fixture, 'OPS', [ticket('OPS', 1, { state: 'decided' })]);

      expect(cardsIn(page, 'COW', 'review')).toEqual([1]);
      expect(cardsIn(page, 'COW', 'refinement')).toEqual([2]);
      expect(cardsIn(page, 'COW', 'next')).toEqual([3]);
      expect(cardsIn(page, 'COW', 'ready')).toEqual([]);
      expect(cardsIn(page, 'COW', 'in-progress')).toEqual([]);
      expect(cardsIn(page, 'OPS', 'ready')).toEqual([1]);
      expect(text(lane(page, 'COW'), '[data-testid="lane-count"]')).toBe('3 on the board');
    });

    it('count against the WIP limits of their own project', async () => {
      projects.set([project('COW', { wip_limits: { 'in-progress': 1 } }), project('OPS')]);
      const { fixture, page } = await render();
      await load(fixture, 'COW', [ticket('COW', 1), ticket('COW', 2)]);
      await load(fixture, 'OPS', [ticket('OPS', 1), ticket('OPS', 2)]);

      expect(text(lane(page, 'COW'), '[data-testid="count-in-progress"]')).toBe('2 / 1');
      expect(
        lane(page, 'COW')?.querySelector('[data-testid="column-in-progress"]')?.classList,
      ).toContain('over');
      expect(text(lane(page, 'OPS'), '[data-testid="count-in-progress"]')).toBe('2');
    });

    it('move a card another person moved, as the cache learns of it', async () => {
      const { fixture, page } = await render();
      await load(fixture, 'COW', [ticket('COW', 1, { state: 'decided' })]);

      cache.put('acme/COW-1', ticket('COW', 1, { state: 'in-progress', version: 2 }));
      await fixture.whenStable();

      expect(cardsIn(page, 'COW', 'ready')).toEqual([]);
      expect(cardsIn(page, 'COW', 'in-progress')).toEqual([1]);
    });

    it('say so when one could not be loaded', async () => {
      const { fixture, page } = await render();
      await see(fixture, 'OPS');

      listOf('OPS')?.error.set(refusal(500, 'internal', 'Internal error', 'Something broke.'));
      await fixture.whenStable();

      expect(text(lane(page, 'OPS'), '[data-testid="lane-failure"]')).toBe(
        'The board of OPS could not be loaded: Something broke.',
      );
      expect(lane(page, 'COW')?.querySelector('[data-testid="lane-failure"]')).toBeNull();
    });

    it('show that they load', async () => {
      const { fixture, page } = await render();
      await see(fixture, 'COW');

      listOf('COW')?.isLoading.set(true);
      await fixture.whenStable();

      const board = lane(page, 'COW')?.querySelector('[data-testid="board"]');
      expect(board?.classList).toContain('loading');
      expect(board?.getAttribute('aria-busy')).toBe('true');
    });

    it('wait for the projects, and say when the tenant has none', async () => {
      projects.set([]);
      projectsLoading.set(true);
      const { fixture, page } = await render();
      expect(page.querySelector('p-skeleton')).not.toBeNull();
      expect(page.querySelector('[data-testid="no-projects"]')).toBeNull();

      projectsLoading.set(false);
      await fixture.whenStable();

      expect(text(page, '[data-testid="no-projects"]')).toBe('This team has no projects yet.');
    });

    it('show nothing of the work to a global administrator without a role (docs/adr/0034 D2)', async () => {
      oversight.set(true);
      const { page } = await render();

      expect(page.querySelector('app-board-lane')).toBeNull();
      expect(page.querySelector('[data-testid="project-filter"]')).toBeNull();
      expect(page.querySelector('app-saved-filters')).toBeNull();
    });

    it('are none outside a tenant', async () => {
      tenant.set(null);
      const { page } = await render();

      expect(page.querySelector('app-board-lane')).toBeNull();
    });
  });

  describe('lazy loading, a swimlane at a time', () => {
    it('asks for no tickets of a swimlane that is not near the view', async () => {
      await render();

      expect(lists).toHaveLength(2);
      expect(lists.map((each) => each.params())).toEqual([undefined, undefined]);
    });

    it('watches each swimlane from the area the page scrolls in, a screen ahead', async () => {
      const { page } = await render();

      expect(FakeObserver.all.map((each) => each.target)).toEqual([
        lane(page, 'COW'),
        lane(page, 'OPS'),
      ]);
      expect(FakeObserver.all[0].options?.rootMargin).toBe(nearMargin);
      expect(nearMargin).toBe('100% 0px');
    });

    it('asks for the tickets of a swimlane that comes near, as the project board asks for them', async () => {
      const { fixture } = await render();

      await see(fixture, 'OPS');

      expect(lists[1].params()).toEqual({
        team: 'acme',
        project: 'OPS',
        pages: Number.POSITIVE_INFINITY,
        horizon: ['now', 'release', 'next'],
      });
      expect(lists[0].params()).toBeUndefined();
    });

    it('says nothing of an empty column before its swimlane has loaded', async () => {
      const { fixture, page } = await render();
      expect(lane(page, 'COW')?.querySelector('[data-testid="empty-review"]')).toBeNull();
      expect(text(lane(page, 'COW'), '[data-testid="lane-count"]')).toBe('');

      await load(fixture, 'COW', []);

      expect(text(lane(page, 'COW'), '[data-testid="empty-review"]')).toBe('No tickets');
      expect(text(lane(page, 'COW'), '[data-testid="empty-next"]')).toBe('Nothing is next');
      expect(text(lane(page, 'COW'), '[data-testid="lane-count"]')).toBe('0 on the board');
    });

    it('stops following a swimlane that leaves the view, keeps what it showed, and asks again when it comes back', async () => {
      const { fixture, page } = await render();
      await load(fixture, 'COW', [ticket('COW', 1)]);
      const cow = lists[0];

      await see(fixture, 'COW', false);
      cow.list.value.set(undefined);
      await fixture.whenStable();

      expect(cow.params()).toBeUndefined();
      expect(cardsIn(page, 'COW', 'in-progress')).toEqual([1]);

      await see(fixture, 'COW');
      expect(cow.params()?.project).toBe('COW');
    });

    it('lets the observer of a swimlane go with it', async () => {
      const { fixture } = await render();
      const ops = FakeObserver.all.find((each) => each.target?.getAttribute('data-lane') === 'OPS');

      fixture.componentRef.setInput('project', 'COW');
      await fixture.whenStable();

      expect(ops?.disconnect).toHaveBeenCalled();
    });
  });

  describe('the project filter', () => {
    it('shows the swimlanes of the projects the address names', async () => {
      projects.set([project('COW'), project('DOC'), project('OPS')]);
      const { fixture, page } = await render(['OPS', 'COW']);
      expect(lanesShown(page)).toEqual(['COW', 'OPS']);

      fixture.componentRef.setInput('project', 'DOC');
      await fixture.whenStable();
      expect(lanesShown(page)).toEqual(['DOC']);

      fixture.componentRef.setInput('project', undefined);
      await fixture.whenStable();
      expect(lanesShown(page)).toEqual(['COW', 'DOC', 'OPS']);
    });

    it('offers every project that is not archived, and shows the choice of the address', async () => {
      projects.set([
        project('COW', { name: 'Cowork' }),
        project('OLD', { archived_at: '2026-10-02T09:00:00Z' }),
        project('OPS', { name: 'Operations' }),
      ]);
      const { fixture } = await render('OPS');
      const select = fixture.debugElement.query(By.directive(Select));

      expect((select.componentInstance as Select).options()).toEqual([
        { key: 'COW', label: 'COW · Cowork' },
        { key: 'OPS', label: 'OPS · Operations' },
      ]);
      expect(
        (select.nativeElement as HTMLElement).querySelector('.p-select-label')?.textContent?.trim(),
      ).toBe('OPS · Operations');
    });

    it('writes the choice into the address, in place of the page shown', async () => {
      const { fixture } = await render();
      const select = fixture.debugElement.query(By.directive(Select));

      select.triggerEventHandler('ngModelChange', ['OPS', 'COW']);

      expect(navigate).toHaveBeenCalledExactlyOnceWith([], {
        relativeTo: TestBed.inject(ActivatedRoute),
        queryParams: { project: ['OPS', 'COW'] },
        queryParamsHandling: 'merge',
        replaceUrl: true,
      });
    });

    it('drops the filter from the address when nothing is chosen, or the choice is cleared', async () => {
      const { fixture } = await render('OPS');
      const select = fixture.debugElement.query(By.directive(Select));

      select.triggerEventHandler('ngModelChange', []);
      select.triggerEventHandler('ngModelChange', null);

      expect(navigate.mock.calls.map(([, extras]) => extras?.queryParams)).toEqual([
        { project: null },
        { project: null },
      ]);
    });

    it('says when it names no project that is here, and offers every project', async () => {
      const { fixture, page } = await render('GONE');

      expect(page.querySelector('app-board-lane')).toBeNull();
      expect(text(page, '[data-testid="no-match"]')).toContain(
        'None of the projects the filter names is here.',
      );

      page.querySelector<HTMLElement>('[data-testid="show-every-project"]')?.click();
      await fixture.whenStable();

      expect(navigate.mock.calls[0][1]?.queryParams).toEqual({ project: null });
    });
  });

  describe('a saved filter (docs/adr/0018 D5, docs/adr/0049 D6)', () => {
    const saved = (parameters: SavedFilter['parameters']): SavedFilter => ({
      id: 'f-1',
      name: 'Mine',
      owner: { id: 'p-ada', display_name: 'Ada', username: 'ada' },
      shared: false,
      parameters,
      redacted: false,
      warnings: [],
      version: 1,
      created_at: '2026-10-01T00:00:00Z',
      updated_at: '2026-10-01T00:00:00Z',
    });
    const bar = (fixture: ComponentFixture<TenantBoard>) =>
      fixture.debugElement.query(By.directive(SavedFilters)).componentInstance as SavedFilters;
    const projectsWritten = () => navigate.mock.lastCall?.[1]?.queryParams?.['project'];

    /** The person picks a filter, or none, and the router hands the page the projects it wrote. */
    async function apply(fixture: ComponentFixture<TenantBoard>, filter: SavedFilter | null) {
      bar(fixture).chosen.emit(filter);
      fixture.componentRef.setInput('project', projectsWritten() ?? undefined);
      await fixture.whenStable();
    }

    it('puts its projects into the address and hands every other condition to the swimlanes’ lists', async () => {
      projects.set([project('COW'), project('DOC'), project('OPS')]);
      const { fixture, page } = await render();
      const filter = saved({
        project: ['OPS', 'COW'],
        assignee: ['me'],
        severity: ['high'],
        q: 'deploy',
      });

      await apply(fixture, filter);
      await see(fixture, 'COW');
      await see(fixture, 'OPS');

      expect(navigate).toHaveBeenCalledExactlyOnceWith([], {
        relativeTo: TestBed.inject(ActivatedRoute),
        queryParams: { project: ['OPS', 'COW'] },
        queryParamsHandling: 'merge',
        replaceUrl: true,
      });
      expect(lanesShown(page)).toEqual(['COW', 'OPS']);
      expect(paramsOf('OPS')).toEqual({
        team: 'acme',
        project: 'OPS',
        pages: Number.POSITIVE_INFINITY,
        horizon: ['now', 'release', 'next'],
        assignee: ['me'],
        severity: ['high'],
        q: 'deploy',
      });
      expect(paramsOf('COW')).toMatchObject({ assignee: ['me'], severity: ['high'], q: 'deploy' });
      expect(bar(fixture).applied()).toBe(filter);
      expect(text(page, '[data-testid="board-beyond"]')).toBe(
        'Also filtered by assignee=me · severity=high · q="deploy"',
      );
    });

    it('narrows the board’s horizons to those the filter names, and never widens them', async () => {
      const { fixture } = await render();
      await see(fixture, 'COW');

      await apply(fixture, saved({ horizon: ['now', 'later'] }));
      expect(paramsOf('COW')?.horizon).toEqual(['now']);

      await apply(fixture, saved({ horizon: ['!next'] }));
      expect(paramsOf('COW')?.horizon).toEqual(['now', 'release']);
    });

    it('hands the bar what the board applies now, to save', async () => {
      const { fixture } = await render(['COW', '!OPS']);
      expect(bar(fixture).current()).toEqual({ project: ['COW', '!OPS'] });

      await apply(fixture, saved({ project: ['OPS'], type: ['bug'] }));
      fixture.componentRef.setInput('project', 'DOC');
      await fixture.whenStable();

      expect(bar(fixture).current()).toEqual({ project: ['DOC'], type: ['bug'] });
    });

    it('says under the bar that a board leaves include_terminal out, and does not ask for it', async () => {
      const { fixture, page } = await render();
      await see(fixture, 'COW');

      await apply(fixture, saved({ include_terminal: true, type: ['bug'] }));

      expect(bar(fixture).leftOut()).toEqual(boardLeftOut);
      expect(text(page, '[data-testid="filter-notes"]')).toBe(
        'include_terminal: a board shows no closed ticket; the team’s ticket list applies this condition',
      );
      expect(paramsOf('COW')).not.toHaveProperty('include_terminal');
      expect(paramsOf('COW')?.type).toEqual(['bug']);
      expect(text(page, '[data-testid="board-beyond"]')).toBe('Also filtered by type=bug');
    });

    it('shows no swimlane of a project it excludes, and the select keeps the exclusion', async () => {
      projects.set([project('COW'), project('DOC'), project('OPS')]);
      const { fixture, page } = await render();

      await apply(fixture, saved({ project: ['!OPS'] }));

      expect(lanesShown(page)).toEqual(['COW', 'DOC']);
      const select = fixture.debugElement.query(By.css('[data-testid="project-filter"]'));
      expect((select.componentInstance as Select).modelValue()).toEqual([]);
      expect(text(page, '[data-testid="board-beyond"]')).toBe('Also filtered by project=!OPS');

      select.triggerEventHandler('ngModelChange', ['COW']);

      expect(projectsWritten()).toEqual(['COW', '!OPS']);
    });

    it('clears every condition when none is chosen', async () => {
      const { fixture, page } = await render();
      await see(fixture, 'COW');
      await apply(fixture, saved({ project: ['COW'], severity: ['high'] }));

      await apply(fixture, null);

      expect(projectsWritten()).toBeNull();
      expect(paramsOf('COW')).toEqual({
        team: 'acme',
        project: 'COW',
        pages: Number.POSITIVE_INFINITY,
        horizon: ['now', 'release', 'next'],
      });
      expect(bar(fixture).applied()).toBeNull();
      expect(page.querySelector('[data-testid="board-beyond"]')).toBeNull();
    });

    it('starts another tenant without the filter applied', async () => {
      const { fixture } = await render();
      await apply(fixture, saved({ severity: ['high'] }));

      tenant.set('globex');
      await fixture.whenStable();

      expect(bar(fixture).applied()).toBeNull();
      expect(bar(fixture).current()).toEqual({});
    });
  });

  describe('a drag between the columns of a swimlane', () => {
    it('is the transition of the matrix, as on the project board', async () => {
      const { fixture, page } = await render();
      await load(fixture, 'COW', [ticket('COW', 1, { state: 'analysed' })]);

      drop(fixture, 'COW', 1, 'ready');
      await settle(fixture);

      expect(transition).toHaveBeenCalledExactlyOnceWith('acme/COW-1', { to: 'decided' });
      expect(cardsIn(page, 'COW', 'ready')).toEqual([1]);
      expect(text(page, '[data-testid="status"]')).toBe('COW-1 moved to Ready');
    });

    it('asks for what a move needs with the dialog of the project board', async () => {
      const { fixture, page } = await render();
      await load(fixture, 'OPS', [ticket('OPS', 4, { state: 'in-progress' })]);

      drop(fixture, 'OPS', 4, 'refinement');
      await settle(fixture);

      const dialog = fixture.debugElement.query(By.directive(MoveDialog))
        ?.componentInstance as MoveDialog;
      expect(transition).not.toHaveBeenCalled();
      expect(dialog.request()).toEqual({
        kind: 'move',
        move: expect.objectContaining({ label: 'Back to analysed', to: 'analysed' }),
      });
      expect(dialog.ticket().key).toBe('acme/OPS-4');
      expect(cardsIn(page, 'OPS', 'refinement')).toEqual([4]);
    });

    it('puts the card back and loads its own swimlane again when the server refuses', async () => {
      transition.mockRejectedValueOnce(
        refusal(409, 'state_conflict', 'The ticket moved meanwhile', 'It is decided now.'),
      );
      const { fixture, page } = await render();
      await load(fixture, 'COW', [ticket('COW', 1, { state: 'analysed' })]);
      await load(fixture, 'OPS', [ticket('OPS', 1)]);

      drop(fixture, 'COW', 1, 'ready');
      await settle(fixture);

      expect(cardsIn(page, 'COW', 'refinement')).toEqual([1]);
      expect(toast).toHaveBeenCalledWith(
        expect.objectContaining({ summary: 'The ticket moved meanwhile' }),
      );
      expect(listOf('COW')?.reload).toHaveBeenCalled();
      expect(listOf('OPS')?.reload).not.toHaveBeenCalled();
    });

    it('keeps the lists of each swimlane to itself: a card is dragged only among its own columns', async () => {
      const { fixture } = await render();
      await load(fixture, 'COW', [ticket('COW', 1)]);
      await load(fixture, 'OPS', [ticket('OPS', 1)]);

      // The CDK connects the lists of one group, and each swimlane is a group of its own.
      for (const key of ['COW', 'OPS']) {
        const own = laneDebug(fixture, key);
        const groups = own.queryAll(By.directive(CdkDropListGroup));
        expect(groups).toHaveLength(1);
        const lists = [...groups[0].injector.get(CdkDropListGroup<CdkDropList>)._items];
        expect(lists).toHaveLength(5);
        expect(
          lists.every((list) =>
            (own.nativeElement as HTMLElement).contains(list.element.nativeElement),
          ),
        ).toBe(true);
        expect(lists.every((list) => list.connectedTo.length === 0)).toBe(true);
      }
    });
  });

  describe('a drag across swimlanes, which is refused (docs/adr/0018 D4)', () => {
    async function dragging() {
      const result = await render();
      await load(result.fixture, 'COW', [ticket('COW', 1, { state: 'analysed' })]);
      await load(result.fixture, 'OPS', [ticket('OPS', 2)]);
      card(result.fixture, 'COW', 1).triggerEventHandler('cdkDragStarted', {});
      await result.fixture.whenStable();
      return result;
    }

    function move(fixture: ComponentFixture<TenantBoard>) {
      card(fixture, 'COW', 1).triggerEventHandler('cdkDragMoved', {
        pointerPosition: { x: 10, y: 20 },
      });
    }

    function letGo(fixture: ComponentFixture<TenantBoard>) {
      card(fixture, 'COW', 1).triggerEventHandler('cdkDragEnded', { dropPoint: { x: 10, y: 20 } });
    }

    it('steps the other swimlanes back while a card is dragged', async () => {
      const { fixture, page } = await dragging();

      expect(lane(page, 'OPS')?.classList).toContain('elsewhere');
      expect(lane(page, 'COW')?.classList).not.toContain('elsewhere');

      letGo(fixture);
      await fixture.whenStable();
      expect(lane(page, 'OPS')?.classList).not.toContain('elsewhere');
    });

    it('holds every swimlane still while a card is dragged, and shows what changed once it is let go', async () => {
      const { fixture, page } = await dragging();

      await load(fixture, 'OPS', [ticket('OPS', 2), ticket('OPS', 3)]);
      expect(cardsIn(page, 'OPS', 'in-progress')).toEqual([2]);

      letGo(fixture);
      await fixture.whenStable();
      expect(cardsIn(page, 'OPS', 'in-progress')).toEqual([2, 3]);
    });

    it('has the swimlane under the card say no, and only while the card is over it', async () => {
      const { fixture, page } = await dragging();

      pointerOver(page, 'OPS');
      move(fixture);
      await fixture.whenStable();
      expect(pointAt).toHaveBeenCalledWith(10, 20);
      expect(lane(page, 'OPS')?.classList).toContain('refusing');
      expect(text(lane(page, 'OPS'), '[data-testid="lane-refusal"]')).toBe(
        'A ticket never changes project on a board',
      );
      expect(lane(page, 'COW')?.querySelector('[data-testid="lane-refusal"]')).toBeNull();

      pointerOver(page, 'COW');
      move(fixture);
      await fixture.whenStable();
      expect(lane(page, 'OPS')?.classList).not.toContain('refusing');
      expect(page.querySelector('[data-testid="lane-refusal"]')).toBeNull();

      pointerOver(page, null);
      move(fixture);
      await fixture.whenStable();
      expect(page.querySelector('[data-testid="lane-refusal"]')).toBeNull();
    });

    it('refuses the card let go over another swimlane, and a toast says why; nothing is written', async () => {
      const { fixture, page } = await dragging();
      pointerOver(page, 'OPS');
      move(fixture);
      await fixture.whenStable();

      letGo(fixture);
      // The CDK reports the drop to the card's own list, the pointer over none of it.
      const own = laneDebug(fixture, 'COW')
        .queryAll(By.directive(CdkDropList))
        .find((each) => each.injector.get(CdkDropList).data === 'refinement');
      own?.triggerEventHandler('cdkDropListDropped', {
        item: { data: card(fixture, 'COW', 1).injector.get(CdkDrag).data },
        container: own.injector.get(CdkDropList),
        previousContainer: own.injector.get(CdkDropList),
        isPointerOverContainer: false,
      });
      await settle(fixture);

      expect(toast).toHaveBeenCalledExactlyOnceWith({
        severity: 'warn',
        summary: 'Not moved',
        detail: 'A ticket never changes project on a board: COW-1 stays in COW.',
        life: 6000,
      });
      expect(transition).not.toHaveBeenCalled();
      expect(cardsIn(page, 'COW', 'refinement')).toEqual([1]);
      expect(cardsIn(page, 'OPS', 'refinement')).toEqual([]);
      expect(page.querySelector('[data-testid="lane-refusal"]')).toBeNull();
      expect(lane(page, 'OPS')?.classList).not.toContain('elsewhere');
    });

    it('tells nothing of a card let go over its own swimlane or beside every one', async () => {
      const { fixture, page } = await dragging();

      pointerOver(page, 'COW');
      letGo(fixture);
      await fixture.whenStable();
      card(fixture, 'COW', 1).triggerEventHandler('cdkDragStarted', {});
      pointerOver(page, null);
      letGo(fixture);
      await fixture.whenStable();

      expect(toast).not.toHaveBeenCalled();
    });

    it('offers in the card menu the columns of its own swimlane only', async () => {
      const { fixture, page } = await render();
      await load(fixture, 'COW', [ticket('COW', 1, { state: 'in-progress' })]);

      lane(page, 'COW')
        ?.querySelector<HTMLElement>('[data-testid="card-menu-acme/COW-1"]')
        ?.click();
      await fixture.whenStable();

      const labels = fixture.debugElement
        .queryAll(By.css('p-menu'))
        .flatMap((each) =>
          ((each.componentInstance.model() ?? []) as { label?: string }[]).map(
            (item) => item.label,
          ),
        );
      expect(labels).toEqual(['Back to Refinement…', 'Back to Ready…', 'Block…', 'Move to Review']);
    });
  });
});
