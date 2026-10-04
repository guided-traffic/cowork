import { HttpErrorResponse } from '@angular/common/http';
import { signal, Type, WritableSignal } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { MessageService } from 'primeng/api';
import { Tooltip } from 'primeng/tooltip';
import { By } from '@angular/platform-browser';
import type { MockInstance } from 'vitest';
import {
  Attachment,
  AttachmentList,
  Me,
  Problem,
  TimeEntry,
  TimeEntryList,
} from '../../api/models';
import { SessionService } from '../../core/session.service';
import { TicketRecords } from '../../core/ticket-records.service';
import { Clock } from '../../shared/time';
import { AttachmentsCard, fileIcon, TimeCard } from './records-cards';
import { TicketRelations } from './ticket-relations';

/** Any Idempotency-Key a form makes: a UUID (docs/adr/0045 D3). */
const formKey = expect.stringMatching(
  /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/,
);

const now = Date.parse('2026-10-03T12:00:00Z');

/** An instant by the clock on the wall of whoever runs the test: the page counts days that way. */
const local = (year: number, month: number, day: number, hour = 12, minute = 0) =>
  new Date(year, month - 1, day, hour, minute).getTime();
const ada = { id: 'p1', display_name: 'Ada Lovelace', username: 'local:ada' };
const sam = { id: 'p2', display_name: 'Sam Rivera', username: 'local:sam' };
const script = { id: 'tok-1', name: 'ci-script' };
const key = 'acme/COW-12';

function attachment(overrides: Partial<Attachment> = {}): Attachment {
  return {
    id: 'f-1',
    file_name: 'screenshot.png',
    content_type: 'image/png',
    content_url: '/api/v1/tenants/acme/projects/COW/tickets/12/attachments/f-1/content',
    sha256: 'ab12cd34',
    size: 2048,
    uploaded_by: ada,
    agent: null,
    token: null,
    comment: null,
    created_at: '2026-10-03T11:55:00Z',
    ...overrides,
  };
}

function entry(overrides: Partial<TimeEntry> = {}): TimeEntry {
  return {
    id: 'e-1',
    ticket: key,
    person: ada,
    author: ada,
    token: null,
    day: '2026-10-02',
    minutes: 90,
    note: '',
    edited: false,
    voided: false,
    voided_at: null,
    version: 1,
    created_at: '2026-10-02T17:00:00Z',
    updated_at: '2026-10-02T17:00:00Z',
    ...overrides,
  };
}

function refusal(status: number, title: string, detail: string) {
  const body: Problem = { type: 'about:blank', title, status, detail, code: 'validation_failed' };
  return new HttpErrorResponse({ status, statusText: title, error: body });
}

/** The part of a `resource()` that a card reads. */
function part<T>(value?: T) {
  const state = signal<T | undefined>(value);
  return { value: state, hasValue: () => state() !== undefined, error: signal<unknown>(undefined) };
}

describe('fileIcon', () => {
  it.each([
    ['image/png', 'pi pi-image'],
    ['image/jpeg', 'pi pi-image'],
    ['image/gif', 'pi pi-image'],
    ['image/webp', 'pi pi-image'],
    ['image/svg+xml', 'pi pi-image'],
    ['application/pdf', 'pi pi-file-pdf'],
    ['text/plain; charset=utf-8', 'pi pi-file'],
    ['application/octet-stream', 'pi pi-file'],
  ])('shows a file of the type %s with %s', (contentType, icon) => {
    expect(fileIcon(contentType)).toBe(icon);
  });
});

