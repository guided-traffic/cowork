import { HttpErrorResponse } from '@angular/common/http';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { By } from '@angular/platform-browser';
import { MessageService } from 'primeng/api';
import { Dialog } from 'primeng/dialog';
import type { MockInstance } from 'vitest';
import { Problem, Ticket } from '../../api/models';
import { ProblemView } from '../../core/problem.service';
import { StaleWrite, TicketActions } from '../../core/ticket-actions.service';
import { Move } from '../../shared/transitions';
import { MoveDialog, MoveRequest } from './move-dialog';

/** The part of a ticket the dialog reads. */
function ticket(fields: Partial<Ticket> = {}): Ticket {
  return {
    key: 'acme/COW-12',
    number: 12,
    state: 'in-progress',
    open_prerequisites: 0,
    progress_derived: false,
    progress_refinement: 100,
    progress: 60,
    progress_review: 0,
    done_by_hand: false,
    done_from: null,
    ...fields,
  } as Ticket;
}

const move = (fields: Partial<Move>): Move => ({
  to: 'dropped',
  kind: 'drop',
  label: 'Drop',
  input: 'reason',
  ...fields,
});
const drop: MoveRequest = { kind: 'move', move: move({}) };
const doneByHand: MoveRequest = {
  kind: 'move',
  move: move({ to: 'done', kind: 'done', label: 'Done by hand', input: 'note' }),
};
const block: MoveRequest = {
  kind: 'move',
  move: move({ to: 'blocked', kind: 'block', label: 'Block', input: 'block' }),
};

function refusal(status: number, code: Problem['code'], title: string, detail?: string) {
  const body: Problem = { type: 'about:blank', title, status, code, ...(detail ? { detail } : {}) };
  return new HttpErrorResponse({ status, statusText: title, error: body });
}

