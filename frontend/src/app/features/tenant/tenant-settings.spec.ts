import { HttpErrorResponse } from '@angular/common/http';
import { signal, WritableSignal } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { By } from '@angular/platform-browser';
import { MessageService } from 'primeng/api';
import type { MockInstance } from 'vitest';
import { Problem, Tenant } from '../../api/models';
import { TenantService } from '../../core/tenant.service';
import { TenantSettings } from './tenant-settings';

function tenant(overrides: Partial<Tenant> = {}): Tenant {
  return {
    slug: 'acme',
    name: 'Acme Corp',
    members_create_projects: true,
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

describe('TenantSettings', () => {
  let value: WritableSignal<Tenant | undefined>;
  let isAdmin: WritableSignal<boolean>;
  let update: MockInstance<TenantService['update']>;

  beforeEach(() => {
    value = signal<Tenant | undefined>(tenant());
    isAdmin = signal(true);
    update = vi.fn<TenantService['update']>().mockResolvedValue(tenant());
    TestBed.configureTestingModule({
      providers: [MessageService, { provide: TenantService, useValue: { value, isAdmin, update } }],
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