describe('record cards', () => {
  let relations: {
    attachments: ReturnType<typeof part<AttachmentList>>;
    time: ReturnType<typeof part<TimeEntryList>>;
    reloadTime: MockInstance<() => void>;
  };
  let records: {
    attach: MockInstance<TicketRecords['attach']>;
    book: MockInstance<TicketRecords['book']>;
    void: MockInstance<TicketRecords['void']>;
  };
  let person: WritableSignal<Me | undefined>;
  let clockNow: WritableSignal<number>;

  beforeEach(() => {
    clockNow = signal(now);
    relations = {
      attachments: part<AttachmentList>(),
      time: part<TimeEntryList>(),
      reloadTime: vi.fn<() => void>(),
    };
    records = {
      attach: vi.fn<TicketRecords['attach']>().mockResolvedValue(attachment()),
      book: vi.fn<TicketRecords['book']>().mockResolvedValue(entry()),
      void: vi.fn<TicketRecords['void']>().mockResolvedValue(entry({ voided: true })),
    };
    person = signal<Me | undefined>({
      ...ada,
      memberships: [],
      global_admin: false,
      local: true,
      password_change_required: false,
    });
    TestBed.configureTestingModule({
      providers: [
        MessageService,
        { provide: TicketRelations, useValue: relations },
        { provide: TicketRecords, useValue: records },
        { provide: SessionService, useValue: { person } },
        { provide: Clock, useValue: { now: clockNow } },
      ],
    });
  });

  async function render<T>(component: Type<T>) {
    const fixture = TestBed.createComponent(component);
    fixture.componentRef.setInput('ticketKey', key);
    await settle(fixture);
    return fixture;
  }

  /** Lets what a click started finish, and shows it. */
  async function settle(fixture: ComponentFixture<unknown>) {
    await new Promise((resolve) => setTimeout(resolve));
    fixture.detectChanges();
    await fixture.whenStable();
  }

  const host = (fixture: ComponentFixture<unknown>) => fixture.nativeElement as HTMLElement;

  const el = (fixture: ComponentFixture<unknown>, testId: string) =>
    host(fixture).querySelector<HTMLElement>(`[data-testid="${testId}"]`);

  /** A button of PrimeNG's directive carries its test id itself. */
  const button = (fixture: ComponentFixture<unknown>, testId: string) =>
    el(fixture, testId) as HTMLButtonElement | null;

  const text = (fixture: ComponentFixture<unknown>, selector: string) =>
    host(fixture).querySelector(selector)?.textContent?.replace(/\s+/g, ' ').trim();

  function typeInto(fixture: ComponentFixture<unknown>, testId: string, value: string) {
    const field = el(fixture, testId) as HTMLInputElement;
    field.value = value;
    field.dispatchEvent(new Event('input'));
    fixture.detectChanges();
  }

  const submit = (fixture: ComponentFixture<unknown>) =>
    host(fixture)
      .querySelector('form')
      ?.dispatchEvent(new Event('submit', { cancelable: true }));

  describe('AttachmentsCard', () => {
    describe('the files', () => {
      it('shows the heading, and no list until the files are loaded', async () => {
        const fixture = await render(AttachmentsCard);

        expect(text(fixture, 'h2')).toBe('Attachments');
        expect(host(fixture).querySelector('.files')).toBeNull();
        expect(el(fixture, 'attach')).not.toBeNull();
      });

      it('says there are no files when there are none', async () => {
        relations.attachments.value.set({ items: [], next_cursor: null });

        const fixture = await render(AttachmentsCard);

        expect(text(fixture, '.files')).toBe('No files.');
      });

      it('shows each file with a link that downloads it, its size, its uploader and its age', async () => {
        relations.attachments.value.set({
          items: [
            attachment(),
            attachment({
              id: 'f-2',
              file_name: 'report.pdf',
              content_type: 'application/pdf',
              size: 840,
              uploaded_by: sam,
            }),
          ],
          next_cursor: null,
        });

        const fixture = await render(AttachmentsCard);

        const first = el(fixture, 'attachment-f-1');
        const link = first?.querySelector('a');
        expect(link?.textContent).toBe('screenshot.png');
        expect(link?.getAttribute('href')).toBe(
          '/api/v1/tenants/acme/projects/COW/tickets/12/attachments/f-1/content',
        );
        expect(link?.getAttribute('target')).toBe('_blank');
        expect(link?.getAttribute('rel')).toBe('noopener');
        expect(text(fixture, '[data-testid="attachment-f-1"] .muted')).toBe(
          '2 KB · Ada Lovelace · 5 minutes ago',
        );
        const second = el(fixture, 'attachment-f-2');
        expect(second?.querySelector('a')?.textContent).toBe('report.pdf');
        expect(text(fixture, '[data-testid="attachment-f-2"] .muted')).toBe(
          '840 B · Sam Rivera · 5 minutes ago',
        );
        expect(first?.querySelector('[data-testid="agent-mark"]')).toBeNull();
      });

      it('marks a file an agent uploaded in the name of its uploader', async () => {
        relations.attachments.value.set({
          items: [attachment({ agent: 'claude-code/opus/s-1' })],
          next_cursor: null,
        });

        const fixture = await render(AttachmentsCard);

        expect(text(fixture, '[data-testid="attachment-f-1"] > .muted')).toBe(
          '2 KB · Ada Lovelace by the agent claude-code (claude-code/opus/s-1) · 5 minutes ago',
        );
        expect(
          el(fixture, 'attachment-f-1')
            ?.querySelector('[data-testid="agent-mark"]')
            ?.getAttribute('data-agent'),
        ).toBe('claude-code/opus/s-1');
      });

      it("marks a file uploaded through a token with the token, and an agent's with both", async () => {
        relations.attachments.value.set({
          items: [
            attachment({ id: 'f-1', token: script }),
            attachment({
              id: 'f-2',
              agent: 'claude-code/opus/s-1',
              token: { id: 'tok-2', name: 'claude-laptop' },
            }),
          ],
          next_cursor: null,
        });

        const fixture = await render(AttachmentsCard);

        expect(text(fixture, '[data-testid="attachment-f-1"] > .muted')).toBe(
          '2 KB · Ada Lovelace through the token ci-script · 5 minutes ago',
        );
        const plain = el(fixture, 'attachment-f-1')?.querySelector('[data-testid="agent-mark"]');
        expect(plain?.getAttribute('data-token')).toBe('tok-1');
        expect(plain?.hasAttribute('data-agent')).toBe(false);
        expect(text(fixture, '[data-testid="attachment-f-2"] > .muted')).toBe(
          '2 KB · Ada Lovelace by the agent claude-code (claude-code/opus/s-1), through the token claude-laptop · 5 minutes ago',
        );
      });

      it('marks each file with the icon of its type', async () => {
        relations.attachments.value.set({
          items: [
            attachment({ id: 'f-1', content_type: 'image/png' }),
            attachment({ id: 'f-2', content_type: 'application/pdf' }),
            attachment({ id: 'f-3', content_type: 'text/plain; charset=utf-8' }),
          ],
          next_cursor: null,
        });

        const fixture = await render(AttachmentsCard);

        const icons = [1, 2, 3].map(
          (n) => el(fixture, `attachment-f-${n}`)?.querySelector('i')?.className,
        );
        expect(icons).toEqual(['pi pi-image', 'pi pi-file-pdf', 'pi pi-file']);
      });

      it('names the checksum of a file in a tooltip on its link', async () => {
        relations.attachments.value.set({ items: [attachment()], next_cursor: null });

        const fixture = await render(AttachmentsCard);

        const link = fixture.debugElement.query(By.css('[data-testid="attachment-f-1"] a'));
        expect(link.injector.get(Tooltip).content()).toBe('ab12cd34');
      });

      it('follows the files when they change', async () => {
        relations.attachments.value.set({ items: [attachment()], next_cursor: null });
        const fixture = await render(AttachmentsCard);

        relations.attachments.value.set({
          items: [attachment(), attachment({ id: 'f-2', file_name: 'log.txt' })],
          next_cursor: null,
        });
        await settle(fixture);

        expect(el(fixture, 'attachment-f-2')).not.toBeNull();
      });
    });

    describe('attaching a file', () => {
      const chosen = new File(['hello'], 'notes.txt', { type: 'text/plain' });

      /** What choosing a file in the file dialog does to the input. */
      function pick(fixture: ComponentFixture<unknown>, files: File[]) {
        const input = el(fixture, 'attach-input') as HTMLInputElement;
        const cleared = vi.fn();
        Object.defineProperty(input, 'files', { configurable: true, value: files });
        Object.defineProperty(input, 'value', {
          configurable: true,
          get: () => '',
          set: cleared,
        });
        input.dispatchEvent(new Event('change'));
        return cleared;
      }

      it('opens the file dialog from the button, through the file input that stays hidden', async () => {
        const fixture = await render(AttachmentsCard);
        const input = el(fixture, 'attach-input') as HTMLInputElement;
        const open = vi.spyOn(input, 'click');

        button(fixture, 'attach')?.click();

        expect(open).toHaveBeenCalledOnce();
        expect(input.type).toBe('file');
      });

      it('uploads the chosen file to the ticket and clears the input so the same file can be chosen again', async () => {
        const fixture = await render(AttachmentsCard);

        const cleared = pick(fixture, [chosen]);
        await settle(fixture);

        expect(records.attach).toHaveBeenCalledExactlyOnceWith(key, chosen, formKey);
        expect(cleared).toHaveBeenCalledWith('');
      });

      it('sends the same file again with the same Idempotency-Key after a lost answer, and another file or the next upload with a new one (docs/adr/0045 D3)', async () => {
        records.attach.mockRejectedValueOnce(
          new HttpErrorResponse({ status: 0, statusText: 'Unknown Error' }),
        );
        records.attach.mockRejectedValueOnce(
          new HttpErrorResponse({ status: 0, statusText: 'Unknown Error' }),
        );
        const fixture = await render(AttachmentsCard);
        const keys = () => records.attach.mock.calls.map((call) => call[2]);

        pick(fixture, [chosen]);
        await settle(fixture);
        pick(fixture, [
          new File(['hello'], 'notes.txt', {
            type: 'text/plain',
            lastModified: chosen.lastModified,
          }),
        ]);
        await settle(fixture);
        expect(keys()[1]).toBe(keys()[0]);

        pick(fixture, [new File(['other'], 'other.txt', { type: 'text/plain' })]);
        await settle(fixture);
        expect(keys()[2]).not.toBe(keys()[0]);

        // Uploaded: the same file again is another upload.
        pick(fixture, [new File(['other'], 'other.txt', { type: 'text/plain' })]);
        await settle(fixture);
        expect(keys()[3]).not.toBe(keys()[2]);
      });

      it('does nothing when the dialog is dismissed without a file', async () => {
        const fixture = await render(AttachmentsCard);

        const cleared = pick(fixture, []);
        await settle(fixture);

        expect(records.attach).not.toHaveBeenCalled();
        expect(cleared).toHaveBeenCalledWith('');
      });

      it('shows its button as busy until the upload is done', async () => {
        let finish: (file: Attachment) => void = () => undefined;
        records.attach.mockReturnValue(
          new Promise<Attachment>((resolve) => {
            finish = resolve;
          }),
        );
        const fixture = await render(AttachmentsCard);
        const trigger = button(fixture, 'attach');
        expect(trigger?.disabled).toBe(false);

        pick(fixture, [chosen]);
        await settle(fixture);
        expect(trigger?.disabled).toBe(true);
        expect(trigger?.querySelector('i.pi-spinner')).not.toBeNull();
        expect(trigger?.querySelector('i.pi-paperclip')).toBeNull();

        finish(attachment());
        await settle(fixture);
        expect(trigger?.disabled).toBe(false);
        expect(trigger?.querySelector('i.pi-paperclip')).not.toBeNull();
      });

      it('toasts what the server refuses, such as a file that is too large', async () => {
        records.attach.mockRejectedValue(
          refusal(413, 'The file is too large', 'A file may hold 10 MB at most.'),
        );
        const add = vi.spyOn(TestBed.inject(MessageService), 'add');
        const fixture = await render(AttachmentsCard);

        pick(fixture, [chosen]);
        await settle(fixture);

        expect(add).toHaveBeenCalledExactlyOnceWith(
          expect.objectContaining({
            summary: 'The file is too large',
            detail: 'A file may hold 10 MB at most.',
          }),
        );
        expect(button(fixture, 'attach')?.disabled).toBe(false);
      });
    });
  });

  describe('TimeCard', () => {
    const booked = (...entries: TimeEntry[]) =>
      relations.time.value.set({ items: entries, next_cursor: null, total_minutes: 0 });

    describe('the entries', () => {
      it('shows the heading, and neither a total nor a list until the entries are loaded', async () => {
        const fixture = await render(TimeCard);

        expect(text(fixture, 'h2')).toBe('Time');
        expect(el(fixture, 'time-total')).toBeNull();
        expect(host(fixture).querySelector('.entries')).toBeNull();
      });

      it('says that no time is booked when there is none', async () => {
        booked();

        const fixture = await render(TimeCard);

        expect(text(fixture, '.entries')).toBe('No time booked.');
      });

      it.each([
        [95, '1 h 35 min'],
        [45, '45 min'],
        [120, '2 h'],
        [0, '0 min'],
      ])('shows the total of %i minutes as %s', async (minutes, shownTotal) => {
        relations.time.value.set({ items: [], next_cursor: null, total_minutes: minutes });

        const fixture = await render(TimeCard);

        expect(text(fixture, '[data-testid="time-total"]')).toBe(shownTotal);
      });

      it('counts zero when the answer carries no total', async () => {
        relations.time.value.set({ items: [], next_cursor: null });

        const fixture = await render(TimeCard);

        expect(text(fixture, '[data-testid="time-total"]')).toBe('0 min');
      });

      it('shows each entry with its duration, who booked it, the day and the note', async () => {
        relations.time.value.set({
          items: [
            entry({ id: 'e-1', minutes: 90, day: '2026-10-02', note: 'Reproducing it' }),
            entry({ id: 'e-2', minutes: 45, day: '2026-10-03', person: sam }),
          ],
          next_cursor: null,
          total_minutes: 135,
        });

        const fixture = await render(TimeCard);

        expect(text(fixture, '[data-testid="time-e-1"] .tabular')).toBe('1 h 30 min');
        expect(text(fixture, '[data-testid="time-e-1"] .muted')).toBe('Ada Lovelace · 2026-10-02');
        expect(text(fixture, '[data-testid="time-e-1"] .note')).toBe('Reproducing it');
        expect(text(fixture, '[data-testid="time-e-2"] .tabular')).toBe('45 min');
        expect(text(fixture, '[data-testid="time-e-2"] .muted')).toBe('Sam Rivera · 2026-10-03');
        expect(el(fixture, 'time-e-2')?.querySelector('.note')).toBeNull();
        expect(text(fixture, '[data-testid="time-total"]')).toBe('2 h 15 min');
      });

      it('marks an entry booked through a token with the token, and leaves the others unmarked', async () => {
        booked(entry({ id: 'e-1', token: script }), entry({ id: 'e-2', person: sam }));

        const fixture = await render(TimeCard);

        expect(text(fixture, '[data-testid="time-e-1"] .muted')).toBe(
          'Ada Lovelace through the token ci-script · 2026-10-02',
        );
        const mark = el(fixture, 'time-e-1')?.querySelector('[data-testid="agent-mark"]');
        expect(mark?.getAttribute('data-token')).toBe('tok-1');
        expect(mark?.hasAttribute('data-agent')).toBe(false);
        expect(mark?.querySelector('.pi-microchip-ai')).not.toBeNull();
        expect(el(fixture, 'time-e-2')?.querySelector('[data-testid="agent-mark"]')).toBeNull();
      });

      it('leaves out the entries that were voided', async () => {
        booked(
          entry({ id: 'e-1' }),
          entry({ id: 'e-2', voided: true, voided_at: '2026-10-03T08:00:00Z' }),
        );

        const fixture = await render(TimeCard);

        expect(el(fixture, 'time-e-1')).not.toBeNull();
        expect(el(fixture, 'time-e-2')).toBeNull();
      });

      it('offers to void the entries of the person only', async () => {
        booked(entry({ id: 'e-1', person: ada }), entry({ id: 'e-2', person: sam }));

        const fixture = await render(TimeCard);

        expect(el(fixture, 'void-e-1')).not.toBeNull();
        expect(el(fixture, 'void-e-2')).toBeNull();
      });

      it('offers to void nothing while the person is not known', async () => {
        person.set(undefined);
        booked(entry({ id: 'e-1', person: ada }));

        const fixture = await render(TimeCard);

        expect(el(fixture, 'void-e-1')).toBeNull();
      });

      it('follows the entries when they change', async () => {
        booked(entry({ id: 'e-1' }));
        const fixture = await render(TimeCard);

        booked(entry({ id: 'e-1' }), entry({ id: 'e-3', minutes: 30 }));
        await settle(fixture);

        expect(el(fixture, 'time-e-3')).not.toBeNull();
      });
    });

    describe('booking', () => {
      beforeEach(() => {
        clockNow.set(local(2026, 10, 3));
      });

      it('starts at today, with nothing typed, and cannot book yet', async () => {
        const fixture = await render(TimeCard);

        expect((el(fixture, 'time-day') as HTMLInputElement).value).toBe('2026-10-03');
        expect((el(fixture, 'time-duration') as HTMLInputElement).value).toBe('');
        expect((el(fixture, 'time-note') as HTMLInputElement).value).toBe('');
        expect(button(fixture, 'time-book')?.disabled).toBe(true);
      });

      it.each([
        ['90', 90],
        ['1:30', 90],
        ['1h 30m', 90],
        ['1.5h', 90],
        ['45m', 45],
        ['  2h  ', 120],
      ])('books %j as %i minutes', async (typed, minutes) => {
        const fixture = await render(TimeCard);
        typeInto(fixture, 'time-duration', typed);
        expect(button(fixture, 'time-book')?.disabled).toBe(false);

        submit(fixture);
        await settle(fixture);

        expect(records.book).toHaveBeenCalledExactlyOnceWith(
          key,
          '2026-10-03',
          minutes,
          '',
          formKey,
        );
      });

      it.each(['', '   ', 'abc', '0', '0:00', '1:75', '-5', '1h 30'])(
        'cannot book %j, which is no duration',
        async (typed) => {
          const fixture = await render(TimeCard);

          typeInto(fixture, 'time-duration', typed);
          expect(button(fixture, 'time-book')?.disabled).toBe(true);
          submit(fixture);
          await settle(fixture);

          expect(records.book).not.toHaveBeenCalled();
        },
      );

      it('books again with the same Idempotency-Key after a lost answer, and with a new one for another content or the next booking (docs/adr/0045 D3)', async () => {
        records.book.mockRejectedValueOnce(
          new HttpErrorResponse({ status: 0, statusText: 'Unknown Error' }),
        );
        records.book.mockRejectedValueOnce(
          new HttpErrorResponse({ status: 0, statusText: 'Unknown Error' }),
        );
        const fixture = await render(TimeCard);
        const keys = () => records.book.mock.calls.map((call) => call[4]);
        typeInto(fixture, 'time-duration', '1h');
        submit(fixture);
        await settle(fixture);
        submit(fixture);
        await settle(fixture);
        expect(keys()[1]).toBe(keys()[0]);

        typeInto(fixture, 'time-note', 'Reproducing it');
        submit(fixture);
        await settle(fixture);
        expect(keys()[2]).not.toBe(keys()[0]);

        // Booked: the same time again is another booking.
        typeInto(fixture, 'time-duration', '1h');
        typeInto(fixture, 'time-note', 'Reproducing it');
        submit(fixture);
        await settle(fixture);
        expect(keys()[3]).not.toBe(keys()[2]);
      });

      it('books on the day that was chosen, with the note', async () => {
        const fixture = await render(TimeCard);
        typeInto(fixture, 'time-day', '2026-10-01');
        typeInto(fixture, 'time-duration', '2h');
        typeInto(fixture, 'time-note', 'Reproducing it');

        submit(fixture);
        await settle(fixture);

        expect(records.book).toHaveBeenCalledExactlyOnceWith(
          key,
          '2026-10-01',
          120,
          'Reproducing it',
          formKey,
        );
      });

      it('loads the entries again, empties the duration and the note and keeps the day', async () => {
        const fixture = await render(TimeCard);
        typeInto(fixture, 'time-day', '2026-10-01');
        typeInto(fixture, 'time-duration', '2h');
        typeInto(fixture, 'time-note', 'Reproducing it');

        submit(fixture);
        await settle(fixture);

        expect(relations.reloadTime).toHaveBeenCalledOnce();
        expect((el(fixture, 'time-duration') as HTMLInputElement).value).toBe('');
        expect((el(fixture, 'time-note') as HTMLInputElement).value).toBe('');
        expect((el(fixture, 'time-day') as HTMLInputElement).value).toBe('2026-10-01');
        expect(button(fixture, 'time-book')?.disabled).toBe(true);
      });

      it('keeps what was typed and toasts the problem when the booking is refused', async () => {
        records.book.mockRejectedValue(
          refusal(409, 'The period is locked', 'Time up to 2026-09-30 cannot change.'),
        );
        const add = vi.spyOn(TestBed.inject(MessageService), 'add');
        const fixture = await render(TimeCard);
        typeInto(fixture, 'time-duration', '2h');
        typeInto(fixture, 'time-note', 'Reproducing it');

        submit(fixture);
        await settle(fixture);

        expect(add).toHaveBeenCalledExactlyOnceWith(
          expect.objectContaining({
            summary: 'The period is locked',
            detail: 'Time up to 2026-09-30 cannot change.',
          }),
        );
        expect(relations.reloadTime).not.toHaveBeenCalled();
        expect((el(fixture, 'time-duration') as HTMLInputElement).value).toBe('2h');
        expect((el(fixture, 'time-note') as HTMLInputElement).value).toBe('Reproducing it');
        expect(button(fixture, 'time-book')?.disabled).toBe(false);
      });

      it('books once while the booking is on its way', async () => {
        let finish: (booking: TimeEntry) => void = () => undefined;
        records.book.mockReturnValue(
          new Promise<TimeEntry>((resolve) => {
            finish = resolve;
          }),
        );
        const fixture = await render(TimeCard);
        typeInto(fixture, 'time-duration', '2h');

        submit(fixture);
        await settle(fixture);
        expect(button(fixture, 'time-book')?.disabled).toBe(true);
        expect(button(fixture, 'time-book')?.querySelector('i.pi-spinner')).not.toBeNull();
        finish(entry());
        await settle(fixture);

        expect(records.book).toHaveBeenCalledOnce();
      });
    });

    describe('the day to book on', () => {
      beforeEach(() => {
        clockNow.set(local(2026, 10, 3));
      });

      const dayShown = (fixture: ComponentFixture<unknown>) =>
        (el(fixture, 'time-day') as HTMLInputElement).value;

      /** The clock goes on to `instant`, and the page shows what that changed. */
      async function passTo(fixture: ComponentFixture<unknown>, instant: number) {
        clockNow.set(instant);
        await settle(fixture);
      }

      it('is the day of the clock, whatever the hour', async () => {
        for (const [hour, minute] of [
          [0, 0],
          [0, 1],
          [12, 0],
          [23, 59],
        ]) {
          clockNow.set(local(2026, 10, 3, hour, minute));
          const fixture = await render(TimeCard);

          expect(dayShown(fixture), `${hour}:${minute}`).toBe('2026-10-03');
          fixture.destroy();
        }
      });

      it('moves on to the new date when the clock passes midnight', async () => {
        const fixture = await render(TimeCard);

        await passTo(fixture, local(2026, 10, 4, 0, 1));

        expect(dayShown(fixture)).toBe('2026-10-04');
      });

      it.each([
        [
          'into the next month',
          local(2026, 10, 31, 23, 59),
          local(2026, 11, 1, 0, 1),
          '2026-10-31',
          '2026-11-01',
        ],
        [
          'into the next year',
          local(2026, 12, 31, 23, 59),
          local(2027, 1, 1, 0, 1),
          '2026-12-31',
          '2027-01-01',
        ],
        [
          'past the end of February in a leap year',
          local(2028, 2, 28, 23, 59),
          local(2028, 2, 29, 0, 1),
          '2028-02-28',
          '2028-02-29',
        ],
        [
          'past the end of February in another year',
          local(2027, 2, 28, 23, 59),
          local(2027, 3, 1, 0, 1),
          '2027-02-28',
          '2027-03-01',
        ],
      ])('moves on %s', async (_, before, after, dayBefore, dayAfter) => {
        clockNow.set(before);
        const fixture = await render(TimeCard);
        expect(dayShown(fixture)).toBe(dayBefore);

        await passTo(fixture, after);

        expect(dayShown(fixture)).toBe(dayAfter);
      });

      it('stays while the clock goes on within the day', async () => {
        const fixture = await render(TimeCard);

        await passTo(fixture, local(2026, 10, 3, 18, 30));
        await passTo(fixture, local(2026, 10, 3, 23, 59));

        expect(dayShown(fixture)).toBe('2026-10-03');
      });

      it('holds a day the person picked while the clock goes on within the day', async () => {
        const fixture = await render(TimeCard);
        typeInto(fixture, 'time-day', '2026-10-01');

        await passTo(fixture, local(2026, 10, 3, 18, 30));

        expect(dayShown(fixture)).toBe('2026-10-01');
      });

      it('replaces a day the person picked with the new date once the clock passes midnight', async () => {
        const fixture = await render(TimeCard);
        typeInto(fixture, 'time-day', '2026-10-01');
        await passTo(fixture, local(2026, 10, 3, 23, 59));
        expect(dayShown(fixture)).toBe('2026-10-01');

        await passTo(fixture, local(2026, 10, 4, 0, 1));

        expect(dayShown(fixture)).toBe('2026-10-04');
      });

      it('books on the day that was picked, also after the clock went on within the day', async () => {
        const fixture = await render(TimeCard);
        typeInto(fixture, 'time-day', '2026-10-01');
        await passTo(fixture, local(2026, 10, 3, 18, 30));
        typeInto(fixture, 'time-duration', '2h');

        submit(fixture);
        await settle(fixture);

        expect(records.book).toHaveBeenCalledExactlyOnceWith(key, '2026-10-01', 120, '', formKey);
      });

      it('books on the new date after midnight, however the page was left open', async () => {
        const fixture = await render(TimeCard);
        typeInto(fixture, 'time-duration', '2h');
        await passTo(fixture, local(2026, 10, 4, 0, 1));

        submit(fixture);
        await settle(fixture);

        expect(records.book).toHaveBeenCalledExactlyOnceWith(key, '2026-10-04', 120, '', formKey);
      });

      it('keeps the duration and the note that were typed when the day moves on', async () => {
        const fixture = await render(TimeCard);
        typeInto(fixture, 'time-duration', '1:30');
        typeInto(fixture, 'time-note', 'Reproducing it');

        await passTo(fixture, local(2026, 10, 4, 0, 1));

        expect((el(fixture, 'time-duration') as HTMLInputElement).value).toBe('1:30');
        expect((el(fixture, 'time-note') as HTMLInputElement).value).toBe('Reproducing it');
        expect(button(fixture, 'time-book')?.disabled).toBe(false);
      });

      it('keeps a booking that is on its way as it was sent when the day moves on meanwhile', async () => {
        let finish: (booking: TimeEntry) => void = () => undefined;
        records.book.mockReturnValue(
          new Promise<TimeEntry>((resolve) => {
            finish = resolve;
          }),
        );
        const fixture = await render(TimeCard);
        typeInto(fixture, 'time-duration', '2h');
        submit(fixture);
        await settle(fixture);

        await passTo(fixture, local(2026, 10, 4, 0, 1));
        finish(entry());
        await settle(fixture);

        expect(records.book).toHaveBeenCalledExactlyOnceWith(key, '2026-10-03', 120, '', formKey);
        expect(dayShown(fixture)).toBe('2026-10-04');
      });
    });

    describe('voiding', () => {
      it('voids the entry and loads the entries again', async () => {
        const mine = entry({ id: 'e-1' });
        booked(mine);
        const fixture = await render(TimeCard);

        button(fixture, 'void-e-1')?.click();
        await settle(fixture);

        expect(records.void).toHaveBeenCalledExactlyOnceWith(key, mine);
        expect(relations.reloadTime).toHaveBeenCalledOnce();
      });

      it('toasts the problem and loads nothing when the entry cannot be voided', async () => {
        records.void.mockRejectedValue(
          refusal(409, 'The period is locked', 'Time up to 2026-10-02 cannot change.'),
        );
        const add = vi.spyOn(TestBed.inject(MessageService), 'add');
        booked(entry({ id: 'e-1' }));
        const fixture = await render(TimeCard);

        button(fixture, 'void-e-1')?.click();
        await settle(fixture);

        expect(add).toHaveBeenCalledExactlyOnceWith(
          expect.objectContaining({ summary: 'The period is locked' }),
        );
        expect(relations.reloadTime).not.toHaveBeenCalled();
      });
    });
  });
});