describe('MoveDialog', () => {
  let transition: MockInstance<TicketActions['transition']>;
  let update: MockInstance<TicketActions['update']>;
  let closings: boolean[];

  beforeEach(() => {
    transition = vi.fn<TicketActions['transition']>().mockResolvedValue({} as Ticket);
    update = vi.fn<TicketActions['update']>().mockResolvedValue({} as Ticket);
    closings = [];
    TestBed.configureTestingModule({
      providers: [MessageService, { provide: TicketActions, useValue: { transition, update } }],
    });
  });

  async function render(request: MoveRequest | null, current: Ticket = ticket()) {
    const fixture = TestBed.createComponent(MoveDialog);
    fixture.componentRef.setInput('ticket', current);
    fixture.componentRef.setInput('request', request);
    fixture.componentInstance.closed.subscribe((written) => {
      closings.push(written);
      // The parent closes the dialog when it ends.
      fixture.componentRef.setInput('request', null);
    });
    await settle(fixture);
    return fixture;
  }

  /** Lets what a click started finish, and shows it; fields of the dialog register a moment later. */
  async function settle(fixture: ComponentFixture<MoveDialog>) {
    await new Promise((resolve) => setTimeout(resolve));
    fixture.detectChanges();
    await fixture.whenStable();
  }

  const host = (fixture: ComponentFixture<MoveDialog>) => fixture.nativeElement as HTMLElement;

  const el = (fixture: ComponentFixture<MoveDialog>, testId: string) =>
    host(fixture).querySelector<HTMLElement>(`[data-testid="${testId}"]`);

  function typeInto(fixture: ComponentFixture<MoveDialog>, testId: string, value: string) {
    const field = el(fixture, testId) as HTMLInputElement | HTMLTextAreaElement;
    field.value = value;
    field.dispatchEvent(new Event('input'));
    fixture.detectChanges();
  }

  async function choose(fixture: ComponentFixture<MoveDialog>, testId: string, value: unknown) {
    fixture.debugElement
      .query(By.css(`[data-testid="${testId}"]`))
      .triggerEventHandler('ngModelChange', value);
    await settle(fixture);
  }

  async function submit(fixture: ComponentFixture<MoveDialog>) {
    host(fixture)
      .querySelector('form')
      ?.dispatchEvent(new Event('submit', { cancelable: true }));
    await settle(fixture);
  }

  const send = (fixture: ComponentFixture<MoveDialog>) =>
    el(fixture, 'move-send') as HTMLButtonElement | null;

  const title = (fixture: ComponentFixture<MoveDialog>) =>
    host(fixture).querySelector('.p-dialog-title')?.textContent;

  const fieldLabel = (fixture: ComponentFixture<MoveDialog>) =>
    el(fixture, 'move-text')?.closest('label')?.querySelector('span')?.textContent;

  const tick = async (fixture: ComponentFixture<MoveDialog>, value: boolean) => {
    fixture.debugElement.query(By.css('p-checkbox')).triggerEventHandler('ngModelChange', value);
    await settle(fixture);
  };

  it('is closed without a request', async () => {
    const fixture = await render(null);

    expect(host(fixture).querySelector('.p-dialog')).toBeNull();
  });

  describe('a move that asks for a reason', () => {
    it('is titled with the move and asks why', async () => {
      const fixture = await render(drop);

      expect(title(fixture)).toBe('Drop');
      expect(fieldLabel(fixture)).toBe('Reason');
      expect((el(fixture, 'move-text') as HTMLTextAreaElement).placeholder).toBe('Why');
      expect(el(fixture, 'move-explanation')).toBeNull();
      expect(send(fixture)?.textContent?.trim()).toBe('Drop');
    });

    it('cannot be sent without a reason, nor with spaces only', async () => {
      const fixture = await render(drop);
      expect(send(fixture)?.disabled).toBe(true);

      typeInto(fixture, 'move-text', '   ');
      expect(send(fixture)?.disabled).toBe(true);

      await submit(fixture);
      expect(transition).not.toHaveBeenCalled();
    });

    it('sends the reason, without its surrounding spaces, and ends as written', async () => {
      const fixture = await render(drop);

      typeInto(fixture, 'move-text', '  Not wanted any more  ');
      expect(send(fixture)?.disabled).toBe(false);
      await submit(fixture);

      expect(transition).toHaveBeenCalledExactlyOnceWith('acme/COW-12', {
        to: 'dropped',
        reason: 'Not wanted any more',
      });
      expect(closings).toEqual([true]);
      expect(host(fixture).querySelector('.p-dialog')).toBeNull();
    });

    it('ends unwritten on Cancel and sends nothing', async () => {
      const fixture = await render(drop);
      typeInto(fixture, 'move-text', 'half a thought');

      el(fixture, 'move-cancel')?.click();
      await settle(fixture);

      expect(closings).toEqual([false]);
      expect(transition).not.toHaveBeenCalled();
    });

    it('ends unwritten when the dialog asks to be closed, but not when it asks to stay open', async () => {
      const fixture = await render(drop);
      const dialog = fixture.debugElement.query(By.css('p-dialog'));

      dialog.triggerEventHandler('visibleChange', true);
      expect(closings).toEqual([]);

      dialog.triggerEventHandler('visibleChange', false);
      expect(closings).toEqual([false]);
    });

    it('starts empty for the next request', async () => {
      const fixture = await render(drop);
      typeInto(fixture, 'move-text', 'half a thought');

      fixture.componentRef.setInput('request', { ...drop });
      await settle(fixture);

      expect((el(fixture, 'move-text') as HTMLTextAreaElement).value).toBe('');
    });
  });

  describe('done by hand (docs/adr/0009 D5)', () => {
    it('asks how it was verified, and says the stages stay as they are', async () => {
      const fixture = await render(doneByHand);

      expect(title(fixture)).toBe('Done by hand: how was it verified?');
      expect(el(fixture, 'move-explanation')?.textContent).toBe(
        'The progress stages stay as they are.',
      );
      expect(fieldLabel(fixture)).toBe('Verification note');
      expect((el(fixture, 'move-text') as HTMLTextAreaElement).placeholder).toBe(
        'What was run, against what, with what result',
      );
    });

    it('sends the note', async () => {
      const fixture = await render(doneByHand);

      typeInto(fixture, 'move-text', 'make test: 812 passed');
      await submit(fixture);

      expect(transition).toHaveBeenCalledExactlyOnceWith('acme/COW-12', {
        to: 'done',
        note: 'make test: 812 passed',
      });
    });
  });

  describe('the withdrawal of a done by hand', () => {
    const withdraw: MoveRequest = {
      kind: 'move',
      move: move({ to: 'review', kind: 'withdraw', label: 'Withdraw done', input: 'reason' }),
    };

    it('says the ticket goes back to the state it was done from, and sends the reason', async () => {
      const fixture = await render(
        withdraw,
        ticket({ state: 'done', done_by_hand: true, done_from: 'review' }),
      );

      expect(title(fixture)).toBe('Withdraw done');
      expect(el(fixture, 'move-explanation')?.textContent).toBe('It goes back to review.');
      typeInto(fixture, 'move-text', 'Closed too early');
      await submit(fixture);

      expect(transition).toHaveBeenCalledExactlyOnceWith('acme/COW-12', {
        to: 'review',
        reason: 'Closed too early',
      });
    });

    it('says a ticket whose three stages are full stays done, by its stages', async () => {
      const fixture = await render(
        withdraw,
        ticket({ state: 'done', done_by_hand: true, progress: 100, progress_review: 100 }),
      );

      expect(el(fixture, 'move-explanation')?.textContent).toBe(
        'Its three stages are full: it stays done, by its stages.',
      );
    });
  });

  describe('a block', () => {
    it('asks what it waits on, decision first, and why', async () => {
      const fixture = await render(block);

      expect(title(fixture)).toBe('Block');
      expect(el(fixture, 'block-kind')?.querySelector('.p-select-label')?.textContent?.trim()).toBe(
        'decision',
      );
      expect(el(fixture, 'block-ticket')).toBeNull();
      expect(fieldLabel(fixture)).toBe('Reason');
    });

    it('offers the six kinds of block', async () => {
      const fixture = await render(block);

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

    it('sends the kind and the reason', async () => {
      const fixture = await render(block);
      await choose(fixture, 'block-kind', 'human');
      typeInto(fixture, 'move-text', 'Waiting for the owner');

      await submit(fixture);

      expect(transition).toHaveBeenCalledExactlyOnceWith('acme/COW-12', {
        to: 'blocked',
        reason: 'Waiting for the owner',
        block: { kind: 'human' },
      });
    });

    it('names the ticket it waits on when that is the kind, and needs it', async () => {
      const fixture = await render(block);
      await choose(fixture, 'block-kind', 'ticket');
      typeInto(fixture, 'move-text', 'The board comes first');
      expect(send(fixture)?.disabled).toBe(true);

      typeInto(fixture, 'block-ticket', '  COW-3 ');
      expect(send(fixture)?.disabled).toBe(false);
      await submit(fixture);

      expect(transition).toHaveBeenCalledExactlyOnceWith('acme/COW-12', {
        to: 'blocked',
        reason: 'The board comes first',
        block: { kind: 'ticket', ticket: 'COW-3' },
      });
    });

    it('does not send the ticket of an earlier choice with another kind', async () => {
      const fixture = await render(block);
      await choose(fixture, 'block-kind', 'ticket');
      typeInto(fixture, 'block-ticket', 'COW-3');
      await choose(fixture, 'block-kind', 'release');
      typeInto(fixture, 'move-text', 'Waits for the release');

      await submit(fixture);

      expect(transition).toHaveBeenCalledExactlyOnceWith('acme/COW-12', {
        to: 'blocked',
        reason: 'Waits for the release',
        block: { kind: 'release' },
      });
    });

    it('starts again with decision and no ticket for the next request', async () => {
      const fixture = await render(block);
      await choose(fixture, 'block-kind', 'ticket');

      fixture.componentRef.setInput('request', { ...block });
      await settle(fixture);

      expect(el(fixture, 'block-ticket')).toBeNull();
    });
  });

  describe('the write of the stages that fills the last of them (docs/adr/0009 D5)', () => {
    const complete: MoveRequest = { kind: 'complete', patch: { progress_review: 100 } };

    it('is the done act: it asks how it was verified, and says what it does', async () => {
      const fixture = await render(complete);

      expect(title(fixture)).toBe('Done: how was it verified?');
      expect(el(fixture, 'move-explanation')?.textContent).toBe(
        'Review to 100% fills the last progress stage: the ticket is done.',
      );
      expect(fieldLabel(fixture)).toBe('Verification note');
      expect(send(fixture)?.textContent?.trim()).toBe('Done');
    });

    it('names every stage of the write', async () => {
      const fixture = await render({
        kind: 'complete',
        patch: { progress_refinement: 100, progress: 100 },
      });

      expect(el(fixture, 'move-explanation')?.textContent).toBe(
        'Refinement to 100%, Implementation to 100% fills the last progress stage: the ticket is done.',
      );
    });

    it('sends the stages with the note, as one patch', async () => {
      const fixture = await render(complete);

      typeInto(fixture, 'move-text', ' Tried it on the board ');
      await submit(fixture);

      expect(update).toHaveBeenCalledExactlyOnceWith('acme/COW-12', {
        progress_review: 100,
        note: 'Tried it on the board',
      });
      expect(transition).not.toHaveBeenCalled();
      expect(closings).toEqual([true]);
    });
  });

  describe('the write that lowers a stage of a ticket done by its stages', () => {
    const reopen: MoveRequest = { kind: 'reopen', patch: { progress_review: 75 } };
    const doneByStages = ticket({
      state: 'done',
      done_from: 'review',
      progress: 100,
      progress_review: 100,
    });

    it('reopens it: it asks why, and says where it goes', async () => {
      const fixture = await render(reopen, doneByStages);

      expect(title(fixture)).toBe('Reopen COW-12: why?');
      expect(el(fixture, 'move-explanation')?.textContent).toBe(
        'Review to 75%: the ticket was done by its stages and goes back to review.',
      );
      expect(fieldLabel(fixture)).toBe('Reason');
      expect(send(fixture)?.textContent?.trim()).toBe('Reopen');
    });

    it('sends the stage with the reason', async () => {
      const fixture = await render(reopen, doneByStages);

      typeInto(fixture, 'move-text', 'The check missed a case');
      await submit(fixture);

      expect(update).toHaveBeenCalledExactlyOnceWith('acme/COW-12', {
        progress_review: 75,
        reason: 'The check missed a case',
      });
    });

    it('says only that it goes back where it came from when the ticket does not say which state', async () => {
      const fixture = await render(reopen, { ...doneByStages, done_from: null });

      expect(el(fixture, 'move-explanation')?.textContent).toBe(
        'Review to 75%: the ticket was done by its stages and goes back to the state it was done from.',
      );
    });

    it('offers no override of prerequisites: only done is refused by them', async () => {
      const fixture = await render(reopen, { ...doneByStages, open_prerequisites: 2 });

      expect(host(fixture).querySelector('p-checkbox')).toBeNull();
    });
  });

  describe('open prerequisites (docs/adr/0012 D7)', () => {
    it('offers the override at once where the ticket counts open prerequisites', async () => {
      const fixture = await render(doneByHand, ticket({ open_prerequisites: 2 }));

      expect(host(fixture).querySelector('p-checkbox')).not.toBeNull();
      expect(el(fixture, 'override-label')?.textContent).toBe(
        'Close it over its 2 open prerequisites',
      );
      expect(el(fixture, 'override-reason')).toBeNull();
    });

    it('counts one prerequisite in the singular', async () => {
      const fixture = await render(doneByHand, ticket({ open_prerequisites: 1 }));

      expect(el(fixture, 'override-label')?.textContent).toBe(
        'Close it over its 1 open prerequisite',
      );
    });

    it('offers no override where the ticket counts none, before the server refused', async () => {
      const fixture = await render(doneByHand);

      expect(host(fixture).querySelector('p-checkbox')).toBeNull();
    });

    it('offers no override for a move that does not close the ticket', async () => {
      const fixture = await render(drop, ticket({ open_prerequisites: 2 }));

      expect(host(fixture).querySelector('p-checkbox')).toBeNull();
    });

    it('asks why it may close before them once the person chooses to, and needs that reason', async () => {
      const fixture = await render(doneByHand, ticket({ open_prerequisites: 2 }));
      typeInto(fixture, 'move-text', 'Verified by hand');

      await tick(fixture, true);
      expect(el(fixture, 'override-reason')).not.toBeNull();
      expect(send(fixture)?.disabled).toBe(true);
      typeInto(fixture, 'override-reason', '   ');
      expect(send(fixture)?.disabled).toBe(true);
      typeInto(fixture, 'override-reason', 'The owner agreed');

      expect(send(fixture)?.disabled).toBe(false);
    });

    it('sends the override with its own reason next to the note of done by hand', async () => {
      const fixture = await render(doneByHand, ticket({ open_prerequisites: 2 }));
      typeInto(fixture, 'move-text', 'Verified by hand');
      await tick(fixture, true);
      typeInto(fixture, 'override-reason', '  The owner agreed ');

      await submit(fixture);

      expect(transition).toHaveBeenCalledExactlyOnceWith('acme/COW-12', {
        to: 'done',
        note: 'Verified by hand',
        override_prerequisites: true,
        reason: 'The owner agreed',
      });
    });

    it('sends the override with the stages and the note of the done act', async () => {
      const fixture = await render(
        { kind: 'complete', patch: { progress: 100 } },
        ticket({ open_prerequisites: 1, progress_review: 100 }),
      );
      typeInto(fixture, 'move-text', 'All green');
      await tick(fixture, true);
      typeInto(fixture, 'override-reason', 'It does not need COW-3');

      await submit(fixture);

      expect(update).toHaveBeenCalledExactlyOnceWith('acme/COW-12', {
        progress: 100,
        note: 'All green',
        override_prerequisites: true,
        reason: 'It does not need COW-3',
      });
    });

    it('drops the override, and its reason, when the ticket has no open prerequisite any more', async () => {
      const fixture = await render(doneByHand, ticket({ open_prerequisites: 1 }));
      typeInto(fixture, 'move-text', 'Verified by hand');
      await tick(fixture, true);

      fixture.componentRef.setInput('ticket', ticket({ open_prerequisites: 0, version: 2 }));
      await settle(fixture);
      expect(host(fixture).querySelector('p-checkbox')).toBeNull();
      expect(send(fixture)?.disabled).toBe(false);
      await submit(fixture);

      expect(transition).toHaveBeenCalledExactlyOnceWith('acme/COW-12', {
        to: 'done',
        note: 'Verified by hand',
      });
    });

    it('sends no override when the person changes their mind', async () => {
      const fixture = await render(doneByHand, ticket({ open_prerequisites: 2 }));
      typeInto(fixture, 'move-text', 'Verified by hand');
      await tick(fixture, true);
      typeInto(fixture, 'override-reason', 'The owner agreed');
      await tick(fixture, false);

      await submit(fixture);

      expect(transition).toHaveBeenCalledExactlyOnceWith('acme/COW-12', {
        to: 'done',
        note: 'Verified by hand',
      });
    });

    describe('that the server knows of and the ticket did not show', () => {
      async function refused(request: MoveRequest = doneByHand) {
        const fixture = await render(request);
        (request.kind === 'move' ? transition : update).mockRejectedValueOnce(
          refusal(409, 'open_prerequisites', 'Open prerequisites', 'COW-3 is not done yet.'),
        );
        typeInto(fixture, 'move-text', 'Verified by hand');
        await submit(fixture);
        return fixture;
      }

      it('keeps the dialog open, says which prerequisite is open, and offers the override', async () => {
        const fixture = await refused();

        expect(closings).toEqual([]);
        expect(el(fixture, 'move-error')?.textContent).toBe('COW-3 is not done yet.');
        expect(el(fixture, 'override-label')?.textContent).toBe(
          'Close it over its open prerequisites',
        );
        expect(send(fixture)?.disabled).toBe(false);
      });

      it('offers it for the done act of the stages too', async () => {
        const fixture = await refused({ kind: 'complete', patch: { progress: 100 } });

        expect(host(fixture).querySelector('p-checkbox')).not.toBeNull();
      });

      it('starts without the override and the message for the next request', async () => {
        const fixture = await refused();
        await tick(fixture, true);

        fixture.componentRef.setInput('request', { ...doneByHand });
        await settle(fixture);

        expect(el(fixture, 'move-error')).toBeNull();
        expect(host(fixture).querySelector('p-checkbox')).toBeNull();
        expect(el(fixture, 'override-reason')).toBeNull();
      });
    });
  });

  describe('another refusal', () => {
    async function refusedWith(error: unknown, request: MoveRequest = drop) {
      (request.kind === 'move' ? transition : update).mockRejectedValueOnce(error);
      const fixture = await render(request);
      typeInto(fixture, 'move-text', 'Not wanted');
      await submit(fixture);
      return fixture;
    }

    it('is shown in the dialog, which stays open with what was typed, without a toast', async () => {
      const add = vi.spyOn(TestBed.inject(MessageService), 'add');

      const fixture = await refusedWith(
        refusal(409, 'state_conflict', 'The ticket moved meanwhile', 'It is analysed now.'),
      );

      expect(el(fixture, 'move-error')?.textContent).toBe('It is analysed now.');
      expect(el(fixture, 'move-error')?.getAttribute('role')).toBe('alert');
      expect(title(fixture)).toBe('Drop');
      expect((el(fixture, 'move-text') as HTMLTextAreaElement).value).toBe('Not wanted');
      expect(host(fixture).querySelector('p-checkbox')).toBeNull();
      expect(add).not.toHaveBeenCalled();
      expect(closings).toEqual([]);
    });

    it('is shown by its title when it has no detail', async () => {
      const fixture = await refusedWith(refusal(403, 'forbidden', 'Forbidden'));

      expect(el(fixture, 'move-error')?.textContent).toBe('Forbidden');
    });

    it('can be sent again, and then ends as written', async () => {
      const fixture = await refusedWith(refusal(403, 'forbidden', 'Forbidden'));

      await submit(fixture);

      expect(transition).toHaveBeenCalledTimes(2);
      expect(closings).toEqual([true]);
    });

    it('says so when the ticket changed meanwhile under a write of the stages', async () => {
      const problem: ProblemView = {
        status: 412,
        code: 'version_mismatch',
        title: 'Changed meanwhile',
        detail: '',
        fields: {},
        current: {},
      };
      const fixture = await refusedWith(new StaleWrite(problem, ticket()), {
        kind: 'reopen',
        patch: { progress: 50 },
      });

      expect(el(fixture, 'move-error')?.textContent).toBe(
        'COW-12 was changed by someone else meanwhile. Check what it shows now, and send again to write yours over it.',
      );
    });
  });

  describe('while its request runs', () => {
    let finish: (ticket: Ticket) => void;

    async function sending() {
      transition.mockReturnValue(
        new Promise<Ticket>((resolve) => {
          finish = resolve;
        }),
      );
      const fixture = await render(drop);
      typeInto(fixture, 'move-text', 'Not wanted');
      await submit(fixture);
      return fixture;
    }

    it('shows its button as loading, and Cancel and the cross cannot close it', async () => {
      const fixture = await sending();

      expect(send(fixture)?.disabled).toBe(true);
      expect(send(fixture)?.querySelector('i.pi-spinner')).not.toBeNull();
      expect((el(fixture, 'move-cancel') as HTMLButtonElement).disabled).toBe(true);
      expect(
        (fixture.debugElement.query(By.directive(Dialog)).componentInstance as Dialog).closable(),
      ).toBe(false);

      fixture.debugElement.query(By.css('p-dialog')).triggerEventHandler('visibleChange', false);
      expect(closings).toEqual([]);

      finish({} as Ticket);
      await settle(fixture);
      expect(closings).toEqual([true]);
    });

    it('is not sent twice', async () => {
      const fixture = await sending();

      await submit(fixture);

      expect(transition).toHaveBeenCalledOnce();
      finish({} as Ticket);
      await settle(fixture);
    });

    it('keeps Escape from closing it', async () => {
      const fixture = await sending();
      const escape = new KeyboardEvent('keydown', { key: 'Escape', bubbles: true });
      const stop = vi.spyOn(escape, 'stopPropagation');

      document.body.dispatchEvent(escape);

      expect(stop).toHaveBeenCalled();
      finish({} as Ticket);
      await settle(fixture);
    });
  });
});
