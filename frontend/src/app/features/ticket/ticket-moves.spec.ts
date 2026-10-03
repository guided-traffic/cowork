import { HttpErrorResponse } from '@angular/common/http';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { By } from '@angular/platform-browser';
import { MenuItem, MessageService } from 'primeng/api';
import { Menu } from 'primeng/menu';
import type { MockInstance } from 'vitest';
import { Block, Problem, Ticket, TicketState } from '../../api/models';
import { TicketActions } from '../../core/ticket-actions.service';
import { TicketMoves } from './ticket-moves';

/** The part of a ticket that the moves read. */
function ticket(state: TicketState, block: Block | null = null): Ticket {
  return { key: 'acme/COW-12', state, block } as Ticket;
}

function refusal(status: number, code: Problem['code'], title: string, detail?: string) {
  const body: Problem = { type: 'about:blank', title, status, code, ...(detail ? { detail } : {}) };
  return new HttpErrorResponse({ status, statusText: title, error: body });
}

describe('TicketMoves', () => {
  let transition: MockInstance<TicketActions['transition']>;

  beforeEach(() => {
    transition = vi.fn<TicketActions['transition']>().mockResolvedValue({} as Ticket);
    TestBed.configureTestingModule({
      providers: [MessageService, { provide: TicketActions, useValue: { transition } }],
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

  function typeInto(fixture: ComponentFixture<TicketMoves>, testId: string, value: string) {
    const field = el(fixture, testId) as HTMLInputElement | HTMLTextAreaElement;
    field.value = value;
    field.dispatchEvent(new Event('input'));
    fixture.detectChanges();
  }

  async function choose(fixture: ComponentFixture<TicketMoves>, testId: string, value: unknown) {
    fixture.debugElement
      .query(By.css(`[data-testid="${testId}"]`))
      .triggerEventHandler('ngModelChange', value);
    await settle(fixture);
  }

  const submit = (fixture: ComponentFixture<TicketMoves>) =>
    host(fixture)
      .querySelector('form')
      ?.dispatchEvent(new Event('submit', { cancelable: true }));

  const sendButton = (fixture: ComponentFixture<TicketMoves>) => button(fixture, 'move-send');

  const dialogTitle = (fixture: ComponentFixture<TicketMoves>) =>
    host(fixture).querySelector('.p-dialog-title')?.textContent;

  describe('what a ticket can do', () => {
    it.each([
      ['filed', undefined, 'Move to analysed', ['Block', 'Drop']],
      ['analysed', undefined, 'Move to decided', ['Block', 'Drop']],
      ['decided', undefined, 'Move to in-progress', ['Back to analysed', 'Block', 'Drop']],
      ['in-progress', undefined, 'Done', ['Back to decided', 'Back to analysed', 'Block', 'Drop']],
      ['blocked', 'in-progress', 'Unblock to in-progress', ['Drop']],
      ['blocked', 'decided', 'Unblock to decided', ['Drop']],
      ['done', undefined, 'Reopen', []],
      ['dropped', undefined, 'Reopen', []],
    ] as [TicketState, TicketState | undefined, string, string[]][])(
      'offers a ticket that is %s (blocked from %s) %s, and the menu %j',
      async (state, from, main, others) => {
        const block = from ? ({ kind: 'human', from, reason: 'Waiting' } as Block) : null;

        const fixture = await render(ticket(state, block));

        expect(offered(fixture)).toEqual({ main, others });
      },
    );

    it('offers a blocked ticket that does not say where it came from only to drop it', async () => {
      const fixture = await render(ticket('blocked'));

      expect(offered(fixture)).toEqual({ main: undefined, others: ['Drop'] });
    });

    it('names the state that the main button moves to in its test id', async () => {
      const fixture = await render(ticket('filed'));

      expect(el(fixture, 'move-analysed')).not.toBeNull();
    });

    it('shows no menu button when the main move is the only one', async () => {
      const fixture = await render(ticket('done'));

      expect(el(fixture, 'more-moves')).toBeNull();
      expect(host(fixture).querySelector('p-menu')).toBeNull();
    });

    it('opens the menu of the other moves from its button', async () => {
      const fixture = await render(ticket('decided'));
      const menu = fixture.debugElement.query(By.directive(Menu)).componentInstance as Menu;
      const toggle = vi.spyOn(menu, 'toggle');

      button(fixture, 'more-moves')?.click();
      await settle(fixture);

      expect(toggle).toHaveBeenCalledOnce();
      expect(document.body.querySelector('[role="menu"]')).not.toBeNull();
      expect(
        [...document.body.querySelectorAll('[role="menuitem"]')].map((item) =>
          item.textContent?.trim(),
        ),
      ).toEqual(['Back to analysed', 'Block', 'Drop']);
    });

    it('shows a menu button when there are other moves', async () => {
      const fixture = await render(ticket('filed'));

      expect(el(fixture, 'more-moves')).not.toBeNull();
      expect(el(fixture, 'more-moves')?.getAttribute('aria-label')).toBe('More moves');
    });

    it.each([
      ['filed', 'pi-arrow-right'],
      ['in-progress', 'pi-check'],
      ['done', 'pi-replay'],
    ] as [TicketState, string][])(
      'marks the main move of a %s ticket with %s',
      async (state, icon) => {
        const fixture = await render(ticket(state));

        const main = host(fixture).querySelector('.moves > button[data-testid^="move-"]');
        expect(main?.querySelector('i')?.classList).toContain(icon);
      },
    );

    it('follows the ticket when its state changes', async () => {
      const fixture = await render(ticket('filed'));

      fixture.componentRef.setInput('ticket', ticket('analysed'));
      await settle(fixture);

      expect(offered(fixture).main).toBe('Move to decided');
    });
  });

  describe('a move that asks for nothing', () => {
    it('is sent at once, from the main button', async () => {
      const fixture = await render(ticket('filed'));

      button(fixture, 'move-analysed')?.click();
      await settle(fixture);

      expect(transition).toHaveBeenCalledExactlyOnceWith('acme/COW-12', { to: 'analysed' });
      expect(host(fixture).querySelector('.p-dialog')).toBeNull();
    });

    it('is sent at once, from the menu as well', async () => {
      const fixture = await render(
        ticket('blocked', { kind: 'human', from: 'decided', reason: 'x' }),
      );

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
    });
  });

  describe('a move that asks for a reason', () => {
    it('opens a dialog with the label of the move as its title', async () => {
      const fixture = await render(ticket('decided'));
      expect(host(fixture).querySelector('.p-dialog')).toBeNull();

      pick(fixture, 'Back to analysed');
      await settle(fixture);

      expect(dialogTitle(fixture)).toBe('Back to analysed');
      expect(host(fixture).querySelector('form label span')?.textContent).toBe('Reason');
      expect(transition).not.toHaveBeenCalled();
    });

    it('sends the reason, without its surrounding spaces, and closes', async () => {
      const fixture = await render(ticket('filed'));
      pick(fixture, 'Drop');
      await settle(fixture);
      expect(sendButton(fixture)?.disabled).toBe(true);

      typeInto(fixture, 'move-text', '  Not wanted any more  ');
      expect(sendButton(fixture)?.disabled).toBe(false);
      submit(fixture);
      await settle(fixture);

      expect(transition).toHaveBeenCalledExactlyOnceWith('acme/COW-12', {
        to: 'dropped',
        reason: 'Not wanted any more',
      });
      expect(host(fixture).querySelector('.p-dialog')).toBeNull();
    });

    it('asks for the reason of a reopen as well', async () => {
      const fixture = await render(ticket('done'));

      button(fixture, 'move-filed')?.click();
      await settle(fixture);
      typeInto(fixture, 'move-text', 'It came back');
      submit(fixture);
      await settle(fixture);

      expect(dialogTitle(fixture)).toBeUndefined();
      expect(transition).toHaveBeenCalledExactlyOnceWith('acme/COW-12', {
        to: 'filed',
        reason: 'It came back',
      });
    });

    it('cannot be sent with a reason of spaces only', async () => {
      const fixture = await render(ticket('filed'));
      pick(fixture, 'Drop');
      await settle(fixture);

      typeInto(fixture, 'move-text', '   ');

      expect(sendButton(fixture)?.disabled).toBe(true);
    });

    it('is closed by Cancel, and starts empty the next time', async () => {
      const fixture = await render(ticket('filed'));
      pick(fixture, 'Drop');
      await settle(fixture);
      typeInto(fixture, 'move-text', 'half a thought');

      const cancel = [...host(fixture).querySelectorAll('.p-dialog button')].find(
        (button) => button.textContent?.trim() === 'Cancel',
      );
      cancel?.dispatchEvent(new MouseEvent('click', { bubbles: true }));
      await settle(fixture);
      expect(host(fixture).querySelector('.p-dialog')).toBeNull();
      expect(transition).not.toHaveBeenCalled();

      pick(fixture, 'Drop');
      await settle(fixture);
      expect((el(fixture, 'move-text') as HTMLTextAreaElement).value).toBe('');
    });

    it('is closed when the dialog asks to be closed, but not when it asks to stay open', async () => {
      const fixture = await render(ticket('filed'));
      pick(fixture, 'Drop');
      await settle(fixture);
      const dialog = fixture.debugElement.query(By.css('p-dialog'));

      dialog.triggerEventHandler('visibleChange', true);
      await settle(fixture);
      expect(dialogTitle(fixture)).toBe('Drop');

      dialog.triggerEventHandler('visibleChange', false);
      await settle(fixture);
      expect(dialogTitle(fixture)).toBeUndefined();
    });
  });

  describe('done', () => {
    it('asks how it was verified and sends that as the note', async () => {
      const fixture = await render(ticket('in-progress'));

      button(fixture, 'move-done')?.click();
      await settle(fixture);
      expect(dialogTitle(fixture)).toBe('Done: how was it verified?');
      expect(host(fixture).querySelector('form label span')?.textContent).toBe('Verification note');
      expect((el(fixture, 'move-text') as HTMLTextAreaElement).placeholder).toBe(
        'What was run, against what, with what result',
      );
      typeInto(fixture, 'move-text', 'make test: 812 passed');
      submit(fixture);
      await settle(fixture);

      expect(transition).toHaveBeenCalledExactlyOnceWith('acme/COW-12', {
        to: 'done',
        note: 'make test: 812 passed',
      });
    });
  });

  describe('a block', () => {
    async function blocking() {
      const fixture = await render(ticket('in-progress'));
      pick(fixture, 'Block');
      await settle(fixture);
      return fixture;
    }

    it('asks what it waits on, decision first, and why', async () => {
      const fixture = await blocking();

      expect(dialogTitle(fixture)).toBe('Block');
      expect(el(fixture, 'block-kind')?.querySelector('.p-select-label')?.textContent?.trim()).toBe(
        'decision',
      );
      expect(el(fixture, 'block-ticket')).toBeNull();
    });

    it('sends the kind and the reason', async () => {
      const fixture = await blocking();
      await choose(fixture, 'block-kind', 'human');
      typeInto(fixture, 'move-text', 'Waiting for the owner');

      submit(fixture);
      await settle(fixture);

      expect(transition).toHaveBeenCalledExactlyOnceWith('acme/COW-12', {
        to: 'blocked',
        reason: 'Waiting for the owner',
        block: { kind: 'human' },
      });
    });

    it('offers the six kinds of block', async () => {
      const fixture = await blocking();

      const select = fixture.debugElement.query(By.css('[data-testid="block-kind"]'));
      expect((select.componentInstance as { options(): string[] }).options()).toEqual([
        'decision',
        'human',
        'product',
        'release',
        'external',
        'ticket',
      ]);
    });

    it('names the ticket it waits on when that is the kind, and needs it', async () => {
      const fixture = await blocking();
      await choose(fixture, 'block-kind', 'ticket');
      typeInto(fixture, 'move-text', 'The board comes first');
      expect(sendButton(fixture)?.disabled).toBe(true);

      typeInto(fixture, 'block-ticket', '  COW-3 ');
      expect(sendButton(fixture)?.disabled).toBe(false);
      submit(fixture);
      await settle(fixture);

      expect(transition).toHaveBeenCalledExactlyOnceWith('acme/COW-12', {
        to: 'blocked',
        reason: 'The board comes first',
        block: { kind: 'ticket', ticket: 'COW-3' },
      });
    });

    it('does not send the ticket of an earlier choice with another kind', async () => {
      const fixture = await blocking();
      await choose(fixture, 'block-kind', 'ticket');
      typeInto(fixture, 'block-ticket', 'COW-3');
      await choose(fixture, 'block-kind', 'release');
      typeInto(fixture, 'move-text', 'Waits for the release');

      submit(fixture);
      await settle(fixture);

      expect(transition).toHaveBeenCalledExactlyOnceWith('acme/COW-12', {
        to: 'blocked',
        reason: 'Waits for the release',
        block: { kind: 'release' },
      });
    });
  });

  describe('a ticket with open prerequisites', () => {
    const prerequisites = () =>
      refusal(409, 'open_prerequisites', 'Open prerequisites', 'COW-3 is not done yet.');

    /** Done, with a note, which the server refuses because COW-3 is open. */
    async function refused() {
      transition.mockRejectedValueOnce(prerequisites());
      const fixture = await render(ticket('in-progress'));
      button(fixture, 'move-done')?.click();
      await settle(fixture);
      typeInto(fixture, 'move-text', 'Verified by hand');
      submit(fixture);
      await settle(fixture);
      return fixture;
    }

    const tick = async (fixture: ComponentFixture<TicketMoves>, value: boolean) => {
      fixture.debugElement.query(By.css('p-checkbox')).triggerEventHandler('ngModelChange', value);
      await settle(fixture);
    };

    it('keeps the dialog open and says which prerequisite is open', async () => {
      const fixture = await refused();

      expect(el(fixture, 'move-error')?.textContent).toBe('COW-3 is not done yet.');
      expect(dialogTitle(fixture)).toBe('Done: how was it verified?');
      expect(host(fixture).querySelector('p-checkbox')).not.toBeNull();
      expect(host(fixture).textContent).toContain('Close it over its open prerequisites');
      expect(el(fixture, 'override-reason')).toBeNull();
      expect(sendButton(fixture)?.disabled).toBe(false);
    });

    it('offers no override before the server has refused', async () => {
      const fixture = await render(ticket('in-progress'));
      button(fixture, 'move-done')?.click();
      await settle(fixture);

      expect(host(fixture).querySelector('p-checkbox')).toBeNull();
      expect(el(fixture, 'move-error')).toBeNull();
    });

    it('asks why it may close before them once the person chooses to, and needs that reason', async () => {
      const fixture = await refused();

      await tick(fixture, true);
      expect(el(fixture, 'override-reason')).not.toBeNull();
      expect(sendButton(fixture)?.disabled).toBe(true);
      typeInto(fixture, 'override-reason', '   ');
      expect(sendButton(fixture)?.disabled).toBe(true);
      typeInto(fixture, 'override-reason', 'The owner agreed');

      expect(sendButton(fixture)?.disabled).toBe(false);
    });

    it('sends the override with its own reason next to the note, and closes', async () => {
      const fixture = await refused();
      await tick(fixture, true);
      typeInto(fixture, 'override-reason', '  The owner agreed ');

      submit(fixture);
      await settle(fixture);

      expect(transition).toHaveBeenCalledTimes(2);
      expect(transition).toHaveBeenLastCalledWith('acme/COW-12', {
        to: 'done',
        note: 'Verified by hand',
        override_prerequisites: true,
        reason: 'The owner agreed',
      });
      expect(dialogTitle(fixture)).toBeUndefined();
    });

    it('sends no override when the person changes their mind', async () => {
      const fixture = await refused();
      await tick(fixture, true);
      typeInto(fixture, 'override-reason', 'The owner agreed');
      await tick(fixture, false);

      submit(fixture);
      await settle(fixture);

      expect(transition).toHaveBeenLastCalledWith('acme/COW-12', {
        to: 'done',
        note: 'Verified by hand',
      });
    });

    it('starts without the override and the message the next time the dialog opens', async () => {
      const fixture = await refused();
      await tick(fixture, true);
      fixture.debugElement.query(By.css('p-dialog')).triggerEventHandler('visibleChange', false);
      await settle(fixture);

      button(fixture, 'move-done')?.click();
      await settle(fixture);

      expect(el(fixture, 'move-error')).toBeNull();
      expect(host(fixture).querySelector('p-checkbox')).toBeNull();
      expect(el(fixture, 'override-reason')).toBeNull();
    });
  });

  describe('another refusal while the dialog is open', () => {
    async function refusedWith(error: HttpErrorResponse) {
      transition.mockRejectedValueOnce(error);
      const fixture = await render(ticket('filed'));
      pick(fixture, 'Drop');
      await settle(fixture);
      typeInto(fixture, 'move-text', 'Not wanted');
      submit(fixture);
      await settle(fixture);
      return fixture;
    }

    it('is shown in the dialog, which stays open with what was typed, without a toast', async () => {
      const add = vi.spyOn(TestBed.inject(MessageService), 'add');

      const fixture = await refusedWith(
        refusal(409, 'state_conflict', 'The ticket moved meanwhile', 'It is analysed now.'),
      );

      expect(el(fixture, 'move-error')?.textContent).toBe('It is analysed now.');
      expect(dialogTitle(fixture)).toBe('Drop');
      expect((el(fixture, 'move-text') as HTMLTextAreaElement).value).toBe('Not wanted');
      expect(host(fixture).querySelector('p-checkbox')).toBeNull();
      expect(add).not.toHaveBeenCalled();
    });

    it('is shown by its title when it has no detail', async () => {
      const fixture = await refusedWith(refusal(403, 'forbidden', 'Forbidden'));

      expect(el(fixture, 'move-error')?.textContent).toBe('Forbidden');
    });

    it('can be sent again, and then shows no old message', async () => {
      const fixture = await refusedWith(refusal(403, 'forbidden', 'Forbidden'));
      expect(sendButton(fixture)?.disabled).toBe(false);

      submit(fixture);
      await settle(fixture);

      expect(transition).toHaveBeenCalledTimes(2);
      expect(dialogTitle(fixture)).toBeUndefined();
    });

    it('shows its button as loading until the answer is in', async () => {
      let finish: (ticket: Ticket) => void = () => undefined;
      transition.mockReturnValue(
        new Promise<Ticket>((resolve) => {
          finish = resolve;
        }),
      );
      const fixture = await render(ticket('filed'));
      pick(fixture, 'Drop');
      await settle(fixture);
      typeInto(fixture, 'move-text', 'Not wanted');

      submit(fixture);
      await settle(fixture);
      expect(sendButton(fixture)?.disabled).toBe(true);
      expect(sendButton(fixture)?.querySelector('i.pi-spinner')).not.toBeNull();

      finish({} as Ticket);
      await settle(fixture);
      expect(dialogTitle(fixture)).toBeUndefined();
    });
  });
});
