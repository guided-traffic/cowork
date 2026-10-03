import { HttpErrorResponse } from '@angular/common/http';
import { signal, WritableSignal } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { By } from '@angular/platform-browser';
import { provideRouter, Router, UrlTree } from '@angular/router';
import { MessageService } from 'primeng/api';
import { Select } from 'primeng/select';
import { Table, TableLazyLoadEvent } from 'primeng/table';
import { Tooltip } from 'primeng/tooltip';
import type { MockInstance } from 'vitest';
import { Problem, Project, Ticket, TicketState } from '../../api/models';
import { EntityCache } from '../../core/entity-cache';
import { MembersService } from '../../core/members.service';
import { ProjectsService } from '../../core/projects.service';
import { SessionService } from '../../core/session.service';
import { TicketActions } from '../../core/ticket-actions.service';
import { TicketPage, TicketsService } from '../../core/tickets.service';
import { Clock } from '../../shared/time';
import { NewTicketDialog } from '../ticket/new-ticket-dialog';
import { Backlog } from './backlog';

const now = Date.parse('2026-10-03T12:00:00Z');
const ada = { id: 'p1', display_name: 'Ada Lovelace', username: 'local:ada' };
const sam = { id: 'p2', display_name: 'Sam Rivera', username: 'local:sam' };

