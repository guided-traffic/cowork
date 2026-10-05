import { HttpErrorResponse } from '@angular/common/http';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { By } from '@angular/platform-browser';
import { MenuItem, MessageService } from 'primeng/api';
import { Menu } from 'primeng/menu';
import type { MockInstance } from 'vitest';
import { Block, Problem, Ticket, TicketState } from '../../api/models';
import { TicketActions } from '../../core/ticket-actions.service';
import { MoveDialog } from './move-dialog';
import { TicketMoves } from './ticket-moves';

/** The part of a ticket that the moves read. */
function ticket(state: TicketState, fields: Partial<Ticket> = {}): Ticket {
  return {
    key: 'acme/COW-12',
    number: 12,
    state,
    block: null,
    done_from: null,
    done_by_hand: false,
    open_prerequisites: 0,
    score: null,
    score_version: null,
    ...fields,
  } as Ticket;
}

const blocked = (from: TicketState) =>
  ticket('blocked', { block: { kind: 'human', from, reason: 'Waiting' } as Block });

function refusal(status: number, code: Problem['code'], title: string, detail?: string) {
  const body: Problem = { type: 'about:blank', title, status, code, ...(detail ? { detail } : {}) };
  return new HttpErrorResponse({ status, statusText: title, error: body });
}

