import { HttpErrorResponse } from '@angular/common/http';
import { isSignal, signal, WritableSignal } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { By } from '@angular/platform-browser';
import { MessageService } from 'primeng/api';
import { Select } from 'primeng/select';
import type { MockInstance } from 'vitest';
import { Capability, Membership, Problem, Project, TokenCreated } from '../../api/models';
import { CAPABILITY } from '../../api/models/capability-array';
import { SessionService } from '../../core/session.service';
import { TokensService } from '../../core/tokens.service';
import {
  assisted,
  capabilityMeanings,
  defaultLifetimeDays,
  maxLifetimeDays,
  NewTokenDialog,
  scopeMeanings,
} from './new-token-dialog';

const acme: Membership = { role: 'admin', tenant: { slug: 'acme', name: 'Acme Corp' } };
const globex: Membership = { role: 'member', tenant: { slug: 'globex', name: 'Globex' } };

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
  capabilities: [...CAPABILITY],
  created_at: '2026-10-03T10:00:00Z',
  expires_at: '2026-12-31T10:00:00Z',
  last_used_on: null,
  revoked_at: null,
  restricted_tenant: null,
  restricted_project_id: null,
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
    expect(CAPABILITY).toHaveLength(9);
    expect(Object.keys(capabilityMeanings).sort()).toEqual([...CAPABILITY].sort());
  });

  it('has the assisted set of docs/adr/0043 D4: decide, close, rank, create-project and record-answer are off', () => {
    expect(assisted).toEqual(['drop', 'override-urgency', 'interest', 'upload']);
  });

  it('starts a token at ninety days and offers a year at most (docs/adr/0035 D4)', () => {
    expect(defaultLifetimeDays).toBe(90);
    expect(maxLifetimeDays).toBe(365);
  });
});

