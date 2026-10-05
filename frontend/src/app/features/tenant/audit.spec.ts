import { HttpErrorResponse } from '@angular/common/http';
import { signal, WritableSignal } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { By } from '@angular/platform-browser';
import { provideRouter } from '@angular/router';
import { MessageService } from 'primeng/api';
import { Paginator } from 'primeng/paginator';
import { Select } from 'primeng/select';
import type { MockInstance } from 'vitest';
import { AuditEvent, AuditList, Member, Problem } from '../../api/models';
import { AuditQuery, AuditService } from '../../core/audit.service';
import { MembersService } from '../../core/members.service';
import { SessionService } from '../../core/session.service';
import { TenantService } from '../../core/tenant.service';
import { Audit, changes, entityTypes, period, tokenIdOrEmpty } from './audit';

const ada: Member = {
  person: { id: 'p1', username: 'ada', display_name: 'Ada Lovelace' },
  role: 'admin',
  origins: [{ source: 'grant', role: 'admin' }],
  local: true,
  email: null,
};

const tokenId = '0199aaaa-0000-7000-8000-0000000000aa';

function act(id: string, overrides: Partial<AuditEvent> = {}): AuditEvent {
  return {
    id,
    created_at: '2026-10-03T10:00:00Z',
    actor: { person: { id: 'p1', username: 'ada', display_name: 'Ada Lovelace' }, system: null },
    agent: null,
    token_id: null,
    token_name: null,
    entity_type: 'ticket',
    entity_id: 'e1',
    ticket_key: 'acme/VKO-12',
    action: 'transitioned',
    before: { state: 'filed' },
    after: { state: 'analysed' },
    reason: null,
    note: null,
    request_id: null,
    idempotency_key: null,
    ...overrides,
  };
}

const pageOf = (items: AuditEvent[], total = items.length, page = 1, perPage = 25): AuditList => ({
  items,
  next_cursor: null,
  total,
  page,
  per_page: perPage,
});

describe('the audit page helpers', () => {
  it('takes an empty token field or a token id, and nothing else', () => {
    expect(tokenIdOrEmpty('')).toBe(true);
    expect(tokenIdOrEmpty('  ')).toBe(true);
    expect(tokenIdOrEmpty(tokenId)).toBe(true);
    expect(tokenIdOrEmpty(` ${tokenId.toUpperCase()} `)).toBe(true);
    expect(tokenIdOrEmpty('claude-laptop')).toBe(false);
    expect(tokenIdOrEmpty(tokenId.slice(1))).toBe(false);
  });

  it("turns two days of the browser's calendar into a period whose last day counts whole (docs/adr/0055 D3)", () => {
    expect(period('', '')).toEqual({});
    expect(period('2026-10-01', '')).toEqual({
      from: new Date('2026-10-01T00:00:00').toISOString(),
    });
    expect(period('', '2026-10-03')).toEqual({
      to: new Date('2026-10-04T00:00:00').toISOString(),
    });
    expect(period('2026-12-31', '2026-12-31')).toEqual({
      from: new Date('2026-12-31T00:00:00').toISOString(),
      to: new Date('2027-01-01T00:00:00').toISOString(),
    });
  });

  it('shows each changed field with its value before and after, as JSON', () => {
    expect(changes(act('a'))).toEqual(['state: "filed" → "analysed"']);
    expect(changes(act('a', { before: null, after: { title: 'New', progress: 20 } }))).toEqual([
      'title: "New"',
      'progress: 20',
    ]);
    expect(changes(act('a', { before: { assignee: 'p1' }, after: {} }))).toEqual([
      'assignee: "p1" → —',
    ]);
    expect(changes(act('a', { before: undefined, after: undefined }))).toEqual([]);
  });

  it('offers the entities the record names most', () => {
    expect(entityTypes).toContain('ticket');
    expect(entityTypes).toContain('membership');
    expect(new Set(entityTypes).size).toBe(entityTypes.length);
  });
});

