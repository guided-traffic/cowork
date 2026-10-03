import { HttpErrorResponse } from '@angular/common/http';
import { signal, WritableSignal } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { By } from '@angular/platform-browser';
import { ConfirmationService, MessageService } from 'primeng/api';
import { Select } from 'primeng/select';
import { SelectButton } from 'primeng/selectbutton';
import { Slider } from 'primeng/slider';
import { Tooltip } from 'primeng/tooltip';
import type { MockInstance } from 'vitest';
import { Member, Problem, Ticket } from '../../api/models';
import { MembersService } from '../../core/members.service';
import { ProblemView } from '../../core/problem.service';
import { StaleWrite, TicketActions } from '../../core/ticket-actions.service';
import { Stage, stages } from '../../shared/stages';
import { Clock, dateTime } from '../../shared/time';
import { MoveDialog } from './move-dialog';
import { shown, TicketFields } from './ticket-fields';

const now = Date.parse('2026-10-03T12:00:00Z');
const ada = { id: 'p1', display_name: 'Ada Lovelace', username: 'local:ada' };
const sam = { id: 'p2', display_name: 'Sam Rivera', username: 'local:sam' };

function ticket(overrides: Partial<Ticket> = {}): Ticket {
  return {
    id: 't-12',
    key: 'acme/COW-12',
    number: 12,
    project: 'COW',
    title: 'The board flickers',
    body: '',
    type: 'bug',
    state: 'in-progress',
    severity: 'high',
    security: 'none',
    confidential: false,
    assignee: sam,
    reporter: ada,
    block: null,
    threat: null,
    parent: null,
    effort: 'M',
    progress: 25,
    progress_derived: false,
    progress_refinement: 0,
    progress_review: 0,
    urgency: 'next',
    urgency_derived: 'next',
    urgency_override: null,
    urgency_rule: 'v1:default',
    opened_at: '2026-10-01T09:00:00Z',
    created_at: '2026-10-01T09:00:00Z',
    updated_at: '2026-10-03T11:55:00Z',
    decided_at: null,
    done_at: null,
    done_from: null,
    done_by_hand: false,
    open_prerequisites: 0,
    version: 3,
    ...overrides,
  };
}

/** The server refuses a write as invalid. */
const refused = () => {
  const body: Problem = {
    type: 'about:blank',
    title: 'Validation failed',
    status: 422,
    detail: 'The threat names no principal.',
    code: 'validation_failed',
  };
  return new HttpErrorResponse({ status: 422, statusText: body.title, error: body });
};

const stale = (current: Partial<Ticket> = {}) =>
  new StaleWrite(
    {
      status: 412,
      code: 'precondition_failed',
      title: 'The ticket changed',
      detail: 'The ticket changed since you read it.',
      fields: {},
      current: {},
    } satisfies ProblemView,
    ticket(current),
  );

describe('shown', () => {
  it.each([
    [null, 'nothing'],
    [undefined, 'nothing'],
    ['', 'nothing'],
    ['boundary', 'boundary'],
    [0, '0'],
    [40, '40'],
    [false, 'false'],
    [{ id: 'p1', display_name: 'Ada' }, '{"id":"p1","display_name":"Ada"}'],
    [['a', 'b'], '["a","b"]'],
  ])('shows %j as %j', (value, expected) => {
    expect(shown(value)).toBe(expected);
  });
});

