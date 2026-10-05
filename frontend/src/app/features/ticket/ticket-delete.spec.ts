import { HttpErrorResponse } from '@angular/common/http';
import { Component, signal } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideRouter, Router } from '@angular/router';
import { ConfirmationService, MessageService } from 'primeng/api';
import type { MockInstance } from 'vitest';
import { Problem, Ticket } from '../../api/models';
import { TicketActions } from '../../core/ticket-actions.service';
import { ConfirmDialog } from '../../shared/confirm-dialog';
import { TicketDelete } from './ticket-delete';

const ticket = { key: 'acme/COW-12', title: 'Pasted into the wrong tenant' } as Ticket;

/** The page around the button: the confirmation is the page's, as on the detail page. */
@Component({
  imports: [ConfirmDialog, TicketDelete],
  providers: [ConfirmationService],
  template: `<app-confirm-dialog /><app-ticket-delete [ticket]="ticket()" />`,
})
class Host {
  readonly ticket = signal(ticket);
}

describe('TicketDelete', () => {
  let remove: MockInstance<TicketActions['delete']>;
  let dependents: MockInstance<TicketActions['dependents']>;
  let navigate: MockInstance<Router['navigate']>;

  beforeEach(() => {
    remove = vi.fn<TicketActions['delete']>().mockResolvedValue(undefined);
    dependents = vi.fn<TicketActions['dependents']>().mockResolvedValue([]);
    TestBed.configureTestingModule({
      providers: [
        provideRouter([]),
        MessageService,
        { provide: TicketActions, useValue: { delete: remove, dependents } },
      ],
    });
    navigate = vi.spyOn(TestBed.inject(Router), 'navigate').mockResolvedValue(true);
  });

  afterEach(() => vi.restoreAllMocks());

  async function settle(fixture: ComponentFixture<Host>) {
    for (let round = 0; round < 3; round++) {
      await new Promise((resolve) => setTimeout(resolve));
      fixture.detectChanges();
    }
    await fixture.whenStable();
  }

  async function ask() {
    const fixture = TestBed.createComponent(Host);
    await settle(fixture);
    (fixture.nativeElement as HTMLElement)
      .querySelector<HTMLElement>('[data-testid="delete-ticket"]')
      ?.click();
    await settle(fixture);
    return fixture;
  }

  const dialog = () => document.body.querySelector('.p-confirmdialog');
  const press = (label: string) =>
    [...(dialog()?.querySelectorAll('button') ?? [])]
      .find((button) => button.textContent?.trim() === label)
      ?.dispatchEvent(new MouseEvent('click', { bubbles: true }));

  it('asks first, saying that the ticket can be restored until it is purged', async () => {
    await ask();

    expect(dependents).toHaveBeenCalledExactlyOnceWith('acme/COW-12');
    expect(dialog()?.textContent).toContain('Delete COW-12?');
    expect(dialog()?.textContent).toContain('leaves every list, board and search at once');
    expect(dialog()?.textContent).toContain('restore it from the deleted tickets for thirty days');
    expect(dialog()?.textContent).not.toContain('wait on it');
    expect(remove).not.toHaveBeenCalled();
  });

  it('names the open tickets that wait on it, and does not refuse over them (docs/adr/0024 D7)', async () => {
    dependents.mockResolvedValue(['COW-13', 'OPS-2']);

    await ask();

    expect(dialog()?.textContent).toContain('2 open tickets wait on it: COW-13, OPS-2.');
    press('Delete');
    await new Promise((resolve) => setTimeout(resolve));
    expect(remove).toHaveBeenCalledExactlyOnceWith('acme/COW-12');
  });

  it('deletes it on confirmation, says where it went and leaves for the backlog of its project', async () => {
    const add = vi.spyOn(TestBed.inject(MessageService), 'add');
    const fixture = await ask();

    press('Delete');
    await settle(fixture);

    expect(remove).toHaveBeenCalledExactlyOnceWith('acme/COW-12');
    expect(add).toHaveBeenCalledWith(expect.objectContaining({ summary: 'Ticket deleted' }));
    expect(navigate).toHaveBeenCalledExactlyOnceWith(['/t', 'acme', 'p', 'COW', 'backlog']);
  });

  it('deletes nothing when the person keeps it', async () => {
    const fixture = await ask();

    press('Keep it');
    await settle(fixture);

    expect(remove).not.toHaveBeenCalled();
    expect(navigate).not.toHaveBeenCalled();
  });

  it('toasts a refusal and stays', async () => {
    const body: Problem = {
      type: 'about:blank',
      title: 'Forbidden',
      status: 403,
      code: 'forbidden',
      detail: 'the act needs the admin role',
    };
    remove.mockRejectedValue(new HttpErrorResponse({ status: 403, error: body }));
    const add = vi.spyOn(TestBed.inject(MessageService), 'add');
    const fixture = await ask();

    press('Delete');
    await settle(fixture);

    expect(add).toHaveBeenCalledWith(
      expect.objectContaining({ detail: 'the act needs the admin role' }),
    );
    expect(navigate).not.toHaveBeenCalled();
  });
});
