import { CdkDrag, CdkDragDrop, CdkDropList } from '@angular/cdk/drag-drop';
import { ScrollDispatcher } from '@angular/cdk/scrolling';
import { HttpErrorResponse } from '@angular/common/http';
import { signal, WritableSignal } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { By } from '@angular/platform-browser';
import { provideRouter, Router } from '@angular/router';
import { MenuItem, MessageService } from 'primeng/api';
import { Menu } from 'primeng/menu';
import { Tooltip } from 'primeng/tooltip';
import type { Mock, MockInstance } from 'vitest';
import { ListProjectTickets$Params } from '../../api/fn/tickets/list-project-tickets';
import { Block, Horizon, Problem, Project, Ticket, TicketState } from '../../api/models';
import { EntityCache } from '../../core/entity-cache';
import { ImportsService } from '../../core/imports.service';
import { MembersService } from '../../core/members.service';
import { ProblemView } from '../../core/problem.service';
import { ProjectsService } from '../../core/projects.service';
import { SessionService } from '../../core/session.service';
import { TenantService } from '../../core/tenant.service';
import { StaleWrite, TicketActions } from '../../core/ticket-actions.service';
import { ProjectTicketPagesParams, TicketPage, TicketsService } from '../../core/tickets.service';
import { Clock } from '../../shared/time';
import { MoveDialog } from '../ticket/move-dialog';
import { Board } from './board';
import { ColumnId } from './board-model';
import { ProjectHeader } from './project-header';

/** 2026-10-03, 12:34 UTC: the count of the done tickets starts at 2026-09-19, 12:00. */
const now = Date.parse('2026-10-03T12:34:56Z');

function ticket(number: number, fields: Partial<Ticket> = {}): Ticket {
  return {
    id: `id-${number}`,
    key: `acme/COW-${number}`,
    number,
    project: 'COW',
    title: `Ticket ${number}`,
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
    score: null,
    score_version: null,
    version: 1,
    ...fields,
  } as Ticket;
}

const blocked = (number: number, from: TicketState, fields: Partial<Ticket> = {}) =>
  ticket(number, {
    state: 'blocked',
    block: { kind: 'external', from, reason: 'The provider' } as Block,
    ...fields,
  });

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
  let reject!: (error: unknown) => void;
  const promise = new Promise<T>((done, fail) => {
    resolve = done;
    reject = fail;
  });
  return { promise, resolve, reject };
}

const refusal = (status: number, code: Problem['code'], title: string, detail?: string) => {
  const body: Problem = { type: 'about:blank', title, status, code, ...(detail ? { detail } : {}) };
  return new HttpErrorResponse({ status, statusText: title, error: body });
};

