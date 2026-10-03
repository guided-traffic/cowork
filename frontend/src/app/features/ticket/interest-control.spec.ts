import { HttpErrorResponse } from '@angular/common/http';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { By } from '@angular/platform-browser';
import { MessageService } from 'primeng/api';
import { SelectButton } from 'primeng/selectbutton';
import type { MockInstance } from 'vitest';
import { Interest, InterestWeight, Problem } from '../../api/models';
import { Conversation } from '../../core/conversation.service';
import { InterestControl } from './interest-control';

const ada = { id: 'p1', display_name: 'Ada Lovelace', username: 'local:ada' };
const sam = { id: 'p2', display_name: 'Sam Rivera', username: 'local:sam' };

const key = 'acme/COW-12';

function interest(person: Interest['person'], weight: InterestWeight, note = ''): Interest {
  return {
    person,
    weight,
    note,
    settled: false,
    since: '2026-10-02T09:00:00Z',
    updated_at: '2026-10-02T09:00:00Z',
  };
}

function refusal(status: number, title: string, detail: string) {
  const body: Problem = { type: 'about:blank', title, status, detail, code: 'validation_failed' };
  return new HttpErrorResponse({ status, statusText: title, error: body });
}

describe('InterestControl', () => {
  let setInterest: MockInstance<Conversation['setInterest']>;
  let removeInterest: MockInstance<Conversation['removeInterest']>;

  beforeEach(() => {
    setInterest = vi.fn<Conversation['setInterest']>().mockResolvedValue(undefined);
    removeInterest = vi.fn<Conversation['removeInterest']>().mockResolvedValue(undefined);
    TestBed.configureTestingModule({
      providers: [
        MessageService,
        { provide: Conversation, useValue: { setInterest, removeInterest } },
      ],
    });
  });

  async function render(interests: Interest[] = [], me: string | undefined = 'p1') {
    const fixture = TestBed.createComponent(InterestControl);
    fixture.componentRef.setInput('ticketKey', key);
    fixture.componentRef.setInput('interests', interests);
    fixture.componentRef.setInput('me', me);
    await settle(fixture);
    return fixture;
  }

  /** Lets what a click started finish, and shows it; fields that appear register a moment later. */
  async function settle(fixture: ComponentFixture<InterestControl>) {
    await new Promise((resolve) => setTimeout(resolve));
    fixture.detectChanges();
    await fixture.whenStable();
  }

  const host = (fixture: ComponentFixture<InterestControl>) => fixture.nativeElement as HTMLElement;

  const el = (fixture: ComponentFixture<InterestControl>, testId: string) =>
    host(fixture).querySelector<HTMLElement>(`[data-testid="${testId}"]`);

  /** A button of PrimeNG's directive carries its test id itself. */
  const button = (fixture: ComponentFixture<InterestControl>, testId: string) =>
    el(fixture, testId) as HTMLButtonElement | null;

  /** What clicking a weight of the select button tells its model. */
  async function choose(fixture: ComponentFixture<InterestControl>, weight: InterestWeight | null) {
    fixture.debugElement
      .query(By.css('[data-testid="interest-weight"]'))
      .triggerEventHandler('ngModelChange', weight);
    await settle(fixture);
  }

  const chosen = (fixture: ComponentFixture<InterestControl>) =>
    el(fixture, 'interest-weight')?.querySelector('.p-togglebutton-checked')?.textContent?.trim();

  function typeNote(fixture: ComponentFixture<InterestControl>, value: string) {
    const note = el(fixture, 'interest-note') as HTMLTextAreaElement;
    note.value = value;
    note.dispatchEvent(new Event('input'));
    fixture.detectChanges();
  }

  const submit = (fixture: ComponentFixture<InterestControl>) =>
    host(fixture)
      .querySelector('form')
      ?.dispatchEvent(new Event('submit', { cancelable: true }));

  const saveButton = (fixture: ComponentFixture<InterestControl>) =>
    [...host(fixture).querySelectorAll('form button')].find(
      (candidate) => candidate.textContent?.trim() === 'Save',
    ) as HTMLButtonElement | undefined;

  const holders = (fixture: ComponentFixture<InterestControl>) =>
    [...host(fixture).querySelectorAll('.holders li')].map((item) => ({
      who: item.querySelector('.who')?.textContent,
      weight: item.querySelector('.weight')?.getAttribute('data-weight'),
      note: item.querySelector('.note')?.textContent,
    }));

  describe('who holds a stake', () => {
    it('lists each person with their weight and their reason, if they gave one', async () => {
      const fixture = await render([
        interest(ada, 'urgent', 'The release waits for it'),
        interest(sam, 'watch'),
      ]);

      expect(holders(fixture)).toEqual([
        { who: 'Ada Lovelace', weight: 'urgent', note: 'The release waits for it' },
        { who: 'Sam Rivera', weight: 'watch', note: undefined },
      ]);
    });

    it('says that nobody does when nobody does', async () => {
      const fixture = await render([]);

      expect(host(fixture).querySelector('.holders')?.textContent?.trim()).toBe(
        'Nobody holds a stake yet.',
      );
    });

    it('follows the list when it changes', async () => {
      const fixture = await render([interest(sam, 'watch')]);

      fixture.componentRef.setInput('interests', [interest(sam, 'need', 'Blocks me')]);
      await settle(fixture);

      expect(holders(fixture)).toEqual([{ who: 'Sam Rivera', weight: 'need', note: 'Blocks me' }]);
    });
  });

  describe('the stake of the person', () => {
    it('offers the three weights, watch, need and urgent', async () => {
      const fixture = await render();

      const weights = fixture.debugElement.query(By.directive(SelectButton))
        .componentInstance as SelectButton;
      expect((weights.options() as { value: string }[]).map((option) => option.value)).toEqual([
        'watch',
        'need',
        'urgent',
      ]);
    });

    it('shows the weight the person holds as the chosen one', async () => {
      const fixture = await render([interest(sam, 'watch'), interest(ada, 'need', 'Blocks me')]);

      expect(chosen(fixture)).toBe('need');
    });

    it('shows none as chosen when the person holds no stake, or is not known', async () => {
      const fixture = await render([interest(sam, 'watch')]);
      expect(chosen(fixture)).toBeUndefined();

      fixture.componentRef.setInput('me', undefined);
      await settle(fixture);
      expect(chosen(fixture)).toBeUndefined();
      expect(el(fixture, 'interest-remove')).toBeNull();
    });

    it('offers to remove the stake only when the person holds one', async () => {
      const fixture = await render([interest(sam, 'watch')]);
      expect(el(fixture, 'interest-remove')).toBeNull();

      fixture.componentRef.setInput('interests', [interest(sam, 'watch'), interest(ada, 'watch')]);
      await settle(fixture);

      expect(el(fixture, 'interest-remove')).not.toBeNull();
    });
  });

  describe('choosing watch', () => {
    it('is written at once, without a reason', async () => {
      const fixture = await render();

      await choose(fixture, 'watch');

      expect(setInterest).toHaveBeenCalledExactlyOnceWith(key, 'watch', '');
      expect(el(fixture, 'interest-note')).toBeNull();
    });

    it('drops the reason that was waiting for need or urgent', async () => {
      const fixture = await render();
      await choose(fixture, 'need');
      expect(el(fixture, 'interest-note')).not.toBeNull();

      await choose(fixture, 'watch');

      expect(el(fixture, 'interest-note')).toBeNull();
      expect(setInterest).toHaveBeenCalledExactlyOnceWith(key, 'watch', '');
    });

    it('is written without the reason that was typed for need or urgent', async () => {
      const fixture = await render([interest(ada, 'urgent', 'The release waits for it')]);
      await choose(fixture, 'need');
      typeNote(fixture, 'Blocks me');

      await choose(fixture, 'watch');

      expect(setInterest).toHaveBeenCalledExactlyOnceWith(key, 'watch', '');
    });

    it('toasts the problem when it is refused', async () => {
      setInterest.mockRejectedValue(refusal(403, 'Forbidden', 'A viewer cannot hold a stake.'));
      const add = vi.spyOn(TestBed.inject(MessageService), 'add');
      const fixture = await render();

      await choose(fixture, 'watch');

      expect(add).toHaveBeenCalledExactlyOnceWith(
        expect.objectContaining({ detail: 'A viewer cannot hold a stake.' }),
      );
    });
  });

  describe('choosing need or urgent', () => {
    it.each(['need', 'urgent'] as const)(
      'waits for the reason when the weight is %s, and shows the weight as chosen meanwhile',
      async (weight) => {
        const fixture = await render();

        await choose(fixture, weight);

        expect(setInterest).not.toHaveBeenCalled();
        expect(el(fixture, 'interest-note')).not.toBeNull();
        expect(chosen(fixture)).toBe(weight);
        expect(saveButton(fixture)?.disabled).toBe(true);
      },
    );

    it('starts the reason from the one the person gave before, and from nothing otherwise', async () => {
      const fixture = await render([interest(ada, 'need', 'Blocks me')]);

      await choose(fixture, 'urgent');
      expect((el(fixture, 'interest-note') as HTMLTextAreaElement).value).toBe('Blocks me');

      const other = await render([], 'p9');
      await choose(other, 'need');
      expect((el(other, 'interest-note') as HTMLTextAreaElement).value).toBe('');
    });

    it.each(['need', 'urgent'] as const)(
      'writes the weight %s with the reason when it is saved, and closes the form',
      async (weight) => {
        const fixture = await render();
        await choose(fixture, weight);
        typeNote(fixture, '  The release waits for it  ');
        expect(saveButton(fixture)?.disabled).toBe(false);

        submit(fixture);
        await settle(fixture);

        expect(setInterest).toHaveBeenCalledExactlyOnceWith(
          key,
          weight,
          '  The release waits for it  ',
        );
        expect(el(fixture, 'interest-note')).toBeNull();
      },
    );

    it('cannot be saved without a reason, or with spaces only', async () => {
      const fixture = await render();
      await choose(fixture, 'need');

      typeNote(fixture, '   ');
      expect(saveButton(fixture)?.disabled).toBe(true);
      typeNote(fixture, 'Blocks me');
      expect(saveButton(fixture)?.disabled).toBe(false);
    });

    it('keeps the form open, with the reason, and toasts the problem when it is refused', async () => {
      setInterest.mockRejectedValue(refusal(422, 'Validation failed', 'The reason is too long.'));
      const add = vi.spyOn(TestBed.inject(MessageService), 'add');
      const fixture = await render();
      await choose(fixture, 'need');
      typeNote(fixture, 'Blocks me');

      submit(fixture);
      await settle(fixture);

      expect(add).toHaveBeenCalledExactlyOnceWith(
        expect.objectContaining({ detail: 'The reason is too long.' }),
      );
      expect((el(fixture, 'interest-note') as HTMLTextAreaElement).value).toBe('Blocks me');
      expect(chosen(fixture)).toBe('need');
    });

    it('goes back to the weight the person holds when it is saved and the stake changed', async () => {
      const fixture = await render([interest(ada, 'watch')]);
      await choose(fixture, 'need');
      typeNote(fixture, 'Blocks me');
      submit(fixture);
      await settle(fixture);

      fixture.componentRef.setInput('interests', [interest(ada, 'need', 'Blocks me')]);
      await settle(fixture);

      expect(chosen(fixture)).toBe('need');
      expect(el(fixture, 'interest-note')).toBeNull();
    });
  });

  describe('clicking the weight that is chosen again', () => {
    it('does nothing, because the select button then offers no weight at all', async () => {
      const fixture = await render([interest(ada, 'watch')]);

      await choose(fixture, null);

      expect(setInterest).not.toHaveBeenCalled();
      expect(removeInterest).not.toHaveBeenCalled();
      expect(el(fixture, 'interest-note')).toBeNull();
    });

    it('keeps the reason that is being written for need or urgent', async () => {
      const fixture = await render();
      await choose(fixture, 'need');
      typeNote(fixture, 'Blocks me');

      await choose(fixture, null);

      expect((el(fixture, 'interest-note') as HTMLTextAreaElement).value).toBe('Blocks me');
      expect(chosen(fixture)).toBe('need');
      expect(setInterest).not.toHaveBeenCalled();
    });
  });

  describe('removing the stake', () => {
    it('removes it from the ticket', async () => {
      const fixture = await render([interest(ada, 'watch')]);

      button(fixture, 'interest-remove')?.click();
      await settle(fixture);

      expect(removeInterest).toHaveBeenCalledExactlyOnceWith(key);
    });

    it('drops the reason that was waiting', async () => {
      const fixture = await render([interest(ada, 'watch')]);
      await choose(fixture, 'urgent');
      expect(el(fixture, 'interest-note')).not.toBeNull();

      button(fixture, 'interest-remove')?.click();
      await settle(fixture);

      expect(el(fixture, 'interest-note')).toBeNull();
      expect(removeInterest).toHaveBeenCalledOnce();
    });

    it('toasts the problem when it is refused', async () => {
      removeInterest.mockRejectedValue(
        refusal(409, 'The ticket is done', 'The stake stays on record.'),
      );
      const add = vi.spyOn(TestBed.inject(MessageService), 'add');
      const fixture = await render([interest(ada, 'watch')]);

      button(fixture, 'interest-remove')?.click();
      await settle(fixture);

      expect(add).toHaveBeenCalledExactlyOnceWith(
        expect.objectContaining({ detail: 'The stake stays on record.' }),
      );
    });

    it('is not offered again while it is on its way', async () => {
      let finish: (value: unknown) => void = () => undefined;
      removeInterest.mockReturnValue(
        new Promise((resolve) => {
          finish = resolve;
        }),
      );
      const fixture = await render([interest(ada, 'watch')]);
      const remove = button(fixture, 'interest-remove');
      expect(remove?.disabled).toBe(false);

      remove?.click();
      await settle(fixture);
      expect(remove?.disabled).toBe(true);

      finish(undefined);
      await settle(fixture);
      expect(remove?.disabled).toBe(false);
    });
  });
});
