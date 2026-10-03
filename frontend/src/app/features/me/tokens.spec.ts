import { HttpErrorResponse } from '@angular/common/http';
import { isSignal, signal, WritableSignal } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { By } from '@angular/platform-browser';
import { MessageService } from 'primeng/api';
import { Tooltip } from 'primeng/tooltip';
import type { MockInstance } from 'vitest';
import { Membership, Problem, Token, TokenCreated } from '../../api/models';
import { CAPABILITY } from '../../api/models/capability-array';
import { SessionService } from '../../core/session.service';
import { TokensService } from '../../core/tokens.service';
import { dateTime } from '../../shared/time';
import { NewTokenDialog, scopeMeanings } from './new-token-dialog';
import { day, Tokens, tokenStateMeanings } from './tokens';

function token(id: string, overrides: Partial<Token> = {}): Token {
  return {
    id,
    name: `Token ${id}`,
    scope: 'write',
    agent: false,
    capabilities: [],
    created_at: '2026-10-01T10:00:00Z',
    expires_at: '2026-12-30T10:00:00Z',
    last_used_on: null,
    revoked_at: null,
    restricted_tenant: null,
    restricted_project_id: null,
    state: 'active',
    ...overrides,
  };
}

const laptop = token('t1', {
  name: 'claude on my laptop',
  agent: true,
  capabilities: [...CAPABILITY],
  restricted_tenant: 'acme',
  restricted_project_id: '0199aaaa-0000-7000-8000-00000000c0de',
  last_used_on: '2026-10-02',
});
const script = token('t2', { name: 'backup script', scope: 'read' });
const old = token('t3', {
  name: 'old one',
  scope: 'admin',
  state: 'expired',
  expires_at: '2026-09-01T10:00:00Z',
});
const gone = token('t4', {
  name: 'leaked',
  state: 'revoked',
  revoked_at: '2026-10-02T08:00:00Z',
});

const plaintext = `cwk_${'C'.repeat(43)}`;
const made: TokenCreated = {
  ...token('t9', { name: 'new one', scope: 'write', expires_at: '2026-12-31T10:00:00Z' }),
  token: plaintext,
};

function refusal(status: number, title: string, detail: string) {
  const body: Problem = { type: 'about:blank', title, status, detail, code: 'forbidden' };
  return new HttpErrorResponse({ status, statusText: title, error: body });
}

describe('day', () => {
  it('shows an API day in the browser locale', () => {
    expect(day('2026-10-03', 'en-US')).toBe('Oct 3, 2026');
    expect(day('2026-10-03', 'de-DE')).toBe('03.10.2026');
  });

  afterEach(() => vi.unstubAllEnvs());

  it.each(['America/Los_Angeles', 'Pacific/Auckland', 'UTC'])(
    'shows the day the API names in %s, not the day before or after it',
    (zone) => {
      vi.stubEnv('TZ', zone);

      // The zone is in effect: a midnight in UTC is the evening before in Los Angeles.
      expect(new Date('2026-01-01T00:00:00Z').getDate()).toBe(
        zone === 'America/Los_Angeles' ? 31 : 1,
      );
      expect(day('2026-01-01', 'en-US')).toBe('Jan 1, 2026');
      expect(day('2026-12-31', 'en-US')).toBe('Dec 31, 2026');
    },
  );
});

describe('tokenStateMeanings', () => {
  it('says what each of the three states means', () => {
    expect(Object.keys(tokenStateMeanings)).toEqual(['active', 'expired', 'revoked']);
  });
});

