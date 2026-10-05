import { HttpErrorResponse } from '@angular/common/http';
import { signal, WritableSignal } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { MessageService } from 'primeng/api';
import type { MockInstance } from 'vitest';
import { DeletedTicket, Problem, Ticket } from '../../api/models';
import { DeletedTicketsService } from '../../core/deleted-tickets.service';
import { SessionService } from '../../core/session.service';
import { TenantService } from '../../core/tenant.service';
import { Clock, dateTime } from '../../shared/time';
import { DeletedTickets } from './deleted-tickets';

const entry = (number: number, overrides: Partial<DeletedTicket> = {}): DeletedTicket => ({
  key: `acme/COW-${number}`,
  project: 'COW',
  number,
  type: 'bug',
  title: `Pasted into the wrong tenant ${number}`,
  state: 'filed',
  confidential: false,
  deleted_at: '2026-10-04T10:00:00Z',
  deleted_by: { id: 'p-ada', display_name: 'Ada Admin', username: 'ada' },
  purge_at: '2026-11-03T10:00:00Z',
  ...overrides,
});

const refusal = (status: number, title: string, detail: string) => {
  const body: Problem = { type: 'about:blank', title, status, code: 'not_found', detail };
  return new HttpErrorResponse({ status, statusText: title, error: body });
};

describe('DeletedTickets', () => {
  let list: WritableSignal<DeletedTicket[]>;
  let isAdmin: WritableSignal<boolean>;
  let restore: MockInstance<DeletedTicketsService['restore']>;
  let purge: MockInstance<DeletedTicketsService['purge']>;
  let reload: MockInstance<() => boolean>;

  beforeEach(() => {
    list = signal([entry(12), entry(9, { confidential: true })]);
    isAdmin = signal(true);
    restore = vi
      .fn<DeletedTicketsService['restore']>()
      .mockResolvedValue({ key: 'acme/COW-12' } as Ticket);
    purge = vi.fn<DeletedTicketsService['purge']>().mockResolvedValue(undefined);
    reload = vi.fn<() => boolean>().mockReturnValue(true);
    TestBed.configureTestingModule({
      providers: [
        MessageService,
        {
          provide: DeletedTicketsService,
          useValue: {
            list,
            bin: { isLoading: signal(false), error: signal(undefined), reload },
            restore,
            purge,
          },
        },
        { provide: SessionService, useValue: { tenant: signal('acme') } },
        { provide: TenantService, useValue: { isAdmin } },
        { provide: Clock, useValue: { now: signal(Date.parse('2026-10-05T10:00:00Z')) } },
      ],
    });
  });

  afterEach(() => vi.restoreAllMocks());

  async function render() {
    const fixture = TestBed.createComponent(DeletedTickets);
    await settle(fixture);
    return fixture;
  }

  async function settle(fixture: ComponentFixture<DeletedTickets>) {
    for (let round = 0; round < 3; round++) {
      await new Promise((resolve) => setTimeout(resolve));
      fixture.detectChanges();
    }
    await fixture.whenStable();
  }

  const host = (fixture: ComponentFixture<DeletedTickets>) => fixture.nativeElement as HTMLElement;
  const el = (fixture: ComponentFixture<DeletedTickets>, testId: string) =>
    host(fixture).querySelector<HTMLElement>(`[data-testid="${testId}"]`);
  const dialog = () => document.body.querySelector('.p-confirmdialog');
  const press = (label: string) =>
    [...(dialog()?.querySelectorAll('button') ?? [])]
      .find((button) => button.textContent?.trim() === label)
      ?.dispatchEvent(new MouseEvent('click', { bubbles: true }));

  it('asks for the bin again on the way in and lists each ticket with who deleted it and when it is purged', async () => {
    const fixture = await render();

    expect(reload).toHaveBeenCalled();
    const row = el(fixture, 'bin-COW-12');
    expect(row?.textContent).toContain('COW-12');
    expect(row?.textContent).toContain('Pasted into the wrong tenant 12');
    expect(row?.textContent).toContain('by Ada Admin');
    expect(row?.textContent).toContain(dateTime('2026-11-03T10:00:00Z'));
    expect(el(fixture, 'bin-COW-9')?.querySelector('.pi-lock')).not.toBeNull();
  });

  it('says that the bin is empty', async () => {
    list.set([]);

    const fixture = await render();

    expect(el(fixture, 'bin-empty')?.textContent).toContain('The bin is empty.');
  });

  it('shows nothing of it to anybody but an administrator', async () => {
    isAdmin.set(false);

    const fixture = await render();

    expect(el(fixture, 'bin')).toBeNull();
    expect(el(fixture, 'bin-not-admin')).not.toBeNull();
  });

  it('restores a ticket at once and says so', async () => {
    const add = vi.spyOn(TestBed.inject(MessageService), 'add');
    const fixture = await render();

    el(fixture, 'restore-COW-12')?.click();
    await settle(fixture);

    expect(restore).toHaveBeenCalledExactlyOnceWith(entry(12));
    expect(add).toHaveBeenCalledWith(expect.objectContaining({ summary: 'Ticket restored' }));
  });

  it('asks twice before a purge, the second time saying that nothing brings it back', async () => {
    const fixture = await render();

    el(fixture, 'purge-COW-12')?.click();
    await settle(fixture);
    expect(dialog()?.textContent).toContain('Purge COW-12 now?');
    expect(dialog()?.textContent).toContain('would be purged by itself');
    press('Continue');
    await settle(fixture);

    expect(purge).not.toHaveBeenCalled();
    expect(dialog()?.textContent).toContain('Purge COW-12 for good?');
    expect(dialog()?.textContent).toContain('Nothing brings COW-12 back');
    const accept = [...(dialog()?.querySelectorAll('button') ?? [])].find(
      (button) => button.textContent?.trim() === 'Purge for good',
    );
    expect(accept?.className).toContain('p-button-danger');
    press('Purge for good');
    await settle(fixture);

    expect(purge).toHaveBeenCalledExactlyOnceWith(entry(12));
  });

  it.each(['first', 'second'])(
    'purges nothing when the person keeps it at the %s question',
    async (which) => {
      const fixture = await render();
      el(fixture, 'purge-COW-12')?.click();
      await settle(fixture);
      if (which === 'second') {
        press('Continue');
        await settle(fixture);
      }

      press('Keep it');
      await settle(fixture);

      expect(purge).not.toHaveBeenCalled();
    },
  );

  it('toasts the problem when a restoration is refused', async () => {
    restore.mockRejectedValue(refusal(404, 'Not found', 'no such deleted ticket'));
    const add = vi.spyOn(TestBed.inject(MessageService), 'add');
    const fixture = await render();

    el(fixture, 'restore-COW-12')?.click();
    await settle(fixture);

    expect(add).toHaveBeenCalledWith(expect.objectContaining({ detail: 'no such deleted ticket' }));
  });
});
