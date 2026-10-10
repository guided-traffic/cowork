import { HttpErrorResponse, provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { TestBed } from '@angular/core/testing';
import { provideApiConfiguration } from '../api/api-configuration';
import { Attachment, TimeEntry } from '../api/models';
import { TicketRecords } from './ticket-records.service';

const key = 'acme/VKO-12';
const base = '/api/v1/teams/acme/projects/VKO/tickets/12';
/** The key a form holds for its content (docs/adr/0045 D3). */
const formKey = '0199aaaa-0000-7000-8000-00000000f0f0';

const person = { id: '0199aaaa-0000-7000-8000-000000000001', display_name: 'Hans' };

function entry(overrides: Partial<TimeEntry> = {}): TimeEntry {
  return {
    id: '0199aaaa-0000-7000-8000-0000000000e1',
    ticket: key,
    day: '2026-10-03',
    minutes: 90,
    note: '',
    person,
    author: person,
    token: null,
    edited: false,
    voided: false,
    voided_at: null,
    created_at: '2026-10-03T10:00:00Z',
    updated_at: '2026-10-03T10:00:00Z',
    version: 3,
    ...overrides,
  };
}

function attachment(overrides: Partial<Attachment> = {}): Attachment {
  return {
    id: '0199aaaa-0000-7000-8000-0000000000a1',
    file_name: 'notes.txt',
    content_type: 'text/plain; charset=utf-8',
    content_url: '/api/v1/teams/acme/attachments/a1/content',
    size: 5,
    sha256: '0'.repeat(64),
    comment: null,
    agent: null,
    token: null,
    uploaded_by: person,
    created_at: '2026-10-03T10:00:00Z',
    ...overrides,
  };
}

const rejection = (promise: Promise<unknown>) =>
  promise.then(
    () => null,
    (error: unknown) => error,
  );

describe('TicketRecords', () => {
  let records: TicketRecords;
  let http: HttpTestingController;

  beforeEach(() => {
    TestBed.configureTestingModule({
      providers: [provideHttpClient(), provideHttpClientTesting(), provideApiConfiguration('')],
    });
    records = TestBed.inject(TicketRecords);
    http = TestBed.inject(HttpTestingController);
  });

  afterEach(() => {
    try {
      http.verify();
    } finally {
      TestBed.resetTestingModule();
    }
  });

  describe('attach', () => {
    const file = () => new File(['hello'], 'notes.txt', { type: 'text/plain' });

    it('posts the file as multipart form data to the attachments of the ticket', async () => {
      const upload = file();
      const done = records.attach(key, upload, formKey);

      const sent = http.expectOne(`${base}/attachments`);
      expect(sent.request.method).toBe('POST');
      expect(sent.request.body).toBeInstanceOf(FormData);
      const body = sent.request.body as FormData;
      const sentFile = body.get('file') as File;
      expect(sentFile).toBeInstanceOf(Blob);
      expect(sentFile.name).toBe('notes.txt');
      expect(sentFile.size).toBe(5);
      expect(await sentFile.text()).toBe('hello');
      sent.flush(attachment());

      expect((await done).file_name).toBe('notes.txt');
    });

    it('sends the file and nothing else, no comment being named', async () => {
      const done = records.attach(key, file(), formKey);

      const sent = http.expectOne(`${base}/attachments`);
      expect([...(sent.request.body as FormData).keys()]).toEqual(['file']);
      sent.flush(attachment());
      await done;
    });

    it('leaves the content type of the multipart body to the browser, which adds its boundary', async () => {
      const done = records.attach(key, file(), formKey);

      const sent = http.expectOne(`${base}/attachments`);
      expect(sent.request.headers.has('Content-Type')).toBe(false);
      sent.flush(attachment());
      await done;
    });

    it("sends the card's Idempotency-Key, one for each file it sends (docs/adr/0045 D3)", async () => {
      const done = records.attach(key, file(), formKey);
      const sent = http.expectOne(`${base}/attachments`);
      expect(sent.request.headers.get('Idempotency-Key')).toBe(formKey);
      sent.flush(attachment());
      await done;
    });

    it('addresses the ticket through the tenant, the project and the number of its key', async () => {
      const done = records.attach('globex/COW-3', file(), formKey);

      http.expectOne('/api/v1/teams/globex/projects/COW/tickets/3/attachments').flush(attachment());
      await done;
    });

    it.each([
      [413, 'Payload Too Large'],
      [415, 'Unsupported Media Type'],
      [503, 'Service Unavailable'],
    ])(
      'rejects with the HTTP error of a %i, which the caller shows',
      async (status, statusText) => {
        const outcome = rejection(records.attach(key, file(), formKey));

        http
          .expectOne(`${base}/attachments`)
          .flush({ code: 'payload_too_large', status }, { status, statusText });
        const error = await outcome;

        expect(error).toBeInstanceOf(HttpErrorResponse);
        expect((error as HttpErrorResponse).status).toBe(status);
      },
    );
  });

  describe('book', () => {
    it('posts the day and the minutes to the time entries of the ticket', async () => {
      const done = records.book(key, '2026-10-03', 90, '', formKey);

      const sent = http.expectOne(`${base}/time-entries`);
      expect(sent.request.method).toBe('POST');
      expect(sent.request.body).toEqual({ day: '2026-10-03', minutes: 90 });
      sent.flush(entry());

      expect((await done).minutes).toBe(90);
    });

    it('sends the minutes as a number and the day as it was given', async () => {
      const done = records.book(key, '2026-01-05', 45, '', formKey);

      const sent = http.expectOne(`${base}/time-entries`);
      const body = sent.request.body as { day: string; minutes: number };
      expect(body.minutes).toBe(45);
      expect(typeof body.minutes).toBe('number');
      expect(body.day).toBe('2026-01-05');
      sent.flush(entry());
      await done;
    });

    it('sends the note, trimmed, when there is one', async () => {
      const done = records.book(key, '2026-10-03', 30, '  Reviewed the patch.  \n', formKey);

      const sent = http.expectOne(`${base}/time-entries`);
      expect(sent.request.body).toEqual({
        day: '2026-10-03',
        minutes: 30,
        note: 'Reviewed the patch.',
      });
      sent.flush(entry({ note: 'Reviewed the patch.' }));
      await done;
    });

    it.each(['', ' ', '   ', '\n', '\t \n'])(
      'leaves a note that is empty (%j) out of the body',
      async (note) => {
        const done = records.book(key, '2026-10-03', 30, note, formKey);

        const sent = http.expectOne(`${base}/time-entries`);
        expect(sent.request.body).toEqual({ day: '2026-10-03', minutes: 30 });
        expect('note' in (sent.request.body as object)).toBe(false);
        sent.flush(entry());
        await done;
      },
    );

    it("sends the form's Idempotency-Key, one for each content it holds (docs/adr/0045 D3)", async () => {
      const done = records.book(key, '2026-10-03', 30, '', formKey);
      const sent = http.expectOne(`${base}/time-entries`);
      expect(sent.request.headers.get('Idempotency-Key')).toBe(formKey);
      sent.flush(entry());
      await done;
    });

    it('asks for no version, a booking overwriting nothing', async () => {
      const done = records.book(key, '2026-10-03', 30, '', formKey);

      const sent = http.expectOne(`${base}/time-entries`);
      expect(sent.request.headers.has('If-Match')).toBe(false);
      sent.flush(entry());
      await done;
    });

    it('rejects with the HTTP error, such as a day in a locked period', async () => {
      const outcome = rejection(records.book(key, '2026-01-05', 30, '', formKey));

      http
        .expectOne(`${base}/time-entries`)
        .flush({ code: 'period_locked', status: 409 }, { status: 409, statusText: 'Conflict' });
      const error = await outcome;

      expect((error as HttpErrorResponse).status).toBe(409);
    });
  });

  describe('edit', () => {
    it('patches the entry over the version that was read, as If-Match (docs/adr/0050 D3)', async () => {
      const done = records.edit(key, entry({ version: 3 }), { minutes: 120 });

      const sent = http.expectOne(`${base}/time-entries/0199aaaa-0000-7000-8000-0000000000e1`);
      expect(sent.request.method).toBe('PATCH');
      expect(sent.request.headers.get('If-Match')).toBe('"3"');
      expect(sent.request.body).toEqual({ minutes: 120 });
      sent.flush(entry({ minutes: 120, version: 4, edited: true }));

      const edited = await done;
      expect(edited.minutes).toBe(120);
      expect(edited.version).toBe(4);
    });

    it('takes the version from the entry it is given, whatever it is', async () => {
      const done = records.edit(key, entry({ version: 41 }), { note: 'Corrected.' });

      const sent = http.expectOne(`${base}/time-entries/0199aaaa-0000-7000-8000-0000000000e1`);
      expect(sent.request.headers.get('If-Match')).toBe('"41"');
      sent.flush(entry({ version: 42 }));
      await done;
    });

    it('sends the patch as it is, every member it names and none it does not', async () => {
      const done = records.edit(key, entry(), { day: '2026-10-02', minutes: 15, note: 'Fixed.' });

      const sent = http.expectOne(`${base}/time-entries/0199aaaa-0000-7000-8000-0000000000e1`);
      expect(sent.request.body).toEqual({ day: '2026-10-02', minutes: 15, note: 'Fixed.' });
      sent.flush(entry());
      await done;
    });

    it('addresses the entry by its id', async () => {
      const done = records.edit(key, entry({ id: 'another-entry' }), { minutes: 10 });

      http.expectOne(`${base}/time-entries/another-entry`).flush(entry({ id: 'another-entry' }));
      await done;
    });

    it('needs no key, being a patch of an entry that exists', async () => {
      const done = records.edit(key, entry(), { minutes: 10 });

      const sent = http.expectOne(`${base}/time-entries/0199aaaa-0000-7000-8000-0000000000e1`);
      expect(sent.request.headers.has('Idempotency-Key')).toBe(false);
      sent.flush(entry());
      await done;
    });

    it('rejects with the HTTP error of a 412, so that the caller can show the merge', async () => {
      const outcome = rejection(records.edit(key, entry(), { minutes: 10 }));

      http
        .expectOne(`${base}/time-entries/0199aaaa-0000-7000-8000-0000000000e1`)
        .flush(
          { code: 'precondition_failed', status: 412 },
          { status: 412, statusText: 'Precondition Failed' },
        );
      const error = await outcome;

      expect((error as HttpErrorResponse).status).toBe(412);
    });
  });

  describe('void', () => {
    it('puts the void of the entry and hands back the entry as it is kept', async () => {
      const done = records.void(key, entry());

      const sent = http.expectOne(`${base}/time-entries/0199aaaa-0000-7000-8000-0000000000e1/void`);
      expect(sent.request.method).toBe('PUT');
      expect(sent.request.body).toBeNull();
      sent.flush(entry({ voided: true, voided_at: '2026-10-03T11:00:00Z', version: 4 }));

      const voided = await done;
      expect(voided.voided).toBe(true);
      expect(voided.voided_at).toBe('2026-10-03T11:00:00Z');
    });

    it('needs neither a key nor a version, voiding it again being harmless', async () => {
      const done = records.void(key, entry({ version: 9 }));

      const sent = http.expectOne(`${base}/time-entries/0199aaaa-0000-7000-8000-0000000000e1/void`);
      expect(sent.request.headers.has('Idempotency-Key')).toBe(false);
      expect(sent.request.headers.has('If-Match')).toBe(false);
      sent.flush(entry({ voided: true }));
      await done;
    });

    it('addresses the entry by its id, and the ticket by its key', async () => {
      const done = records.void('globex/COW-3', entry({ id: 'another-entry' }));

      http
        .expectOne('/api/v1/teams/globex/projects/COW/tickets/3/time-entries/another-entry/void')
        .flush(entry({ id: 'another-entry', voided: true }));
      await done;
    });

    it('rejects with the HTTP error, such as an entry of a locked period', async () => {
      const outcome = rejection(records.void(key, entry()));

      http
        .expectOne(`${base}/time-entries/0199aaaa-0000-7000-8000-0000000000e1/void`)
        .flush({ code: 'period_locked', status: 409 }, { status: 409, statusText: 'Conflict' });
      const error = await outcome;

      expect((error as HttpErrorResponse).status).toBe(409);
    });
  });

  describe('attach to a comment (docs/adr/0016 D1)', () => {
    it('names the comment the file belongs to beside the file', async () => {
      const done = records.attach(
        key,
        new File(['hello'], 'shot.png', { type: 'image/png' }),
        formKey,
        '0199aaaa-0000-7000-8000-0000000000c1',
      );

      const sent = http.expectOne(`${base}/attachments`);
      expect(sent.request.headers.get('Idempotency-Key')).toBe(formKey);
      const body = sent.request.body as FormData;
      expect(body.get('comment_id')).toBe('0199aaaa-0000-7000-8000-0000000000c1');
      expect((body.get('file') as File).name).toBe('shot.png');
      sent.flush(attachment({ comment: '0199aaaa-0000-7000-8000-0000000000c1' }));

      expect((await done).comment).toBe('0199aaaa-0000-7000-8000-0000000000c1');
    });
  });

  describe('revisions (docs/adr/0017 D7)', () => {
    it('reads the earlier values of an entry, oldest first, as the API orders them', async () => {
      const done = records.revisions(key, entry());

      http
        .expectOne(`${base}/time-entries/0199aaaa-0000-7000-8000-0000000000e1/revisions?limit=200`)
        .flush({ items: [{ minutes: 60 }, { minutes: 75 }], next_cursor: null });

      expect((await done).map((revision) => revision.minutes)).toEqual([60, 75]);
    });
  });
});
