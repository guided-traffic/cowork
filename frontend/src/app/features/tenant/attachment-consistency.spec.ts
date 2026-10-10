import { HttpErrorResponse } from '@angular/common/http';
import { signal, WritableSignal } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideRouter } from '@angular/router';
import { MessageService } from 'primeng/api';
import type { MockInstance } from 'vitest';
import { AttachmentConsistency, Problem, ProblemCode } from '../../api/models';
import { AttachmentConsistencyService } from '../../core/attachment-consistency.service';
import { SessionService } from '../../core/session.service';
import { Clock } from '../../shared/time';
import {
  AttachmentConsistencySection,
  consistencySummary,
  counted,
  listsCut,
} from './attachment-consistency';

const result = (overrides: Partial<AttachmentConsistency> = {}): AttachmentConsistency => ({
  check_id: 'c1',
  checked_at: '2026-10-06T03:12:00Z',
  dangling: 1,
  accepted: 1,
  orphans: 2,
  orphan_bytes: 3 * 1024,
  dangling_attachments: [
    {
      id: 'a1',
      file_name: 'lost-diagram.png',
      size: 2048,
      content_type: 'image/png',
      ticket: 'acme/COW-7',
      ticket_deleted: false,
      uploaded_at: '2026-10-01T10:00:00Z',
      accepted: false,
    },
    {
      id: 'a2',
      file_name: 'old.pdf',
      size: 100,
      content_type: 'application/pdf',
      ticket: 'acme/COW-3',
      ticket_deleted: true,
      uploaded_at: '2026-09-01T10:00:00Z',
      accepted: true,
    },
  ],
  orphaned_objects: [
    { key: 'tenant-id/o1', size: 1024, last_modified: '2026-10-05T10:00:00Z' },
    { key: 'tenant-id/stray.bin', size: 2048, last_modified: '2026-10-05T11:00:00Z' },
  ],
  orphan_removal: null,
  last_exported_at: '2026-10-05T02:30:00Z',
  ...overrides,
});

const refusal = (status: number, code: ProblemCode, detail: string) => {
  const body: Problem = { type: 'about:blank', title: 'Refused', status, code, detail };
  return new HttpErrorResponse({ status, statusText: 'Refused', error: body });
};

describe('the consistency helpers', () => {
  it('counts with the noun in the singular or the plural', () => {
    expect(counted(1, 'file', 'files')).toBe('1 file');
    expect(counted(0, 'file', 'files')).toBe('0 files');
    expect(counted(3, 'object', 'objects')).toBe('3 objects');
  });

  it('says in one sentence what the check found', () => {
    expect(consistencySummary(result())).toBe(
      '1 file whose bytes are missing, 2 objects no file names (3.0 KiB), 1 file accepted as lost.',
    );
    expect(
      consistencySummary(result({ dangling: 0, accepted: 0, orphans: 0, orphan_bytes: 0 })),
    ).toBe('Every file has its bytes, and the bucket holds nothing else.');
    expect(consistencySummary(result({ dangling: 0, orphans: 0 }))).toBe(
      '1 file accepted as lost.',
    );
  });

  it('knows when the lists show fewer entries than the counts', () => {
    expect(listsCut(result())).toBe(false);
    expect(listsCut(result({ orphans: 1500 }))).toBe(true);
    expect(listsCut(result({ dangling: 1200 }))).toBe(true);
  });
});

