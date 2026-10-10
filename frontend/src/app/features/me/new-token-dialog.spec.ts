import { HttpErrorResponse } from '@angular/common/http';
import { isSignal, signal, WritableSignal } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { By } from '@angular/platform-browser';
import { MessageService } from 'primeng/api';
import { Select } from 'primeng/select';
import type { MockInstance } from 'vitest';
import {
  AuthOptions,
  Capability,
  Membership,
  Problem,
  Project,
  TokenCreated,
} from '../../api/models';
import { CAPABILITY } from '../../api/models/capability-array';
import { AuthService } from '../../core/auth.service';
import { SessionService } from '../../core/session.service';
import { TokensService } from '../../core/tokens.service';
import { assisted, capabilityMeanings, selectableCapabilities } from '../../shared/capabilities';
import { maxLifetimeDays, NewTokenDialog, scopeMeanings } from './new-token-dialog';

const acme: Membership = {
  role: 'admin',
  team: { slug: 'acme', name: 'Acme Corp' },
  tenant: { slug: 'acme', name: 'Acme Corp' },
  origins: [{ source: 'grant', role: 'admin' }],
};
const globex: Membership = {
  role: 'member',
  team: { slug: 'globex', name: 'Globex' },
  tenant: { slug: 'globex', name: 'Globex' },
  origins: [{ source: 'grant', role: 'member' }],
};

const project = (key: string, name: string): Project => ({
  id: `id-${key}`,
  key,
  name,
  description: '',
  restricted: false,
  created_at: '2026-10-01T10:00:00Z',
  updated_at: '2026-10-01T10:00:00Z',
  version: 1,
  wip_limits: {},
});

const plaintext = `cwk_${'B'.repeat(43)}`;
const issued: TokenCreated = {
  id: 'id-new',
  name: 'claude on my laptop',
  scope: 'write',
  agent: true,
  capabilities: [...selectableCapabilities],
  created_at: '2026-10-03T10:00:00Z',
  expires_at: '2026-12-31T10:00:00Z',
  last_used_on: null,
  revoked_at: null,
  restricted_team: null,
  restricted_tenant: null,
  restricted_project: null,
  state: 'active',
  token: plaintext,
};

function refusal(
  status: number,
  code: Problem['code'],
  errors: { pointer: string; message: string }[] = [],
) {
  const body: Problem = {
    type: 'about:blank',
    title: 'The token is not valid',
    status,
    detail: 'Check the fields.',
    code,
    errors,
  };
  return new HttpErrorResponse({ status, statusText: body.title, error: body });
}

describe('the vocabulary of a token', () => {
  it('says what each scope reaches', () => {
    expect(Object.keys(scopeMeanings)).toEqual(['read', 'write', 'admin']);
  });

  it('says what each of the nine capabilities lets an agent do', () => {
    expect(selectableCapabilities).toHaveLength(9);
    expect(Object.keys(capabilityMeanings).sort()).toEqual([...selectableCapabilities].sort());
  });

  it('offers a switch for every capability of the catalogue, in its order (docs/adr/0043 D4)', () => {
    expect(selectableCapabilities).toEqual(CAPABILITY);
  });

  it('has the assisted set of docs/adr/0043 D4: decide, close, rank, create-project and record-answer are off', () => {
    expect(assisted).toEqual(['drop', 'set-horizon', 'interest', 'upload']);
  });

  it('takes up to 3650 days, the bound of the schema, within which the installation holds its own maximum (docs/adr/0035 D4)', () => {
    expect(maxLifetimeDays).toBe(3650);
  });

  it('names the strongest acts of the admin scope, the irreversible ones among them', () => {
    expect(scopeMeanings.admin).toContain('team settings and the time lock');
    expect(scopeMeanings.admin).toContain('archiving projects');
    expect(scopeMeanings.admin).toContain('the confidential flag');
    expect(scopeMeanings.admin).toContain("withdrawing other people's comments");
    expect(scopeMeanings.admin).toContain(
      'unlocking, ending the sessions of and deactivating (for good) the local accounts of the team',
    );
  });

  it('says that the write scope also revokes the other tokens of the person and creates projects', () => {
    expect(scopeMeanings.write).toContain('revokes your other tokens');
    expect(scopeMeanings.write).toContain('creates projects where you may');
  });
});