describe('Audit', () => {
  let page: MockInstance<AuditService['page']>;
  let csv: MockInstance<AuditService['csv']>;
  let isAdmin: WritableSignal<boolean>;
  let tenant: WritableSignal<string | null>;

  beforeEach(() => {
    page = vi.fn<AuditService['page']>().mockResolvedValue(pageOf([act('a1')], 60));
    csv = vi.fn<AuditService['csv']>().mockResolvedValue({
      text: 'id\na1\n',
      until: '2026-10-04T12:00:00.000Z',
      total: 1,
    });
    isAdmin = signal(true);
    tenant = signal<string | null>('acme');
    TestBed.configureTestingModule({
      providers: [
        provideRouter([]),
        MessageService,
        { provide: AuditService, useValue: { page, csv } },
        { provide: MembersService, useValue: { list: signal([ada]) } },
        {
          provide: SessionService,
          useValue: { tenant, me: { isLoading: signal(false) } },
        },
        { provide: TenantService, useValue: { isAdmin } },
      ],
    });
  });

  afterEach(() => vi.restoreAllMocks());

  async function render() {
    const fixture = TestBed.createComponent(Audit);
    await settle(fixture);
    return fixture;
  }

  async function settle(fixture: ComponentFixture<Audit>) {
    for (let round = 0; round < 2; round++) {
      await new Promise((resolve) => setTimeout(resolve));
      fixture.detectChanges();
    }
    await fixture.whenStable();
  }

  const host = (fixture: ComponentFixture<Audit>) => fixture.nativeElement as HTMLElement;
  const el = (fixture: ComponentFixture<Audit>, testId: string) =>
    host(fixture).querySelector<HTMLElement>(`[data-testid="${testId}"]`);
  const text = (element: Element | null | undefined) =>
    element?.textContent?.replace(/\s+/g, ' ').trim();
  const lastQuery = (): AuditQuery => page.mock.lastCall?.[0] as AuditQuery;

  function typeInto(fixture: ComponentFixture<Audit>, testId: string, value: string) {
    const field = el(fixture, testId) as HTMLInputElement;
    field.value = value;
    field.dispatchEvent(new Event('input'));
    fixture.detectChanges();
  }

  async function choose(fixture: ComponentFixture<Audit>, testId: string, value: unknown) {
    fixture.debugElement
      .query(By.css(`[data-testid="${testId}"]`))
      .triggerEventHandler('ngModelChange', value);
    await settle(fixture);
  }

  describe('the page', () => {
    it('asks for the first page of 25, unfiltered, and shows the acts', async () => {
      const fixture = await render();

      expect(host(fixture).querySelector('h1')?.textContent).toBe('Audit record');
      expect(page).toHaveBeenCalledExactlyOnceWith({ tenant: 'acme' }, 1, 25);
      const row = el(fixture, 'act-a1');
      expect(text(row?.querySelector('[data-testid="act-person"]'))).toBe('Ada Lovelace');
      expect(text(row?.querySelector('[data-testid="act-action"]'))).toBe('transitioned');
      expect(text(row?.querySelector('[data-testid="act-entity"]'))).toBe('ticket');
      expect(row?.querySelector('[data-testid="act-ticket"]')?.getAttribute('href')).toBe(
        '/t/acme/tickets/VKO-12',
      );
      expect(text(row?.querySelector('[data-testid="act-change"]'))).toBe(
        'state: "filed" → "analysed"',
      );
    });

    it('names a system actor, the reason and the note, and marks an act made through a token', async () => {
      page.mockResolvedValue(
        pageOf([
          act('a2', {
            actor: { person: null, system: 'system:identity-provider' },
            ticket_key: null,
            entity_type: 'membership',
            reason: 'groups changed',
            note: 'a note',
          }),
          act('a3', { token_id: tokenId, token_name: 'ci-script' }),
        ]),
      );
      const fixture = await render();

      const system = el(fixture, 'act-a2');
      expect(text(system?.querySelector('[data-testid="act-system"]'))).toBe(
        'system:identity-provider',
      );
      expect(system?.querySelector('[data-testid="act-ticket"]')).toBeNull();
      expect(text(system?.querySelector('[data-testid="act-reason"]'))).toBe(
        'Reason: groups changed',
      );
      expect(text(system?.querySelector('[data-testid="act-note"]'))).toBe('Note: a note');
      expect(system?.querySelector('app-agent-mark')).toBeNull();

      const viaToken = el(fixture, 'act-a3');
      expect(text(viaToken?.querySelector('app-agent-mark'))).toContain('token ci-script');
      expect(viaToken?.querySelector('[data-testid="act-by-token"]')).not.toBeNull();
    });

    it('says so when nothing matches, and when the record cannot be loaded', async () => {
      page.mockResolvedValue(pageOf([], 0));
      let fixture = await render();
      expect(text(el(fixture, 'audit-empty'))).toBe('No act matches.');
      expect(el(fixture, 'audit-pages')).toBeNull();

      const body: Problem = {
        type: 'about:blank',
        title: 'Unavailable',
        status: 503,
        detail: 'The database does not answer.',
        code: 'not_ready',
      };
      page.mockRejectedValue(new HttpErrorResponse({ status: 503, error: body }));
      fixture = await render();
      expect(text(el(fixture, 'audit-empty'))).toBe(
        'The record could not be loaded: The database does not answer.',
      );
    });

    it('asks nothing for a member who is no administrator, and says why', async () => {
      isAdmin.set(false);
      const fixture = await render();

      expect(page).not.toHaveBeenCalled();
      expect(text(el(fixture, 'audit-notice'))).toBe(
        'Only the administrators of this tenant read its audit record.',
      );
      expect(el(fixture, 'audit-csv')).toBeNull();
    });
  });

  describe('the filters (docs/adr/0026 D6)', () => {
    it('sends the actor, the actions, the entity and the period, and starts at the first page again', async () => {
      const fixture = await render();
      fixture.debugElement
        .query(By.directive(Paginator))
        .triggerEventHandler('onPageChange', { page: 1, rows: 25, first: 25 });
      await settle(fixture);
      expect(page).toHaveBeenLastCalledWith({ tenant: 'acme' }, 2, 25);

      await choose(fixture, 'audit-actor', 'p1');
      expect(page).toHaveBeenLastCalledWith({ tenant: 'acme', actor: 'p1' }, 1, 25);

      await choose(fixture, 'audit-action', ['created', 'deleted']);
      typeInto(fixture, 'audit-entity', ' ticket ');
      await settle(fixture);
      typeInto(fixture, 'audit-from', '2026-10-01');
      typeInto(fixture, 'audit-until', '2026-10-03');
      await settle(fixture);

      expect(lastQuery()).toEqual({
        tenant: 'acme',
        actor: 'p1',
        action: ['created', 'deleted'],
        entity_type: 'ticket',
        ...period('2026-10-01', '2026-10-03'),
      });
      expect(page.mock.lastCall?.[1]).toBe(1);
    });

    it('offers the members as actors by name and username', async () => {
      const fixture = await render();

      const actor = fixture.debugElement.query(By.css('[data-testid="audit-actor"]'))
        .componentInstance as Select;
      expect(actor.options()).toEqual([{ id: 'p1', label: 'Ada Lovelace (ada)' }]);
    });

    it("filters by a token's id, and asks nothing while the field holds something else", async () => {
      const fixture = await render();
      page.mockClear();

      typeInto(fixture, 'audit-token', 'claude-laptop');
      await settle(fixture);
      expect(page).not.toHaveBeenCalled();
      expect(el(fixture, 'audit-token-error')).not.toBeNull();
      expect(el(fixture, 'audit-token')?.getAttribute('aria-invalid')).toBe('true');
      expect(text(el(fixture, 'audit-empty'))).toBe("Type a token's id to filter by it.");

      typeInto(fixture, 'audit-token', tokenId.toUpperCase());
      await settle(fixture);
      expect(lastQuery()).toEqual({ tenant: 'acme', token: tokenId });
      expect(el(fixture, 'audit-token-error')).toBeNull();
    });

    it('shows what a token did from the act that names it', async () => {
      page.mockResolvedValue(pageOf([act('a3', { token_id: tokenId, token_name: 'ci-script' })]));
      const fixture = await render();

      el(fixture, 'act-by-token')?.click();
      await settle(fixture);

      expect(lastQuery()).toEqual({ tenant: 'acme', token: tokenId });
      expect((el(fixture, 'audit-token') as HTMLInputElement).value).toBe(tokenId);
    });

    it('clears every filter at once', async () => {
      const fixture = await render();
      expect(el(fixture, 'audit-clear')).toBeNull();
      await choose(fixture, 'audit-actor', 'p1');
      typeInto(fixture, 'audit-entity', 'ticket');
      await settle(fixture);

      el(fixture, 'audit-clear')?.click();
      await settle(fixture);

      expect(lastQuery()).toEqual({ tenant: 'acme' });
      expect(el(fixture, 'audit-clear')).toBeNull();
    });
  });

  describe('the pages (docs/adr/0048 D2)', () => {
    it('shows the total and turns to the page asked for', async () => {
      const fixture = await render();
      const paginator = fixture.debugElement.query(By.directive(Paginator))
        .componentInstance as Paginator;
      expect(paginator.totalRecords()).toBe(60);
      expect(paginator.rows()).toBe(25);
      expect(paginator.first()).toBe(0);

      fixture.debugElement
        .query(By.directive(Paginator))
        .triggerEventHandler('onPageChange', { page: 2, rows: 25, first: 50 });
      await settle(fixture);

      expect(page).toHaveBeenLastCalledWith({ tenant: 'acme' }, 3, 25);
      expect(paginator.first()).toBe(50);
    });

    it('takes 25, 50 or 100 a page, and starts at the first page with another size', async () => {
      const fixture = await render();
      const paginator = fixture.debugElement.query(By.directive(Paginator));
      expect((paginator.componentInstance as Paginator).rowsPerPageOptions()).toEqual([
        25, 50, 100,
      ]);
      paginator.triggerEventHandler('onPageChange', { page: 1, rows: 25, first: 25 });
      await settle(fixture);

      paginator.triggerEventHandler('onPageChange', { page: 1, rows: 100, first: 100 });
      await settle(fixture);

      expect(page).toHaveBeenLastCalledWith({ tenant: 'acme' }, 1, 100);
    });
  });

  describe('the CSV', () => {
    let created: MockInstance<typeof URL.createObjectURL>;
    let clicked: MockInstance<HTMLAnchorElement['click']>;

    beforeEach(() => {
      created = vi.fn<typeof URL.createObjectURL>().mockReturnValue('blob:audit');
      vi.stubGlobal(
        'URL',
        Object.assign(URL, { createObjectURL: created, revokeObjectURL: vi.fn() }),
      );
      clicked = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => undefined);
    });

    afterEach(() => vi.unstubAllGlobals());

    it('downloads what the filters select, named by the tenant and the moment it ends at', async () => {
      const fixture = await render();
      await choose(fixture, 'audit-actor', 'p1');

      el(fixture, 'audit-csv')?.click();
      await settle(fixture);

      expect(csv).toHaveBeenCalledExactlyOnceWith({ tenant: 'acme', actor: 'p1' });
      expect(created).toHaveBeenCalledOnce();
      const blob = created.mock.calls[0][0] as Blob;
      expect(blob.type).toBe('text/csv');
      expect(await blob.text()).toBe('id\na1\n');
      const link = clicked.mock.contexts[0] as HTMLAnchorElement;
      expect(link.download).toBe('audit-acme-2026-10-04-12-00-00.csv');
      expect(link.href).toBe('blob:audit');
      expect(el(fixture, 'audit-csv-note')).toBeNull();
    });

    it('says when the file holds only the newest ten thousand acts', async () => {
      csv.mockResolvedValue({ text: 'id\n', until: '2026-10-04T12:00:00.000Z', total: 25_000 });
      const fixture = await render();

      el(fixture, 'audit-csv')?.click();
      await settle(fixture);

      expect(text(el(fixture, 'audit-csv-note'))).toContain('newest 10,000 of 25,000 acts');
    });

    it('toasts a download that fails, and offers it again', async () => {
      csv.mockRejectedValue(new HttpErrorResponse({ status: 0, statusText: 'Unknown Error' }));
      const add = vi.spyOn(TestBed.inject(MessageService), 'add');
      const fixture = await render();

      el(fixture, 'audit-csv')?.click();
      await settle(fixture);

      expect(add).toHaveBeenCalledOnce();
      expect(created).not.toHaveBeenCalled();
      expect((el(fixture, 'audit-csv') as HTMLButtonElement).disabled).toBe(false);
    });
  });
});