describe('AttachmentConsistencySection', () => {
  let latest: WritableSignal<AttachmentConsistency | undefined>;
  let loadError: WritableSignal<unknown>;
  let acceptLoss: MockInstance<AttachmentConsistencyService['acceptLoss']>;
  let removeOrphans: MockInstance<AttachmentConsistencyService['removeOrphans']>;
  let reload: MockInstance<() => boolean>;

  beforeEach(() => {
    latest = signal<AttachmentConsistency | undefined>(result());
    loadError = signal<unknown>(undefined);
    acceptLoss = vi
      .fn<AttachmentConsistencyService['acceptLoss']>()
      .mockResolvedValue({ accepted: 1 });
    removeOrphans = vi
      .fn<AttachmentConsistencyService['removeOrphans']>()
      .mockResolvedValue({ removed: 2, kept: 0, failed: 0 });
    reload = vi.fn<() => boolean>().mockReturnValue(true);
    TestBed.configureTestingModule({
      providers: [
        MessageService,
        provideRouter([]),
        {
          provide: AttachmentConsistencyService,
          useValue: {
            latest: {
              hasValue: () => latest() !== undefined,
              value: latest,
              error: loadError,
              reload,
            },
            acceptLoss,
            removeOrphans,
          },
        },
        { provide: SessionService, useValue: { tenant: signal('acme') } },
        { provide: Clock, useValue: { now: signal(Date.parse('2026-10-06T10:00:00Z')) } },
      ],
    });
  });

  afterEach(() => vi.restoreAllMocks());

  async function render() {
    const fixture = TestBed.createComponent(AttachmentConsistencySection);
    await settle(fixture);
    return fixture;
  }

  async function settle(fixture: ComponentFixture<AttachmentConsistencySection>) {
    for (let round = 0; round < 3; round++) {
      await new Promise((resolve) => setTimeout(resolve));
      fixture.detectChanges();
    }
    await fixture.whenStable();
  }

  const host = (fixture: ComponentFixture<AttachmentConsistencySection>) =>
    fixture.nativeElement as HTMLElement;
  const el = (fixture: ComponentFixture<AttachmentConsistencySection>, testId: string) =>
    host(fixture).querySelector<HTMLElement>(`[data-testid="${testId}"]`);
  const text = (node: Element | null | undefined) => node?.textContent?.replace(/\s+/g, ' ').trim();
  const dialog = () => document.body.querySelector('.p-confirmdialog');
  const press = (label: string) =>
    [...(dialog()?.querySelectorAll('button') ?? [])]
      .find((button) => button.textContent?.trim() === label)
      ?.dispatchEvent(new MouseEvent('click', { bubbles: true }));

  it('says when the tenant has not been checked yet', async () => {
    latest.set(
      result({
        check_id: null,
        checked_at: null,
        dangling: 0,
        accepted: 0,
        orphans: 0,
        orphan_bytes: 0,
        dangling_attachments: [],
        orphaned_objects: [],
        last_exported_at: null,
      }),
    );
    const fixture = await render();

    expect(text(el(fixture, 'consistency-never'))).toContain('Not checked yet');
    expect(el(fixture, 'consistency-summary')).toBeNull();
    expect(text(el(fixture, 'consistency-last-export'))).toBe(
      "Never exported. An export on a schedule is the second line of the team's backup.",
    );
  });

  it('says beside the check when the tenant was last exported (docs/adr/0059 D2)', async () => {
    const fixture = await render();

    const line = el(fixture, 'consistency-last-export');
    expect(text(line)).toBe('Last export yesterday, of the team or one of its projects.');
    expect(line?.previousElementSibling?.getAttribute('data-testid')).toBe('consistency-summary');
  });

  it('lists the files whose bytes are missing with their tickets, and the objects no file names', async () => {
    const fixture = await render();

    expect(text(el(fixture, 'consistency-summary'))).toBe(
      'Checked 6 hours ago: 1 file whose bytes are missing, 2 objects no file names (3.0 KiB), 1 file accepted as lost.',
    );
    const missing = el(fixture, 'dangling-a1');
    expect(missing?.textContent).toContain('lost-diagram.png');
    expect(missing?.textContent).toContain('Missing');
    expect(missing?.querySelector('a')?.getAttribute('href')).toBe('/t/acme/tickets/COW-7');
    const lost = el(fixture, 'dangling-a2');
    expect(lost?.textContent).toContain('Accepted as lost');
    expect(lost?.querySelector('a')).toBeNull();
    expect(lost?.querySelector('.pi-trash')).not.toBeNull();
    expect(
      [...host(fixture).querySelectorAll('[data-testid="orphan"] code')].map((c) => c.textContent),
    ).toEqual(['tenant-id/o1', 'tenant-id/stray.bin']);
    expect(el(fixture, 'consistency-cut')).toBeNull();
  });

  it('offers neither act where there is nothing to settle', async () => {
    latest.set(
      result({ dangling: 0, orphans: 0, orphan_bytes: 0, orphaned_objects: [], accepted: 2 }),
    );
    const fixture = await render();

    expect(el(fixture, 'consistency-accept')).toBeNull();
    expect(el(fixture, 'consistency-remove')).toBeNull();
  });

  it('asks once before it accepts the loss, saying that nothing is removed', async () => {
    const add = vi.spyOn(TestBed.inject(MessageService), 'add');
    const fixture = await render();

    el(fixture, 'consistency-accept')?.click();
    await settle(fixture);
    expect(dialog()?.textContent).toContain('Accept the loss of 1 file?');
    expect(dialog()?.textContent).toContain('Nothing is removed');
    press('Accept the loss');
    await settle(fixture);

    expect(acceptLoss).toHaveBeenCalledExactlyOnceWith('c1');
    expect(add).toHaveBeenCalledWith(expect.objectContaining({ summary: 'Loss accepted' }));
  });

  it('accepts nothing when the person says not yet', async () => {
    const fixture = await render();

    el(fixture, 'consistency-accept')?.click();
    await settle(fixture);
    press('Not yet');
    await settle(fixture);

    expect(acceptLoss).not.toHaveBeenCalled();
  });

  it('asks twice before it removes the objects, the second time saying that nothing brings them back', async () => {
    const add = vi.spyOn(TestBed.inject(MessageService), 'add');
    const fixture = await render();

    el(fixture, 'consistency-remove')?.click();
    await settle(fixture);
    expect(dialog()?.textContent).toContain('Remove 2 objects from the bucket?');
    expect(dialog()?.textContent).toContain('copy them out first');
    press('Continue');
    await settle(fixture);

    expect(removeOrphans).not.toHaveBeenCalled();
    expect(dialog()?.textContent).toContain('Remove 2 objects for good?');
    expect(dialog()?.textContent).toContain('Nothing brings a removed object back');
    const accept = [...(dialog()?.querySelectorAll('button') ?? [])].find(
      (button) => button.textContent?.trim() === 'Remove for good',
    );
    expect(accept?.className).toContain('p-button-danger');
    press('Remove for good');
    await settle(fixture);

    expect(removeOrphans).toHaveBeenCalledExactlyOnceWith('c1');
    expect(add).toHaveBeenCalledWith(
      expect.objectContaining({ severity: 'success', detail: '2 objects removed.' }),
    );
  });

  it.each(['first', 'second'])(
    'removes nothing when the person keeps them at the %s question',
    async (which) => {
      const fixture = await render();
      el(fixture, 'consistency-remove')?.click();
      await settle(fixture);
      if (which === 'second') {
        press('Continue');
        await settle(fixture);
      }

      press('Keep them');
      await settle(fixture);

      expect(removeOrphans).not.toHaveBeenCalled();
    },
  );

  it('says what stayed and what failed after a removal', async () => {
    removeOrphans.mockResolvedValue({ removed: 1, kept: 1, failed: 1 });
    const add = vi.spyOn(TestBed.inject(MessageService), 'add');
    const fixture = await render();

    el(fixture, 'consistency-remove')?.click();
    await settle(fixture);
    press('Continue');
    await settle(fixture);
    press('Remove for good');
    await settle(fixture);

    expect(add).toHaveBeenCalledWith(
      expect.objectContaining({
        severity: 'warn',
        detail:
          '1 object removed. 1 object had gained a file and stayed. 1 removal failed; the next check lists them again.',
      }),
    );
  });

  it('toasts a refusal and reads the check again — a newer check replaced the lists', async () => {
    acceptLoss.mockRejectedValue(
      refusal(409, 'consistency_check_stale', 'the check is not the team latest'),
    );
    const add = vi.spyOn(TestBed.inject(MessageService), 'add');
    const fixture = await render();

    el(fixture, 'consistency-accept')?.click();
    await settle(fixture);
    press('Accept the loss');
    await settle(fixture);

    expect(add).toHaveBeenCalledWith(
      expect.objectContaining({ detail: 'the check is not the team latest' }),
    );
    expect(reload).toHaveBeenCalled();
  });

  it('says who removed the objects of the check, when, and what stayed', async () => {
    latest.set(
      result({
        orphans: 0,
        orphan_bytes: 0,
        orphaned_objects: [],
        orphan_removal: {
          removed: 2,
          kept: 1,
          removed_at: '2026-10-06T09:00:00Z',
          removed_by: { id: 'p-ada', display_name: 'Ada Admin', username: 'ada' },
        },
      }),
    );
    const fixture = await render();

    expect(text(el(fixture, 'consistency-removal'))).toBe(
      '2 objects of this check removed by Ada Admin 1 hour ago; 1 object had gained a file and stayed.',
    );
    expect(el(fixture, 'consistency-remove')).toBeNull();
  });

  it('says that the lists show the first thousand', async () => {
    latest.set(result({ orphans: 1500 }));
    const fixture = await render();

    expect(el(fixture, 'consistency-cut')).not.toBeNull();
  });

  it('says why when the check could not be loaded', async () => {
    latest.set(undefined);
    loadError.set(refusal(503, 'not_ready', 'The database is starting.'));
    const fixture = await render();

    expect(text(el(fixture, 'consistency-failure'))).toBe(
      'The consistency check could not be loaded: The database is starting.',
    );
  });
});
