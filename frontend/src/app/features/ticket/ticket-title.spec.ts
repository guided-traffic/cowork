import { HttpErrorResponse } from '@angular/common/http';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { ConfirmationService, MessageService } from 'primeng/api';
import type { MockInstance } from 'vitest';
import { Problem, Ticket } from '../../api/models';
import { StaleWrite, TicketActions } from '../../core/ticket-actions.service';
import { TicketTitle } from './ticket-title';

function ticket(overrides: Partial<Ticket> = {}): Ticket {
  return {
    key: 'acme/COW-12',
    number: 12,
    project: 'COW',
    title: 'The board flickers',
    body: '',
    version: 3,
    ...overrides,
  } as Ticket;
}

const stale = (current: Partial<Ticket>) =>
  new StaleWrite(
    {
      status: 412,
      code: 'precondition_failed',
      title: 'The ticket changed',
      detail: '',
      fields: {},
      current: {},
    },
    ticket({ version: 4, ...current }),
  );

describe('TicketTitle', () => {
  let update: MockInstance<TicketActions['update']>;
  let confirm: MockInstance<ConfirmationService['confirm']>;

  beforeEach(() => {
    update = vi.fn<TicketActions['update']>().mockResolvedValue(ticket());
    confirm = vi.fn<ConfirmationService['confirm']>();
    TestBed.configureTestingModule({
      providers: [
        MessageService,
        { provide: ConfirmationService, useValue: { confirm } },
        { provide: TicketActions, useValue: { update } },
      ],
    });
  });

  async function render(current: Ticket = ticket()) {
    const fixture = TestBed.createComponent(TicketTitle);
    fixture.componentRef.setInput('ticket', current);
    await settle(fixture);
    return fixture;
  }

  async function settle(fixture: ComponentFixture<TicketTitle>) {
    await new Promise((resolve) => setTimeout(resolve));
    fixture.detectChanges();
    await fixture.whenStable();
  }

  const el = (fixture: ComponentFixture<TicketTitle>, testId: string) =>
    (fixture.nativeElement as HTMLElement).querySelector<HTMLElement>(`[data-testid="${testId}"]`);

  const input = (fixture: ComponentFixture<TicketTitle>) =>
    el(fixture, 'title-input') as HTMLInputElement | null;

  async function edit(fixture: ComponentFixture<TicketTitle>, text?: string) {
    el(fixture, 'title-edit')?.click();
    await settle(fixture);
    if (text !== undefined) {
      const field = input(fixture) as HTMLInputElement;
      field.value = text;
      field.dispatchEvent(new Event('input'));
      fixture.detectChanges();
    }
  }

  async function save(fixture: ComponentFixture<TicketTitle>) {
    (el(fixture, 'title-save') as HTMLButtonElement).click();
    await settle(fixture);
  }

  it('shows the title as the heading of the page, with a button to edit it', async () => {
    const fixture = await render();

    expect(el(fixture, 'ticket-title')?.tagName).toBe('H1');
    expect(el(fixture, 'ticket-title')?.textContent).toBe('The board flickers');
    expect(el(fixture, 'title-edit')?.getAttribute('aria-label')).toBe('Edit the title');
  });

  it('edits the title in place, starting from the one the ticket has', async () => {
    const fixture = await render();

    await edit(fixture);

    expect(el(fixture, 'ticket-title')).toBeNull();
    expect(input(fixture)?.value).toBe('The board flickers');
  });

  it('writes the title, trimmed, over the version the editing began with, and closes', async () => {
    const fixture = await render();
    await edit(fixture, '  The board no longer flickers ');

    await save(fixture);

    expect(update).toHaveBeenCalledExactlyOnceWith(
      'acme/COW-12',
      { title: 'The board no longer flickers' },
      ticket(),
    );
    expect(input(fixture)).toBeNull();
  });

  it('writes over the version it began with even when a newer one arrived meanwhile', async () => {
    const fixture = await render();
    await edit(fixture, 'Mine');

    fixture.componentRef.setInput('ticket', ticket({ version: 4, severity: 'high' }));
    await settle(fixture);
    await save(fixture);

    expect(update.mock.calls[0][2]?.version).toBe(3);
    expect(input(fixture)).toBeNull();
  });

  it('closes without a write when the title did not change', async () => {
    const fixture = await render();
    await edit(fixture, 'The board flickers ');

    await save(fixture);

    expect(update).not.toHaveBeenCalled();
    expect(input(fixture)).toBeNull();
  });

  it('cannot save an empty title', async () => {
    const fixture = await render();
    await edit(fixture, '   ');

    expect((el(fixture, 'title-save') as HTMLButtonElement).disabled).toBe(true);
  });

  it.each([
    ['Cancel', (fixture: ComponentFixture<TicketTitle>) => el(fixture, 'title-cancel')?.click()],
    [
      'Escape',
      (fixture: ComponentFixture<TicketTitle>) =>
        input(fixture)?.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' })),
    ],
  ])('closes on %s without a write', async (_, close) => {
    const fixture = await render();
    await edit(fixture, 'Mine');

    close(fixture);
    await settle(fixture);

    expect(update).not.toHaveBeenCalled();
    expect(el(fixture, 'ticket-title')?.textContent).toBe('The board flickers');
  });

  describe('a title somebody else changed meanwhile', () => {
    async function conflicted() {
      update.mockRejectedValueOnce(stale({ title: 'Their title' }));
      const fixture = await render();
      await edit(fixture, 'Mine');
      await save(fixture);
      return fixture;
    }

    it('asks in the dialog of the page, naming theirs and the person own', async () => {
      await conflicted();

      expect(confirm.mock.calls[0][0]).toMatchObject({
        header: 'Changed meanwhile',
        message:
          'Someone changed the title while you edited it: now “Their title”, yours “Mine”. Write yours over it?',
        acceptLabel: 'Write mine',
        rejectLabel: 'Keep theirs',
      });
    });

    it('writes the person own over the new version on request', async () => {
      const fixture = await conflicted();

      confirm.mock.calls[0][0].accept?.();
      await settle(fixture);

      expect(update).toHaveBeenLastCalledWith(
        'acme/COW-12',
        { title: 'Mine' },
        expect.objectContaining({ version: 4, title: 'Their title' }),
      );
      expect(input(fixture)).toBeNull();
    });

    it('keeps theirs and closes on request', async () => {
      const fixture = await conflicted();

      confirm.mock.calls[0][0].reject?.();
      await settle(fixture);

      expect(update).toHaveBeenCalledOnce();
      expect(input(fixture)).toBeNull();
    });
  });

  it('toasts a refusal and keeps what was typed', async () => {
    const body: Problem = {
      type: 'about:blank',
      title: 'Forbidden',
      status: 403,
      code: 'forbidden',
    };
    update.mockRejectedValueOnce(new HttpErrorResponse({ status: 403, error: body }));
    const add = vi.spyOn(TestBed.inject(MessageService), 'add');
    const fixture = await render();
    await edit(fixture, 'Mine');

    await save(fixture);

    expect(add).toHaveBeenCalledOnce();
    expect(input(fixture)?.value).toBe('Mine');
  });

  it('closes when the page turns to another ticket, which nothing is written to', async () => {
    const fixture = await render();
    await edit(fixture, 'Mine');

    fixture.componentRef.setInput(
      'ticket',
      ticket({ key: 'acme/COW-13', number: 13, title: 'Next' }),
    );
    await settle(fixture);

    expect(input(fixture)).toBeNull();
    expect(el(fixture, 'ticket-title')?.textContent).toBe('Next');
    expect(update).not.toHaveBeenCalled();
  });

  it('drops the question about a newer title for a ticket the page no longer shows', async () => {
    update.mockRejectedValueOnce(stale({ title: 'Their title' }));
    const fixture = await render();
    await edit(fixture, 'Mine');
    await save(fixture);

    fixture.componentRef.setInput('ticket', ticket({ key: 'acme/COW-13', number: 13 }));
    await settle(fixture);
    confirm.mock.calls[0][0].accept?.();
    await settle(fixture);

    expect(update).toHaveBeenCalledOnce();
  });
});
