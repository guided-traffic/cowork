import { HttpErrorResponse } from '@angular/common/http';
import { signal, WritableSignal } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { By } from '@angular/platform-browser';
import { MessageService } from 'primeng/api';
import type { MockInstance } from 'vitest';
import { MemberToken, MemberTokenList, Problem } from '../../api/models';
import { SessionService } from '../../core/session.service';
import { TenantTokensService } from '../../core/tenant-tokens.service';
import { TenantService } from '../../core/tenant.service';
import { revocationMessage, TenantTokens, unrestricted } from './tenant-tokens';

function token(id: string, overrides: Partial<MemberToken> = {}): MemberToken {
  return {
    id,
    name: `token ${id}`,
    person: { id: 'p2', username: 'sam', display_name: 'Sam Rivera' },
    scope: 'write',
    agent: false,
    capabilities: [],
    restricted_tenant: null,
    restricted_project: null,
    created_at: '2026-10-01T10:00:00Z',
    expires_at: '2026-12-30T10:00:00Z',
    last_used_on: null,
    revoked_at: null,
    state: 'active',
    ...overrides,
  };
}

const everywhere = token('t1', { name: 'laptop' });
const here = token('t2', { name: 'ci', restricted_tenant: 'acme', restricted_project: 'COW' });

const pageOf = (
  items: MemberToken[],
  total = items.length,
  page = 1,
  perPage = 25,
): MemberTokenList => ({
  items,
  next_cursor: null,
  total,
  page,
  per_page: perPage,
});

describe('the tenant tokens helpers', () => {
  it('tells an unrestricted token from one restricted to the tenant', () => {
    expect(unrestricted(everywhere)).toBe(true);
    expect(unrestricted(here)).toBe(false);
  });

  it('says before a revocation that an unrestricted token ends in every tenant of its person', () => {
    expect(revocationMessage(everywhere)).toMatch(
      /^It is not restricted to this tenant: revoking it ends it in every tenant Sam Rivera belongs to, not only here\./,
    );
    expect(revocationMessage(here)).toMatch(/^It is restricted to this tenant and ends here\./);
    expect(revocationMessage(here)).toContain('cannot be undone');
    expect(revocationMessage(here)).toContain("recorded in this tenant's audit record");
  });
});