describe('Board', () => {
  let tenant: WritableSignal<string | null>;
  let projects: WritableSignal<Project[]>;
  let cache: EntityCache<Ticket>;
  let open: FakeList;
  let done: FakeList;
  let openParams: () => ProjectTicketPagesParams | undefined;
  let doneParams: () => ListProjectTickets$Params | undefined;
  let transition: Mock<TicketActions['transition']>;
  let setHorizon: Mock<TicketActions['setHorizon']>;
  let navigate: MockInstance<Router['navigate']>;
  let toast: MockInstance<MessageService['add']>;

  beforeEach(() => {
    tenant = signal<string | null>('acme');
    projects = signal<Project[]>([cowork]);
    cache = new EntityCache<Ticket>();
    open = fakeList();
    done = fakeList();
    // The writes put their answers into the cache, as the real ones do, with a new version.
    transition = vi.fn<TicketActions['transition']>(async (key, move) => {
      const held = cache.value(key) as Ticket;
      const answer = { ...held, state: move.to, version: held.version + 1 } as Ticket;
      cache.put(key, answer);
      return answer;
    });
    setHorizon = vi.fn<TicketActions['setHorizon']>(async (key, value) => {
      const held = cache.value(key) as Ticket;
      const answer = {
        ...held,
        horizon: value,
        horizon_set: value === 'later' ? null : { value, at: 'then', reason: null },
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
          // No rank: a move on the board never changes the rank, and a call would fail.
          useValue: {
            create: vi.fn(),
            transition,
            setHorizon,
            update: vi.fn(),
          },
        },
        { provide: MembersService, useValue: { list: signal([]) } },
        // The header's export and its way to the import (docs/adr/0051).
        { provide: ImportsService, useValue: {} },
        { provide: TenantService, useValue: { isAdmin: signal(false) } },
        { provide: SessionService, useValue: { tenant } },
        {
          provide: ProjectsService,
          useValue: { byKey: (key: string) => projects().find((p) => p.key === key) },
        },
        {
          provide: TicketsService,
          useValue: {
            cache,
            projectTicketPages: (params: () => ProjectTicketPagesParams | undefined) => {
              openParams = params;
              return open;
            },
            projectTickets: (params: () => ListProjectTickets$Params | undefined) => {
              doneParams = params;
              return done;
            },
          },
        },
        { provide: Clock, useValue: { now: signal(now) } },
      ],
    });
    const router = TestBed.inject(Router);
    navigate = vi.spyOn(router, 'navigate').mockResolvedValue(true);
    toast = vi.spyOn(TestBed.inject(MessageService), 'add');
  });

  /** Puts the tickets into the cache and shows their keys, in this rank, as the list answered. */
  function load(tickets: Ticket[]) {
    for (const each of tickets) {
      cache.put(each.key, each);
    }
    open.value.set({
      keys: tickets.map((each) => each.key),
      nextCursor: null,
      versions: new Map(tickets.map((each) => [each.key, each.version])),
    });
  }

  async function render(project = 'COW') {
    const fixture = TestBed.createComponent(Board);
    fixture.componentRef.setInput('project', project);
    await fixture.whenStable();
    return { fixture, page: fixture.nativeElement as HTMLElement };
  }

  /** Lets what a drop or a click started finish, and shows it. */
  async function settle(fixture: ComponentFixture<Board>) {
    for (let round = 0; round < 3; round++) {
      await new Promise((resolve) => setTimeout(resolve));
      fixture.detectChanges();
    }
    await fixture.whenStable();
  }

  const text = (page: HTMLElement, selector: string) =>
    page.querySelector(selector)?.textContent?.replace(/\s+/g, ' ').trim();

  const numberOf = (card: Element) => Number(card.getAttribute('data-testid')?.split('-').pop());

  /** The numbers of the cards of a column, in the order shown. */
  const cardsIn = (page: HTMLElement, column: ColumnId | 'next') =>
    [...page.querySelectorAll(`[data-testid="column-${column}"] app-board-card`)].map(numberOf);

  const columnsShown = (page: HTMLElement) =>
    Object.fromEntries(
      (['next', 'refinement', 'ready', 'in-progress', 'blocked', 'review'] as const).map((id) => [
        id,
        cardsIn(page, id),
      ]),
    ) as Record<ColumnId | 'next', number[]>;

  const dropLists = (fixture: ComponentFixture<Board>) =>
    fixture.debugElement.queryAll(By.directive(CdkDropList));

  const listFor = (fixture: ComponentFixture<Board>, column: ColumnId) =>
    dropLists(fixture).find((each) => each.injector.get(CdkDropList).data === column);

  const card = (fixture: ComponentFixture<Board>, number: number) =>
    fixture.debugElement.query(By.css(`[data-testid="card-acme/COW-${number}"]`));

  /** A drop of the card on a column, as the CDK reports it. */
  function drop(fixture: ComponentFixture<Board>, number: number, to: ColumnId, over = true) {
    const dragged = card(fixture, number);
    const source = dropLists(fixture).find((each) =>
      (each.nativeElement as HTMLElement).contains(dragged.nativeElement),
    );
    const target = listFor(fixture, to);
    const event = {
      item: { data: dragged.injector.get(CdkDrag).data },
      previousContainer: source?.injector.get(CdkDropList),
      container: target?.injector.get(CdkDropList),
      previousIndex: 0,
      currentIndex: 0,
      isPointerOverContainer: over,
      distance: { x: 0, y: 0 },
      dropPoint: { x: 0, y: 0 },
      event: new MouseEvent('mouseup'),
    } as unknown as CdkDragDrop<ColumnId, ColumnId, Ticket>;
    target?.triggerEventHandler('cdkDropListDropped', event);
  }

  function pickUp(fixture: ComponentFixture<Board>, number: number) {
    card(fixture, number).triggerEventHandler('cdkDragStarted', {});
  }

  function putDown(fixture: ComponentFixture<Board>, number: number) {
    card(fixture, number).triggerEventHandler('cdkDragEnded', {});
  }

  const dialog = (fixture: ComponentFixture<Board>) =>
    fixture.debugElement.query(By.directive(MoveDialog))?.componentInstance as
      MoveDialog | undefined;

  describe('loading', () => {
    it('shows the header of its project, with the tabs', async () => {
      const { fixture, page } = await render();

      expect(
        fixture.debugElement.query(By.directive(ProjectHeader)).componentInstance.project(),
      ).toBe('COW');
      expect(page.querySelector('[data-testid="tab-board"]')?.getAttribute('href')).toBe(
        '/t/acme/p/COW/board',
      );
    });

    it('asks for the open tickets of the horizon now, release and next, every page of them', async () => {
      await render();

      expect(openParams()).toEqual({
        tenant: 'acme',
        project: 'COW',
        pages: Number.POSITIVE_INFINITY,
        horizon: ['now', 'release', 'next'],
      });
    });

    it('asks for the tickets done in the last fourteen days, counted to the hour, with a total', async () => {
      await render();

      expect(doneParams()).toEqual({
        tenant: 'acme',
        project: 'COW',
        state: ['done'],
        done_after: '2026-09-19T12:00:00.000Z',
        page: 1,
        per_page: 25,
      });
    });

    it('asks for nothing outside a tenant', async () => {
      tenant.set(null);
      const { page } = await render();

      expect(openParams()).toBeUndefined();
      expect(doneParams()).toBeUndefined();
      expect(page.querySelector('[data-testid="board"]')).toBeNull();
    });

    it('asks for the project of its path', async () => {
      const { fixture } = await render();

      fixture.componentRef.setInput('project', 'OPS');
      await fixture.whenStable();

      expect(openParams()?.project).toBe('OPS');
      expect(doneParams()?.project).toBe('OPS');
    });

    it('says when the board could not be loaded', async () => {
      open.error.set(refusal(500, 'internal', 'Internal error', 'Something broke.'));
      const { page } = await render();

      expect(text(page, '[data-testid="failure"]')).toBe(
        'The board could not be loaded: Something broke.',
      );
    });

    it('shows that it loads', async () => {
      open.isLoading.set(true);
      const { page } = await render();

      expect(page.querySelector('[data-testid="board"]')?.classList).toContain('loading');
      expect(page.querySelector('[data-testid="board"]')?.getAttribute('aria-busy')).toBe('true');
    });

    it('keeps the cards of its project while the list loads again', async () => {
      load([ticket(1)]);
      const { fixture, page } = await render();

      open.value.set(undefined);
      open.isLoading.set(true);
      await fixture.whenStable();

      expect(cardsIn(page, 'in-progress')).toEqual([1]);
    });
  });

  describe('the header', () => {
    it('counts the tickets done in the last fourteen days and leads to them in the backlog', async () => {
      done.value.set({ keys: [], total: 7, nextCursor: null });
      const { page } = await render();

      const link = page.querySelector('[data-testid="done-count"]');
      expect(text(page, '[data-testid="done-count"]')).toBe('7 tickets done in the last 14 days');
      expect(link?.getAttribute('href')).toBe(
        '/t/acme/p/COW/backlog?closed=true&done_after=2026-09-19T12:00:00.000Z',
      );
    });

    it('counts one done ticket in the singular', async () => {
      done.value.set({ keys: [], total: 1, nextCursor: null });
      const { page } = await render();

      expect(text(page, '[data-testid="done-count"]')).toBe('1 ticket done in the last 14 days');
    });

    it('shows no count until it is loaded', async () => {
      const { page } = await render();

      expect(page.querySelector('[data-testid="done-count"]')).toBeNull();
    });

    it('says how many cards are on the board', async () => {
      load([ticket(1), ticket(2, { horizon: 'next' }), ticket(3, { horizon: 'later' })]);
      const { page } = await render();

      expect(text(page, '[data-testid="card-count"]')).toBe('2 on the board');
    });
  });

  describe('the columns (docs/adr/0018 D1)', () => {
    it('are Next, Refinement, Ready, In Progress, Blocked and Review', async () => {
      const { page } = await render();

      expect([...page.querySelectorAll('.column h2')].map((h) => h.textContent?.trim())).toEqual([
        'Next',
        'Refinement',
        'Ready',
        'In Progress',
        'Blocked',
        'Review',
      ]);
    });

    it('explain Next as the tickets in the horizon next', async () => {
      const { fixture } = await render();

      expect(
        fixture.debugElement.query(By.css('#column-next')).injector.get(Tooltip).content(),
      ).toBe(
        'Tickets in the horizon next, in whatever state; Now moves one to now, and it shows in the column of its state',
      );
    });

    it('put the open leaves of the horizon now and release in the column of their state, in rank, and next on the left', async () => {
      load([
        ticket(1, { state: 'review' }),
        ticket(2, { state: 'filed' }),
        ticket(3, { state: 'decided', horizon: 'release' }),
        ticket(4, { state: 'in-progress', horizon: 'next' }),
        ticket(5, { state: 'analysed' }),
        blocked(6, 'in-progress'),
        ticket(7, { state: 'in-progress' }),
        ticket(8, { state: 'filed', horizon: 'next' }),
      ]);

      const { page } = await render();

      expect(columnsShown(page)).toEqual({
        next: [4, 8],
        refinement: [2, 5],
        ready: [3],
        'in-progress': [7],
        blocked: [6],
        review: [1],
      });
    });

    it('leave out a parent, and a ticket the cache says is later now', async () => {
      load([ticket(1, { progress_derived: true }), ticket(2, { horizon: 'later' }), ticket(3)]);

      const { page } = await render();

      expect(cardsIn(page, 'in-progress')).toEqual([3]);
    });

    it('make the cards of next compact, and the others full, with their actions', async () => {
      load([ticket(1, { state: 'filed', horizon: 'next' }), ticket(2, { state: 'filed' })]);

      const { page } = await render();

      expect(page.querySelector('[data-testid="card-acme/COW-1"]')?.classList).toContain('compact');
      expect(page.querySelector('[data-testid="card-acme/COW-2"]')?.classList).not.toContain(
        'compact',
      );
      expect(page.querySelector('[data-testid="card-action-acme/COW-2"]')).not.toBeNull();
      expect(page.querySelector('[data-testid="card-menu-acme/COW-2"]')).not.toBeNull();
    });

    it('give no menu to a blocked card that does not say where it came from', async () => {
      load([ticket(1, { state: 'blocked', block: null })]);

      const { page } = await render();

      expect(cardsIn(page, 'blocked')).toEqual([1]);
      expect(page.querySelector('[data-testid="card-menu-acme/COW-1"]')).toBeNull();
    });

    it('say so when they hold nothing', async () => {
      const { page } = await render();

      expect(text(page, '[data-testid="empty-next"]')).toBe('Nothing is next');
      expect(text(page, '[data-testid="empty-review"]')).toBe('No tickets');
    });

    it('count their cards, and the next column too', async () => {
      load([ticket(1), ticket(2), ticket(3, { horizon: 'next' })]);

      const { page } = await render();

      expect(text(page, '[data-testid="count-in-progress"]')).toBe('2');
      expect(text(page, '[data-testid="count-next"]')).toBe('1');
      expect(
        page.querySelector('[data-testid="count-in-progress"]')?.getAttribute('aria-label'),
      ).toBe('2 tickets, no WIP limit');
    });

    it('count against the WIP limits of the project, Refinement against analysed, and mark one over it (docs/adr/0019 D3)', async () => {
      projects.set([
        { ...cowork, wip_limits: { analysed: 1, 'in-progress': 2, review: 3, decided: 1 } },
      ]);
      load([
        ticket(1, { state: 'filed' }),
        ticket(2, { state: 'analysed' }),
        ticket(3),
        ticket(4),
        ticket(5, { state: 'decided' }),
      ]);

      const { page } = await render();

      expect(text(page, '[data-testid="count-refinement"]')).toBe('2 / 1');
      expect(text(page, '[data-testid="count-in-progress"]')).toBe('2 / 2');
      expect(text(page, '[data-testid="count-review"]')).toBe('0 / 3');
      expect(page.querySelector('[data-testid="column-refinement"]')?.classList).toContain('over');
      expect(page.querySelector('[data-testid="count-refinement"]')?.classList).toContain('over');
      expect(page.querySelector('[data-testid="column-in-progress"]')?.classList).not.toContain(
        'over',
      );
      const label = (id: string) =>
        page.querySelector(`[data-testid="count-${id}"]`)?.getAttribute('aria-label');
      expect(label('refinement')).toBe('2 tickets, over its WIP limit of 1');
      expect(label('in-progress')).toBe('2 tickets, at its WIP limit of 2');
      expect(label('review')).toBe('0 tickets, under its WIP limit of 3');
      expect(label('ready')).toBe('1 ticket, at its WIP limit of 1');
    });

    it('move a card another person moved, as the cache learns of it', async () => {
      load([ticket(1, { state: 'decided' })]);
      const { fixture, page } = await render();

      cache.put('acme/COW-1', ticket(1, { state: 'in-progress', version: 2 }));
      await fixture.whenStable();

      expect(cardsIn(page, 'ready')).toEqual([]);
      expect(cardsIn(page, 'in-progress')).toEqual([1]);
    });
  });

  describe('a click on a card', () => {
    it('opens the ticket', async () => {
      load([ticket(12)]);
      const { page } = await render();

      page.querySelector<HTMLElement>('[data-testid="card-acme/COW-12"]')?.click();

      expect(navigate).toHaveBeenCalledExactlyOnceWith(['/t', 'acme', 'tickets', 'COW-12']);
    });

    it('opens the ticket of a card of next as well', async () => {
      load([ticket(12, { horizon: 'next' })]);
      const { page } = await render();

      page.querySelector<HTMLElement>('[data-testid="card-acme/COW-12"]')?.click();

      expect(navigate).toHaveBeenCalledExactlyOnceWith(['/t', 'acme', 'tickets', 'COW-12']);
    });

    it('opens nothing when it ends a drag', async () => {
      load([ticket(12)]);
      const { fixture, page } = await render();

      pickUp(fixture, 12);
      putDown(fixture, 12);
      page.querySelector<HTMLElement>('[data-testid="card-acme/COW-12"]')?.click();
      expect(navigate).not.toHaveBeenCalled();

      await settle(fixture);
      page.querySelector<HTMLElement>('[data-testid="card-acme/COW-12"]')?.click();
      expect(navigate).toHaveBeenCalledOnce();
    });
  });

  describe('a drag', () => {
    it('makes only the cards of the state columns draggable; next takes no drop', async () => {
      load([ticket(1), ticket(2, { horizon: 'next' })]);
      const { fixture } = await render();

      expect(
        fixture.debugElement
          .queryAll(By.directive(CdkDrag))
          .map((each) => numberOf(each.nativeElement)),
      ).toEqual([1]);
      expect(dropLists(fixture).map((each) => each.injector.get(CdkDropList).data)).toEqual([
        'refinement',
        'ready',
        'in-progress',
        'blocked',
        'review',
      ]);
    });

    it('scrolls the board sideways at its edges and follows its scroll: it is the scroll container of every column', async () => {
      const { fixture, page } = await render();

      // What a drop list asks for when it measures itself as a drag starts.
      const dispatcher = TestBed.inject(ScrollDispatcher);
      for (const list of dropLists(fixture)) {
        expect(
          dispatcher
            .getAncestorScrollContainers(list.nativeElement as HTMLElement)
            .map((scrollable) => scrollable.getElementRef().nativeElement),
        ).toEqual([page.querySelector('[data-testid="board"]')]);
      }
    });

    it('sorts nothing inside a column: no drop changes the rank', async () => {
      const { fixture } = await render();

      expect(
        dropLists(fixture).every((each) => each.injector.get(CdkDropList).sortingDisabled),
      ).toBe(true);
    });

    it('lets a card go only to the columns the matrix allows, and back to its own', async () => {
      load([ticket(1, { state: 'decided' })]);
      const { fixture } = await render();
      const drag = card(fixture, 1).injector.get(CdkDrag);
      const accepts = (column: ColumnId) => {
        const list = listFor(fixture, column)!.injector.get(CdkDropList);
        return list.enterPredicate(drag, list);
      };

      expect(accepts('refinement')).toBe(true);
      expect(accepts('ready')).toBe(true);
      expect(accepts('in-progress')).toBe(true);
      expect(accepts('blocked')).toBe(true);
      expect(accepts('review')).toBe(false);
    });

    it('marks the columns the card may go to while it is dragged, and the others step back', async () => {
      load([ticket(1, { state: 'review' })]);
      const { fixture, page } = await render();
      const marked = () =>
        Object.fromEntries(
          [...page.querySelectorAll('section.column:not(.next)')].map((column) => [
            column.getAttribute('data-testid'),
            column.classList.contains('target')
              ? 'target'
              : column.classList.contains('refused')
                ? 'refused'
                : '',
          ]),
        );

      pickUp(fixture, 1);

      expect(marked()).toEqual({
        'column-refinement': 'refused',
        'column-ready': 'refused',
        'column-in-progress': 'target',
        'column-blocked': 'target',
        'column-review': 'refused',
      });
      expect(page.querySelector('[data-testid="board"]')?.classList).toContain('dragging');
      expect(page.querySelector('[data-testid="column-next"]')?.classList).toContain('refused');

      putDown(fixture, 1);
      await fixture.whenStable();
      expect(Object.values(marked()).every((mark) => mark === '')).toBe(true);
      expect(page.querySelector('[data-testid="column-next"]')?.classList).not.toContain('refused');
    });

    it('holds the board while a card is dragged, and shows what changed meanwhile once it is put down', async () => {
      load([ticket(1), ticket(2, { state: 'decided' })]);
      const { fixture, page } = await render();

      pickUp(fixture, 1);
      cache.put('acme/COW-2', ticket(2, { state: 'in-progress', version: 2 }));
      load([ticket(1), ticket(2, { state: 'in-progress', version: 2 }), ticket(3)]);
      await fixture.whenStable();
      expect(columnsShown(page)['in-progress']).toEqual([1]);
      expect(columnsShown(page).ready).toEqual([2]);

      putDown(fixture, 1);
      await fixture.whenStable();
      expect(columnsShown(page)['in-progress']).toEqual([1, 2, 3]);
    });
  });

  describe('a drop (docs/adr/0009)', () => {
    it('makes the move of the matrix that needs nothing at once, and shows the card in its column before the answer', async () => {
      const answer = deferred<Ticket>();
      transition.mockImplementationOnce(() => answer.promise);
      load([ticket(1, { state: 'analysed' })]);
      const { fixture, page } = await render();

      drop(fixture, 1, 'ready');
      await fixture.whenStable();

      expect(transition).toHaveBeenCalledExactlyOnceWith('acme/COW-1', { to: 'decided' });
      expect(cardsIn(page, 'ready')).toEqual([1]);
      expect(cardsIn(page, 'refinement')).toEqual([]);

      const decided = ticket(1, { state: 'decided', version: 2 });
      cache.put(decided.key, decided);
      answer.resolve(decided);
      await settle(fixture);
      expect(cardsIn(page, 'ready')).toEqual([1]);
      expect(text(page, '[data-testid="status"]')).toBe('COW-1 moved to Ready');
    });

    it.each([
      ['decided', 'in-progress', 'in-progress'],
      ['in-progress', 'review', 'review'],
    ] as [TicketState, ColumnId, TicketState][])(
      'moves a %s card dropped on %s forward to %s',
      async (state, column, to) => {
        load([ticket(1, { state })]);
        const { fixture } = await render();

        drop(fixture, 1, column);
        await settle(fixture);

        expect(transition).toHaveBeenCalledExactlyOnceWith('acme/COW-1', { to });
      },
    );

    it('shows an unblocked card without its block while the move is on its way', async () => {
      const answer = deferred<Ticket>();
      transition.mockImplementationOnce(() => answer.promise);
      load([blocked(1, 'review')]);
      const { fixture, page } = await render();

      drop(fixture, 1, 'review');
      await fixture.whenStable();

      expect(cardsIn(page, 'review')).toEqual([1]);
      expect(
        page.querySelector('[data-testid="card-acme/COW-1"] [data-testid="card-block"]'),
      ).toBeNull();
      answer.resolve(ticket(1, { state: 'review', version: 2 }));
      await settle(fixture);
    });

    it('unblocks a card dropped on the column of the state it came from', async () => {
      load([blocked(1, 'review')]);
      const { fixture, page } = await render();

      drop(fixture, 1, 'review');
      await settle(fixture);

      expect(transition).toHaveBeenCalledExactlyOnceWith('acme/COW-1', { to: 'review' });
      expect(cardsIn(page, 'review')).toEqual([1]);
    });

    it('reads the card as the cache holds it now, not as it was picked up', async () => {
      load([ticket(1, { state: 'in-progress' })]);
      const { fixture, page } = await render();
      pickUp(fixture, 1);
      cache.put('acme/COW-1', ticket(1, { state: 'review', version: 2 }));

      // From in-progress, Ready is a step back; from review, where it went meanwhile, it is none.
      drop(fixture, 1, 'ready');
      await settle(fixture);

      expect(transition).not.toHaveBeenCalled();
      expect(dialog(fixture)).toBeUndefined();
      expect(cardsIn(page, 'review')).toEqual([1]);
    });

    it('puts the card back and says why when the server refuses', async () => {
      transition.mockRejectedValueOnce(
        refusal(409, 'state_conflict', 'The ticket moved meanwhile', 'It is decided now.'),
      );
      load([ticket(1, { state: 'analysed' })]);
      const { fixture, page } = await render();

      drop(fixture, 1, 'ready');
      await settle(fixture);

      expect(cardsIn(page, 'refinement')).toEqual([1]);
      expect(cardsIn(page, 'ready')).toEqual([]);
      expect(toast).toHaveBeenCalledWith(
        expect.objectContaining({
          summary: 'The ticket moved meanwhile',
          detail: 'It is decided now.',
        }),
      );
      expect(open.reload).toHaveBeenCalled();
    });

    it('does nothing for a card dropped beside the board, or back in its own column', async () => {
      load([ticket(1, { state: 'analysed' }), ticket(2, { state: 'filed' })]);
      const { fixture, page } = await render();

      drop(fixture, 1, 'ready', false);
      drop(fixture, 2, 'refinement');
      await settle(fixture);

      expect(transition).not.toHaveBeenCalled();
      expect(cardsIn(page, 'refinement')).toEqual([1, 2]);
    });

    it('does nothing for a column the matrix refuses, should one ever report a drop', async () => {
      load([ticket(1, { state: 'filed' })]);
      const { fixture, page } = await render();

      drop(fixture, 1, 'review');
      await settle(fixture);

      expect(transition).not.toHaveBeenCalled();
      expect(cardsIn(page, 'refinement')).toEqual([1]);
    });

    describe('that needs input', () => {
      async function dropped(state: TicketState, column: ColumnId) {
        load([ticket(1, { state })]);
        const result = await render();
        drop(result.fixture, 1, column);
        await settle(result.fixture);
        return result;
      }

      it.each([
        ['review', 'in-progress', 'Back to in-progress', 'in-progress'],
        ['in-progress', 'refinement', 'Back to analysed', 'analysed'],
        ['decided', 'refinement', 'Back to analysed', 'analysed'],
        ['in-progress', 'ready', 'Back to decided', 'decided'],
        ['filed', 'blocked', 'Block', 'blocked'],
      ] as [TicketState, ColumnId, string, TicketState][])(
        'asks for it first: a %s card dropped on %s asks for %s',
        async (state, column, label, to) => {
          const { fixture, page } = await dropped(state, column);

          expect(transition).not.toHaveBeenCalled();
          expect(dialog(fixture)?.request()).toEqual({
            kind: 'move',
            move: expect.objectContaining({ label, to }),
          });
          expect(dialog(fixture)?.ticket().key).toBe('acme/COW-1');
          expect(cardsIn(page, column)).toEqual([1]);
        },
      );

      it('shows the card as the move makes it while it asks: the state, and the bar of that state', async () => {
        load([ticket(1, { state: 'in-progress', progress: 60, progress_refinement: 100 })]);
        const { fixture, page } = await render();

        drop(fixture, 1, 'refinement');
        await settle(fixture);

        const shown = page.querySelector('[data-testid="card-acme/COW-1"]');
        expect(shown?.querySelector('[data-state]')?.getAttribute('data-state')).toBe('analysed');
        expect(shown?.querySelector('app-stage-bar')?.getAttribute('aria-label')).toBe(
          'Refinement 100%',
        );
        expect(dialog(fixture)?.ticket().state).toBe('in-progress');
      });

      it('puts the card back when the person cancels', async () => {
        const { fixture, page } = await dropped('review', 'in-progress');

        dialog(fixture)?.closed.emit(false);
        await settle(fixture);

        expect(dialog(fixture)).toBeUndefined();
        expect(cardsIn(page, 'review')).toEqual([1]);
        expect(text(page, '[data-testid="status"]')).toBe('');
      });

      it('shows the card where the move the dialog wrote put it', async () => {
        const { fixture, page } = await dropped('review', 'in-progress');

        cache.put('acme/COW-1', ticket(1, { state: 'in-progress', version: 2 }));
        dialog(fixture)?.closed.emit(true);
        await settle(fixture);

        expect(dialog(fixture)).toBeUndefined();
        expect(cardsIn(page, 'in-progress')).toEqual([1]);
        expect(text(page, '[data-testid="status"]')).toBe('COW-1 moved to In Progress');
      });
    });
  });

  describe('the card action', () => {
    it('moves a filed card to analysed, where it stays in Refinement', async () => {
      load([ticket(1, { state: 'filed' })]);
      const { fixture, page } = await render();

      page.querySelector<HTMLElement>('[data-testid="card-action-acme/COW-1"]')?.click();
      await settle(fixture);

      expect(transition).toHaveBeenCalledExactlyOnceWith('acme/COW-1', { to: 'analysed' });
      expect(cardsIn(page, 'refinement')).toEqual([1]);
      expect(navigate).not.toHaveBeenCalled();
    });

    it('keeps the keyboard on the card, whose button goes with the state it leaves: on its menu button', async () => {
      load([ticket(1, { state: 'filed' })]);
      const { fixture, page } = await render();
      const button = page.querySelector<HTMLElement>('[data-testid="card-action-acme/COW-1"]');

      button?.focus();
      button?.click();
      await settle(fixture);

      expect(page.querySelector('[data-testid="card-action-acme/COW-1"]')).toBeNull();
      expect(document.activeElement).toBe(
        page.querySelector('[data-testid="card-menu-acme/COW-1"]'),
      );
    });
  });

  describe('the card menu, the keyboard way of a drag', () => {
    async function opened(current: Ticket) {
      load([current]);
      const result = await render();
      result.page.querySelector<HTMLElement>(`[data-testid="card-menu-${current.key}"]`)?.click();
      await result.fixture.whenStable();
      const menu = result.fixture.debugElement.query(By.directive(Menu)).componentInstance as Menu;
      return { ...result, items: (menu.model() ?? []) as MenuItem[] };
    }

    it('offers the card action and the drops the matrix allows', async () => {
      const { items } = await opened(ticket(1, { state: 'filed' }));

      expect(items.map((item) => item.label)).toEqual(['Move to analysed', 'Block…']);
    });

    it('makes the move that needs nothing at once, and gives the keyboard back to the card', async () => {
      const { fixture, page, items } = await opened(ticket(1, { state: 'in-progress' }));

      items.find((item) => item.label === 'Move to Review')?.command?.({});
      await settle(fixture);

      expect(transition).toHaveBeenCalledExactlyOnceWith('acme/COW-1', { to: 'review' });
      expect(cardsIn(page, 'review')).toEqual([1]);
      expect(document.activeElement).toBe(
        page.querySelector('[data-testid="card-menu-acme/COW-1"]'),
      );
    });

    it('asks for what a move needs, and gives the keyboard back to the card when the dialog ends', async () => {
      const { fixture, page, items } = await opened(ticket(1, { state: 'in-progress' }));

      items.find((item) => item.label === 'Block…')?.command?.({});
      await settle(fixture);
      expect(dialog(fixture)?.request()).toEqual({
        kind: 'move',
        move: expect.objectContaining({ to: 'blocked', input: 'block' }),
      });
      expect(cardsIn(page, 'blocked')).toEqual([1]);

      dialog(fixture)?.closed.emit(false);
      await settle(fixture);
      expect(cardsIn(page, 'in-progress')).toEqual([1]);
      expect(document.activeElement).toBe(
        page.querySelector('[data-testid="card-menu-acme/COW-1"]'),
      );
    });
  });

  describe('now, the way into the state columns (docs/adr/0018 D1)', () => {
    it('sets the horizon now without a reason, and moves the card to the column of its state at once', async () => {
      const answer = deferred<Ticket>();
      setHorizon.mockImplementationOnce(() => answer.promise);
      load([ticket(1, { state: 'review', horizon: 'next' })]);
      const { fixture, page } = await render();

      page.querySelector<HTMLElement>('[data-testid="card-now-acme/COW-1"]')?.click();
      await fixture.whenStable();

      expect(setHorizon).toHaveBeenCalledExactlyOnceWith('acme/COW-1', 'now');
      expect(cardsIn(page, 'next')).toEqual([]);
      expect(cardsIn(page, 'review')).toEqual([1]);
      expect(navigate).not.toHaveBeenCalled();

      const now = ticket(1, { state: 'review', horizon: 'now', version: 2 });
      cache.put(now.key, now);
      answer.resolve(now);
      await settle(fixture);
      expect(cardsIn(page, 'review')).toEqual([1]);
      expect(text(page, '[data-testid="status"]')).toBe('COW-1 moved to Review');
    });

    it('sets the horizon now whatever the ticket held before, nothing deriving it', async () => {
      load([ticket(1, { horizon: 'next', horizon_set: { value: 'next', at: 'then', reason: 'x' } })]);
      const { fixture, page } = await render();

      page.querySelector<HTMLElement>('[data-testid="card-now-acme/COW-1"]')?.click();
      await settle(fixture);

      expect(setHorizon).toHaveBeenCalledExactlyOnceWith('acme/COW-1', 'now');
      expect(cardsIn(page, 'in-progress')).toEqual([1]);
    });

    it('puts the card back into next and says why when the server refuses', async () => {
      setHorizon.mockRejectedValueOnce(refusal(403, 'forbidden', 'Forbidden', 'Not yours.'));
      load([ticket(1, { horizon: 'next' })]);
      const { fixture, page } = await render();

      page.querySelector<HTMLElement>('[data-testid="card-now-acme/COW-1"]')?.click();
      await settle(fixture);

      expect(cardsIn(page, 'next')).toEqual([1]);
      expect(toast).toHaveBeenCalledWith(expect.objectContaining({ detail: 'Not yours.' }));
    });

    it('takes the keyboard with the card to its column, onto its menu button, before the answer', async () => {
      const answer = deferred<Ticket>();
      setHorizon.mockImplementationOnce(() => answer.promise);
      load([ticket(1, { state: 'in-progress', horizon: 'next' })]);
      const { fixture, page } = await render();
      const button = page.querySelector<HTMLElement>('[data-testid="card-now-acme/COW-1"]');

      button?.focus();
      button?.click();
      await fixture.whenStable();

      expect(cardsIn(page, 'in-progress')).toEqual([1]);
      expect(document.activeElement).toBe(
        page.querySelector('[data-testid="card-menu-acme/COW-1"]'),
      );

      const now = ticket(1, { state: 'in-progress', horizon: 'now', version: 2 });
      cache.put(now.key, now);
      answer.resolve(now);
      await settle(fixture);
      expect(document.activeElement).toBe(
        page.querySelector('[data-testid="card-menu-acme/COW-1"]'),
      );
    });

    it('brings the keyboard back to the button Now when the server refuses', async () => {
      const answer = deferred<Ticket>();
      setHorizon.mockImplementationOnce(() => answer.promise);
      load([ticket(1, { horizon: 'next' })]);
      const { fixture, page } = await render();
      const button = page.querySelector<HTMLElement>('[data-testid="card-now-acme/COW-1"]');

      button?.focus();
      button?.click();
      await fixture.whenStable();
      expect(cardsIn(page, 'in-progress')).toEqual([1]);

      answer.reject(refusal(403, 'forbidden', 'Forbidden', 'Not yours.'));
      await settle(fixture);

      expect(cardsIn(page, 'next')).toEqual([1]);
      expect(document.activeElement).toBe(
        page.querySelector('[data-testid="card-now-acme/COW-1"]'),
      );
    });

    it('says when somebody else decided its horizon meanwhile', async () => {
      const problem: ProblemView = {
        status: 412,
        code: 'version_mismatch',
        title: 'Changed meanwhile',
        detail: '',
        fields: {},
        current: {},
      };
      setHorizon.mockRejectedValueOnce(
        new StaleWrite(problem, ticket(1, { horizon: 'later' as Horizon })),
      );
      load([ticket(1, { horizon: 'next' })]);
      const { fixture } = await render();

      (fixture.nativeElement as HTMLElement)
        .querySelector<HTMLElement>('[data-testid="card-now-acme/COW-1"]')
        ?.click();
      await settle(fixture);

      expect(toast).toHaveBeenCalledWith(
        expect.objectContaining({
          severity: 'warn',
          summary: 'Changed meanwhile',
          detail: 'COW-1 was changed by someone else: its horizon is later now.',
        }),
      );
    });
  });
});
