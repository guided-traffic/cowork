import { HttpErrorResponse, HttpHeaders } from '@angular/common/http';
import { signal, WritableSignal } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { By } from '@angular/platform-browser';
import { MessageService } from 'primeng/api';
import { Subject } from 'rxjs';
import type { MockInstance } from 'vitest';
import { AttachmentUsage, Problem, Tenant } from '../../api/models';
import { Api } from '../../api/api';
import { AttachmentConsistencyService } from '../../core/attachment-consistency.service';
import { EventStreamService, StreamEvent } from '../../core/event-stream.service';
import { ExportArchive, ImportsService } from '../../core/imports.service';
import { SessionService } from '../../core/session.service';
import { TenantService } from '../../core/tenant.service';
import { changesUsage, quotaShare, TenantSettings } from './tenant-settings';

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

describe('the attachment usage helpers', () => {
  it('gives the share of the quota in whole percent, at most 100, and none without a quota', () => {
    expect(quotaShare({ used_bytes: 25, attachments: 1, quota_bytes: 100 })).toBe(25);
    expect(quotaShare({ used_bytes: 999, attachments: 3, quota_bytes: 1000 })).toBe(99);
    expect(quotaShare({ used_bytes: 150, attachments: 3, quota_bytes: 100 })).toBe(100);
    expect(quotaShare({ used_bytes: 150, attachments: 3, quota_bytes: null })).toBeNull();
  });

  it('reads an upload or a purge in the tenant, a gap and a poll as moving the usage', () => {
    const ticket = (key: string, kind: string): StreamEvent => ({
      name: 'ticket.changed',
      id: 'e1',
      key,
      version: 2,
      kind,
    });
    expect(changesUsage(ticket('acme/COW-1', 'uploaded'), 'acme')).toBe(true);
    expect(changesUsage(ticket('acme/COW-1', 'purged'), 'acme')).toBe(true);
    expect(changesUsage(ticket('acme/COW-1', 'deleted'), 'acme')).toBe(false);
    expect(changesUsage(ticket('acme/COW-1', 'commented'), 'acme')).toBe(false);
    expect(changesUsage(ticket('globex/COW-1', 'uploaded'), 'acme')).toBe(false);
    expect(changesUsage({ name: 'resync' }, 'acme')).toBe(true);
    expect(changesUsage({ name: 'poll' }, 'acme')).toBe(true);
    expect(changesUsage({ name: 'inbox.changed', unread: 1 }, 'acme')).toBe(false);
  });
});