describe('NewTokenDialog', () => {
  let create: MockInstance<TokensService['create']>;
  let projectsOf: MockInstance<TokensService['projectsOf']>;
  let memberships: WritableSignal<Membership[]>;
  /** What `/auth/options` answered; undefined while it has not, or could not be read. */
  let options: WritableSignal<AuthOptions | undefined>;
  let warn: MockInstance<typeof console.warn>;

  beforeEach(() => {
    create = vi.fn<TokensService['create']>().mockResolvedValue(issued);
    projectsOf = vi
      .fn<TokensService['projectsOf']>()
      .mockResolvedValue([project('COW', 'cowork'), project('OPS', 'operations')]);
    memberships = signal([acme, globex]);
    options = signal<AuthOptions | undefined>(undefined);
    warn = vi.spyOn(console, 'warn');
    TestBed.configureTestingModule({
      providers: [
        MessageService,
        { provide: TokensService, useValue: { create, projectsOf } },
        { provide: SessionService, useValue: { memberships } },
        {
          provide: AuthService,
          useValue: {
            options: { hasValue: () => options() !== undefined, value: () => options() },
          },
        },
      ],
    });
  });

  afterEach(() => {
    // Restore first, so that an assertion that fails does not leave the spy or the stub behind. A
    // field that cannot register with the form it sits in is a warning of development builds
    // (NG01354); the dialog must not cause one.
    vi.unstubAllGlobals();
    const warnings = warn.mock.calls.filter((call) => String(call[0]).includes('NG01354'));
    warn.mockRestore();
    expect(warnings).toEqual([]);
  });

  async function render(visible = true) {
    const fixture = TestBed.createComponent(NewTokenDialog);
    fixture.componentRef.setInput('visible', visible);
    await settle(fixture);
    return fixture;
  }

  /** Lets what a submit started finish, and shows it; fields register with the form a moment later. */
  async function settle(fixture: ComponentFixture<NewTokenDialog>) {
    for (let round = 0; round < 2; round++) {
      await new Promise((resolve) => setTimeout(resolve));
      fixture.detectChanges();
    }
    await fixture.whenStable();
  }

  const host = (fixture: ComponentFixture<NewTokenDialog>) => fixture.nativeElement as HTMLElement;
  const el = (fixture: ComponentFixture<NewTokenDialog>, testId: string) =>
    host(fixture).querySelector<HTMLElement>(`[data-testid="${testId}"]`);
  const text = (fixture: ComponentFixture<NewTokenDialog>, testId: string) =>
    el(fixture, testId)?.textContent?.replace(/\s+/g, ' ').trim();
  const select = (fixture: ComponentFixture<NewTokenDialog>, testId: string) =>
    fixture.debugElement.query(By.css(`[data-testid="${testId}"]`)).componentInstance as Select;
  const label = (fixture: ComponentFixture<NewTokenDialog>, testId: string) =>
    el(fixture, testId)?.querySelector('.p-select-label')?.textContent?.trim();

  function typeInto(fixture: ComponentFixture<NewTokenDialog>, testId: string, value: string) {
    const field = el(fixture, testId) as HTMLInputElement;
    field.value = value;
    field.dispatchEvent(new Event('input'));
    fixture.detectChanges();
  }

  /** What choosing in a PrimeNG select, checkbox or number field tells its model; what it shows follows. */
  async function choose(fixture: ComponentFixture<NewTokenDialog>, testId: string, value: unknown) {
    fixture.debugElement
      .query(By.css(`[data-testid="${testId}"]`))
      .triggerEventHandler('ngModelChange', value);
    await settle(fixture);
  }

  const submit = (fixture: ComponentFixture<NewTokenDialog>) =>
    host(fixture)
      .querySelector('form')
      ?.dispatchEvent(new Event('submit', { cancelable: true }));

  const saveButton = (fixture: ComponentFixture<NewTokenDialog>) =>
    el(fixture, 'token-save') as HTMLButtonElement | null;

  const cancelButton = (fixture: ComponentFixture<NewTokenDialog>) =>
    el(fixture, 'token-cancel') as HTMLButtonElement | null;

  /** A key press as the browser makes one: aimed at the focused element, on its way up to the document. */
  const press = (key: string, target: EventTarget = document.body) =>
    target.dispatchEvent(new KeyboardEvent('keydown', { key, bubbles: true, cancelable: true }));

  const mask = () => document.querySelector('.p-dialog-mask') as HTMLElement | null;

  async function fill(fixture: ComponentFixture<NewTokenDialog>, name = 'claude on my laptop') {
    typeInto(fixture, 'token-name', name);
    await settle(fixture);
  }

  describe('the dialog', () => {
    it('is closed until it is asked to open', async () => {
      const fixture = await render(false);

      expect(el(fixture, 'token-name')).toBeNull();
    });

    it('asks for a name, a scope, the agent flag, a tenant and a lifetime under the header of a new token', async () => {
      const fixture = await render();

      expect(host(fixture).querySelector('.p-dialog-title')?.textContent).toBe('New token');
      for (const testId of [
        'token-name',
        'token-scope',
        'token-agent',
        'token-tenant',
        'token-lifetime',
      ]) {
        expect(el(fixture, testId), testId).not.toBeNull();
      }
      expect(el(fixture, 'token-capabilities')).toBeNull();
      expect(el(fixture, 'token-project')).toBeNull();
    });

    it('limits the name to a hundred characters, as the schema does', async () => {
      const fixture = await render();

      expect(el(fixture, 'token-name')?.getAttribute('maxlength')).toBe('100');
    });

    it('says that the name shows on what the token does, to everyone who reads the ticket', async () => {
      const fixture = await render();

      const hint = el(fixture, 'token-name-hint');
      expect(hint?.id).toBe('token-name-hint');
      expect(hint?.textContent?.replace(/\s+/g, ' ').trim()).toBe(
        'The name shows on what the token does: everyone who can read a ticket sees it beside the ' +
          'acts made through this token — changes, comments, questions, answers, files, time — also ' +
          'after you revoke it.',
      );
    });

    it('starts at the least it can be: read scope, no agent, no restriction, the default lifetime of the installation', async () => {
      const fixture = await render();

      expect(label(fixture, 'token-scope')).toBe('read');
      expect(text(fixture, 'token-scope-meaning')).toBe(scopeMeanings.read);
      expect((el(fixture, 'token-agent')?.querySelector('input') as HTMLInputElement).checked).toBe(
        false,
      );
      expect(label(fixture, 'token-tenant')).toBe('Any team of yours');
      const lifetime = el(fixture, 'token-lifetime')?.querySelector('input');
      expect(lifetime?.value).toBe('');
      expect(lifetime?.placeholder).toBe('Installation default');
    });

    it('closes with Escape, with the cross and with a click beside it while nothing is running', async () => {
      const fixture = await render();
      expect(document.querySelector('.p-dialog-close-button')).not.toBeNull();

      press('Escape');
      await settle(fixture);
      expect(fixture.componentInstance.visible()).toBe(false);

      fixture.componentInstance.visible.set(true);
      await settle(fixture);
      mask()?.dispatchEvent(new MouseEvent('mousedown', { bubbles: true }));
      await settle(fixture);
      expect(fixture.componentInstance.visible()).toBe(false);
    });

    it('closes when the dialog asks to be closed', async () => {
      const fixture = await render();

      fixture.debugElement.query(By.css('p-dialog')).triggerEventHandler('visibleChange', false);
      await settle(fixture);

      expect(fixture.componentInstance.visible()).toBe(false);
      expect(el(fixture, 'token-name')).toBeNull();
    });

    it('closes with Cancel, without creating anything', async () => {
      const fixture = await render();
      await fill(fixture);

      el(fixture, 'token-cancel')?.click();
      await settle(fixture);

      expect(fixture.componentInstance.visible()).toBe(false);
      expect(create).not.toHaveBeenCalled();
    });
  });

  describe('the scope', () => {
    it('offers read, write and admin, and says what the chosen one reaches', async () => {
      const fixture = await render();

      expect(select(fixture, 'token-scope').options()).toEqual([
        { value: 'read', disabled: false },
        { value: 'write', disabled: false },
        { value: 'admin', disabled: false },
      ]);

      await choose(fixture, 'token-scope', 'admin');
      expect(label(fixture, 'token-scope')).toBe('admin');
      expect(text(fixture, 'token-scope-meaning')).toBe(scopeMeanings.admin);
    });

    it('does not offer admin to an agent token, whose scope is at most write', async () => {
      const fixture = await render();

      await choose(fixture, 'token-agent', true);

      expect(select(fixture, 'token-scope').options()).toEqual([
        { value: 'read', disabled: false },
        { value: 'write', disabled: false },
        { value: 'admin', disabled: true },
      ]);
      expect(text(fixture, 'token-scope-meaning')).toContain(scopeMeanings.read);
      expect(text(fixture, 'token-scope-meaning')).toContain(
        'An agent token has at most write scope',
      );
      expect(text(fixture, 'token-scope-meaning')).toContain(
        'booking time, revoking tokens, administration — is never its own',
      );
    });

    it('lowers an admin choice to write when the agent flag is asked for, and keeps read and write as they are', async () => {
      const fixture = await render();
      await choose(fixture, 'token-scope', 'admin');
      await choose(fixture, 'token-agent', true);
      expect(label(fixture, 'token-scope')).toBe('write');

      await choose(fixture, 'token-agent', false);
      await choose(fixture, 'token-scope', 'read');
      await choose(fixture, 'token-agent', true);
      expect(label(fixture, 'token-scope')).toBe('read');
    });

    it('offers admin again when the agent flag goes, and the hint goes with it', async () => {
      const fixture = await render();
      await choose(fixture, 'token-agent', true);

      await choose(fixture, 'token-agent', false);

      expect(select(fixture, 'token-scope').options()?.[2]).toEqual({
        value: 'admin',
        disabled: false,
      });
      expect(text(fixture, 'token-scope-meaning')).toBe(scopeMeanings.read);
    });
  });

  describe('the capabilities of an agent', () => {
    async function agent(fixture: ComponentFixture<NewTokenDialog>) {
      await choose(fixture, 'token-agent', true);
    }

    it('are asked for only when the token is for an agent, all nine on', async () => {
      const fixture = await render();
      expect(el(fixture, 'token-capabilities')).toBeNull();

      await agent(fixture);

      expect(el(fixture, 'token-capabilities')).not.toBeNull();
      expect(select(fixture, 'token-capabilities').multiple()).toBe(true);
      expect(text(fixture, 'token-capabilities-count')).toContain('9 of 9 chosen');
    });

    it('offer the nine in the order of the vocabulary, each with what it lets an agent do', async () => {
      const fixture = await render();
      await agent(fixture);

      const options = select(fixture, 'token-capabilities').options() as {
        value: Capability;
        meaning: string;
      }[];
      expect(options.map((option) => option.value)).toEqual([...selectableCapabilities]);
      expect(options.map((option) => option.meaning)).toEqual(
        selectableCapabilities.map((each) => capabilityMeanings[each]),
      );
    });

    it('show what each one lets an agent do beside its name when the list is open', async () => {
      // The overlay of a select asks the browser whether the screen is a small one; jsdom cannot say.
      vi.stubGlobal(
        'matchMedia',
        vi.fn().mockReturnValue({
          matches: false,
          addEventListener: () => undefined,
          removeEventListener: () => undefined,
        }),
      );
      const fixture = await render();
      await agent(fixture);

      select(fixture, 'token-capabilities').show();
      await settle(fixture);

      const items = [...document.body.querySelectorAll('.p-select-option .capability')];
      expect(items.map((item) => item.querySelector('span')?.textContent)).toEqual([...selectableCapabilities]);
      expect(items.map((item) => item.querySelector('small')?.textContent)).toEqual(
        selectableCapabilities.map((each) => capabilityMeanings[each]),
      );
    });

    it('show the capabilities that are chosen, by name', async () => {
      const fixture = await render();
      await agent(fixture);

      expect(label(fixture, 'token-capabilities')).toBe(selectableCapabilities.join(', '));
    });

    it('are named for a screen reader, as the other selects of the dialog are, and grouped with their shortcuts', async () => {
      const fixture = await render();
      await agent(fixture);

      const group = host(fixture).querySelector('[role="group"]');
      expect(group?.getAttribute('aria-labelledby')).toBe('token-capabilities-label');
      expect(group?.contains(el(fixture, 'token-capabilities-full'))).toBe(true);
      expect(group?.contains(el(fixture, 'token-capabilities-assisted'))).toBe(true);

      expect(host(fixture).querySelector('#token-capabilities-label')?.textContent).toBe(
        'Capabilities',
      );
      expect(select(fixture, 'token-capabilities').ariaLabelledBy()).toBe(
        'token-capabilities-label',
      );
    });

    it('follow the choice, and say how many are chosen', async () => {
      const fixture = await render();
      await agent(fixture);

      await choose(fixture, 'token-capabilities', ['rank', 'drop']);

      expect(label(fixture, 'token-capabilities')).toBe('rank, drop');
      expect(text(fixture, 'token-capabilities-count')).toContain('2 of 9 chosen');
    });

    it('become the assisted set with its button, and all of them again with the other', async () => {
      const fixture = await render();
      await agent(fixture);

      el(fixture, 'token-capabilities-assisted')?.click();
      await settle(fixture);
      expect(label(fixture, 'token-capabilities')).toBe('drop, set-horizon, interest, upload');
      expect(text(fixture, 'token-capabilities-count')).toContain('4 of 9 chosen');

      el(fixture, 'token-capabilities-full')?.click();
      await settle(fixture);
      expect(text(fixture, 'token-capabilities-count')).toContain('9 of 9 chosen');
    });

    it('may be left empty: the agent then has the baseline only, which the dialog says', async () => {
      const fixture = await render();
      await agent(fixture);
      await fill(fixture);
      expect(el(fixture, 'token-capabilities-none')).toBeNull();

      await choose(fixture, 'token-capabilities', []);
      expect(text(fixture, 'token-capabilities-none')).toBe(
        'No capability: the baseline only — filing and editing tickets, comments, links, questions, progress and a watch stake.',
      );
      expect(el(fixture, 'token-capabilities-none')?.classList).toContain('muted');
      expect(el(fixture, 'token-capabilities-none')?.classList).not.toContain('error');
      expect(saveButton(fixture)?.disabled).toBe(false);
      expect(text(fixture, 'token-capabilities-count')).toContain('0 of 9 chosen');
      expect(label(fixture, 'token-capabilities')).toBe('None: the baseline only');

      await choose(fixture, 'token-capabilities', null);
      expect(saveButton(fixture)?.disabled).toBe(false);
      expect(el(fixture, 'token-capabilities-none')).not.toBeNull();

      await choose(fixture, 'token-capabilities', ['upload']);
      expect(el(fixture, 'token-capabilities-none')).toBeNull();
      expect(saveButton(fixture)?.disabled).toBe(false);
    });

    it('never say that a token that names none is given every capability, which the API no longer does', async () => {
      const fixture = await render();
      await agent(fixture);
      await choose(fixture, 'token-capabilities', []);

      expect(host(fixture).textContent).not.toContain('every capability');
      expect(host(fixture).textContent).not.toContain('Choose at least one');
      expect(host(fixture).querySelector('.error')).toBeNull();
    });

    it('can be emptied with nothing but a click on the selected options, and the baseline is what is left', async () => {
      const fixture = await render();
      await agent(fixture);
      await fill(fixture);

      await choose(fixture, 'token-capabilities', []);
      submit(fixture);
      await settle(fixture);

      expect(create.mock.calls[0][0]).toStrictEqual({
        name: 'claude on my laptop',
        scope: 'read',
        agent: true,
        capabilities: [],
      });
    });

    it('go with the flag: a token that is no agent sends none, and the next one starts with all nine', async () => {
      const fixture = await render();
      await agent(fixture);
      await choose(fixture, 'token-capabilities', ['rank']);
      await choose(fixture, 'token-agent', false);
      await fill(fixture);

      submit(fixture);
      await settle(fixture);

      expect(create.mock.calls[0][0]).not.toHaveProperty('capabilities');
      expect(create.mock.calls[0][0]).not.toHaveProperty('agent');
      fixture.componentInstance.visible.set(true);
      await settle(fixture);
      await agent(fixture);
      expect(text(fixture, 'token-capabilities-count')).toContain('9 of 9 chosen');
    });
  });

  describe('the restriction', () => {
    it('offers the tenants of the person, by name and slug, and none to choose from while there are none', async () => {
      const fixture = await render();

      expect(select(fixture, 'token-tenant').options()).toEqual([
        { slug: 'acme', label: 'Acme Corp (acme)' },
        { slug: 'globex', label: 'Globex (globex)' },
      ]);

      memberships.set([]);
      await settle(fixture);
      expect(select(fixture, 'token-tenant').options()).toEqual([]);
    });

    it('can be cleared again, and says so with a clear icon', async () => {
      const fixture = await render();

      expect(select(fixture, 'token-tenant').showClear()).toBe(true);
    });

    it('asks for a project only once there is a tenant, and loads the projects of that tenant', async () => {
      const fixture = await render();
      expect(projectsOf).not.toHaveBeenCalled();

      await choose(fixture, 'token-tenant', 'acme');

      expect(projectsOf).toHaveBeenCalledExactlyOnceWith('acme');
      expect(el(fixture, 'token-project')).not.toBeNull();
      expect(select(fixture, 'token-project').options()).toEqual([
        { key: 'COW', label: 'COW · cowork' },
        { key: 'OPS', label: 'OPS · operations' },
      ]);
      expect(label(fixture, 'token-project')).toBe('Any project');
    });

    it('takes the projects of the tenant chosen last, and none of the one before', async () => {
      const fixture = await render();
      await choose(fixture, 'token-tenant', 'acme');
      projectsOf.mockResolvedValue([project('GLX', 'globex work')]);

      await choose(fixture, 'token-tenant', 'globex');

      expect(projectsOf).toHaveBeenLastCalledWith('globex');
      expect(select(fixture, 'token-project').options()).toEqual([
        { key: 'GLX', label: 'GLX · globex work' },
      ]);
    });

    it('forgets the project when the tenant changes or goes, and no project is asked for without one', async () => {
      const fixture = await render();
      await choose(fixture, 'token-tenant', 'acme');
      await choose(fixture, 'token-project', 'COW');
      expect(label(fixture, 'token-project')).toBe('COW · cowork');

      await choose(fixture, 'token-tenant', 'globex');
      expect(label(fixture, 'token-project')).toBe('Any project');

      await choose(fixture, 'token-project', 'COW');
      await choose(fixture, 'token-tenant', null);
      expect(el(fixture, 'token-project')).toBeNull();
      await fill(fixture);
      submit(fixture);
      await settle(fixture);
      expect(create.mock.calls[0][0]).not.toHaveProperty('tenant');
      expect(create.mock.calls[0][0]).not.toHaveProperty('project');
    });

    it('shows the project select as loading while the projects come', async () => {
      let finish: (projects: Project[]) => void = () => undefined;
      projectsOf.mockReturnValue(new Promise<Project[]>((resolve) => (finish = resolve)));
      const fixture = await render();

      // A promise that is not answered keeps the fixture from ever being stable: do not wait for it.
      fixture.debugElement
        .query(By.css('[data-testid="token-tenant"]'))
        .triggerEventHandler('ngModelChange', 'acme');
      await new Promise((resolve) => setTimeout(resolve));
      fixture.detectChanges();
      expect(select(fixture, 'token-project').loading()).toBe(true);

      finish([project('COW', 'cowork')]);
      await settle(fixture);
      expect(select(fixture, 'token-project').loading()).toBe(false);
      expect(select(fixture, 'token-project').options()).toHaveLength(1);
    });

    it('says so when the projects of the tenant could not be loaded, and offers none', async () => {
      projectsOf.mockRejectedValue(new HttpErrorResponse({ status: 404, statusText: 'Not Found' }));
      const fixture = await render();

      await choose(fixture, 'token-tenant', 'acme');

      expect(text(fixture, 'token-project-failed')).toBe(
        'The projects of this team could not be loaded.',
      );
      expect(select(fixture, 'token-project').options()).toEqual([]);
    });

    it('says that a restriction narrows the token, and that none reaches every tenant', async () => {
      const fixture = await render();

      expect(host(fixture).textContent).toContain(
        'An unrestricted one reaches every team you belong to, now and later.',
      );
    });
  });

  describe('the lifetime', () => {
    const input = (fixture: ComponentFixture<NewTokenDialog>) =>
      el(fixture, 'token-lifetime')?.querySelector('input') as HTMLInputElement;

    it('is empty to begin with, which is the installation default, and says so', async () => {
      const fixture = await render();

      expect(input(fixture).value).toBe('');
      expect(input(fixture).placeholder).toBe('Installation default');
      expect(text(fixture, 'token-lifetime-hint')).toContain(
        "Empty is the installation's default.",
      );
      expect(text(fixture, 'token-lifetime-hint')).toContain('Up to 3650 days');
      expect(text(fixture, 'token-lifetime-hint')).toContain(
        'the installation may shorten it, and the token shows its expiry once it exists',
      );
    });

    it('is a number of days from one to 3650, whole days only', async () => {
      const fixture = await render();

      const field = fixture.debugElement.query(By.css('[data-testid="token-lifetime"]'))
        .componentInstance as { min(): number; max(): number; maxFractionDigits(): number };
      expect(field.min()).toBe(1);
      expect(field.max()).toBe(3650);
      expect(field.maxFractionDigits()).toBe(0);
    });

    it('needs no value, because empty is the installation default', async () => {
      const fixture = await render();
      await fill(fixture);

      expect(saveButton(fixture)?.disabled).toBe(false);
    });

    it.each<[number, string]>([
      [0, 'zero days'],
      [-5, 'a negative number of days'],
      [3651, 'more than the schema takes'],
      [1.5, 'a fraction of a day'],
    ])('cannot be %j: %s', async (days) => {
      const fixture = await render();
      await fill(fixture);
      expect(saveButton(fixture)?.disabled).toBe(false);

      await choose(fixture, 'token-lifetime', days);

      expect(saveButton(fixture)?.disabled).toBe(true);
      submit(fixture);
      await settle(fixture);
      expect(create).not.toHaveBeenCalled();
    });

    it.each([1, 30, 365, 366, 3650])('can be %i days, which is sent as it is', async (days) => {
      const fixture = await render();
      await fill(fixture);

      await choose(fixture, 'token-lifetime', days);
      expect(saveButton(fixture)?.disabled).toBe(false);
      submit(fixture);
      await settle(fixture);

      expect(create.mock.calls[0][0].lifetime_days).toBe(days);
    });

    describe("with the installation's maximum (docs/adr/0035 D4)", () => {
      const installation = (days: number): AuthOptions => ({
        local: true,
        oidc: false,
        oidc_name: null,
        password_min_length: 12,
        token_max_lifetime_days: days,
      });
      const field = (fixture: ComponentFixture<NewTokenDialog>) =>
        fixture.debugElement.query(By.css('[data-testid="token-lifetime"]')).componentInstance as {
          max(): number;
          $disabled(): boolean;
        };

      it('goes up to the longest lifetime the installation gives a token, and says so', async () => {
        options.set(installation(30));
        const fixture = await render();

        expect(field(fixture).max()).toBe(30);
        expect(text(fixture, 'token-lifetime-hint')).toBe(
          "Empty is the installation's default. Up to 30 days, the longest this installation gives a token.",
        );
      });

      it('takes the maximum and refuses a day more', async () => {
        options.set(installation(30));
        const fixture = await render();
        await fill(fixture);

        await choose(fixture, 'token-lifetime', 31);
        expect(saveButton(fixture)?.disabled).toBe(true);

        await choose(fixture, 'token-lifetime', 30);
        expect(saveButton(fixture)?.disabled).toBe(false);
        submit(fixture);
        await settle(fixture);
        expect(create.mock.calls[0][0].lifetime_days).toBe(30);
      });

      it('follows the answer that arrives after the dialog opened', async () => {
        const fixture = await render();
        expect(field(fixture).max()).toBe(3650);

        options.set(installation(365));
        await settle(fixture);

        expect(field(fixture).max()).toBe(365);
        expect(text(fixture, 'token-lifetime-hint')).toContain('Up to 365 days');
      });

      it('stays within the bound of the schema when the installation allows longer', async () => {
        options.set(installation(7300));
        const fixture = await render();

        expect(field(fixture).max()).toBe(3650);
        expect(text(fixture, 'token-lifetime-hint')).toContain('Up to 3650 days');
      });

      it('takes no number when the installation gives a token less than a day, only its default', async () => {
        options.set(installation(0));
        const fixture = await render();
        await fill(fixture);

        expect(field(fixture).$disabled()).toBe(true);
        expect(text(fixture, 'token-lifetime-hint')).toBe(
          'This installation gives a token less than a day: leave it empty for its default.',
        );
        expect(saveButton(fixture)?.disabled).toBe(false);
        submit(fixture);
        await settle(fixture);
        expect(create.mock.calls[0][0]).not.toHaveProperty('lifetime_days');
      });
    });

    it('is left out of the request again when it was filled and emptied, which is the default once more', async () => {
      const fixture = await render();
      await fill(fixture);
      await choose(fixture, 'token-lifetime', 30);
      await choose(fixture, 'token-lifetime', null);

      expect(saveButton(fixture)?.disabled).toBe(false);
      submit(fixture);
      await settle(fixture);

      expect(create.mock.calls[0][0]).not.toHaveProperty('lifetime_days');
    });
  });

  describe('what a token needs', () => {
    it('cannot be created without a name, and a name of spaces is none', async () => {
      const fixture = await render();
      expect(saveButton(fixture)?.disabled).toBe(true);

      await fill(fixture, 'ci');
      expect(saveButton(fixture)?.disabled).toBe(false);

      await fill(fixture, '   ');
      expect(saveButton(fixture)?.disabled).toBe(true);
      submit(fixture);
      await settle(fixture);
      expect(create).not.toHaveBeenCalled();
    });

    it('cannot be created while another one is on its way, and shows its button as busy', async () => {
      let finish: (token: TokenCreated) => void = () => undefined;
      create.mockReturnValue(new Promise<TokenCreated>((resolve) => (finish = resolve)));
      const fixture = await render();
      await fill(fixture);

      submit(fixture);
      submit(fixture);
      await settle(fixture);
      expect(create).toHaveBeenCalledOnce();
      expect(saveButton(fixture)?.disabled).toBe(true);
      expect(saveButton(fixture)?.querySelector('i.pi-spinner')).not.toBeNull();
      finish(issued);
      await settle(fixture);

      expect(fixture.componentInstance.visible()).toBe(false);
    });
  });

  describe('creating', () => {
    it('creates a read token for the default lifetime of the installation from a name alone, and sends nothing else', async () => {
      const fixture = await render();
      await fill(fixture, '  ci  ');

      submit(fixture);
      await settle(fixture);

      expect(create).toHaveBeenCalledExactlyOnceWith({ name: 'ci', scope: 'read' });
      expect(create.mock.calls[0][0]).toStrictEqual({ name: 'ci', scope: 'read' });
    });

    it('creates what was chosen: scope, agent with its capabilities, tenant, project and lifetime', async () => {
      const fixture = await render();
      await fill(fixture, 'claude on my laptop');
      await choose(fixture, 'token-scope', 'write');
      await choose(fixture, 'token-agent', true);
      await choose(fixture, 'token-capabilities', ['upload', 'rank', 'drop']);
      await choose(fixture, 'token-tenant', 'acme');
      await choose(fixture, 'token-project', 'COW');
      await choose(fixture, 'token-lifetime', 30);

      submit(fixture);
      await settle(fixture);

      expect(create).toHaveBeenCalledExactlyOnceWith({
        name: 'claude on my laptop',
        scope: 'write',
        agent: true,
        // In the order of the vocabulary, not of the clicks.
        capabilities: ['drop', 'rank', 'upload'],
        team: 'acme',
        project: 'COW',
        lifetime_days: 30,
      });
    });

    it('creates an agent token with every capability when none was taken away, named one by one', async () => {
      const fixture = await render();
      await fill(fixture);
      await choose(fixture, 'token-agent', true);

      submit(fixture);
      await settle(fixture);

      expect(create.mock.calls[0][0]).toMatchObject({ agent: true, capabilities: [...selectableCapabilities] });
    });

    it('creates the assisted token with the four capabilities that are left', async () => {
      const fixture = await render();
      await fill(fixture);
      await choose(fixture, 'token-agent', true);
      el(fixture, 'token-capabilities-assisted')?.click();
      await settle(fixture);

      submit(fixture);
      await settle(fixture);

      expect(create.mock.calls[0][0].capabilities).toEqual([
        'drop',
        'set-horizon',
        'interest',
        'upload',
      ]);
    });

    it('creates a token restricted to a tenant alone, without a project', async () => {
      const fixture = await render();
      await fill(fixture);
      await choose(fixture, 'token-tenant', 'globex');

      submit(fixture);
      await settle(fixture);

      expect(create.mock.calls[0][0]).toStrictEqual({
        name: 'claude on my laptop',
        scope: 'read',
        team: 'globex',
      });
    });

    it('hands the token that was made on, with its plaintext, for the page to show once, and closes', async () => {
      const made: TokenCreated[] = [];
      const fixture = await render();
      fixture.componentInstance.created.subscribe((each) => made.push(each));
      await fill(fixture);

      submit(fixture);
      await settle(fixture);

      expect(made).toEqual([issued]);
      expect(made[0].token).toBe(plaintext);
      expect(fixture.componentInstance.visible()).toBe(false);
    });

    it('keeps the plaintext nowhere in the dialog once it is closed', async () => {
      const fixture = await render();
      await fill(fixture);
      submit(fixture);
      await settle(fixture);

      fixture.componentInstance.visible.set(true);
      await settle(fixture);

      expect(document.body.innerHTML).not.toContain(plaintext);
      const held = Object.values(fixture.componentInstance).filter((value) => isSignal(value));
      expect(held.length).toBeGreaterThan(5);
      expect(JSON.stringify(held.map((value) => (value as () => unknown)()))).not.toContain(
        plaintext,
      );
    });

    it('starts again from the beginning when it is opened the next time', async () => {
      const fixture = await render();
      await fill(fixture, 'claude');
      await choose(fixture, 'token-scope', 'write');
      await choose(fixture, 'token-tenant', 'acme');
      await choose(fixture, 'token-lifetime', 7);
      submit(fixture);
      await settle(fixture);

      fixture.componentInstance.visible.set(true);
      await settle(fixture);

      expect((el(fixture, 'token-name') as HTMLInputElement).value).toBe('');
      expect(label(fixture, 'token-scope')).toBe('read');
      expect(label(fixture, 'token-tenant')).toBe('Any team of yours');
      expect(el(fixture, 'token-lifetime')?.querySelector('input')?.value).toBe('');
    });

    it('starts again from the beginning after Cancel as well', async () => {
      const fixture = await render();
      await fill(fixture, 'claude');
      await choose(fixture, 'token-agent', true);

      el(fixture, 'token-cancel')?.click();
      await settle(fixture);
      fixture.componentInstance.visible.set(true);
      await settle(fixture);

      expect((el(fixture, 'token-name') as HTMLInputElement).value).toBe('');
      expect(el(fixture, 'token-capabilities')).toBeNull();
    });
  });

  describe('an answer without a plaintext', () => {
    it('is a repeated answer: nothing is shown, the dialog closes and a problem says to revoke the token', async () => {
      create.mockResolvedValue({ ...issued, token: undefined });
      const add = vi.spyOn(TestBed.inject(MessageService), 'add');
      const made: TokenCreated[] = [];
      const fixture = await render();
      fixture.componentInstance.created.subscribe((each) => made.push(each));
      await fill(fixture);

      submit(fixture);
      await settle(fixture);

      expect(made).toEqual([]);
      expect(fixture.componentInstance.visible()).toBe(false);
      expect(add).toHaveBeenCalledExactlyOnceWith(
        expect.objectContaining({
          severity: 'error',
          detail: expect.stringContaining('Revoke it in the list and make another.'),
        }),
      );
    });
  });

  describe('a token that the server refuses', () => {
    it('shows the problem of each field beside it, and keeps the dialog and what was typed', async () => {
      create.mockRejectedValue(
        refusal(422, 'validation_failed', [
          { pointer: '/name', message: 'must not be blank' },
          { pointer: '/scope', message: 'an agent token has at most write scope' },
          { pointer: '/capabilities', message: 'only an agent token carries capabilities' },
          { pointer: '/team', message: 'no such team' },
          { pointer: '/project', message: 'no such project' },
          { pointer: '/lifetime_days', message: 'must be at least 1' },
        ]),
      );
      const add = vi.spyOn(TestBed.inject(MessageService), 'add');
      const fixture = await render();
      await choose(fixture, 'token-agent', true);
      await fill(fixture, 'claude');
      await choose(fixture, 'token-tenant', 'acme');
      await choose(fixture, 'token-project', 'COW');

      submit(fixture);
      await settle(fixture);

      expect(text(fixture, 'token-name-error')).toBe('must not be blank');
      expect(text(fixture, 'token-scope-error')).toBe('an agent token has at most write scope');
      expect(text(fixture, 'token-capabilities-error')).toBe(
        'only an agent token carries capabilities',
      );
      expect(text(fixture, 'token-tenant-error')).toBe('no such team');
      expect(text(fixture, 'token-project-error')).toBe('no such project');
      expect(text(fixture, 'token-lifetime-error')).toBe('must be at least 1');
      expect(fixture.componentInstance.visible()).toBe(true);
      expect((el(fixture, 'token-name') as HTMLInputElement).value).toBe('claude');
      expect(add).not.toHaveBeenCalled();
      expect(saveButton(fixture)?.disabled).toBe(false);
    });

    // docs/adr/0005 D1: the form sends the restriction as team; a server of the release before
    // points at its name before, /tenant.
    it.each(['/team', '/tenant'])(
      'shows the refusal of the team restriction at %s beside its field',
      async (pointer) => {
        create.mockRejectedValue(
          refusal(422, 'validation_failed', [{ pointer, message: 'no such team' }]),
        );
        const fixture = await render();
        await fill(fixture, 'claude');
        await choose(fixture, 'token-tenant', 'acme');

        submit(fixture);
        await settle(fixture);

        expect(text(fixture, 'token-tenant-error')).toBe('no such team');
        const select = el(fixture, 'token-tenant')?.querySelector('[role="combobox"]');
        expect(select?.getAttribute('aria-describedby')).toContain('token-tenant-error');
      },
    );

    it('toasts a refusal that names no field, such as a token asking for a token (session_required)', async () => {
      const body: Problem = {
        type: 'about:blank',
        title: 'A browser session is needed',
        status: 403,
        detail: 'Tokens are made by a person in a browser session.',
        code: 'session_required',
      };
      create.mockRejectedValue(
        new HttpErrorResponse({ status: 403, statusText: body.title, error: body }),
      );
      const add = vi.spyOn(TestBed.inject(MessageService), 'add');
      const fixture = await render();
      await fill(fixture);

      submit(fixture);
      await settle(fixture);

      expect(add).toHaveBeenCalledExactlyOnceWith(
        expect.objectContaining({
          severity: 'warn',
          summary: 'A browser session is needed',
          detail: 'Tokens are made by a person in a browser session.',
        }),
      );
      expect(fixture.componentInstance.visible()).toBe(true);
    });

    it('shows no old problem when the next attempt is made, and creates the token when it works', async () => {
      create.mockRejectedValueOnce(
        refusal(422, 'validation_failed', [{ pointer: '/name', message: 'must not be blank' }]),
      );
      const fixture = await render();
      await fill(fixture);
      submit(fixture);
      await settle(fixture);
      expect(el(fixture, 'token-name-error')).not.toBeNull();

      await fill(fixture, 'claude 2');
      submit(fixture);
      await settle(fixture);

      expect(create).toHaveBeenCalledTimes(2);
      expect(create.mock.calls[1][0].name).toBe('claude 2');
      expect(fixture.componentInstance.visible()).toBe(false);
    });
  });

  describe('for assistive technology', () => {
    const refused = async () => {
      create.mockRejectedValue(
        refusal(422, 'validation_failed', [
          { pointer: '/name', message: 'must not be blank' },
          { pointer: '/scope', message: 'an agent token has at most write scope' },
          { pointer: '/capabilities', message: 'only an agent token carries capabilities' },
          { pointer: '/team', message: 'no such team' },
          { pointer: '/project', message: 'no such project' },
          { pointer: '/lifetime_days', message: 'must be at least 1' },
        ]),
      );
      const fixture = await render();
      await choose(fixture, 'token-agent', true);
      await fill(fixture, 'claude');
      await choose(fixture, 'token-tenant', 'acme');
      await choose(fixture, 'token-project', 'COW');
      await choose(fixture, 'token-lifetime', 30);
      submit(fixture);
      await settle(fixture);
      return fixture;
    };
    const combobox = (fixture: ComponentFixture<NewTokenDialog>, testId: string) =>
      el(fixture, testId)?.querySelector('[role="combobox"]') as HTMLElement;
    const spinbutton = (fixture: ComponentFixture<NewTokenDialog>) =>
      el(fixture, 'token-lifetime')?.querySelector('[role="spinbutton"]') as HTMLElement;
    const described = (element: Element | null | undefined) =>
      element?.getAttribute('aria-describedby')?.split(' ') ?? [];

    it('marks the name as invalid after a refusal, with the text that says why, which is an alert', async () => {
      const fixture = await refused();

      const name = el(fixture, 'token-name') as HTMLInputElement;
      expect(name.getAttribute('aria-invalid')).toBe('true');
      expect(described(name)).toEqual(['token-name-hint', 'token-name-error']);
      expect(el(fixture, 'token-name-error')?.id).toBe('token-name-error');
      expect(el(fixture, 'token-name-error')?.getAttribute('role')).toBe('alert');
    });

    it.each([
      ['token-scope', 'token-scope-error', 'token-scope-meaning'],
      ['token-capabilities', 'token-capabilities-error', 'token-capabilities-count'],
      ['token-tenant', 'token-tenant-error', null],
      ['token-project', 'token-project-error', null],
    ])(
      'marks the select %s as invalid after a refusal, and describes it by the text that says why',
      async (field, error, hint) => {
        const fixture = await refused();

        expect(combobox(fixture, field).getAttribute('aria-invalid')).toBe('true');
        expect(described(combobox(fixture, field))).toContain(error);
        if (hint) {
          expect(described(combobox(fixture, field))).toContain(hint);
        }
        expect(el(fixture, error)?.id).toBe(error);
        expect(el(fixture, error)?.getAttribute('role')).toBe('alert');
        expect(select(fixture, field).invalid()).toBe(true);
      },
    );

    it('marks the number field as invalid after a refusal, and describes it by its hint and by the text that says why', async () => {
      const fixture = await refused();

      expect(spinbutton(fixture).getAttribute('aria-invalid')).toBe('true');
      expect(described(spinbutton(fixture))).toEqual([
        'token-lifetime-hint',
        'token-lifetime-error',
      ]);
      expect(el(fixture, 'token-lifetime-error')?.id).toBe('token-lifetime-error');
      expect(el(fixture, 'token-lifetime-error')?.getAttribute('role')).toBe('alert');
    });

    it('points every description at an element that is there', async () => {
      const fixture = await refused();

      const fields = [
        el(fixture, 'token-name'),
        combobox(fixture, 'token-scope'),
        combobox(fixture, 'token-capabilities'),
        combobox(fixture, 'token-tenant'),
        combobox(fixture, 'token-project'),
        spinbutton(fixture),
      ];
      for (const field of fields) {
        expect(described(field).length).toBeGreaterThan(0);
        for (const id of described(field)) {
          expect(host(fixture).querySelector(`#${id}`), id).not.toBeNull();
        }
      }
    });

    it('describes the project by the failure to load the projects, which is an alert, and does not call it invalid', async () => {
      projectsOf.mockRejectedValue(new HttpErrorResponse({ status: 404, statusText: 'Not Found' }));
      const fixture = await render();

      await choose(fixture, 'token-tenant', 'acme');

      expect(described(combobox(fixture, 'token-project'))).toEqual(['token-project-failed']);
      expect(el(fixture, 'token-project-failed')?.id).toBe('token-project-failed');
      expect(el(fixture, 'token-project-failed')?.getAttribute('role')).toBe('alert');
      expect(combobox(fixture, 'token-project').hasAttribute('aria-invalid')).toBe(false);
    });

    it('claims nothing about a field that was not refused, and describes the fields by their hints only', async () => {
      const fixture = await render();
      await choose(fixture, 'token-agent', true);

      expect((el(fixture, 'token-name') as HTMLInputElement).getAttribute('aria-invalid')).toBe(
        'false',
      );
      expect(described(el(fixture, 'token-name'))).toEqual(['token-name-hint']);
      for (const field of ['token-scope', 'token-capabilities', 'token-tenant']) {
        expect(combobox(fixture, field).hasAttribute('aria-invalid'), field).toBe(false);
        expect(described(combobox(fixture, field)).join(' '), field).not.toContain('-error');
      }
      expect(described(combobox(fixture, 'token-scope'))).toEqual(['token-scope-meaning']);
      expect(spinbutton(fixture).hasAttribute('aria-invalid')).toBe(false);
      expect(described(spinbutton(fixture))).toEqual(['token-lifetime-hint']);
    });

    it('describes the capabilities by the baseline hint while none are chosen', async () => {
      const fixture = await render();
      await choose(fixture, 'token-agent', true);

      await choose(fixture, 'token-capabilities', []);

      expect(described(combobox(fixture, 'token-capabilities'))).toEqual([
        'token-capabilities-count',
        'token-capabilities-none',
      ]);
      expect(el(fixture, 'token-capabilities-none')?.id).toBe('token-capabilities-none');
    });

    it('takes it all away again with the next attempt', async () => {
      const fixture = await refused();
      let finish: (token: TokenCreated) => void = () => undefined;
      create.mockReturnValue(new Promise<TokenCreated>((resolve) => (finish = resolve)));

      submit(fixture);
      await settle(fixture);

      expect((el(fixture, 'token-name') as HTMLInputElement).getAttribute('aria-invalid')).toBe(
        'false',
      );
      for (const field of ['token-scope', 'token-capabilities', 'token-tenant', 'token-project']) {
        expect(combobox(fixture, field).hasAttribute('aria-invalid'), field).toBe(false);
        expect(described(combobox(fixture, field)).join(' '), field).not.toContain('-error');
      }
      expect(spinbutton(fixture).hasAttribute('aria-invalid')).toBe(false);
      finish(issued);
      await settle(fixture);
    });
  });

  describe('while the request is out', () => {
    let finish: (token: TokenCreated) => void;
    let fail: (error: unknown) => void;

    async function sending() {
      create.mockReturnValue(
        new Promise<TokenCreated>((resolve, reject) => {
          finish = resolve;
          fail = reject;
        }),
      );
      const fixture = await render();
      await fill(fixture);
      submit(fixture);
      await settle(fixture);
      return fixture;
    }

    it('cannot be closed with Cancel, with the cross, with Escape or with a click beside it', async () => {
      const fixture = await sending();

      expect(cancelButton(fixture)?.disabled).toBe(true);
      cancelButton(fixture)?.click();
      expect(document.querySelector('.p-dialog-close-button')).toBeNull();
      press('Escape');
      press('Escape', el(fixture, 'token-name') as HTMLElement);
      mask()?.dispatchEvent(new MouseEvent('mousedown', { bubbles: true }));
      await settle(fixture);

      expect(fixture.componentInstance.visible()).toBe(true);
      expect((el(fixture, 'token-name') as HTMLInputElement).value).toBe('claude on my laptop');
      finish(issued);
      await settle(fixture);
    });

    it('shows a refusal in the form that sent it, because the form is still there', async () => {
      const fixture = await sending();
      press('Escape');

      fail(refusal(422, 'validation_failed', [{ pointer: '/name', message: 'must not be blank' }]));
      await settle(fixture);

      expect(fixture.componentInstance.visible()).toBe(true);
      expect(text(fixture, 'token-name-error')).toBe('must not be blank');
      expect((el(fixture, 'token-name') as HTMLInputElement).value).toBe('claude on my laptop');
    });

    it('can be closed again once the refusal is there, with Cancel, the cross and Escape', async () => {
      const fixture = await sending();
      fail(refusal(422, 'validation_failed', [{ pointer: '/name', message: 'must not be blank' }]));
      await settle(fixture);

      expect(cancelButton(fixture)?.disabled).toBe(false);
      expect(document.querySelector('.p-dialog-close-button')).not.toBeNull();
      press('Escape');
      await settle(fixture);

      expect(fixture.componentInstance.visible()).toBe(false);
    });

    it('closes by itself when the token is made, and the page gets it', async () => {
      const made: TokenCreated[] = [];
      const fixture = await sending();
      fixture.componentInstance.created.subscribe((each) => made.push(each));

      finish(issued);
      await settle(fixture);

      expect(fixture.componentInstance.visible()).toBe(false);
      expect(made).toEqual([issued]);
    });

    it('toasts a refusal that arrives after the page has closed the dialog, and keeps no error for the next token', async () => {
      const add = vi.spyOn(TestBed.inject(MessageService), 'add');
      const fixture = await sending();

      fixture.componentInstance.visible.set(false);
      await settle(fixture);
      fail(refusal(422, 'validation_failed', [{ pointer: '/name', message: 'must not be blank' }]));
      await settle(fixture);

      expect(add).toHaveBeenCalledExactlyOnceWith(
        expect.objectContaining({ summary: 'The token is not valid' }),
      );
      fixture.componentInstance.visible.set(true);
      await settle(fixture);
      expect(el(fixture, 'token-name-error')).toBeNull();
    });
  });
});