describe('TicketFields', () => {
  let update: MockInstance<TicketActions['update']>;
  let confirm: MockInstance<ConfirmationService['confirm']>;
  let people: WritableSignal<Member[]>;

  beforeEach(() => {
    update = vi.fn<TicketActions['update']>().mockResolvedValue(ticket());
    confirm = vi.fn<ConfirmationService['confirm']>();
    people = signal<Member[]>([
      { role: 'admin', person: ada },
      { role: 'member', person: sam },
    ]);
    TestBed.configureTestingModule({
      providers: [
        MessageService,
        { provide: ConfirmationService, useValue: { confirm } },
        { provide: TicketActions, useValue: { update } },
        { provide: MembersService, useValue: { list: people } },
        { provide: Clock, useValue: { now: signal(now) } },
      ],
    });
  });

  async function render(current: Ticket = ticket()) {
    const fixture = TestBed.createComponent(TicketFields);
    fixture.componentRef.setInput('ticket', current);
    await settle(fixture);
    return fixture;
  }

  /** Lets what a change started finish, and shows it; fields that appear register a moment later. */
  async function settle(fixture: ComponentFixture<TicketFields>) {
    await new Promise((resolve) => setTimeout(resolve));
    fixture.detectChanges();
    await fixture.whenStable();
  }

  const host = (fixture: ComponentFixture<TicketFields>) => fixture.nativeElement as HTMLElement;

  const el = (fixture: ComponentFixture<TicketFields>, testId: string) =>
    host(fixture).querySelector<HTMLElement>(`[data-testid="${testId}"]`);

  /** What choosing a value in a PrimeNG field tells its model. */
  function change(fixture: ComponentFixture<TicketFields>, testId: string, value: unknown) {
    fixture.debugElement
      .query(By.css(`[data-testid="${testId}"]`))
      .triggerEventHandler('ngModelChange', value);
  }

  const label = (fixture: ComponentFixture<TicketFields>, testId: string) =>
    el(fixture, testId)?.querySelector('.p-select-label')?.textContent?.trim();

  /** The fields by name: `{ Reporter: 'Ada Lovelace', … }`, for what is plain text. */
  const fields = (fixture: ComponentFixture<TicketFields>) =>
    Object.fromEntries(
      [...host(fixture).querySelectorAll('dl.fields dt')].map((term) => [
        term.textContent,
        term.nextElementSibling?.textContent?.replace(/\s+/g, ' ').trim(),
      ]),
    );

  /** The value each stage shows: `{ refinement: '100%', … }`. */
  const stageShown = (fixture: ComponentFixture<TicketFields>) =>
    Object.fromEntries(
      stages.map((stage) => [stage, el(fixture, `stage-value-${stage}`)?.textContent]),
    ) as Record<Stage, string | undefined>;

  describe('what it shows', () => {
    it('shows the current value of each field it edits', async () => {
      const fixture = await render(ticket({ security: 'hardening', effort: 'L' }));

      expect(label(fixture, 'field-assignee')).toBe('Sam Rivera');
      expect(label(fixture, 'field-type')).toBe('bug');
      expect(label(fixture, 'field-severity')).toBe('high');
      expect(label(fixture, 'field-security')).toBe('hardening');
      expect(
        el(fixture, 'field-effort')?.querySelector('.p-togglebutton-checked')?.textContent,
      ).toBe('L');
    });

    it('shows that nobody is assigned', async () => {
      const fixture = await render(ticket({ assignee: null }));

      expect(label(fixture, 'field-assignee')).toBe('nobody');
    });

    it('offers the members of the tenant, by name, to assign', async () => {
      const fixture = await render();

      const assignee = fixture.debugElement.query(By.css('[data-testid="field-assignee"]'))
        .componentInstance as Select;
      expect(assignee.options()).toEqual([
        { id: 'p1', name: 'Ada Lovelace' },
        { id: 'p2', name: 'Sam Rivera' },
      ]);
    });

    it('offers the five types, the five severities, the four security classes and the four efforts', async () => {
      const fixture = await render();

      const options = (testId: string) =>
        (
          fixture.debugElement.query(By.css(`[data-testid="${testId}"]`)).componentInstance as
            Select | SelectButton
        ).options();
      expect(options('field-type')).toEqual(['task', 'bug', 'feature', 'decision', 'question']);
      expect(options('field-severity')).toEqual(['critical', 'high', 'medium', 'low', 'cosmetic']);
      expect(options('field-security')).toEqual(['none', 'hardening', 'boundary', 'live']);
      expect(options('field-effort')).toEqual(['XS', 'S', 'M', 'L']);
    });

    it('shows the reporter, when it was opened and when it was last updated', async () => {
      const fixture = await render();

      expect(fields(fixture)['Reporter']).toBe('Ada Lovelace');
      expect(fields(fixture)['Opened']).toBe('2 days ago');
      expect(fields(fixture)['Updated']).toBe('5 minutes ago');
    });

    it('gives the exact times in tooltips', async () => {
      const fixture = await render();

      const tooltipOf = (hostText: string) =>
        fixture.debugElement
          .queryAll(By.directive(Tooltip))
          .find((candidate) => candidate.nativeElement.textContent.trim() === hostText)
          ?.injector.get(Tooltip)
          .content();
      expect(tooltipOf('2 days ago')).toBe(dateTime('2026-10-01T09:00:00Z'));
      expect(tooltipOf('5 minutes ago')).toBe(dateTime('2026-10-03T11:55:00Z'));
    });

    it('shows the urgency, explained with the rule that derived it', async () => {
      const fixture = await render(ticket({ urgency: 'next', urgency_rule: 'v1:default' }));

      expect(fields(fixture)['Urgency']).toBe('next');
      const urgency = fixture.debugElement
        .queryAll(By.directive(Tooltip))
        .find((candidate) => candidate.nativeElement.textContent.trim() === 'next');
      expect(urgency?.injector.get(Tooltip).content()).toBe(
        'Medium or worse, and its trigger is live (rule v1:default)',
      );
    });

    it('marks an urgency that a person overrode', async () => {
      const fixture = await render(
        ticket({
          urgency: 'now',
          urgency_override: { value: 'now', reason: 'Today', at: '2026-10-03T09:00:00Z' },
        }),
      );

      expect(fields(fixture)['Urgency']).toBe('now overridden');
    });

    it('names the parent when there is one and shows none otherwise', async () => {
      const fixture = await render(ticket({ parent: 'acme/COW-1' }));
      expect(fields(fixture)['Parent']).toBe('acme/COW-1');

      fixture.componentRef.setInput('ticket', ticket());
      await settle(fixture);
      expect(fields(fixture)).not.toHaveProperty('Parent');
    });

    it('shows the three progress stages with their values, and names the one the ticket works on', async () => {
      const fixture = await render(
        ticket({ state: 'review', progress_refinement: 100, progress: 75, progress_review: 25 }),
      );

      expect(stageShown(fixture)).toEqual({
        refinement: '100%',
        implementation: '75%',
        review: '25%',
      });
      expect(
        [...host(fixture).querySelectorAll('.stage .stage-name')].map((name) => name.textContent),
      ).toEqual(['Refinement', 'Implementation', 'Review']);
      expect(el(fixture, 'stage-review')?.classList).toContain('current');
      expect(host(fixture).querySelectorAll('.stage.current')).toHaveLength(1);
    });

    it('names no stage for a decided ticket, which waits', async () => {
      const fixture = await render(ticket({ state: 'decided' }));

      expect(host(fixture).querySelectorAll('.stage.current')).toHaveLength(0);
    });

    it('gives each stage of a ticket without children a slider, named by its stage', async () => {
      const fixture = await render();

      const sliders = fixture.debugElement.queryAll(By.directive(Slider));
      expect(sliders.map((slider) => (slider.componentInstance as Slider).ariaLabel())).toEqual([
        'Refinement',
        'Implementation',
        'Review',
      ]);
      expect(host(fixture).querySelector('app-stage-bar')).toBeNull();
    });

    it('shows the stages of a parent as bars from its children, without a slider', async () => {
      const fixture = await render(
        ticket({
          progress_derived: true,
          progress_refinement: 50,
          progress: 25,
          progress_review: 0,
        }),
      );

      expect(fixture.debugElement.queryAll(By.directive(Slider))).toHaveLength(0);
      const bars = [...host(fixture).querySelectorAll('app-stage-bar')];
      expect(bars.map((bar) => bar.getAttribute('aria-label'))).toEqual([
        'Refinement 50%, from its children',
        'Implementation 25%, from its children',
        'Review 0%, from its children',
      ]);
      expect(el(fixture, 'stages-note')?.textContent).toBe('From its children');
    });

    it.each([
      ['in-progress', {}, false],
      ['done by hand', { state: 'done', done_by_hand: true }, false],
      ['done by its stages', { state: 'done', progress: 100, progress_review: 100 }, false],
      ['dropped', { state: 'dropped' }, true],
    ] as [string, Partial<Ticket>, boolean][])(
      'lets the person move the sliders of a ticket that is %s: disabled %s',
      async (_, overrides, disabled) => {
        const fixture = await render(ticket(overrides));

        for (const slider of fixture.debugElement.queryAll(By.directive(Slider))) {
          expect((slider.componentInstance as Slider).$disabled()).toBe(disabled);
        }
      },
    );

    it('says how a done ticket is done, and what its stages do then', async () => {
      const fixture = await render(ticket({ state: 'done', done_by_hand: true }));
      expect(el(fixture, 'stages-note')?.textContent).toBe(
        'Done by hand: the stages stay editable.',
      );

      fixture.componentRef.setInput(
        'ticket',
        ticket({ state: 'done', progress_refinement: 100, progress: 100, progress_review: 100 }),
      );
      await settle(fixture);
      expect(el(fixture, 'stages-note')?.textContent).toBe(
        'Done by its stages: lowering one reopens it.',
      );
    });

    it('says nothing about the stages of an open ticket', async () => {
      const fixture = await render();

      expect(el(fixture, 'stages-note')).toBeNull();
    });
  });

  describe('changing a field', () => {
    it('assigns the member that is chosen, with one patch', async () => {
      const fixture = await render(ticket({ assignee: null }));

      change(fixture, 'field-assignee', 'p1');
      await settle(fixture);

      expect(update).toHaveBeenCalledExactlyOnceWith('acme/COW-12', { assignee: 'p1' });
    });

    it('takes the assignee away when the choice is cleared', async () => {
      const fixture = await render();

      change(fixture, 'field-assignee', null);
      change(fixture, 'field-assignee', undefined);
      await settle(fixture);

      expect(update).toHaveBeenNthCalledWith(1, 'acme/COW-12', { assignee: null });
      expect(update).toHaveBeenNthCalledWith(2, 'acme/COW-12', { assignee: null });
    });

    it.each([
      ['field-type', 'feature', { type: 'feature' }],
      ['field-severity', 'critical', { severity: 'critical' }],
      ['field-effort', 'XS', { effort: 'XS' }],
    ])('writes the choice of %s as one patch', async (testId, value, patch) => {
      const fixture = await render();

      change(fixture, testId, value);
      await settle(fixture);

      expect(update).toHaveBeenCalledExactlyOnceWith('acme/COW-12', patch);
    });

    it('writes for the ticket that is shown now', async () => {
      const fixture = await render();
      fixture.componentRef.setInput('ticket', ticket({ key: 'acme/COW-13', number: 13 }));
      await settle(fixture);

      change(fixture, 'field-type', 'task');
      await settle(fixture);

      expect(update).toHaveBeenCalledExactlyOnceWith('acme/COW-13', { type: 'task' });
    });
  });

  describe('the security class', () => {
    it('is written at once, with the threat taken away, when it is none', async () => {
      const fixture = await render(ticket({ security: 'boundary', threat: 'Someone could' }));

      change(fixture, 'field-security', 'none');
      await settle(fixture);

      expect(update).toHaveBeenCalledExactlyOnceWith('acme/COW-12', {
        security: 'none',
        threat: null,
      });
      expect(el(fixture, 'field-threat')).toBeNull();
    });

    it('waits for its threat when it is anything else', async () => {
      const fixture = await render();

      change(fixture, 'field-security', 'boundary');
      await settle(fixture);

      expect(update).not.toHaveBeenCalled();
      expect(el(fixture, 'field-threat')).not.toBeNull();
      expect(label(fixture, 'field-security')).toBe('boundary');
    });

    it('starts the threat from the one the ticket has', async () => {
      const fixture = await render(ticket({ security: 'hardening', threat: 'Someone could' }));

      change(fixture, 'field-security', 'live');
      await settle(fixture);

      expect((el(fixture, 'field-threat') as HTMLTextAreaElement).value).toBe('Someone could');
    });

    it('starts the threat empty when the ticket has none', async () => {
      const fixture = await render();

      change(fixture, 'field-security', 'live');
      await settle(fixture);

      expect((el(fixture, 'field-threat') as HTMLTextAreaElement).value).toBe('');
    });

    it('writes the class with the threat, trimmed, when it is saved', async () => {
      const fixture = await render();
      change(fixture, 'field-security', 'boundary');
      await settle(fixture);
      const threat = el(fixture, 'field-threat') as HTMLTextAreaElement;
      const save = el(fixture, 'field-threat-save') as HTMLButtonElement;
      expect(save.disabled).toBe(true);

      threat.value = '  A member could read another tenant  ';
      threat.dispatchEvent(new Event('input'));
      fixture.detectChanges();
      expect(save.disabled).toBe(false);
      save.click();
      await settle(fixture);

      expect(update).toHaveBeenCalledExactlyOnceWith('acme/COW-12', {
        security: 'boundary',
        threat: 'A member could read another tenant',
      });
      expect(el(fixture, 'field-threat')).toBeNull();
    });

    it('does not save a threat of spaces only', async () => {
      const fixture = await render();
      change(fixture, 'field-security', 'boundary');
      await settle(fixture);
      const threat = el(fixture, 'field-threat') as HTMLTextAreaElement;

      threat.value = '   ';
      threat.dispatchEvent(new Event('input'));
      fixture.detectChanges();
      (el(fixture, 'field-threat-save') as HTMLButtonElement).click();
      await settle(fixture);

      expect(update).not.toHaveBeenCalled();
      expect(el(fixture, 'field-threat')).not.toBeNull();
    });

    it('drops the waiting class when none is chosen after it', async () => {
      const fixture = await render();
      change(fixture, 'field-security', 'boundary');
      await settle(fixture);

      change(fixture, 'field-security', 'none');
      await settle(fixture);

      expect(el(fixture, 'field-threat')).toBeNull();
      expect(update).toHaveBeenCalledExactlyOnceWith('acme/COW-12', {
        security: 'none',
        threat: null,
      });
    });

    describe('a save that does not go through', () => {
      const typed = 'A member could read another tenant';

      /** The editor open for `boundary` with the threat typed, and the save pressed. */
      async function save(fixture: ComponentFixture<TicketFields>) {
        change(fixture, 'field-security', 'boundary');
        await settle(fixture);
        const threat = el(fixture, 'field-threat') as HTMLTextAreaElement;
        threat.value = typed;
        threat.dispatchEvent(new Event('input'));
        fixture.detectChanges();
        (el(fixture, 'field-threat-save') as HTMLButtonElement).click();
        await settle(fixture);
      }

      const threatShown = (fixture: ComponentFixture<TicketFields>) =>
        (el(fixture, 'field-threat') as HTMLTextAreaElement | null)?.value;

      it('keeps the editor with the class that waits and the threat that was typed when the server refuses', async () => {
        update.mockRejectedValueOnce(refused());
        const fixture = await render();

        await save(fixture);

        expect(update).toHaveBeenCalledOnce();
        expect(el(fixture, 'field-threat')).not.toBeNull();
        expect(threatShown(fixture)).toBe(typed);
        expect(label(fixture, 'field-security')).toBe('boundary');
        expect((el(fixture, 'field-threat-save') as HTMLButtonElement).disabled).toBe(false);
      });

      it('toasts the problem when the server refuses', async () => {
        update.mockRejectedValueOnce(refused());
        const add = vi.spyOn(TestBed.inject(MessageService), 'add');
        const fixture = await render();

        await save(fixture);

        expect(add).toHaveBeenCalledExactlyOnceWith(
          expect.objectContaining({ summary: 'Validation failed' }),
        );
        expect(confirm).not.toHaveBeenCalled();
      });

      it('keeps the editor with the class that waits and the threat that was typed when the ticket changed meanwhile', async () => {
        update.mockRejectedValueOnce(stale({ security: 'none' }));
        const fixture = await render();

        await save(fixture);

        expect(confirm).toHaveBeenCalledOnce();
        expect(el(fixture, 'field-threat')).not.toBeNull();
        expect(threatShown(fixture)).toBe(typed);
        expect(label(fixture, 'field-security')).toBe('boundary');
      });

      it('keeps the editor while the question about the new version is open, whatever the answer', async () => {
        update.mockRejectedValueOnce(stale({ security: 'none' }));
        const fixture = await render();

        await save(fixture);
        await settle(fixture);

        expect(el(fixture, 'field-threat')).not.toBeNull();
        expect(update).toHaveBeenCalledOnce();
      });

      it('writes again from the same editor, and closes it once that write went through', async () => {
        update.mockRejectedValueOnce(refused());
        const fixture = await render();
        await save(fixture);
        expect(el(fixture, 'field-threat')).not.toBeNull();

        (el(fixture, 'field-threat-save') as HTMLButtonElement).click();
        await settle(fixture);

        expect(update).toHaveBeenCalledTimes(2);
        expect(update).toHaveBeenLastCalledWith('acme/COW-12', {
          security: 'boundary',
          threat: typed,
        });
        expect(el(fixture, 'field-threat')).toBeNull();
      });

      // A second press while the first write is on its way would send a second PATCH over the same
      // `ETag`, which the server answers with a 412 for the person's own write: Save waits.
      it('writes once when Save is pressed again while the first write is on its way', async () => {
        let finish: (written: Ticket) => void = () => undefined;
        update.mockReturnValue(
          new Promise<Ticket>((resolve) => {
            finish = resolve;
          }),
        );
        const fixture = await render();
        change(fixture, 'field-security', 'boundary');
        await settle(fixture);
        const threat = el(fixture, 'field-threat') as HTMLTextAreaElement;
        threat.value = typed;
        threat.dispatchEvent(new Event('input'));
        fixture.detectChanges();
        const saveButton = el(fixture, 'field-threat-save') as HTMLButtonElement;

        saveButton.click();
        saveButton.click();
        await settle(fixture);
        finish(ticket());
        await settle(fixture);

        expect(update).toHaveBeenCalledOnce();
      });

      // 'Write mine' runs the write again from the confirmation; when that one goes through, the
      // editor closes as after a first write that went through.
      it('closes the editor when the person writes over the new version', async () => {
        update.mockRejectedValueOnce(stale({ security: 'none' }));
        const fixture = await render();
        await save(fixture);

        confirm.mock.calls[0][0].accept?.();
        await settle(fixture);

        expect(update).toHaveBeenCalledTimes(2);
        expect(update).toHaveBeenLastCalledWith('acme/COW-12', {
          security: 'boundary',
          threat: typed,
        });
        expect(el(fixture, 'field-threat')).toBeNull();
      });
    });
  });

  describe('the progress stages', () => {
    beforeEach(() => {
      vi.useFakeTimers();
    });

    afterEach(() => {
      vi.useRealTimers();
    });

    /** Renders without waiting for Angular, whose scheduler would wait for the faked timers. */
    function renderNow(current: Ticket = ticket()) {
      const fixture = TestBed.createComponent(TicketFields);
      fixture.componentRef.setInput('ticket', current);
      fixture.detectChanges();
      return fixture;
    }

    /** Lets the answer of a write arrive, and what follows it run. */
    async function answered(fixture: ComponentFixture<TicketFields>) {
      for (let i = 0; i < 5; i++) {
        await Promise.resolve();
      }
      fixture.detectChanges();
    }

    const request = (fixture: ComponentFixture<TicketFields>) =>
      (
        fixture.debugElement.query(By.directive(MoveDialog)).componentInstance as MoveDialog
      ).request();

    const end = (fixture: ComponentFixture<TicketFields>, written: boolean) => {
      (
        fixture.debugElement.query(By.directive(MoveDialog)).componentInstance as MoveDialog
      ).closed.emit(written);
      fixture.detectChanges();
    };

    it('shows the new value at once and writes it once the slider has rested for 400 ms', () => {
      const fixture = renderNow();

      change(fixture, 'field-implementation', 50);
      fixture.detectChanges();
      expect(stageShown(fixture).implementation).toBe('50%');
      vi.advanceTimersByTime(399);
      expect(update).not.toHaveBeenCalled();

      vi.advanceTimersByTime(1);
      expect(update).toHaveBeenCalledExactlyOnceWith('acme/COW-12', { progress: 50 });
    });

    it.each([
      ['ArrowRight', '30%', 30],
      ['ArrowLeft', '20%', 20],
      ['PageUp', '75%', 75],
    ])(
      'moves a stage in steps of five: %s takes 25 to %s (docs/adr/0017 D2)',
      async (code, shown, written) => {
        const fixture = renderNow();
        // The slider takes its value from the model a moment after the first render.
        await answered(fixture);

        el(fixture, 'field-implementation')
          ?.querySelector('input')
          ?.dispatchEvent(new KeyboardEvent('keydown', { code, bubbles: true }));
        fixture.detectChanges();
        expect(stageShown(fixture).implementation).toBe(shown);

        vi.advanceTimersByTime(400);
        expect(update).toHaveBeenCalledExactlyOnceWith('acme/COW-12', { progress: written });
      },
    );

    it.each([
      ['field-refinement', { progress_refinement: 75 }],
      ['field-implementation', { progress: 75 }],
      ['field-review', { progress_review: 75 }],
    ])('writes the slider %s as the field of its stage', (testId, patch) => {
      const fixture = renderNow();

      change(fixture, testId, 75);
      vi.advanceTimersByTime(400);

      expect(update).toHaveBeenCalledExactlyOnceWith('acme/COW-12', patch);
    });

    it('writes only where the slider came to rest, however far it moved', () => {
      const fixture = renderNow();

      change(fixture, 'field-implementation', 50);
      vi.advanceTimersByTime(300);
      change(fixture, 'field-implementation', 75);
      vi.advanceTimersByTime(300);
      change(fixture, 'field-implementation', 90);
      vi.advanceTimersByTime(400);

      expect(update).toHaveBeenCalledExactlyOnceWith('acme/COW-12', { progress: 90 });
    });

    it('writes the stages moved before the sliders rested in one patch', () => {
      const fixture = renderNow();

      change(fixture, 'field-refinement', 100);
      vi.advanceTimersByTime(200);
      change(fixture, 'field-implementation', 50);
      vi.advanceTimersByTime(400);

      expect(update).toHaveBeenCalledExactlyOnceWith('acme/COW-12', {
        progress_refinement: 100,
        progress: 50,
      });
    });

    it('writes nothing when the slider comes back to where the ticket is', () => {
      const fixture = renderNow(ticket({ progress: 25 }));

      change(fixture, 'field-implementation', 50);
      vi.advanceTimersByTime(200);
      change(fixture, 'field-implementation', 25);
      vi.advanceTimersByTime(400);
      fixture.detectChanges();

      expect(update).not.toHaveBeenCalled();
      expect(stageShown(fixture).implementation).toBe('25%');
    });

    it('leaves out of the patch a stage that came back, and follows the ticket for it', () => {
      const fixture = renderNow(ticket({ progress: 25 }));

      change(fixture, 'field-review', 50);
      change(fixture, 'field-implementation', 50);
      change(fixture, 'field-implementation', 25);
      vi.advanceTimersByTime(400);
      fixture.componentRef.setInput('ticket', ticket({ progress: 40, version: 4 }));
      fixture.detectChanges();

      expect(update).toHaveBeenCalledExactlyOnceWith('acme/COW-12', { progress_review: 50 });
      expect(stageShown(fixture).implementation).toBe('40%');
    });

    it('keeps the value it wrote on the slider until the answer is in, then follows the ticket again', async () => {
      let finish: (value: Ticket) => void = () => undefined;
      update.mockReturnValueOnce(
        new Promise<Ticket>((resolve) => {
          finish = resolve;
        }),
      );
      const fixture = renderNow(ticket({ progress: 25 }));
      change(fixture, 'field-implementation', 50);
      vi.advanceTimersByTime(400);
      fixture.detectChanges();
      expect(stageShown(fixture).implementation).toBe('50%');

      finish(ticket({ progress: 50, version: 4 }));
      fixture.componentRef.setInput('ticket', ticket({ progress: 50, version: 4 }));
      await answered(fixture);
      expect(stageShown(fixture).implementation).toBe('50%');

      fixture.componentRef.setInput('ticket', ticket({ progress: 75, version: 5 }));
      fixture.detectChanges();
      expect(stageShown(fixture).implementation).toBe('75%');
    });

    it('keeps a slider that moved on while its write was on the way', async () => {
      let finish: (value: Ticket) => void = () => undefined;
      update.mockReturnValueOnce(
        new Promise<Ticket>((resolve) => {
          finish = resolve;
        }),
      );
      const fixture = renderNow(ticket({ progress: 25 }));
      change(fixture, 'field-implementation', 50);
      vi.advanceTimersByTime(400);
      change(fixture, 'field-implementation', 75);
      fixture.detectChanges();
      expect(stageShown(fixture).implementation).toBe('75%');

      finish(ticket({ progress: 50, version: 4 }));
      fixture.componentRef.setInput('ticket', ticket({ progress: 50, version: 4 }));
      await answered(fixture);

      expect(stageShown(fixture).implementation).toBe('75%');
      vi.advanceTimersByTime(400);
      expect(update).toHaveBeenLastCalledWith('acme/COW-12', { progress: 75 });
    });

    it('goes back to the ticket when the write is refused', async () => {
      update.mockRejectedValueOnce(refused());
      const fixture = renderNow(ticket({ progress: 25 }));

      change(fixture, 'field-implementation', 50);
      vi.advanceTimersByTime(400);
      await answered(fixture);

      expect(stageShown(fixture).implementation).toBe('25%');
    });

    it('writes what was moved and not yet written when the page goes away', () => {
      const fixture = renderNow();
      change(fixture, 'field-implementation', 75);
      vi.advanceTimersByTime(100);
      expect(update).not.toHaveBeenCalled();

      fixture.destroy();

      expect(update).toHaveBeenCalledExactlyOnceWith('acme/COW-12', { progress: 75 });
      vi.advanceTimersByTime(1000);
      expect(update).toHaveBeenCalledOnce();
    });

    it('writes nothing when the page goes away without a move', () => {
      const fixture = renderNow();

      fixture.destroy();

      expect(update).not.toHaveBeenCalled();
    });

    it('drops a move made on another ticket that the page showed before', () => {
      const fixture = renderNow();
      change(fixture, 'field-implementation', 75);

      fixture.componentRef.setInput('ticket', ticket({ key: 'acme/COW-13', number: 13 }));
      fixture.detectChanges();
      vi.advanceTimersByTime(400);

      expect(update).not.toHaveBeenCalled();
      expect(stageShown(fixture).implementation).toBe('25%');
    });

    describe('a move that fills the last stage (docs/adr/0009 D5)', () => {
      const almost = () =>
        ticket({ state: 'review', progress_refinement: 100, progress: 100, progress_review: 75 });

      it('is not written; the done dialog asks for the verification note first', () => {
        const fixture = renderNow(almost());

        change(fixture, 'field-review', 100);
        vi.advanceTimersByTime(400);
        fixture.detectChanges();

        expect(update).not.toHaveBeenCalled();
        expect(request(fixture)).toEqual({ kind: 'complete', patch: { progress_review: 100 } });
        expect(stageShown(fixture).review).toBe('100%');
      });

      it('goes back to the ticket when the person cancels the dialog', () => {
        const fixture = renderNow(almost());
        change(fixture, 'field-review', 100);
        vi.advanceTimersByTime(400);

        end(fixture, false);

        expect(request(fixture)).toBeNull();
        expect(stageShown(fixture).review).toBe('75%');
        expect(update).not.toHaveBeenCalled();
      });

      it('follows the ticket the dialog wrote once it ends', () => {
        const fixture = renderNow(almost());
        change(fixture, 'field-review', 100);
        vi.advanceTimersByTime(400);

        fixture.componentRef.setInput(
          'ticket',
          ticket({ state: 'done', progress_refinement: 100, progress: 100, progress_review: 100 }),
        );
        end(fixture, true);

        expect(request(fixture)).toBeNull();
        expect(stageShown(fixture).review).toBe('100%');
        expect(el(fixture, 'stages-note')?.textContent).toBe(
          'Done by its stages: lowering one reopens it.',
        );
      });

      it('takes no other slider move while the dialog is open', () => {
        const fixture = renderNow(almost());
        change(fixture, 'field-review', 100);
        vi.advanceTimersByTime(400);

        change(fixture, 'field-refinement', 50);
        vi.advanceTimersByTime(400);
        fixture.detectChanges();

        expect(stageShown(fixture).refinement).toBe('100%');
        expect(request(fixture)).toEqual({ kind: 'complete', patch: { progress_review: 100 } });
      });

      it('is not made when the page goes away before the dialog asked', () => {
        const fixture = renderNow(almost());
        change(fixture, 'field-review', 100);

        fixture.destroy();

        expect(update).not.toHaveBeenCalled();
      });

      it('writes a ticket whose stages were all full already at once: it is closed by hand', () => {
        const fixture = renderNow(
          ticket({
            state: 'review',
            progress_refinement: 100,
            progress: 100,
            progress_review: 100,
          }),
        );

        change(fixture, 'field-review', 75);
        vi.advanceTimersByTime(400);

        expect(update).toHaveBeenCalledExactlyOnceWith('acme/COW-12', { progress_review: 75 });
      });
    });

    describe('a move that lowers a stage of a ticket done by its stages', () => {
      const doneByStages = () =>
        ticket({
          state: 'done',
          done_from: 'review',
          progress_refinement: 100,
          progress: 100,
          progress_review: 100,
        });

      it('asks for the reason first, in the dialog that reopens it', () => {
        const fixture = renderNow(doneByStages());

        change(fixture, 'field-implementation', 75);
        vi.advanceTimersByTime(400);
        fixture.detectChanges();

        expect(update).not.toHaveBeenCalled();
        expect(request(fixture)).toEqual({ kind: 'reopen', patch: { progress: 75 } });
      });

      it('is written at once for a ticket done by hand, which stays done', () => {
        const fixture = renderNow(ticket({ ...doneByStages(), done_by_hand: true }));

        change(fixture, 'field-implementation', 75);
        vi.advanceTimersByTime(400);

        expect(update).toHaveBeenCalledExactlyOnceWith('acme/COW-12', { progress: 75 });
      });
    });
  });

  describe('a ticket that changed meanwhile', () => {
    it('asks whether to write over the new version, naming what changed and what was meant', async () => {
      update.mockRejectedValueOnce(stale({ severity: 'critical' }));
      const fixture = await render();

      change(fixture, 'field-severity', 'low');
      await settle(fixture);

      expect(confirm).toHaveBeenCalledOnce();
      expect(confirm.mock.calls[0][0]).toMatchObject({
        header: 'Changed meanwhile',
        message:
          'Someone changed this ticket while you edited it. severity: now critical, yours low. Write yours over it?',
        acceptLabel: 'Write mine',
        rejectLabel: 'Keep theirs',
      });
    });

    it('writes the same patch again over the new version when the person says so', async () => {
      update.mockRejectedValueOnce(stale({ severity: 'critical' }));
      const fixture = await render();
      change(fixture, 'field-severity', 'low');
      await settle(fixture);
      expect(update).toHaveBeenCalledOnce();

      confirm.mock.calls[0][0].accept?.();
      await settle(fixture);

      expect(update).toHaveBeenCalledTimes(2);
      expect(update).toHaveBeenLastCalledWith('acme/COW-12', { severity: 'low' });
      expect(confirm).toHaveBeenCalledOnce();
    });

    it('writes nothing more when the person keeps the new version', async () => {
      update.mockRejectedValueOnce(stale({ severity: 'critical' }));
      const fixture = await render();
      change(fixture, 'field-severity', 'low');
      await settle(fixture);

      expect(confirm.mock.calls[0][0].reject).toBeUndefined();
      await settle(fixture);

      expect(update).toHaveBeenCalledOnce();
    });

    it('asks again when the ticket changed once more before it was written over', async () => {
      update.mockRejectedValueOnce(stale({ severity: 'critical' }));
      update.mockRejectedValueOnce(stale({ severity: 'high' }));
      const fixture = await render();
      change(fixture, 'field-severity', 'low');
      await settle(fixture);

      confirm.mock.calls[0][0].accept?.();
      await settle(fixture);

      expect(confirm).toHaveBeenCalledTimes(2);
      expect(confirm.mock.calls[1][0].message).toContain('severity: now high, yours low');
    });

    it('names every field of the patch, leaves out the comment and spells nothing as nothing', async () => {
      update.mockRejectedValueOnce(
        stale({ security: 'none', threat: null, assignee: null, progress: 0 }),
      );
      const fixture = await render();

      await fixture.componentInstance.write({
        security: 'boundary',
        threat: 'Someone could',
        assignee: 'p1',
        progress: 50,
        comment: 'Because',
      });

      expect(confirm.mock.calls[0][0].message).toBe(
        'Someone changed this ticket while you edited it. ' +
          'security: now none, yours boundary; ' +
          'threat: now nothing, yours Someone could; ' +
          'assignee: now nothing, yours p1; ' +
          'progress: now 0, yours 50. ' +
          'Write yours over it?',
      );
    });
  });

  describe('what a write says', () => {
    it('resolves true when the update went through', async () => {
      const fixture = await render();

      await expect(fixture.componentInstance.write({ type: 'task' })).resolves.toBe(true);

      expect(update).toHaveBeenCalledExactlyOnceWith('acme/COW-12', { type: 'task' });
      expect(confirm).not.toHaveBeenCalled();
    });

    it('does not toast and asks nothing when it went through', async () => {
      const add = vi.spyOn(TestBed.inject(MessageService), 'add');
      const fixture = await render();

      await fixture.componentInstance.write({ severity: 'low' });

      expect(add).not.toHaveBeenCalled();
      expect(confirm).not.toHaveBeenCalled();
    });

    it('resolves false for a ticket that changed meanwhile, once the question about it is open', async () => {
      update.mockRejectedValueOnce(stale({ severity: 'critical' }));
      const add = vi.spyOn(TestBed.inject(MessageService), 'add');
      const fixture = await render();
      const order: string[] = [];
      confirm.mockImplementation(() => {
        order.push('question opened');
        return {} as ConfirmationService;
      });

      const written = await fixture.componentInstance.write({ severity: 'low' });
      order.push('resolved');

      expect(written).toBe(false);
      expect(order).toEqual(['question opened', 'resolved']);
      expect(add).not.toHaveBeenCalled();
    });

    it('resolves false for any other failure, once the problem is toasted', async () => {
      update.mockRejectedValueOnce(refused());
      const fixture = await render();
      const order: string[] = [];
      vi.spyOn(TestBed.inject(MessageService), 'add').mockImplementation(() => {
        order.push('toasted');
      });

      const written = await fixture.componentInstance.write({ type: 'task' });
      order.push('resolved');

      expect(written).toBe(false);
      expect(order).toEqual(['toasted', 'resolved']);
      expect(confirm).not.toHaveBeenCalled();
    });

    it('resolves false for a failure that is no problem of the API, such as an unreachable backend', async () => {
      update.mockRejectedValueOnce(
        new HttpErrorResponse({ status: 0, statusText: 'Unknown Error' }),
      );
      const fixture = await render();

      await expect(fixture.componentInstance.write({ type: 'task' })).resolves.toBe(false);
    });
  });

  describe('a change that the server refuses', () => {
    it('toasts the problem and asks nothing', async () => {
      const body: Problem = {
        type: 'about:blank',
        title: 'Validation failed',
        status: 422,
        detail: 'The threat names no principal.',
        code: 'validation_failed',
      };
      update.mockRejectedValueOnce(new HttpErrorResponse({ status: 422, error: body }));
      const add = vi.spyOn(TestBed.inject(MessageService), 'add');
      const fixture = await render();

      change(fixture, 'field-type', 'task');
      await settle(fixture);

      expect(add).toHaveBeenCalledExactlyOnceWith(
        expect.objectContaining({
          severity: 'warn',
          summary: 'Validation failed',
          detail: 'The threat names no principal.',
        }),
      );
      expect(confirm).not.toHaveBeenCalled();
    });

    it('toasts an unreachable backend as an error', async () => {
      update.mockRejectedValueOnce(
        new HttpErrorResponse({ status: 0, statusText: 'Unknown Error' }),
      );
      const add = vi.spyOn(TestBed.inject(MessageService), 'add');
      const fixture = await render();

      change(fixture, 'field-severity', 'low');
      await settle(fixture);

      expect(add).toHaveBeenCalledExactlyOnceWith(
        expect.objectContaining({ severity: 'error', summary: 'The backend cannot be reached' }),
      );
    });
  });
});