describe('TenantTokens', () => {
  let page: MockInstance<TenantTokensService['page']>;
  let revoke: MockInstance<TenantTokensService['revoke']>;
  let isAdmin: WritableSignal<boolean>;
  let tenant: WritableSignal<string | null>;

  beforeEach(() => {
    page = vi.fn<TenantTokensService['page']>().mockResolvedValue(pageOf([everywhere, here], 40));
    revoke = vi.fn<TenantTokensService['revoke']>().mockResolvedValue(undefined);
    isAdmin = signal(true);
    tenant = signal<string | null>('acme');
    TestBed.configureTestingModule({
      providers: [
        MessageService,
        { provide: TenantTokensService, useValue: { page, revoke } },
        { provide: SessionService, useValue: { tenant, me: { isLoading: signal(false) } } },
        { provide: TenantService, useValue: { isAdmin } },
      ],
    });
  });

  afterEach(() => vi.restoreAllMocks());

  async function render() {
    const fixture = TestBed.createComponent(TenantTokens);
    await settle(fixture);
    return fixture;
  }

  async function settle(fixture: ComponentFixture<TenantTokens>) {
    for (let round = 0; round < 2; round++) {
      await new Promise((resolve) => setTimeout(resolve));
      fixture.detectChanges();
    }
    await fixture.whenStable();
  }

  const host = (fixture: ComponentFixture<TenantTokens>) => fixture.nativeElement as HTMLElement;
  const el = (fixture: ComponentFixture<TenantTokens>, testId: string) =>
    host(fixture).querySelector<HTMLElement>(`[data-testid="${testId}"]`);
  const dialog = () => document.body.querySelector('.p-confirmdialog');
  const press = (label: string) =>
    [...(dialog()?.querySelectorAll('button') ?? [])]
      .find((button) => button.textContent?.trim() === label)
      ?.dispatchEvent(new MouseEvent('click', { bubbles: true }));

  it('reads the first numbered page of the tenant, 25 a page', async () => {
    const fixture = await render();

    expect(page).toHaveBeenCalledWith('acme', 1, 25);
    expect(el(fixture, 'tenant-tokens-pages')?.textContent).toContain('1–25 of 40');
  });

  it('shows each token with its person and its reach, and never a secret', async () => {
    const fixture = await render();

    const first = el(fixture, 'tenant-token-t1');
    expect(first?.textContent).toContain('laptop');
    expect(first?.textContent).toContain('Sam Rivera');
    expect(first?.querySelector('[data-testid="reach"]')?.textContent?.trim()).toBe(
      'every tenant of the person',
    );
    expect(
      el(fixture, 'tenant-token-t2')
        ?.querySelector('[data-testid="reach"]')
        ?.textContent?.replace(/\s+/g, ' ')
        .trim(),
    ).toBe('this tenant / COW');
    expect(host(fixture).textContent).not.toContain('cwk_');
  });

  it('turns to another page and another size through the paginator', async () => {
    const fixture = await render();

    fixture.debugElement
      .query(By.css('[data-testid="tenant-tokens-pages"]'))
      .triggerEventHandler('onPageChange', { page: 1, rows: 25, first: 25 });
    await settle(fixture);
    expect(page).toHaveBeenLastCalledWith('acme', 2, 25);

    fixture.debugElement
      .query(By.css('[data-testid="tenant-tokens-pages"]'))
      .triggerEventHandler('onPageChange', { page: 1, rows: 100, first: 100 });
    await settle(fixture);
    expect(page).toHaveBeenLastCalledWith('acme', 1, 100);
  });

  it('asks before it revokes an unrestricted token, naming that it ends everywhere, and revokes on Revoke everywhere', async () => {
    const fixture = await render();

    el(fixture, 'tenant-token-revoke-t1')?.click();
    await settle(fixture);
    expect(dialog()?.textContent).toContain('Revoke laptop of Sam Rivera?');
    expect(dialog()?.textContent).toContain(
      'revoking it ends it in every tenant Sam Rivera belongs to, not only here',
    );
    expect(revoke).not.toHaveBeenCalled();
    const calls = page.mock.calls.length;

    press('Revoke everywhere');
    await settle(fixture);

    expect(revoke).toHaveBeenCalledWith('acme', 't1');
    expect(page.mock.calls.length).toBeGreaterThan(calls);
  });

  it('revokes nothing when the person keeps the token', async () => {
    const fixture = await render();

    el(fixture, 'tenant-token-revoke-t2')?.click();
    await settle(fixture);
    expect(dialog()?.textContent).toContain('It is restricted to this tenant and ends here.');
    press('Keep it');
    await settle(fixture);

    expect(revoke).not.toHaveBeenCalled();
  });

  it('offers no revocation of a token that is no longer active', async () => {
    page.mockResolvedValue(
      pageOf([token('t3', { state: 'revoked', revoked_at: '2026-10-02T10:00:00Z' })]),
    );
    const fixture = await render();

    expect(el(fixture, 'tenant-token-t3')).not.toBeNull();
    expect(el(fixture, 'tenant-token-revoke-t3')).toBeNull();
  });

  it('toasts a refused revocation and loads the page again', async () => {
    const body: Problem = {
      type: 'about:blank',
      title: 'Not found',
      status: 404,
      code: 'not_found',
      detail: 'no such token',
    };
    revoke.mockRejectedValue(new HttpErrorResponse({ status: 404, error: body }));
    const add = vi.spyOn(TestBed.inject(MessageService), 'add');
    const fixture = await render();

    el(fixture, 'tenant-token-revoke-t2')?.click();
    await settle(fixture);
    press('Revoke');
    await settle(fixture);

    expect(add).toHaveBeenCalledExactlyOnceWith(
      expect.objectContaining({ detail: 'no such token' }),
    );
    expect(page.mock.calls.length).toBeGreaterThan(1);
  });

  it('asks nothing and says whose the list is for anybody but an administrator', async () => {
    isAdmin.set(false);
    const fixture = await render();

    expect(page).not.toHaveBeenCalled();
    expect(el(fixture, 'tenant-tokens')).toBeNull();
    expect(el(fixture, 'tenant-tokens-notice')?.textContent).toContain("administrators' to see");
  });

  it('says why when the tokens could not be loaded', async () => {
    const body: Problem = {
      type: 'about:blank',
      title: 'Not ready',
      status: 503,
      code: 'not_ready',
      detail: 'The database is starting.',
    };
    page.mockRejectedValue(new HttpErrorResponse({ status: 503, error: body }));
    const fixture = await render();

    expect(el(fixture, 'tenant-tokens-empty')?.textContent?.trim()).toBe(
      'The tokens could not be loaded: The database is starting.',
    );
  });
});
