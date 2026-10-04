import { HttpErrorResponse } from '@angular/common/http';
import { signal, WritableSignal } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { By } from '@angular/platform-browser';
import { MessageService } from 'primeng/api';
import type { MockInstance } from 'vitest';
import { ChatAvailability, Problem, Tenant } from '../../api/models';
import { ChatService } from '../../core/chat.service';
import { TenantService } from '../../core/tenant.service';
import { TenantSettings } from './tenant-settings';

function tenant(overrides: Partial<Tenant> = {}): Tenant {
  return {
    slug: 'acme',
    name: 'Acme Corp',
    members_create_projects: true,
    chat_external_allowed: false,
    time_visible_to_members: false,
    time_locked_until: null,
    version: 2,
    created_at: '2026-09-01T09:00:00Z',
    updated_at: '2026-09-01T09:00:00Z',
    ...overrides,
  };
}

function refusal(status: number, title: string, detail: string) {
  const body: Problem = { type: 'about:blank', title, status, detail, code: 'precondition_failed' };
  return new HttpErrorResponse({ status, statusText: title, error: body });
}

const notConfigured: ChatAvailability = {
  available: false,
  provider: null,
  model: null,
  inside: false,
  reason: 'not_configured',
};

const outside: ChatAvailability = {
  available: false,
  provider: 'openai',
  model: 'gpt-x',
  inside: false,
  reason: 'not_allowed_in_tenant',
};