describe('Tokens', () => {
  let list: WritableSignal<Token[]>;
  let loading: WritableSignal<boolean>;
  let error: WritableSignal<unknown>;
  let keys: Map<string, string>;
  let reload: MockInstance<() => boolean>;
  let revoke: MockInstance<TokensService['revoke']>;
  let memberships: WritableSignal<Membership[]>;

  beforeEach(() => {
    list = signal<Token[]>([laptop, script, old, gone]);
    loading = signal(false);
    error = signal<unknown>(undefined);
    keys = new Map([['0199aaaa-0000-7000-8000-00000000c0de', 'COW']]);
    reload = vi.fn<() => boolean>().mockReturnValue(true);
    revoke = vi.fn<TokensService['revoke']>().mockResolvedValue(undefined);
    memberships = signal<Membership[]>([]);
    TestBed.configureTestingModule({
      providers: [
        MessageService,
        {
          provide: TokensService,
          useValue: {
            list,
            tokens: { isLoading: loading, error, reload },
            keyOfProject: (id: string) => keys.get(id),
            revoke,
            create: vi.fn(),
            projectsOf: vi.fn().mockResolvedValue([]),
          },
        },
        { provide: SessionService, useValue: { memberships } },
      ],
    });
  });

  async function render() {
    const fixture = TestBed.createComponent(Tokens);
    await settle(fixture);
    return fixture;
  }

  /** Lets what a click started finish, and shows it; fields take their model a moment after they appear. */
  async function settle(fixture: ComponentFixture<Tokens>) {
    for (let round = 0; round < 2; round++) {
      await new Promise((resolve) => setTimeout(resolve));
      fixture.detectChanges();
    }
    await fixture.whenStable();
  }

  const host = (fixture: ComponentFixture<Tokens>) => fixture.nativeElement as HTMLElement;
  const el = (fixture: ComponentFixture<Tokens>, testId: string) =>
    host(fixture).querySelector<HTMLElement>(`[data-testid="${testId}"]`);
  const row = (fixture: ComponentFixture<Tokens>, id: string) => el(fixture, `token-${id}`);
  const inRow = (fixture: ComponentFixture<Tokens>, id: string, selector: string) =>
    row(fixture, id)?.querySelector<HTMLElement>(selector);
  const cells = (fixture: ComponentFixture<Tokens>, id: string) => [
    ...(row(fixture, id)?.querySelectorAll('td') ?? []),
  ];
  const text = (node: Element | null | undefined) => node?.textContent?.replace(/\s+/g, ' ').trim();
  const tooltipOf = (fixture: ComponentFixture<Tokens>, id: string, selector: string) =>
    fixture.debugElement
      .query(By.css(`[data-testid="token-${id}"] ${selector}`))
      .injector.get(Tooltip)
      .content();

  describe('the page', () => {
    it('is headed Your tokens, says what a token is and offers a new one', async () => {
      const fixture = await render();

      expect(host(fixture).querySelector('h1')?.textContent).toBe('Your tokens');
      expect(host(fixture).querySelector('.lead')?.textContent).toContain(
        'cowork shows it once, when you make it, and never again',
      );
      expect(el(fixture, 'new-token')?.textContent?.trim()).toBe('New token');
    });

    it('shows the headers of the columns and a row per token, whatever its state', async () => {
      const fixture = await render();

      expect([...host(fixture).querySelectorAll('th')].map((header) => header.textContent)).toEqual(
        ['Name', 'Scope', 'Agent', 'Restriction', 'Created', 'Expires', 'Last used', 'State', ''],
      );
      for (const id of ['t1', 't2', 't3', 't4']) {
        expect(row(fixture, id), id).not.toBeNull();
      }
      expect(el(fixture, 'tokens-empty')).toBeNull();
    });

    it('follows the list when tokens arrive', async () => {
      list.set([]);
      const fixture = await render();
      expect(row(fixture, 't2')).toBeNull();

      list.set([script]);
      await settle(fixture);

      expect(row(fixture, 't2')).not.toBeNull();
    });

    it('asks for the tokens again, because what the list holds is as old as the last visit', async () => {
      await render();

      expect(reload).toHaveBeenCalledOnce();
    });

    it('never shows a plaintext: a token is shown by its name, its metadata and its state', async () => {
      const fixture = await render();

      expect(host(fixture).innerHTML).not.toContain('cwk_');
    });
  });

  describe('a row', () => {
    it('shows the name, the scope, the dates and the state of a token, as the API spells them', async () => {
      const fixture = await render();

      expect(cells(fixture, 't2').map((cell) => text(cell))).toEqual([
        'backup script',
        'read',
        '—',
        'none',
        dateTime('2026-10-01T10:00:00Z'),
        dateTime('2026-12-30T10:00:00Z'),
        'never',
        'active',
        'Revoke',
      ]);
    });

    it('explains the scope in a tooltip, and takes its colour from a token of the preset', async () => {
      list.set([script, token('w', { scope: 'write' }), old]);
      const fixture = await render();

      const accent = (id: string) =>
        inRow(fixture, id, '.pill.outline')?.style.getPropertyValue('--accent');
      expect(tooltipOf(fixture, 't2', '.pill.outline')).toBe(scopeMeanings.read);
      expect(tooltipOf(fixture, 'w', '.pill.outline')).toBe(scopeMeanings.write);
      expect(tooltipOf(fixture, 't3', '.pill.outline')).toBe(scopeMeanings.admin);
      expect(accent('t2')).toBe('var(--p-text-muted-color)');
      expect(accent('w')).toBe('var(--p-state-analysed)');
      expect(accent('t3')).toBe('var(--p-severity-high)');
    });

    it('shows the state as the API spells it, explains it, and colours it from a token of the preset', async () => {
      const fixture = await render();

      const state = (id: string) => inRow(fixture, id, '[data-testid="state"]');
      expect(text(state('t1'))).toBe('active');
      expect(text(state('t3'))).toBe('expired');
      expect(text(state('t4'))).toBe('revoked');
      expect(tooltipOf(fixture, 't1', '[data-testid="state"]')).toBe(tokenStateMeanings.active);
      expect(tooltipOf(fixture, 't3', '[data-testid="state"]')).toBe(tokenStateMeanings.expired);
      expect(tooltipOf(fixture, 't4', '[data-testid="state"]')).toBe(tokenStateMeanings.revoked);
      expect(state('t1')?.style.getPropertyValue('--accent')).toBe('var(--p-state-done)');
      expect(state('t3')?.style.getPropertyValue('--accent')).toBe('var(--p-state-filed)');
      expect(state('t4')?.style.getPropertyValue('--accent')).toBe('var(--p-severity-critical)');
    });

    it('sets a token that does not work any more apart from one that does', async () => {
      const fixture = await render();

      expect(row(fixture, 't1')?.classList).not.toContain('dead');
      expect(row(fixture, 't3')?.classList).toContain('dead');
      expect(row(fixture, 't4')?.classList).toContain('dead');
    });

    it('shows the day a token was last used, in the browser locale, and never when it was not', async () => {
      const fixture = await render();

      expect(text(inRow(fixture, 't1', '[data-testid="last-used"]'))).toBe(day('2026-10-02'));
      expect(text(inRow(fixture, 't2', '[data-testid="last-used"]'))).toBe('never');
    });
  });

  describe('the agent column', () => {
    it('shows an agent token as one, with all capabilities named when it has them all', async () => {
      const fixture = await render();

      expect(text(inRow(fixture, 't1', '[data-testid="agent"]'))).toBe('agent');
      expect(text(inRow(fixture, 't1', '[data-testid="capabilities"]'))).toBe('all capabilities');
      expect(tooltipOf(fixture, 't1', '[data-testid="capabilities"]')).toBe(CAPABILITY.join(', '));
    });

    it('counts the capabilities of an agent token that has some, and lists them in a tooltip', async () => {
      list.set([token('a', { agent: true, capabilities: ['drop', 'upload', 'interest'] })]);
      const fixture = await render();

      expect(text(inRow(fixture, 'a', '[data-testid="capabilities"]'))).toBe('3 of 9 capabilities');
      expect(tooltipOf(fixture, 'a', '[data-testid="capabilities"]')).toBe(
        'drop, upload, interest',
      );
    });

    it('names the baseline for an agent token that has no capability', async () => {
      list.set([token('a', { agent: true, capabilities: [] })]);
      const fixture = await render();

      expect(text(inRow(fixture, 'a', '[data-testid="capabilities"]'))).toBe('the baseline');
      expect(tooltipOf(fixture, 'a', '[data-testid="capabilities"]')).toBe(
        'The baseline of every agent token',
      );
    });

    it('shows a dash for a token that is no agent, without a pill or capabilities', async () => {
      const fixture = await render();

      expect(inRow(fixture, 't2', '[data-testid="agent"]')).toBeNull();
      expect(inRow(fixture, 't2', '[data-testid="capabilities"]')).toBeNull();
      expect(text(cells(fixture, 't2')[2])).toBe('—');
    });
  });

  describe('the restriction', () => {
    const restriction = (fixture: ComponentFixture<Tokens>, id: string) =>
      inRow(fixture, id, '[data-testid="restriction"]');

    it('shows none for a token that reaches every tenant of its person', async () => {
      const fixture = await render();

      expect(text(restriction(fixture, 't2'))).toBe('none');
    });

    it('shows the tenant, and the key of the project when the token has one', async () => {
      const fixture = await render();

      expect(text(restriction(fixture, 't1'))).toBe('acme / COW');
      expect(restriction(fixture, 't1')?.querySelector('.key')?.getAttribute('title')).toBe(
        '0199aaaa-0000-7000-8000-00000000c0de',
      );
    });

    it('shows the tenant alone for a token that is restricted to a tenant only', async () => {
      list.set([token('a', { restricted_tenant: 'globex' })]);
      const fixture = await render();

      expect(text(restriction(fixture, 'a'))).toBe('globex');
      expect(restriction(fixture, 'a')?.querySelector('.key')).toBeNull();
    });

    it('shows the start of the id of a project whose key is not found, with the whole id in a title', async () => {
      keys.clear();
      const fixture = await render();

      expect(text(restriction(fixture, 't1'))).toBe('acme / 0199aaaa');
      expect(restriction(fixture, 't1')?.querySelector('.key')?.getAttribute('title')).toBe(
        '0199aaaa-0000-7000-8000-00000000c0de',
      );
    });
  });

  describe('revoking', () => {
    const dialog = () => document.body.querySelector('.p-confirmdialog');

    const press = (label: string) =>
      [...(dialog()?.querySelectorAll('button') ?? [])]
        .find((button) => button.textContent?.trim() === label)
        ?.dispatchEvent(new MouseEvent('click', { bubbles: true }));

    async function ask(fixture: ComponentFixture<Tokens>, id = 't1') {
      el(fixture, `token-revoke-${id}`)?.click();
      await settle(fixture);
    }

    it('is offered for a token that works, and for none that does not', async () => {
      const fixture = await render();

      expect(el(fixture, 'token-revoke-t1')?.textContent?.trim()).toBe('Revoke');
      expect(el(fixture, 'token-revoke-t2')).not.toBeNull();
      expect(el(fixture, 'token-revoke-t3')).toBeNull();
      expect(el(fixture, 'token-revoke-t4')).toBeNull();
    });

    it('names the token on its button, for a screen reader', async () => {
      const fixture = await render();

      expect(el(fixture, 'token-revoke-t1')?.getAttribute('aria-label')).toBe(
        'Revoke claude on my laptop',
      );
    });

    it('asks first, and says that it is final and that the token stays in the list', async () => {
      const fixture = await render();

      await ask(fixture);

      expect(dialog()?.textContent).toContain('Revoke claude on my laptop?');
      expect(dialog()?.textContent).toContain(
        'Every request that presents it is refused from now on',
      );
      expect(dialog()?.textContent).toContain('a revocation cannot be undone');
      expect(revoke).not.toHaveBeenCalled();
    });

    it('offers the revocation as a dangerous act', async () => {
      const fixture = await render();

      await ask(fixture);

      const accept = [...(dialog()?.querySelectorAll('button') ?? [])].find(
        (button) => button.textContent?.trim() === 'Revoke',
      );
      expect(accept?.className).toContain('p-button-danger');
    });

    it('revokes the token of the row and says so when it is confirmed', async () => {
      const add = vi.spyOn(TestBed.inject(MessageService), 'add');
      const fixture = await render();
      await ask(fixture, 't2');

      press('Revoke');
      await settle(fixture);

      expect(revoke).toHaveBeenCalledExactlyOnceWith(script);
      expect(add).toHaveBeenCalledExactlyOnceWith(
        expect.objectContaining({
          severity: 'success',
          summary: 'Token revoked',
          detail: 'backup script',
        }),
      );
    });

    it('does nothing when the person keeps the token', async () => {
      const add = vi.spyOn(TestBed.inject(MessageService), 'add');
      const fixture = await render();
      await ask(fixture);

      press('Keep it');
      await settle(fixture);

      expect(revoke).not.toHaveBeenCalled();
      expect(add).not.toHaveBeenCalled();
    });

    it('toasts the problem, and says nothing of success, when the token cannot be revoked', async () => {
      revoke.mockRejectedValue(refusal(403, 'Forbidden', 'A token cannot revoke another one.'));
      const add = vi.spyOn(TestBed.inject(MessageService), 'add');
      const fixture = await render();
      await ask(fixture);

      press('Revoke');
      await settle(fixture);

      expect(add).toHaveBeenCalledExactlyOnceWith(
        expect.objectContaining({
          summary: 'Forbidden',
          detail: 'A token cannot revoke another one.',
        }),
      );
    });
  });

  describe('a new token', () => {
    const dialog = (fixture: ComponentFixture<Tokens>) =>
      fixture.debugElement.query(By.directive(NewTokenDialog));

    it('opens the dialog for it', async () => {
      const fixture = await render();
      expect(dialog(fixture).componentInstance.visible()).toBe(false);

      el(fixture, 'new-token')?.click();
      await settle(fixture);

      expect(dialog(fixture).componentInstance.visible()).toBe(true);
      expect(el(fixture, 'token-name')).not.toBeNull();
    });

    it('closes again when the dialog asks to be closed', async () => {
      const fixture = await render();
      el(fixture, 'new-token')?.click();
      await settle(fixture);

      dialog(fixture).triggerEventHandler('visibleChange', false);
      await settle(fixture);

      expect(dialog(fixture).componentInstance.visible()).toBe(false);
      expect(el(fixture, 'token-name')).toBeNull();
    });

    it('shows the plaintext once, when the token is made, with the scope and the expiry that apply', async () => {
      const fixture = await render();

      dialog(fixture).triggerEventHandler('created', made);
      await settle(fixture);

      expect((el(fixture, 'secret-value') as HTMLInputElement).value).toBe(plaintext);
      expect(host(fixture).querySelector('.p-dialog-title')?.textContent).toBe('Your new token');
      const warning = text(host(fixture).querySelector('[role="alert"]')) ?? '';
      expect(warning).toContain('Shown once.');
      expect(warning).toContain('bearer credential: whoever holds it acts as you');
      expect(warning).toContain(`with write scope, until ${dateTime('2026-12-31T10:00:00Z')}.`);
      expect(warning).toContain('cannot show it again');
      expect(warning).toContain('revoke it and make another');
    });

    it('forgets the plaintext when the person has stored it', async () => {
      const fixture = await render();
      dialog(fixture).triggerEventHandler('created', made);
      await settle(fixture);

      el(fixture, 'secret-done')?.click();
      await settle(fixture);

      expect(el(fixture, 'secret-value')).toBeNull();
      expect(fixture.componentInstance['secret']()).toBeNull();
      expect(document.body.innerHTML).not.toContain(plaintext);
    });

    it('keeps the plaintext out of the page when it is closed another way, such as with Escape', async () => {
      const fixture = await render();
      dialog(fixture).triggerEventHandler('created', made);
      await settle(fixture);

      fixture.debugElement
        .query(By.css('app-secret-dialog p-dialog'))
        .triggerEventHandler('visibleChange', false);
      await settle(fixture);

      expect(fixture.componentInstance['secret']()).toBeNull();
      expect(document.body.innerHTML).not.toContain(plaintext);
    });

    it('holds the plaintext in one place only, and in none once the dialog is closed', async () => {
      const fixture = await render();
      const held = () =>
        Object.values(fixture.componentInstance)
          .filter((value) => isSignal(value))
          .map((value) => JSON.stringify((value as () => unknown)()) ?? '')
          .filter((json) => json.includes(plaintext));
      dialog(fixture).triggerEventHandler('created', made);
      await settle(fixture);
      expect(held()).toEqual([JSON.stringify(plaintext)]);

      el(fixture, 'secret-done')?.click();
      await settle(fixture);

      expect(held()).toEqual([]);
    });

    it('shows nothing for an answer that has no plaintext', async () => {
      const fixture = await render();

      dialog(fixture).triggerEventHandler('created', { ...made, token: undefined });
      await settle(fixture);

      expect(el(fixture, 'secret-value')).toBeNull();
      expect(fixture.componentInstance['secret']()).toBeNull();
    });

    it('stores the plaintext nowhere', async () => {
      const setItem = vi.spyOn(Storage.prototype, 'setItem');
      const fixture = await render();
      dialog(fixture).triggerEventHandler('created', made);
      await settle(fixture);
      el(fixture, 'secret-done')?.click();
      await settle(fixture);

      expect(setItem).not.toHaveBeenCalled();
      expect(JSON.stringify({ ...localStorage })).not.toContain(plaintext);
      expect(JSON.stringify({ ...sessionStorage })).not.toContain(plaintext);
      setItem.mockRestore();
    });
  });

  describe('without tokens', () => {
    it('says there are none', async () => {
      list.set([]);

      const fixture = await render();

      expect(el(fixture, 'tokens-empty')?.textContent?.trim()).toBe('You have no tokens yet.');
    });

    it('does not say so while the tokens load', async () => {
      list.set([]);
      loading.set(true);

      const fixture = await render();
      expect(el(fixture, 'tokens-empty')).toBeNull();

      loading.set(false);
      await settle(fixture);
      expect(el(fixture, 'tokens-empty')?.textContent?.trim()).toBe('You have no tokens yet.');
    });

    it('says why the tokens could not be loaded, with the detail of the problem', async () => {
      list.set([]);
      error.set(refusal(503, 'The service is not ready', 'The database is starting.'));

      const fixture = await render();

      expect(el(fixture, 'tokens-empty')?.textContent?.trim()).toBe(
        'The tokens could not be loaded: The database is starting.',
      );
    });

    it('says why with the title when the problem has no detail', async () => {
      list.set([]);
      error.set(refusal(403, 'Forbidden', ''));

      const fixture = await render();

      expect(el(fixture, 'tokens-empty')?.textContent?.trim()).toBe(
        'The tokens could not be loaded: Forbidden',
      );
    });
  });
});