describe('TenantSettings', () => {
  let value: WritableSignal<Tenant | undefined>;
  let isAdmin: WritableSignal<boolean>;
  let update: MockInstance<TenantService['update']>;
  let usage: WritableSignal<AttachmentUsage | undefined>;
  let usageError: WritableSignal<unknown>;
  let getUsage: MockInstance<
    (fn: unknown, params: { tenant: string; 'If-None-Match'?: string }) => Promise<unknown>
  >;
  let events: Subject<StreamEvent>;
  let exportTenant: MockInstance<ImportsService['exportTenant']>;

  beforeEach(() => {
    exportTenant = vi.fn<ImportsService['exportTenant']>();
    value = signal<Tenant | undefined>(tenant());
    isAdmin = signal(true);
    update = vi.fn<TenantService['update']>().mockResolvedValue(tenant());
    usage = signal<AttachmentUsage | undefined>({
      used_bytes: 75 * 1024 * 1024,
      attachments: 41,
      quota_bytes: 100 * 1024 * 1024,
    });
    usageError = signal<unknown>(undefined);
    // The usage is read through ConditionalPages, which asks for the whole answer to keep its
    // weak ETag, and sends that tag back the next time.
    getUsage = vi.fn().mockImplementation(async () => {
      const error = usageError();
      if (error) {
        throw error;
      }
      return { body: usage(), headers: new HttpHeaders({ ETag: 'W/"usage"' }) };
    });
    events = new Subject<StreamEvent>();
    TestBed.configureTestingModule({
      providers: [
        MessageService,
        { provide: TenantService, useValue: { value, isAdmin, update } },
        { provide: SessionService, useValue: { tenant: signal('acme') } },
        { provide: Api, useValue: { invoke$Response: getUsage } },
        { provide: EventStreamService, useValue: { events } },
        // The consistency check's section reads through a service of its own, which its own spec
        // covers; here it has nothing to show.
        {
          provide: AttachmentConsistencyService,
          useValue: {
            latest: {
              hasValue: signal(false),
              value: signal(undefined),
              error: signal(undefined),
              reload: vi.fn(),
            },
          },
        },
        { provide: ImportsService, useValue: { exportTenant } },
      ],
    });
  });

  it('shows the consistency check beside the usage to an administrator, and to nobody else', async () => {
    const fixture = await render();
    expect(el(fixture, 'attachment-consistency')).not.toBeNull();

    isAdmin.set(false);
    await settle(fixture);
    expect(el(fixture, 'attachment-consistency')).toBeNull();
  });

  describe('the export of the tenant (docs/adr/0051 D4)', () => {
    let created: MockInstance<typeof URL.createObjectURL>;
    let clicked: MockInstance<HTMLAnchorElement['click']>;
    const archive = (tickets: number, left: number): ExportArchive => ({
      blob: new Blob(['archive'], { type: 'application/gzip' }),
      filename: 'acme-20261007.tar.gz',
      manifest: {
        format: 'cowork export v1',
        tenant: 'acme',
        projects: [],
        exported_at: '2026-10-07T08:00:00Z',
        exported_by: 'Ada Lovelace <local:ada>',
        tickets,
        confidential_not_included: left,
      },
    });

    beforeEach(() => {
      created = vi.fn<typeof URL.createObjectURL>().mockReturnValue('blob:tenant');
      vi.stubGlobal(
        'URL',
        Object.assign(URL, { createObjectURL: created, revokeObjectURL: vi.fn() }),
      );
      clicked = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => undefined);
    });

    afterEach(() => {
      vi.unstubAllGlobals();
      vi.restoreAllMocks();
    });

    it('saves the archive of the tenant under its name and says what it holds', async () => {
      exportTenant.mockResolvedValue(archive(41, 0));
      const fixture = await render();

      el(fixture, 'tenant-export-button')?.click();
      await settle(fixture);

      expect(exportTenant).toHaveBeenCalledExactlyOnceWith('acme');
      expect(created).toHaveBeenCalledOnce();
      const link = clicked.mock.contexts[0] as HTMLAnchorElement;
      expect(link.download).toBe('acme-20261007.tar.gz');
      expect(link.href).toBe('blob:tenant');
      expect(el(fixture, 'tenant-export-note')?.textContent?.trim()).toBe(
        '41 tickets in acme-20261007.tar.gz.',
      );
    });

    it('names the confidential tickets it leaves out (docs/adr/0065 D5)', async () => {
      exportTenant.mockResolvedValue(archive(41, 1));
      const fixture = await render();

      el(fixture, 'tenant-export-button')?.click();
      await settle(fixture);

      expect(el(fixture, 'tenant-export-note')?.textContent?.trim()).toBe(
        '41 tickets in acme-20261007.tar.gz. 1 confidential ticket you cannot read is not included.',
      );
    });

    it('toasts a refusal and offers the export again', async () => {
      exportTenant.mockRejectedValue(refusal(504, 'Timeout', 'The export took too long.'));
      const add = vi.spyOn(TestBed.inject(MessageService), 'add');
      const fixture = await render();

      el(fixture, 'tenant-export-button')?.click();
      await settle(fixture);

      expect(clicked).not.toHaveBeenCalled();
      expect(add).toHaveBeenCalledOnce();
      expect(el(fixture, 'tenant-export-note')).toBeNull();
      expect((el(fixture, 'tenant-export-button') as HTMLButtonElement).disabled).toBe(false);
    });

    it("is not offered to anybody but the tenant's administrators", async () => {
      isAdmin.set(false);
      const fixture = await render();

      expect(el(fixture, 'tenant-export')).toBeNull();
    });
  });

  describe("the attachments' usage (docs/adr/0016 D6)", () => {
    it('shows an administrator what the files hold of the quota, with a meter', async () => {
      const fixture = await render();

      expect(getUsage).toHaveBeenCalledWith(expect.anything(), { tenant: 'acme' });
      expect(el(fixture, 'attachment-usage-text')?.textContent?.replace(/\s+/g, ' ').trim()).toBe(
        '75 MiB in 41 files of a quota of 100 MiB',
      );
      expect(el(fixture, 'attachment-usage-meter')?.getAttribute('aria-valuenow')).toBe('75');
    });

    it('says that the installation sets no quota where it sets none', async () => {
      usage.set({ used_bytes: 1, attachments: 1, quota_bytes: null });
      const fixture = await render();

      expect(el(fixture, 'attachment-usage-text')?.textContent?.replace(/\s+/g, ' ').trim()).toBe(
        '1 byte in 1 file',
      );
      expect(el(fixture, 'attachment-usage-meter')).toBeNull();
      expect(el(fixture, 'attachment-usage-no-quota')?.textContent).toContain(
        'COWORK_ATTACHMENT_TENANT_QUOTA',
      );
    });

    it('says why when the usage could not be loaded', async () => {
      usage.set(undefined);
      usageError.set(refusal(503, 'Not ready', 'The database is starting.'));
      const fixture = await render();

      expect(el(fixture, 'attachment-usage-failure')?.textContent).toBe(
        'The usage could not be loaded: The database is starting.',
      );
    });

    it('shows nothing of it to anybody but an administrator', async () => {
      isAdmin.set(false);
      const fixture = await render();

      expect(el(fixture, 'attachment-usage')).toBeNull();
      expect(getUsage).not.toHaveBeenCalled();
    });

    it('asks again on an upload in the tenant with the tag it holds, and keeps the usage on a 304', async () => {
      const fixture = await render();
      getUsage.mockRejectedValueOnce(
        new HttpErrorResponse({ status: 304, statusText: 'Not Modified' }),
      );

      events.next({
        name: 'ticket.changed',
        id: 'e1',
        key: 'acme/COW-1',
        version: 2,
        kind: 'uploaded',
      });
      await settle(fixture);

      expect(getUsage).toHaveBeenCalledTimes(2);
      expect(getUsage.mock.lastCall?.[1]).toEqual({ tenant: 'acme', 'If-None-Match': 'W/"usage"' });
      expect(el(fixture, 'attachment-usage-text')?.textContent?.replace(/\s+/g, ' ').trim()).toBe(
        '75 MiB in 41 files of a quota of 100 MiB',
      );
    });

    it('asks nothing again on an act that moves no file', async () => {
      const fixture = await render();

      events.next({
        name: 'ticket.changed',
        id: 'e1',
        key: 'acme/COW-1',
        version: 2,
        kind: 'deleted',
      });
      await settle(fixture);

      expect(getUsage).toHaveBeenCalledTimes(1);
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