function ticket(number: number, overrides: Partial<Ticket> = {}): Ticket {
  return {
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
    parent: null,
    effort: 'M',
    progress: 0,
    progress_derived: false,
    urgency: 'later',
    urgency_derived: 'later',
    urgency_override: null,
    urgency_rule: 'v1:default',
    opened_at: '2026-10-01T09:00:00Z',
    created_at: '2026-10-01T09:00:00Z',
    updated_at: '2026-10-03T11:55:00Z',
    decided_at: null,
    done_at: null,
    version: 1,
    ...overrides,
  };
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

interface ListParams {
  tenant: string;
  project: string;
  page: number;
  per_page: number;
  include_terminal?: boolean;
  state?: TicketState[];
  q?: string;
}

describe('Backlog', () => {
  let tenant: WritableSignal<string | null>;
  let projects: WritableSignal<Project[]>;
  let cache: EntityCache<Ticket>;
  let list: {
    value: WritableSignal<TicketPage | undefined>;
    hasValue: () => boolean;
    isLoading: WritableSignal<boolean>;
    error: WritableSignal<unknown>;
    reload: MockInstance<() => boolean>;
  };
  let listParams: () => ListParams | undefined;
  let navigate: MockInstance<Router['navigate']>;
  let navigateByUrl: MockInstance<Router['navigateByUrl']>;
  let warn: MockInstance<typeof console.warn>;

  beforeEach(() => {
    // The dialog for a new ticket is part of the page, and the select buttons of PrimeNG in its
    // form keep an `ngModel` each, which Angular warns about in development builds (NG01354).
    const original = console.warn.bind(console);
    warn = vi.spyOn(console, 'warn').mockImplementation((...args: unknown[]) => {
      if (!String(args[0]).includes('NG01354')) {
        original(...args);
      }
    });
    tenant = signal<string | null>('acme');
    projects = signal<Project[]>([cowork]);
    cache = new EntityCache<Ticket>();
    const value = signal<TicketPage | undefined>(undefined);
    list = {
      value,
      hasValue: () => value() !== undefined,
      isLoading: signal(false),
      error: signal<unknown>(undefined),
      reload: vi.fn<() => boolean>(() => true),
    };
    TestBed.configureTestingModule({
      providers: [
        provideRouter([]),
        MessageService,
        { provide: TicketActions, useValue: { create: vi.fn() } },
        { provide: MembersService, useValue: { list: signal([]) } },
        { provide: SessionService, useValue: { tenant } },
        {
          provide: ProjectsService,
          useValue: { byKey: (key: string) => projects().find((p) => p.key === key) },
        },
        {
          provide: TicketsService,
          useValue: {
            cache,
            projectTickets: (params: () => ListParams | undefined) => {
              listParams = params;
              return list;
            },
          },
        },
        { provide: Clock, useValue: { now: signal(now) } },
      ],
    });
    const router = TestBed.inject(Router);
    navigate = vi.spyOn(router, 'navigate').mockResolvedValue(true);
    navigateByUrl = vi.spyOn(router, 'navigateByUrl').mockResolvedValue(true);
  });

  afterEach(() => warn.mockRestore());

  /** Puts the tickets into the cache and shows their keys as the one page the list answered. */
  function load(tickets: Ticket[], total = tickets.length) {
    for (const each of tickets) {
      cache.put(each.key, each);
    }
    list.value.set({ keys: tickets.map((each) => each.key), total, nextCursor: null });
  }

  async function render(project = 'COW') {
    const fixture = TestBed.createComponent(Backlog);
    fixture.componentRef.setInput('project', project);
    await fixture.whenStable();
    return { fixture, page: fixture.nativeElement as HTMLElement };
  }

  const text = (page: HTMLElement, selector: string) =>
    page.querySelector(selector)?.textContent?.replace(/\s+/g, ' ').trim();

  const lazyLoad = (fixture: ComponentFixture<Backlog>, event: TableLazyLoadEvent) =>
    fixture.debugElement.query(By.directive(Table)).triggerEventHandler('onLazyLoad', event);

  /** What choosing states in the filter, or clearing it (which gives null), tells its model. */
  const chooseStates = (fixture: ComponentFixture<Backlog>, states: TicketState[] | null) =>
    fixture.debugElement
      .query(By.css('[data-testid="state-filter"]'))
      .triggerEventHandler('ngModelChange', states);

  function type(page: HTMLElement, value: string) {
    const input = page.querySelector<HTMLInputElement>('[data-testid="search"]');
    if (!input) {
      throw new Error('the search field is missing');
    }
    input.value = value;
    input.dispatchEvent(new Event('input'));
  }

  describe('the header', () => {
    it('shows the key, the name and the description of the project', async () => {
      const { page } = await render();

      expect(text(page, '.head .key')).toBe('COW');
      expect(text(page, 'h1')).toBe('Cowork');
      expect(text(page, '.head p.muted')).toBe('The tool itself');
    });

    it('shows the key in place of the name and no description while the project is not known', async () => {
      projects.set([]);

      const { page } = await render();

      expect(text(page, 'h1')).toBe('COW');
      expect(page.querySelector('.head p.muted')).toBeNull();
    });

    it('shows no description for a project that has none', async () => {
      projects.set([{ ...cowork, description: '' }]);

      const { page } = await render();

      expect(page.querySelector('.head p.muted')).toBeNull();
    });

    it('follows the project of the path', async () => {
      const { fixture, page } = await render();

      fixture.componentRef.setInput('project', 'OPS');
      await fixture.whenStable();

      expect(text(page, '.head .key')).toBe('OPS');
      expect(text(page, 'h1')).toBe('OPS');
    });
  });

  describe('the list request', () => {
    it('asks for every ticket of the project, finished ones included, on the first page of 25', async () => {
      await render();

      const params = listParams();
      expect(params).toMatchObject({
        tenant: 'acme',
        project: 'COW',
        page: 1,
        per_page: 25,
        include_terminal: true,
      });
      expect(params?.state).toBeUndefined();
      expect(params?.q).toBeUndefined();
    });

    it('asks for nothing outside a tenant', async () => {
      tenant.set(null);

      await render();

      expect(listParams()).toBeUndefined();
    });

    it('asks for the project of the path', async () => {
      const { fixture } = await render();

      fixture.componentRef.setInput('project', 'OPS');
      await fixture.whenStable();

      expect(listParams()?.project).toBe('OPS');
    });

    // The router reuses the page when only `:project` changes (see app.routes.spec.ts), so the page
    // number the person was on in one project must not be asked for in the next.
    it('goes back to the first page when the project changes', async () => {
      const { fixture } = await render();
      lazyLoad(fixture, { first: 50, rows: 25 });
      expect(listParams()?.page).toBe(3);

      fixture.componentRef.setInput('project', 'OPS');
      await fixture.whenStable();

      expect(listParams()).toMatchObject({ project: 'OPS', page: 1 });
    });

    it('asks for the chosen states only and no longer for finished tickets as such', async () => {
      const { fixture } = await render();

      chooseStates(fixture, ['blocked', 'in-progress']);

      const params = listParams();
      expect(params?.state).toEqual(['blocked', 'in-progress']);
      expect(params?.include_terminal).toBeUndefined();
    });

    it('goes back to the first page when the states change', async () => {
      const { fixture } = await render();
      lazyLoad(fixture, { first: 50, rows: 25 });
      expect(listParams()?.page).toBe(3);

      chooseStates(fixture, ['done']);

      expect(listParams()?.page).toBe(1);
    });

    it('asks for every state again when the filter is cleared, which hands on null', async () => {
      const { fixture } = await render();
      chooseStates(fixture, ['done']);

      chooseStates(fixture, null);

      const params = listParams();
      expect(params?.state).toBeUndefined();
      expect(params?.include_terminal).toBe(true);
    });

    it('asks for every state again when the choice is emptied', async () => {
      const { fixture } = await render();
      chooseStates(fixture, ['done']);

      chooseStates(fixture, []);

      const params = listParams();
      expect(params?.state).toBeUndefined();
      expect(params?.include_terminal).toBe(true);
    });

    it('offers the seven states in board order, each with its meaning', async () => {
      const { fixture } = await render();

      const options = fixture.debugElement.query(By.css('[data-testid="state-filter"]'))
        .componentInstance as Select;
      expect(options.multiple()).toBe(true);
      expect(options.options()?.map((option) => option.state)).toEqual([
        'filed',
        'analysed',
        'decided',
        'in-progress',
        'blocked',
        'done',
        'dropped',
      ]);
      expect(options.options()?.[0].meaning).toBe('Recorded, not analysed yet');
    });

    describe('paging', () => {
      it.each([
        [{ first: 0, rows: 25 }, 1, 25],
        [{ first: 50, rows: 25 }, 3, 25],
        [{ first: 100, rows: 50 }, 3, 50],
        [{ first: 100, rows: 100 }, 2, 100],
      ])('turns %j into page %i of %i rows', async (event, page, perPage) => {
        const { fixture } = await render();

        lazyLoad(fixture, event);

        expect(listParams()).toMatchObject({ page, per_page: perPage });
      });

      it('falls back to 25 rows for a row count that is not offered', async () => {
        const { fixture } = await render();

        lazyLoad(fixture, { first: 50, rows: 10 });

        expect(listParams()).toMatchObject({ page: 3, per_page: 25 });
      });

      it('keeps the page size when the event names no row count', async () => {
        const { fixture } = await render();
        lazyLoad(fixture, { first: 0, rows: 50 });

        lazyLoad(fixture, { first: 100 });

        expect(listParams()).toMatchObject({ page: 3, per_page: 50 });
      });

      it('starts at the first page when the event names no offset', async () => {
        const { fixture } = await render();
        lazyLoad(fixture, { first: 75, rows: 25 });

        lazyLoad(fixture, { rows: 25 });

        expect(listParams()).toMatchObject({ page: 1, per_page: 25 });
      });

      it('starts on the first page of 25 rows', async () => {
        const { fixture } = await render();

        const table = fixture.debugElement.query(By.directive(Table)).componentInstance as Table;
        expect(table.first()).toBe(0);
        expect(table.rows()).toBe(25);
      });

      it('shows the page that the list is on', async () => {
        const { fixture } = await render();

        lazyLoad(fixture, { first: 100, rows: 50 });
        await fixture.whenStable();

        const table = fixture.debugElement.query(By.directive(Table)).componentInstance as Table;
        expect(table.first()).toBe(100);
        expect(table.rows()).toBe(50);
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
        expect(listParams()?.q).toBeUndefined();

        vi.advanceTimersByTime(1);
        expect(listParams()?.q).toBe('flicker');
      });

      it('starts the wait again with every keystroke', () => {
        const { fixture, page } = renderNow();

        type(page, 'flick');
        fixture.detectChanges();
        vi.advanceTimersByTime(200);
        type(page, 'flicker');
        fixture.detectChanges();
        vi.advanceTimersByTime(200);
        expect(listParams()?.q).toBeUndefined();

        vi.advanceTimersByTime(50);
        expect(listParams()?.q).toBe('flicker');
      });

      it('goes back to the first page once the text is asked for', () => {
        const { fixture, page } = renderNow();
        lazyLoad(fixture, { first: 50, rows: 25 });
        expect(listParams()?.page).toBe(3);

        type(page, 'flicker');
        fixture.detectChanges();
        expect(listParams()?.page).toBe(3);
        vi.advanceTimersByTime(250);

        expect(listParams()).toMatchObject({ page: 1, q: 'flicker' });
      });

      it('leaves out spaces around the text and asks for no text that is only spaces', () => {
        const { fixture, page } = renderNow();

        type(page, '  flicker  ');
        fixture.detectChanges();
        vi.advanceTimersByTime(250);
        expect(listParams()?.q).toBe('flicker');

        type(page, '   ');
        fixture.detectChanges();
        vi.advanceTimersByTime(250);
        expect(listParams()?.q).toBeUndefined();
      });

      it('combines the text with the chosen states', () => {
        const { fixture, page } = renderNow();

        chooseStates(fixture, ['blocked']);
        type(page, 'flicker');
        fixture.detectChanges();
        vi.advanceTimersByTime(250);

        expect(listParams()).toMatchObject({ state: ['blocked'], q: 'flicker' });
      });
    });
  });

  describe('the table', () => {
    it('shows the tickets of the page, read from the cache', async () => {
      load([ticket(1, { title: 'The board flickers' }), ticket(2, { title: 'The login hangs' })]);

      const { page } = await render();

      expect(text(page, '[data-testid="row-acme/COW-1"] .text')).toBe('The board flickers');
      expect(text(page, '[data-testid="row-acme/COW-2"] .text')).toBe('The login hangs');
      expect(page.querySelectorAll('tbody tr.row')).toHaveLength(2);
    });

    it('shows what each ticket is: key, type, state, severity, security, progress, urgency and age', async () => {
      load([
        ticket(1, {
          type: 'bug',
          state: 'blocked',
          severity: 'high',
          security: 'boundary',
          progress: 60,
          urgency: 'now',
          assignee: sam,
        }),
      ]);

      const { page } = await render();

      const row = page.querySelector('[data-testid="row-acme/COW-1"]');
      expect(row?.querySelector('.col-key a')?.textContent).toBe('COW-1');
      expect(row?.querySelector('.col-key a')?.getAttribute('href')).toBe('/t/acme/tickets/COW-1');
      expect(row?.querySelector('app-type')?.getAttribute('title')).toContain('bug');
      expect(row?.querySelector('[data-state]')?.getAttribute('data-state')).toBe('blocked');
      expect(row?.querySelector('[data-severity]')?.getAttribute('data-severity')).toBe('high');
      expect(row?.querySelector('[data-security]')?.getAttribute('data-security')).toBe('boundary');
      expect(row?.querySelector('.col-assignee')?.textContent?.trim()).toBe('Sam Rivera');
      expect(row?.querySelector<HTMLElement>('.col-progress span')?.style.width).toBe('60%');
      expect(row?.querySelector('.col-urgency')?.textContent?.trim()).toBe('now');
      expect(row?.querySelector('.col-when')?.textContent).toBe('5 minutes ago');
    });

    it('explains the progress and the urgency of a ticket in tooltips', async () => {
      load([
        ticket(1, { progress: 60, progress_derived: true, urgency: 'now' }),
        ticket(2, { progress: 40, progress_derived: false, urgency: 'icebox' }),
      ]);

      const { fixture } = await render();

      const tooltip = (number: number, selector: string) =>
        fixture.debugElement
          .query(By.css(`[data-testid="row-acme/COW-${number}"] ${selector}`))
          .injector.get(Tooltip)
          .content();
      expect(tooltip(1, '.progress')).toBe('60%, from its children');
      expect(tooltip(2, '.progress')).toBe('40%');
      expect(tooltip(1, '.col-urgency span')).toBe(
        'Now: a defect in unreleased work, or a statement measured false',
      );
      expect(tooltip(2, '.col-urgency span')).toBe(
        'Needs a product call, a decision or an escalation first',
      );
    });

    it('shows a dash for a ticket nobody is assigned to and no security badge for none', async () => {
      load([ticket(1, { assignee: null, security: 'none' })]);

      const { page } = await render();

      const row = page.querySelector('[data-testid="row-acme/COW-1"]');
      expect(row?.querySelector('.col-assignee')?.textContent?.trim()).toBe('—');
      expect(row?.querySelector('[data-security]')).toBeNull();
    });

    it('shows what a refetch of a ticket put into the cache', async () => {
      load([ticket(1, { title: 'Before' })]);
      const { fixture, page } = await render();

      cache.put('acme/COW-1', ticket(1, { title: 'After', version: 2 }));
      await fixture.whenStable();

      expect(text(page, '[data-testid="row-acme/COW-1"] .text')).toBe('After');
    });

    it('leaves out a ticket that the cache no longer holds', async () => {
      load([ticket(1), ticket(2)]);
      const { fixture, page } = await render();

      cache.delete('acme/COW-1');
      await fixture.whenStable();

      expect(page.querySelector('[data-testid="row-acme/COW-1"]')).toBeNull();
      expect(page.querySelector('[data-testid="row-acme/COW-2"]')).not.toBeNull();
    });

    it('shows the total that the server counted, not the rows of the page', async () => {
      load([ticket(1), ticket(2)], 130);

      const { page } = await render();

      expect(text(page, '[data-testid="total"]')).toBe('130 tickets');
    });

    it('counts zero tickets when the answer carries no total', async () => {
      cache.put('acme/COW-1', ticket(1));
      list.value.set({ keys: ['acme/COW-1'], nextCursor: null });

      const { page } = await render();

      expect(text(page, '[data-testid="total"]')).toBe('0 tickets');
      expect(page.querySelectorAll('tbody tr.row')).toHaveLength(1);
    });

    it('counts zero tickets before the list is loaded', async () => {
      const { page } = await render();

      expect(text(page, '[data-testid="total"]')).toBe('0 tickets');
    });

    it('says no ticket matches when the list is empty', async () => {
      load([]);

      const { page } = await render();

      expect(text(page, 'td.empty')).toBe('No tickets match.');
    });

    it('does not claim that no ticket matches while the list loads', async () => {
      list.isLoading.set(true);

      const { fixture, page } = await render();
      expect(page.querySelector('[data-testid="empty"]')).toBeNull();

      load([]);
      list.isLoading.set(false);
      await fixture.whenStable();

      expect(text(page, '[data-testid="empty"]')).toBe('No tickets match.');
    });

    it('says why the tickets could not be loaded, with the detail of the problem', async () => {
      const body: Problem = {
        type: 'about:blank',
        title: 'The service is not ready',
        status: 503,
        detail: 'The database is starting.',
        code: 'not_ready',
      };
      list.error.set(new HttpErrorResponse({ status: 503, statusText: body.title, error: body }));

      const { page } = await render();

      expect(text(page, '[data-testid="empty"]')).toBe(
        'The tickets could not be loaded: The database is starting.',
      );
    });

    it('says why with the title when the problem has no detail', async () => {
      const body: Problem = {
        type: 'about:blank',
        title: 'Forbidden',
        status: 403,
        code: 'forbidden',
      };
      list.error.set(new HttpErrorResponse({ status: 403, statusText: 'Forbidden', error: body }));

      const { page } = await render();

      expect(text(page, '[data-testid="empty"]')).toBe(
        'The tickets could not be loaded: Forbidden',
      );
    });

    it('says why when the backend cannot be reached', async () => {
      list.error.set(new HttpErrorResponse({ status: 0, statusText: 'Unknown Error' }));

      const { page } = await render();

      expect(text(page, '[data-testid="empty"]')).toBe(
        'The tickets could not be loaded: The connection failed; cowork tries again on its own.',
      );
    });

    it('shows the tickets it has instead of the failure when there are some', async () => {
      load([ticket(1)]);
      list.error.set(new HttpErrorResponse({ status: 0, statusText: 'Unknown Error' }));

      const { page } = await render();

      expect(page.querySelector('[data-testid="row-acme/COW-1"]')).not.toBeNull();
      expect(page.querySelector('[data-testid="empty"]')).toBeNull();
    });

    it('hands the total to the paginator', async () => {
      load([ticket(1)], 130);

      const { fixture } = await render();

      const table = fixture.debugElement.query(By.directive(Table)).componentInstance as Table;
      expect(table.totalRecords()).toBe(130);
    });
  });

  describe('the settings of the project', () => {
    const gear = (page: HTMLElement) =>
      page.querySelector<HTMLAnchorElement>('[data-testid="project-settings"]');

    it('links the settings of the project from the header', async () => {
      const { page } = await render();

      expect(gear(page)?.getAttribute('href')).toBe('/t/acme/p/COW/settings');
      expect(gear(page)?.getAttribute('aria-label')).toBe('Project settings');
      expect(gear(page)?.querySelector('i')?.classList).toContain('pi-cog');
    });

    it('follows the project of the path', async () => {
      const { fixture, page } = await render();

      fixture.componentRef.setInput('project', 'OPS');
      await fixture.whenStable();

      expect(gear(page)?.getAttribute('href')).toBe('/t/acme/p/OPS/settings');
    });

    it('offers nothing outside a tenant', async () => {
      tenant.set(null);

      const { page } = await render();

      expect(gear(page)).toBeNull();
    });
  });

  describe('filing a ticket', () => {
    const dialog = (fixture: ComponentFixture<Backlog>) =>
      fixture.debugElement.query(By.directive(NewTicketDialog));

    it('offers a button that opens the dialog for the project', async () => {
      const { fixture, page } = await render();
      const filing = dialog(fixture).componentInstance as NewTicketDialog;
      expect(filing.visible()).toBe(false);
      expect(filing.tenant()).toBe('acme');
      expect(filing.project()).toBe('COW');

      page.querySelector<HTMLButtonElement>('[data-testid="new-ticket"]')?.click();
      await fixture.whenStable();

      expect(filing.visible()).toBe(true);
    });

    it('closes the dialog when it asks to be closed', async () => {
      const { fixture, page } = await render();
      page.querySelector<HTMLButtonElement>('[data-testid="new-ticket"]')?.click();
      await fixture.whenStable();
      const filing = dialog(fixture).componentInstance as NewTicketDialog;
      expect(filing.visible()).toBe(true);

      dialog(fixture).triggerEventHandler('visibleChange', false);
      await fixture.whenStable();

      expect(filing.visible()).toBe(false);
    });

    it('follows the project of the path', async () => {
      const { fixture } = await render();

      fixture.componentRef.setInput('project', 'OPS');
      await fixture.whenStable();

      expect((dialog(fixture).componentInstance as NewTicketDialog).project()).toBe('OPS');
    });

    it('thanks the person with a toast and loads the list again when a ticket was filed', async () => {
      const { fixture } = await render();
      const add = vi.spyOn(TestBed.inject(MessageService), 'add');
      expect(list.reload).not.toHaveBeenCalled();

      dialog(fixture).triggerEventHandler('filed', ticket(7, { title: 'The board flickers' }));

      expect(add).toHaveBeenCalledExactlyOnceWith({
        severity: 'success',
        summary: 'COW-7 filed',
        detail: 'The board flickers',
        life: 4000,
      });
      expect(list.reload).toHaveBeenCalledOnce();
    });

    it('offers nothing outside a tenant', async () => {
      tenant.set(null);

      const { fixture, page } = await render();

      expect(page.querySelector('[data-testid="new-ticket"]')).toBeNull();
      expect(dialog(fixture)).toBeNull();
    });
  });

  describe('opening a ticket', () => {
    it('goes to the page of the ticket under the tenant when its row is clicked', async () => {
      load([ticket(1), ticket(2)]);
      const { page } = await render();

      page.querySelector<HTMLElement>('[data-testid="row-acme/COW-2"]')?.click();

      expect(navigate).toHaveBeenCalledExactlyOnceWith(['/t', 'acme', 'tickets', 'COW-2']);
    });

    it('leaves the navigation to the link when the key itself is clicked', async () => {
      load([ticket(1)]);
      const { page } = await render();

      page.querySelector<HTMLElement>('[data-testid="row-acme/COW-1"] .col-key a')?.click();

      expect(navigate).not.toHaveBeenCalled();
      expect(navigateByUrl).toHaveBeenCalledOnce();
      const target = navigateByUrl.mock.calls[0][0] as UrlTree;
      expect(TestBed.inject(Router).serializeUrl(target)).toBe('/t/acme/tickets/COW-1');
    });
  });
});