describe('TicketMoves', () => {
  let transition: MockInstance<TicketActions['transition']>;

  beforeEach(() => {
    transition = vi.fn<TicketActions['transition']>().mockResolvedValue({} as Ticket);
    TestBed.configureTestingModule({
      providers: [
        MessageService,
        { provide: TicketActions, useValue: { transition, update: vi.fn() } },
      ],
    });
  });

  async function render(current: Ticket) {
    const fixture = TestBed.createComponent(TicketMoves);
    fixture.componentRef.setInput('ticket', current);
    await settle(fixture);
    return fixture;
  }

  /** Lets what a click started finish, and shows it; fields of the dialog register a moment later. */
  async function settle(fixture: ComponentFixture<TicketMoves>) {
    await new Promise((resolve) => setTimeout(resolve));
    fixture.detectChanges();
    await fixture.whenStable();
  }

  const host = (fixture: ComponentFixture<TicketMoves>) => fixture.nativeElement as HTMLElement;

  const el = (fixture: ComponentFixture<TicketMoves>, testId: string) =>
    host(fixture).querySelector<HTMLElement>(`[data-testid="${testId}"]`);

  /** A button of PrimeNG's directive carries its test id itself. */
  const button = (fixture: ComponentFixture<TicketMoves>, testId: string) =>
    el(fixture, testId) as HTMLButtonElement | null;

  /** The label of the main button and the labels of the menu behind it. */
  function offered(fixture: ComponentFixture<TicketMoves>) {
    const main = host(fixture).querySelector('.moves > button[data-testid^="move-"]');
    const menu = fixture.debugElement.query(By.directive(Menu));
    return {
      main: main?.textContent?.trim(),
      others: menu
        ? ((menu.componentInstance as Menu).model() ?? []).map((item) => item.label)
        : [],
    };
  }

  /** What choosing an item of the menu does. */
  function pick(fixture: ComponentFixture<TicketMoves>, label: string) {
    const items =
      (fixture.debugElement.query(By.directive(Menu)).componentInstance as Menu).model() ?? [];
    const item = items.find((candidate: MenuItem) => candidate.label === label);
    if (!item?.command) {
      throw new Error(`no menu item ${label}`);
    }
    item.command({ item });
  }

  const dialog = (fixture: ComponentFixture<TicketMoves>) =>
    fixture.debugElement.query(By.directive(MoveDialog)).componentInstance as MoveDialog;

  const dialogTitle = (fixture: ComponentFixture<TicketMoves>) =>
    host(fixture).querySelector('.p-dialog-title')?.textContent;

  describe('what a ticket can do', () => {
    it.each([
      ['filed', ticket('filed'), 'Move to analysed', ['Done by hand', 'Block', 'Drop']],
      ['analysed', ticket('analysed'), 'Move to decided', ['Done by hand', 'Block', 'Drop']],
      [
        'decided',
        ticket('decided'),
        'Move to in-progress',
        ['Done by hand', 'Back to analysed', 'Block', 'Drop'],
      ],
      [
        'in-progress',
        ticket('in-progress'),
        'Move to review',
        ['Done by hand', 'Back to decided', 'Back to analysed', 'Block', 'Drop'],
      ],
      ['review', ticket('review'), 'Done by hand', ['Back to in-progress', 'Block', 'Drop']],
      [
        'blocked from in-progress',
        blocked('in-progress'),
        'Unblock to in-progress',
        ['Done by hand', 'Drop'],
      ],
      ['blocked from review', blocked('review'), 'Unblock to review', ['Done by hand', 'Drop']],
      [
        'done by hand',
        ticket('done', { done_by_hand: true, done_from: 'review' }),
        'Withdraw done',
        [],
      ],
      ['dropped', ticket('dropped'), 'Reopen', []],
    ] as [string, Ticket, string, string[]][])(
      'offers a ticket that is %s its main move and the menu of the others',
      async (_, current, main, others) => {
        const fixture = await render(current);

        expect(offered(fixture)).toEqual({ main, others });
      },
    );

    it('offers a ticket done by its stages no move: a lower stage is its way out', async () => {
      const fixture = await render(ticket('done', { done_by_hand: false, done_from: 'review' }));

      expect(offered(fixture)).toEqual({ main: undefined, others: [] });
      expect(el(fixture, 'more-moves')).toBeNull();
    });

    it('offers a blocked ticket that does not say where it came from done by hand first, and the drop', async () => {
      const fixture = await render(ticket('blocked'));

      expect(offered(fixture)).toEqual({ main: 'Done by hand', others: ['Drop'] });
    });

    it('names the state that the main button moves to in its test id', async () => {
      const fixture = await render(ticket('filed'));

      expect(el(fixture, 'move-analysed')).not.toBeNull();
    });

    it('shows no menu button when the main move is the only one', async () => {
      const fixture = await render(ticket('dropped'));

      expect(el(fixture, 'more-moves')).toBeNull();
      expect(host(fixture).querySelector('p-menu')).toBeNull();
    });

    it('opens the menu of the other moves from its button', async () => {
      const fixture = await render(ticket('review'));
      const menu = fixture.debugElement.query(By.directive(Menu)).componentInstance as Menu;
      const toggle = vi.spyOn(menu, 'toggle');

      button(fixture, 'more-moves')?.click();
      await settle(fixture);

      expect(toggle).toHaveBeenCalledOnce();
      expect(el(fixture, 'more-moves')?.getAttribute('aria-label')).toBe('More moves');
      expect(
        [...document.body.querySelectorAll('[role="menuitem"]')].map((item) =>
          item.textContent?.trim(),
        ),
      ).toEqual(['Back to in-progress', 'Block', 'Drop']);
    });

    it.each([
      ['filed', ticket('filed'), 'pi-arrow-right'],
      ['review', ticket('review'), 'pi-check'],
      ['done by hand', ticket('done', { done_by_hand: true, done_from: 'review' }), 'pi-replay'],
      ['dropped', ticket('dropped'), 'pi-replay'],
    ] as [string, Ticket, string][])(
      'marks the main move of a ticket that is %s with %s',
      async (_, current, icon) => {
        const fixture = await render(current);

        const main = host(fixture).querySelector('.moves > button[data-testid^="move-"]');
        expect(main?.querySelector('i')?.classList).toContain(icon);
      },
    );

    it('follows the ticket when its state changes', async () => {
      const fixture = await render(ticket('in-progress'));

      fixture.componentRef.setInput('ticket', ticket('review'));
      await settle(fixture);

      expect(offered(fixture).main).toBe('Done by hand');
    });
  });

  describe('a move that asks for nothing', () => {
    it('is sent at once, from the main button', async () => {
      const fixture = await render(ticket('in-progress'));

      button(fixture, 'move-review')?.click();
      await settle(fixture);

      expect(transition).toHaveBeenCalledExactlyOnceWith('acme/COW-12', { to: 'review' });
      expect(host(fixture).querySelector('.p-dialog')).toBeNull();
    });

    it('is sent at once for an unblock, which goes back where the block came from', async () => {
      const fixture = await render(blocked('decided'));

      button(fixture, 'move-decided')?.click();
      await settle(fixture);

      expect(transition).toHaveBeenCalledExactlyOnceWith('acme/COW-12', { to: 'decided' });
    });

    it('shows its button as loading until the answer is in', async () => {
      let finish: (ticket: Ticket) => void = () => undefined;
      transition.mockReturnValue(
        new Promise<Ticket>((resolve) => {
          finish = resolve;
        }),
      );
      const fixture = await render(ticket('filed'));
      const main = button(fixture, 'move-analysed');
      expect(main?.disabled).toBe(false);

      main?.click();
      await settle(fixture);
      expect(main?.disabled).toBe(true);
      expect(main?.querySelector('i.pi-spinner')).not.toBeNull();

      finish({} as Ticket);
      await settle(fixture);
      expect(main?.disabled).toBe(false);
      expect(main?.querySelector('i.pi-spinner')).toBeNull();
    });

    it('toasts the problem when the server refuses', async () => {
      transition.mockRejectedValue(
        refusal(409, 'state_conflict', 'The ticket moved meanwhile', 'It is analysed now.'),
      );
      const add = vi.spyOn(TestBed.inject(MessageService), 'add');
      const fixture = await render(ticket('filed'));

      button(fixture, 'move-analysed')?.click();
      await settle(fixture);

      expect(add).toHaveBeenCalledExactlyOnceWith(
        expect.objectContaining({
          severity: 'warn',
          summary: 'The ticket moved meanwhile',
          detail: 'It is analysed now.',
        }),
      );
      expect(button(fixture, 'move-analysed')?.disabled).toBe(false);
    });
  });

  describe('a move that asks for something', () => {
    it.each([
      ['a drop', ticket('filed'), 'Drop', 'Drop'],
      ['a step back', ticket('review'), 'Back to in-progress', 'Back to in-progress'],
      ['a block', ticket('review'), 'Block', 'Block'],
      ['done by hand', ticket('decided'), 'Done by hand', 'Done by hand: how was it verified?'],
    ] as [string, Ticket, string, string][])(
      'opens the dialog for %s from the menu',
      async (_, current, label, title) => {
        const fixture = await render(current);

        pick(fixture, label);
        await settle(fixture);

        expect(dialogTitle(fixture)).toBe(title);
        expect(dialog(fixture).request()).toEqual({
          kind: 'move',
          move: expect.objectContaining({ label }),
        });
        expect(transition).not.toHaveBeenCalled();
      },
    );

    it('opens the dialog from the main button, for done by hand in review', async () => {
      const fixture = await render(ticket('review'));

      button(fixture, 'move-done')?.click();
      await settle(fixture);

      expect(dialogTitle(fixture)).toBe('Done by hand: how was it verified?');
    });

    it('opens the dialog for the withdrawal of a done by hand, and for the reopen of a dropped ticket', async () => {
      const fixture = await render(
        ticket('done', { done_by_hand: true, done_from: 'in-progress' }),
      );

      button(fixture, 'move-in-progress')?.click();
      await settle(fixture);
      expect(dialogTitle(fixture)).toBe('Withdraw done');

      fixture.componentRef.setInput('ticket', ticket('dropped'));
      dialog(fixture).closed.emit(false);
      await settle(fixture);
      button(fixture, 'move-filed')?.click();
      await settle(fixture);
      expect(dialogTitle(fixture)).toBe('Reopen');
    });

    it('gives the dialog the ticket as it is', async () => {
      const fixture = await render(ticket('decided'));

      expect(dialog(fixture).ticket().key).toBe('acme/COW-12');
    });

    it('closes the dialog when it ends', async () => {
      const fixture = await render(ticket('filed'));
      pick(fixture, 'Drop');
      await settle(fixture);

      dialog(fixture).closed.emit(true);
      await settle(fixture);

      expect(dialogTitle(fixture)).toBeUndefined();
      expect(dialog(fixture).request()).toBeNull();
    });

    it('closes the dialog when the page turns to another ticket, which nothing is sent to', async () => {
      const fixture = await render(ticket('filed'));
      pick(fixture, 'Drop');
      await settle(fixture);
      expect(dialogTitle(fixture)).toBe('Drop');

      fixture.componentRef.setInput('ticket', ticket('filed', { key: 'acme/COW-13', number: 13 }));
      await settle(fixture);

      expect(dialogTitle(fixture)).toBeUndefined();
      expect(dialog(fixture).request()).toBeNull();
      expect(transition).not.toHaveBeenCalled();
    });

    it('keeps the dialog open when the same ticket comes back in a newer version', async () => {
      const fixture = await render(ticket('filed'));
      pick(fixture, 'Drop');
      await settle(fixture);

      fixture.componentRef.setInput('ticket', ticket('filed', { open_prerequisites: 1 }));
      await settle(fixture);

      expect(dialogTitle(fixture)).toBe('Drop');
    });
  });
});