describe('NewTokenDialog', () => {
  let create: MockInstance<TokensService['create']>;
  let projectsOf: MockInstance<TokensService['projectsOf']>;
  let memberships: WritableSignal<Membership[]>;
  let warn: MockInstance<typeof console.warn>;

  beforeEach(() => {
    create = vi.fn<TokensService['create']>().mockResolvedValue(issued);
    projectsOf = vi
      .fn<TokensService['projectsOf']>()
      .mockResolvedValue([project('COW', 'cowork'), project('OPS', 'operations')]);
    memberships = signal([acme, globex]);
    warn = vi.spyOn(console, 'warn');
    TestBed.configureTestingModule({
      providers: [
        MessageService,
        { provide: TokensService, useValue: { create, projectsOf } },
        { provide: SessionService, useValue: { memberships } },
      ],
    });
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    // A field that cannot register with the form it sits in is a warning of development builds
    // (NG01354); the dialog must not cause one.
    expect(warn.mock.calls.filter((call) => String(call[0]).includes('NG01354'))).toEqual([]);
    warn.mockRestore();
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

    it('starts at the least it can be: read scope, no agent, no restriction, ninety days', async () => {
      const fixture = await render();

      expect(label(fixture, 'token-scope')).toBe('read');
      expect(text(fixture, 'token-scope-meaning')).toBe(scopeMeanings.read);
      expect((el(fixture, 'token-agent')?.querySelector('input') as HTMLInputElement).checked).toBe(
        false,
      );
      expect(label(fixture, 'token-tenant')).toBe('Any tenant of yours');
      expect(el(fixture, 'token-lifetime')?.querySelector('input')?.value).toBe('90 days');
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
      expect(text(fixture, 'token-scope-meaning')).toBe(
        `${scopeMeanings.read} An agent token has at most write scope.`,
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
      expect(options.map((option) => option.value)).toEqual([...CAPABILITY]);
      expect(options.map((option) => option.meaning)).toEqual(
        CAPABILITY.map((each) => capabilityMeanings[each]),
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
      expect(items.map((item) => item.querySelector('span')?.textContent)).toEqual([...CAPABILITY]);
      expect(items.map((item) => item.querySelector('small')?.textContent)).toEqual(
        CAPABILITY.map((each) => capabilityMeanings[each]),
      );
    });

    it('show the capabilities that are chosen, by name', async () => {
      const fixture = await render();
      await agent(fixture);

      expect(label(fixture, 'token-capabilities')).toBe(CAPABILITY.join(', '));
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
      expect(label(fixture, 'token-capabilities')).toBe('drop, override-urgency, interest, upload');
      expect(text(fixture, 'token-capabilities-count')).toContain('4 of 9 chosen');

      el(fixture, 'token-capabilities-full')?.click();
      await settle(fixture);
      expect(text(fixture, 'token-capabilities-count')).toContain('9 of 9 chosen');
    });

    it('must not be left empty: an empty set would be given every capability by the server', async () => {
      const fixture = await render();
      await agent(fixture);
      await fill(fixture);
      expect(saveButton(fixture)?.disabled).toBe(false);
      expect(el(fixture, 'token-capabilities-none')).toBeNull();

      await choose(fixture, 'token-capabilities', []);
      expect(el(fixture, 'token-capabilities-none')?.textContent).toContain('Choose at least one');
      expect(saveButton(fixture)?.disabled).toBe(true);
      submit(fixture);
      await settle(fixture);
      expect(create).not.toHaveBeenCalled();

      await choose(fixture, 'token-capabilities', null);
      expect(saveButton(fixture)?.disabled).toBe(true);

      await choose(fixture, 'token-capabilities', ['upload']);
      expect(el(fixture, 'token-capabilities-none')).toBeNull();
      expect(saveButton(fixture)?.disabled).toBe(false);
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
        'The projects of this tenant could not be loaded.',
      );
      expect(select(fixture, 'token-project').options()).toEqual([]);
    });

    it('says that a restriction narrows the token, and that none reaches every tenant', async () => {
      const fixture = await render();

      expect(host(fixture).textContent).toContain(
        'An unrestricted one reaches every tenant you belong to.',
      );
    });
  });

  describe('the lifetime', () => {
    it('is a number of days from one to a year', async () => {
      const fixture = await render();

      const field = fixture.debugElement.query(By.css('[data-testid="token-lifetime"]'))
        .componentInstance as { min(): number; max(): number; maxFractionDigits(): number };
      expect(field.min()).toBe(1);
      expect(field.max()).toBe(365);
      expect(field.maxFractionDigits()).toBe(0);
      expect(host(fixture).textContent).toContain('1 to 365 days. The installation may shorten it');
    });

    it.each<[number | null, string]>([
      [0, 'zero days'],
      [-5, 'a negative number of days'],
      [366, 'more than a year'],
      [1.5, 'a fraction of a day'],
      [null, 'nothing'],
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

    it.each([1, 30, 365])('can be %i days', async (days) => {
      const fixture = await render();
      await fill(fixture);

      await choose(fixture, 'token-lifetime', days);
      submit(fixture);
      await settle(fixture);

      expect(create.mock.calls[0][0].lifetime_days).toBe(days);
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
    it('creates a read token for ninety days from a name alone, and sends nothing else', async () => {
      const fixture = await render();
      await fill(fixture, '  ci  ');

      submit(fixture);
      await settle(fixture);

      expect(create).toHaveBeenCalledExactlyOnceWith({
        name: 'ci',
        scope: 'read',
        lifetime_days: 90,
      });
      expect(create.mock.calls[0][0]).toStrictEqual({
        name: 'ci',
        scope: 'read',
        lifetime_days: 90,
      });
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
        tenant: 'acme',
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

      expect(create.mock.calls[0][0]).toMatchObject({ agent: true, capabilities: [...CAPABILITY] });
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
        'override-urgency',
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
        tenant: 'globex',
        lifetime_days: 90,
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
      expect(label(fixture, 'token-tenant')).toBe('Any tenant of yours');
      expect(el(fixture, 'token-lifetime')?.querySelector('input')?.value).toBe('90 days');
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
          { pointer: '/tenant', message: 'no such tenant' },
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
      expect(text(fixture, 'token-tenant-error')).toBe('no such tenant');
      expect(text(fixture, 'token-project-error')).toBe('no such project');
      expect(text(fixture, 'token-lifetime-error')).toBe('must be at least 1');
      expect(fixture.componentInstance.visible()).toBe(true);
      expect((el(fixture, 'token-name') as HTMLInputElement).value).toBe('claude');
      expect(add).not.toHaveBeenCalled();
      expect(saveButton(fixture)?.disabled).toBe(false);
    });

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
});
