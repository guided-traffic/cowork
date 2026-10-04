import { HttpErrorResponse } from '@angular/common/http';
import { signal, WritableSignal } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { By } from '@angular/platform-browser';
import { MessageService } from 'primeng/api';
import type { MockInstance } from 'vitest';
import { Member, Me, Membership, Problem } from '../../api/models';
import { MembersService } from '../../core/members.service';
import { OpenableTenant, SessionService } from '../../core/session.service';
import { SelfGrant, selfGrantQuestion } from './self-grant';

const ada = { id: 'p-ada', global_admin: true } as Me;

function member(role: Member['role']): Member {
  return {
    person: { id: ada.id, username: 'ada', display_name: 'Ada' },
    email: null,
    role,
    origins: [{ source: 'grant', role }],
    local: true,
  };
}

describe('SelfGrant', () => {
  let tenant: WritableSignal<string | null>;
  let shown: WritableSignal<OpenableTenant | undefined>;
  let person: WritableSignal<Me | undefined>;
  let membership: WritableSignal<Membership | undefined>;
  let setGrant: MockInstance<MembersService['setGrant']>;

  beforeEach(() => {
    tenant = signal<string | null>('acme');
    shown = signal<OpenableTenant | undefined>({ slug: 'acme', name: 'Acme Corp', role: null });
    person = signal<Me | undefined>(ada);
    membership = signal<Membership | undefined>(undefined);
    setGrant = vi
      .fn<MembersService['setGrant']>()
      .mockImplementation(async (_, role) => member(role));
    TestBed.configureTestingModule({
      providers: [
        MessageService,
        {
          provide: SessionService,
          useValue: { tenant, shown, person, membership },
        },
        { provide: MembersService, useValue: { setGrant } },
      ],
    });
  });

  afterEach(() => vi.restoreAllMocks());

  async function render() {
    const fixture = TestBed.createComponent(SelfGrant);
    await settle(fixture);
    return fixture;
  }

  async function settle(fixture: ComponentFixture<SelfGrant>) {
    for (let round = 0; round < 2; round++) {
      await new Promise((resolve) => setTimeout(resolve));
      fixture.detectChanges();
    }
    await fixture.whenStable();
  }

  const host = (fixture: ComponentFixture<SelfGrant>) => fixture.nativeElement as HTMLElement;
  const dialog = () => document.body.querySelector('.p-confirmdialog');
  const press = (label: string) =>
    [...(dialog()?.querySelectorAll('button') ?? [])]
      .find((button) => button.textContent?.trim() === label)
      ?.dispatchEvent(new MouseEvent('click', { bubbles: true }));
  const ask = (fixture: ComponentFixture<SelfGrant>) =>
    host(fixture).querySelector<HTMLButtonElement>('[data-testid="grant-yourself"]')!.click();
  function choose(fixture: ComponentFixture<SelfGrant>, role: string) {
    fixture.debugElement
      .query(By.css('[data-testid="self-grant-role"]'))
      .triggerEventHandler('ngModelChange', role);
  }

  it('says the person has no role in the tenant, what they see, and that the tenant sees a grant', async () => {
    const fixture = await render();

    expect(host(fixture).querySelector('h2')?.textContent).toBe('You have no role in Acme Corp');
    const text = host(fixture).querySelector('p')?.textContent ?? '';
    expect(text).toContain('members, group mappings and settings, and none of its projects');
    expect(text).toContain('the tenant sees the grant in its audit record');
    expect(host(fixture).querySelector('[data-testid="grant-yourself"]')?.textContent?.trim()).toBe(
      'Grant yourself a role',
    );
    const select = fixture.debugElement.query(By.css('[data-testid="self-grant-role"]'));
    expect(select.componentInstance.ariaLabel()).toBe('The role to grant yourself');
    expect(select.componentInstance.options()).toEqual(['viewer', 'member', 'admin']);
  });

  it('asks first, naming the tenant and the role, and grants nothing on Cancel', async () => {
    const fixture = await render();

    ask(fixture);
    await settle(fixture);

    expect(dialog()?.textContent).toContain('Grant yourself a role?');
    expect(dialog()?.textContent).toContain(selfGrantQuestion('Acme Corp', 'admin'));
    press('Cancel');
    await settle(fixture);
    expect(setGrant).not.toHaveBeenCalled();
  });

  it('grants the person admin, unless they pick another role, and says so', async () => {
    const toasts = vi.spyOn(TestBed.inject(MessageService), 'add');
    const fixture = await render();

    ask(fixture);
    await settle(fixture);
    press('Grant yourself admin');
    await settle(fixture);

    expect(setGrant).toHaveBeenCalledExactlyOnceWith('p-ada', 'admin');
    expect(toasts).toHaveBeenCalledWith(
      expect.objectContaining({
        summary: 'Role granted',
        detail: 'You hold the role admin in Acme Corp.',
      }),
    );
  });

  it('grants the role picked', async () => {
    const fixture = await render();
    choose(fixture, 'viewer');
    await settle(fixture);

    ask(fixture);
    await settle(fixture);
    expect(dialog()?.textContent).toContain(selfGrantQuestion('Acme Corp', 'viewer'));
    press('Grant yourself viewer');
    await settle(fixture);

    expect(setGrant).toHaveBeenCalledExactlyOnceWith('p-ada', 'viewer');
  });

  it('shows a refusal as a toast and offers the act again', async () => {
    const body: Problem = {
      type: 'about:blank',
      title: 'Forbidden',
      status: 403,
      detail: 'No.',
      code: 'forbidden',
    };
    setGrant.mockRejectedValue(new HttpErrorResponse({ status: 403, error: body }));
    const toasts = vi.spyOn(TestBed.inject(MessageService), 'add');
    const fixture = await render();

    ask(fixture);
    await settle(fixture);
    press('Grant yourself admin');
    await settle(fixture);

    expect(toasts).toHaveBeenCalledWith(
      expect.objectContaining({ severity: 'warn', summary: 'Forbidden', detail: 'No.' }),
    );
    expect(
      host(fixture).querySelector<HTMLButtonElement>('[data-testid="grant-yourself"]')?.disabled,
    ).toBe(false);
  });

  it('closes a question of another tenant, and starts again from admin there', async () => {
    const fixture = await render();
    choose(fixture, 'member');
    ask(fixture);
    await settle(fixture);
    expect(dialog()?.textContent).toContain('Grant yourself a role?');

    tenant.set('globex');
    shown.set({ slug: 'globex', name: 'Globex', role: null });
    await settle(fixture);

    expect(dialog()).toBeNull();
    ask(fixture);
    await settle(fixture);
    expect(dialog()?.textContent).toContain(selfGrantQuestion('Globex', 'admin'));
    press('Cancel');
    await settle(fixture);
  });

  // docs/adr/0034 D2: a global administrator who holds a role below admin raises their own grant.
  it('says the role held, offers the roles above it, and raises the grant to the one picked', async () => {
    membership.set({
      role: 'viewer',
      tenant: { slug: 'acme', name: 'Acme Corp' },
      origins: [{ source: 'mapping', role: 'viewer' }],
    });
    const fixture = await render();

    expect(host(fixture).querySelector('h2')?.textContent).toBe(
      'You hold the role viewer in Acme Corp',
    );
    expect(host(fixture).querySelector('p')?.textContent).toContain('raise your own grant');
    const select = fixture.debugElement.query(By.css('[data-testid="self-grant-role"]'));
    expect(select.componentInstance.options()).toEqual(['member', 'admin']);

    ask(fixture);
    await settle(fixture);
    expect(dialog()?.textContent).toContain(selfGrantQuestion('Acme Corp', 'admin'));
    press('Grant yourself admin');
    await settle(fixture);

    expect(setGrant).toHaveBeenCalledExactlyOnceWith('p-ada', 'admin');
  });

  it('offers a member admin alone', async () => {
    membership.set({
      role: 'member',
      tenant: { slug: 'acme', name: 'Acme Corp' },
      origins: [{ source: 'grant', role: 'member' }],
    });
    const fixture = await render();

    const select = fixture.debugElement.query(By.css('[data-testid="self-grant-role"]'));
    expect(select.componentInstance.options()).toEqual(['admin']);
  });

  it('names the tenant by its slug while its name is not known', async () => {
    shown.set(undefined);

    const fixture = await render();

    expect(host(fixture).querySelector('h2')?.textContent).toBe('You have no role in acme');
  });

  it('asks nothing before the person is known', async () => {
    person.set(undefined);
    const fixture = await render();

    ask(fixture);
    await settle(fixture);

    expect(dialog()).toBeNull();
  });

  it('holds the act while the grant is on its way', async () => {
    let answer: (member: Member) => void = () => undefined;
    setGrant.mockImplementation(() => new Promise((resolve) => (answer = resolve)));
    const fixture = await render();
    const button = () =>
      host(fixture).querySelector<HTMLButtonElement>('[data-testid="grant-yourself"]')!;

    ask(fixture);
    await settle(fixture);
    press('Grant yourself admin');
    await settle(fixture);

    expect(button().disabled).toBe(true);
    expect(button().querySelector('.pi-spinner')).not.toBeNull();
    answer(member('admin'));
    await settle(fixture);
    expect(button().disabled).toBe(false);
    expect(button().querySelector('.pi-user-plus')).not.toBeNull();
  });

  it('asks nothing of a tenant left meanwhile', async () => {
    const fixture = await render();
    ask(fixture);
    await settle(fixture);

    const accept = [...(dialog()?.querySelectorAll('button') ?? [])].find(
      (button) => button.textContent?.trim() === 'Grant yourself admin',
    );
    tenant.set('globex');
    accept?.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    await settle(fixture);

    expect(setGrant).not.toHaveBeenCalled();
  });
});