describe('TenantSettings', () => {
  let value: WritableSignal<Tenant | undefined>;
  let isAdmin: WritableSignal<boolean>;
  let update: MockInstance<TenantService['update']>;
  let availability: WritableSignal<ChatAvailability | undefined>;
  let reloadAvailability: MockInstance<() => void>;

  beforeEach(() => {
    value = signal<Tenant | undefined>(tenant());
    isAdmin = signal(true);
    update = vi.fn<TenantService['update']>().mockResolvedValue(tenant());
    availability = signal<ChatAvailability | undefined>(notConfigured);
    reloadAvailability = vi.fn<() => void>();
    TestBed.configureTestingModule({
      providers: [
        MessageService,
        { provide: TenantService, useValue: { value, isAdmin, update } },
        {
          provide: ChatService,
          useValue: {
            availability: {
              hasValue: () => availability() !== undefined,
              value: () => availability(),
            },
            reloadAvailability,
          },
        },
      ],
    });
  });

  async function render() {
    const fixture = TestBed.createComponent(TenantSettings);
    await settle(fixture);
    return fixture;
  }

  /** Lets what a click started finish, and shows it; fields take their model a moment after they appear. */
  async function settle(fixture: ComponentFixture<TenantSettings>) {
    for (let round = 0; round < 2; round++) {
      await new Promise((resolve) => setTimeout(resolve));
      fixture.detectChanges();
    }
    await fixture.whenStable();
  }

  const host = (fixture: ComponentFixture<TenantSettings>) => fixture.nativeElement as HTMLElement;

  const el = (fixture: ComponentFixture<TenantSettings>, testId: string) =>
    host(fixture).querySelector<HTMLElement>(`[data-testid="${testId}"]`);

  const switched = (fixture: ComponentFixture<TenantSettings>, testId: string) =>
    el(fixture, testId)?.querySelector('input')?.getAttribute('aria-checked');

  const switchDisabled = (fixture: ComponentFixture<TenantSettings>, testId: string) =>
    el(fixture, testId)?.querySelector('input')?.disabled;

  function flip(fixture: ComponentFixture<TenantSettings>, testId: string, to: boolean) {
    fixture.debugElement
      .query(By.css(`[data-testid="${testId}"]`))
      .triggerEventHandler('ngModelChange', to);
    fixture.detectChanges();
  }

  function typeName(fixture: ComponentFixture<TenantSettings>, name: string) {
    const input = el(fixture, 'tenant-name-input') as HTMLInputElement;
    input.value = name;
    input.dispatchEvent(new Event('input'));
    fixture.detectChanges();
  }

  const submit = (fixture: ComponentFixture<TenantSettings>) =>
    host(fixture)
      .querySelector('form')
      ?.dispatchEvent(new Event('submit', { cancelable: true }));

  const saveButton = (fixture: ComponentFixture<TenantSettings>) =>
    el(fixture, 'tenant-save') as HTMLButtonElement | null;

  describe('what it shows', () => {
    it('shows the heading and nothing else until the tenant is loaded', async () => {
      value.set(undefined);

      const fixture = await render();

      expect(host(fixture).querySelector('h1')?.textContent).toBe('Settings');
      expect(host(fixture).querySelector('form')).toBeNull();
    });

    it('starts the form from the name and the two switches of the tenant', async () => {
      const fixture = await render();

      expect((el(fixture, 'tenant-name-input') as HTMLInputElement).value).toBe('Acme Corp');
      expect(switched(fixture, 'members-create')).toBe('true');
      expect(switched(fixture, 'time-visible')).toBe('false');
    });

    it('names the slug, which never changes', async () => {
      const fixture = await render();

      expect(
        host(fixture).querySelector('.page > p.small')?.textContent?.replace(/\s+/g, ' ').trim(),
      ).toBe('Slug acme, which never changes.');
      expect(host(fixture).querySelector('.page > p.small code')?.textContent).toBe('acme');
    });

    it('follows the tenant when it is replaced by a newer version', async () => {
      const fixture = await render();

      value.set(
        tenant({
          name: 'Acme Inc',
          members_create_projects: false,
          time_visible_to_members: true,
          version: 3,
        }),
      );
      await settle(fixture);

      expect((el(fixture, 'tenant-name-input') as HTMLInputElement).value).toBe('Acme Inc');
      expect(switched(fixture, 'members-create')).toBe('false');
      expect(switched(fixture, 'time-visible')).toBe('true');
    });
  });

  describe('for an administrator', () => {
    it('lets the person edit the fields and offers to save', async () => {
      const fixture = await render();

      expect((el(fixture, 'tenant-name-input') as HTMLInputElement).disabled).toBe(false);
      expect(switchDisabled(fixture, 'members-create')).toBe(false);
      expect(switchDisabled(fixture, 'time-visible')).toBe(false);
      expect(saveButton(fixture)?.textContent?.trim()).toBe('Save');
      expect(host(fixture).querySelector('form p.muted.small')).toBeNull();
    });

    it('writes the name, trimmed, and both switches as they are', async () => {
      const fixture = await render();
      typeName(fixture, '  Acme Inc  ');
      flip(fixture, 'members-create', false);
      flip(fixture, 'time-visible', true);

      submit(fixture);
      await settle(fixture);

      expect(update).toHaveBeenCalledExactlyOnceWith({
        name: 'Acme Inc',
        members_create_projects: false,
        time_visible_to_members: true,
      });
    });

    it('writes what is shown when nothing was changed', async () => {
      const fixture = await render();

      submit(fixture);
      await settle(fixture);

      expect(update).toHaveBeenCalledExactlyOnceWith({
        name: 'Acme Corp',
        members_create_projects: true,
        time_visible_to_members: false,
      });
    });

    it('toasts the problem and keeps what was typed when the write is refused', async () => {
      update.mockRejectedValue(
        refusal(412, 'The tenant changed', 'Somebody saved the settings meanwhile.'),
      );
      const add = vi.spyOn(TestBed.inject(MessageService), 'add');
      const fixture = await render();
      typeName(fixture, 'Acme Inc');

      submit(fixture);
      await settle(fixture);

      expect(add).toHaveBeenCalledExactlyOnceWith(
        expect.objectContaining({
          summary: 'The tenant changed',
          detail: 'Somebody saved the settings meanwhile.',
        }),
      );
      expect((el(fixture, 'tenant-name-input') as HTMLInputElement).value).toBe('Acme Inc');
      expect(saveButton(fixture)?.disabled).toBe(false);
    });

    it('shows its button as busy until the write is done', async () => {
      let finish: (saved: Tenant) => void = () => undefined;
      update.mockReturnValue(
        new Promise<Tenant>((resolve) => {
          finish = resolve;
        }),
      );
      const fixture = await render();

      submit(fixture);
      await settle(fixture);
      expect(saveButton(fixture)?.disabled).toBe(true);
      expect(saveButton(fixture)?.querySelector('i.pi-spinner')).not.toBeNull();
      finish(tenant());
      await settle(fixture);

      expect(saveButton(fixture)?.disabled).toBe(false);
      expect(saveButton(fixture)?.querySelector('i.pi-spinner')).toBeNull();
    });
  });

  describe("the assistant's provider outside the installation", () => {
    const text = (fixture: ComponentFixture<TenantSettings>) =>
      el(fixture, 'chat-external-text')?.textContent?.replace(/\s+/g, ' ').trim();

    it('is offered to an administrator, with the model, its API and what switching it on sends', async () => {
      availability.set(outside);

      const fixture = await render();

      expect(switched(fixture, 'chat-external')).toBe('false');
      expect(switchDisabled(fixture, 'chat-external')).toBe(false);
      expect(text(fixture)).toBe(
        'The assistant may use the model gpt-x through an OpenAI-compatible API — switched on, what the assistant reads in this tenant is sent to that provider, outside this installation',
      );
      expect(el(fixture, 'chat-external-text')?.querySelector('code')?.textContent).toBe('gpt-x');
    });

    it('names the Anthropic API', async () => {
      availability.set({ ...outside, provider: 'anthropic', model: 'claude-x' });

      const fixture = await render();

      expect(text(fixture)).toContain('the model claude-x through the Anthropic API');
    });

    it('shows the consent as the tenant has it, and follows it', async () => {
      availability.set(outside);
      value.set(tenant({ chat_external_allowed: true }));
      const fixture = await render();
      expect(switched(fixture, 'chat-external')).toBe('true');

      value.set(tenant({ chat_external_allowed: false, version: 3 }));
      await settle(fixture);

      expect(switched(fixture, 'chat-external')).toBe('false');
    });

    it('writes the consent with the settings, and asks for the availability again', async () => {
      availability.set(outside);
      const fixture = await render();
      flip(fixture, 'chat-external', true);

      submit(fixture);
      await settle(fixture);

      expect(update).toHaveBeenCalledExactlyOnceWith({
        name: 'Acme Corp',
        members_create_projects: true,
        time_visible_to_members: false,
        chat_external_allowed: true,
      });
      expect(reloadAvailability).toHaveBeenCalledOnce();
    });

    it('asks for nothing again when the write is refused', async () => {
      availability.set(outside);
      update.mockRejectedValue(refusal(400, 'Invalid', 'no'));
      const fixture = await render();

      submit(fixture);
      await settle(fixture);

      expect(reloadAvailability).not.toHaveBeenCalled();
    });

    it.each([
      ['a provider inside the installation, which needs no consent', { ...outside, inside: true }],
      ['no provider', notConfigured],
      ['an availability not known yet', undefined],
    ])('is not offered for %s, and not written', async (_case, known) => {
      availability.set(known);
      const fixture = await render();

      submit(fixture);
      await settle(fixture);

      expect(el(fixture, 'chat-external')).toBeNull();
      expect(Object.keys(update.mock.calls[0][0])).not.toContain('chat_external_allowed');
      expect(reloadAvailability).not.toHaveBeenCalled();
    });

    it('is not offered to anybody else', async () => {
      availability.set(outside);
      isAdmin.set(false);

      const fixture = await render();

      expect(el(fixture, 'chat-external')).toBeNull();
    });
  });

  describe('for anyone else', () => {
    beforeEach(() => {
      isAdmin.set(false);
    });

    it('shows the values, which cannot be edited, and no button to save', async () => {
      const fixture = await render();

      expect((el(fixture, 'tenant-name-input') as HTMLInputElement).disabled).toBe(true);
      expect(switchDisabled(fixture, 'members-create')).toBe(true);
      expect(switchDisabled(fixture, 'time-visible')).toBe(true);
      expect(saveButton(fixture)).toBeNull();
      expect((el(fixture, 'tenant-name-input') as HTMLInputElement).value).toBe('Acme Corp');
    });

    it('says who changes them', async () => {
      const fixture = await render();

      expect(host(fixture).querySelector('form p.muted.small')?.textContent).toBe(
        "Only the tenant's administrators change these.",
      );
    });

    it('offers the button once the person turns out to be an administrator', async () => {
      const fixture = await render();

      isAdmin.set(true);
      await settle(fixture);

      expect(saveButton(fixture)).not.toBeNull();
      expect(host(fixture).querySelector('form p.muted.small')).toBeNull();
    });
  });
});
