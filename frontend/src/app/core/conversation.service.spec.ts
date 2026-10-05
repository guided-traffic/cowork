import { HttpErrorResponse, provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { TestBed } from '@angular/core/testing';
import { provideApiConfiguration } from '../api/api-configuration';
import { Comment, Question } from '../api/models';
import { Conversation } from './conversation.service';

const base = '/api/v1/tenants/acme/projects/VKO/tickets/12';
const key = 'acme/VKO-12';
/** The key a form holds for its content (docs/adr/0045 D3). */
const formKey = '0199aaaa-0000-7000-8000-00000000f0f0';

function question(overrides: Partial<Question> = {}): Question {
  return {
    id: '0199aaaa-0000-7000-8000-0000000000q1',
    number: 3,
    question: 'Which way?',
    options: '',
    recommendation: '',
    status: 'open',
    answer: null,
    answered_at: null,
    answered_by: null,
    asked_by: { id: 'p1', display_name: 'Hans' },
    asked_by_agent: null,
    asked_by_token: null,
    asked_of: null,
    recorded_by_agent: false,
    answered_by_token: null,
    withdrawn_at: null,
    created_at: '2026-10-01T10:00:00Z',
    updated_at: '2026-10-01T10:00:00Z',
    version: 4,
    ...overrides,
  };
}

describe('Conversation', () => {
  let conversation: Conversation;
  let http: HttpTestingController;

  beforeEach(() => {
    TestBed.configureTestingModule({
      providers: [provideHttpClient(), provideHttpClientTesting(), provideApiConfiguration('')],
    });
    conversation = TestBed.inject(Conversation);
    http = TestBed.inject(HttpTestingController);
  });

  afterEach(() => {
    try {
      http.verify();
    } finally {
      TestBed.resetTestingModule();
    }
  });

  describe('comment', () => {
    it("posts the text to the comments of the ticket, with the form's key (docs/adr/0045 D3)", async () => {
      const done = conversation.comment(key, 'Looks right.', formKey);

      const sent = http.expectOne(`${base}/comments`);
      expect(sent.request.method).toBe('POST');
      expect(sent.request.body).toEqual({ body: 'Looks right.' });
      expect(sent.request.headers.get('Idempotency-Key')).toBe(formKey);
      sent.flush({ id: 'c1', body: 'Looks right.' });

      expect(await done).toMatchObject({ id: 'c1' });
    });

    it('rejects with the HTTP error', async () => {
      const outcome = conversation.comment(key, 'x', formKey).then(
        () => null,
        (error: unknown) => error,
      );

      http
        .expectOne(`${base}/comments`)
        .flush({ code: 'forbidden' }, { status: 403, statusText: 'Forbidden' });

      expect(((await outcome) as HttpErrorResponse).status).toBe(403);
    });
  });

  describe('ask', () => {
    it("posts the question to the questions of the ticket, with the form's key (docs/adr/0045 D3)", async () => {
      const done = conversation.ask(
        key,
        { question: 'Which way?', options: 'A or B', recommendation: 'A' },
        formKey,
      );

      const sent = http.expectOne(`${base}/questions`);
      expect(sent.request.method).toBe('POST');
      expect(sent.request.body).toEqual({
        question: 'Which way?',
        options: 'A or B',
        recommendation: 'A',
      });
      expect(sent.request.headers.get('Idempotency-Key')).toBe(formKey);
      sent.flush(question());

      expect((await done).number).toBe(3);
    });
  });

  describe('answer', () => {
    it('puts the answer to an open question, which overwrites nothing and so needs no version', async () => {
      const done = conversation.answer(key, question({ status: 'open' }), 'Take A.');

      const sent = http.expectOne(`${base}/questions/3/answer`);
      expect(sent.request.method).toBe('PUT');
      expect(sent.request.body).toEqual({ answer: 'Take A.' });
      expect(sent.request.headers.has('If-Match')).toBe(false);
      sent.flush(question({ status: 'answered', answer: 'Take A.' }));

      expect((await done).answer).toBe('Take A.');
    });

    it('sends the version it read when it changes an answer that was given (docs/adr/0050 D3)', async () => {
      const done = conversation.answer(
        key,
        question({ status: 'answered', answer: 'Take A.', version: 9 }),
        'Take B.',
      );

      const sent = http.expectOne(`${base}/questions/3/answer`);
      expect(sent.request.headers.get('If-Match')).toBe('"9"');
      expect(sent.request.body).toEqual({ answer: 'Take B.' });
      sent.flush(question({ status: 'answered', answer: 'Take B.', version: 10 }));
      await done;
    });

    it('addresses the question by its number within the ticket', async () => {
      const done = conversation.answer(key, question({ number: 11 }), 'Yes.');

      http.expectOne(`${base}/questions/11/answer`).flush(question({ number: 11 }));
      await done;
    });

    it('needs no version for a withdrawn question either, which is not answered', async () => {
      const done = conversation.answer(key, question({ status: 'withdrawn' }), 'Yes.');

      const sent = http.expectOne(`${base}/questions/3/answer`);
      expect(sent.request.headers.has('If-Match')).toBe(false);
      sent.flush(question());
      await done;
    });

    it('rejects with the HTTP error of a 412 so that the caller can show the merge', async () => {
      const outcome = conversation
        .answer(key, question({ status: 'answered', version: 9 }), 'Take B.')
        .then(
          () => null,
          (error: unknown) => error,
        );

      http
        .expectOne(`${base}/questions/3/answer`)
        .flush({ code: 'precondition_failed' }, { status: 412, statusText: 'Precondition Failed' });

      expect(((await outcome) as HttpErrorResponse).status).toBe(412);
    });
  });

  describe('withdraw', () => {
    it('puts the withdrawal of the question', async () => {
      const done = conversation.withdraw(key, question({ number: 5 }));

      const sent = http.expectOne(`${base}/questions/5/withdrawal`);
      expect(sent.request.method).toBe('PUT');
      sent.flush(question({ number: 5, status: 'withdrawn' }));

      expect((await done).status).toBe('withdrawn');
    });
  });

  describe('link', () => {
    it('puts the link at an address of its own, from this ticket as the source to the other by its short key', async () => {
      const done = conversation.link(key, 'blocks', 'VKO-7');

      const sent = http.expectOne(`${base}/links/blocks/VKO-7`);
      expect(sent.request.method).toBe('PUT');
      sent.flush({ type: 'blocks' });
      await done;
    });

    it.each(['blocks', 'relates-to', 'duplicates', 'found-in'] as const)(
      'names the type %s in the address',
      async (type) => {
        const done = conversation.link(key, type, 'COW-1');

        http.expectOne(`${base}/links/${type}/COW-1`).flush({ type });
        await done;
      },
    );
  });

  describe('unlink', () => {
    it('deletes the link from the side of its source, naming the target by its short key', async () => {
      const done = conversation.unlink(key, 'blocks', 'acme/VKO-7');

      const sent = http.expectOne(`${base}/links/blocks/VKO-7`);
      expect(sent.request.method).toBe('DELETE');
      sent.flush('', { status: 204, statusText: 'No Content' });
      await done;
    });

    it('cuts the tenant off the key of the target at the first slash', async () => {
      const done = conversation.unlink('acme/VKO-12', 'relates-to', 'acme/COW-3');

      http
        .expectOne(`${base}/links/relates-to/COW-3`)
        .flush('', { status: 204, statusText: 'No Content' });
      await done;
    });
  });

  describe('setInterest', () => {
    it('puts the weight and the note of the stake', async () => {
      const done = conversation.setInterest(key, 'need', 'Blocks the release.');

      const sent = http.expectOne(`${base}/interest`);
      expect(sent.request.method).toBe('PUT');
      expect(sent.request.body).toEqual({ weight: 'need', note: 'Blocks the release.' });
      sent.flush({ weight: 'need' });
      await done;
    });

    it('trims the note', async () => {
      const done = conversation.setInterest(key, 'urgent', '  Now.  \n');

      const sent = http.expectOne(`${base}/interest`);
      expect(sent.request.body).toEqual({ weight: 'urgent', note: 'Now.' });
      sent.flush({ weight: 'urgent' });
      await done;
    });

    it.each(['', '   ', '\n\t'])('leaves a note that is empty (%j) out', async (note) => {
      const done = conversation.setInterest(key, 'watch', note);

      const sent = http.expectOne(`${base}/interest`);
      expect(sent.request.body).toEqual({ weight: 'watch' });
      expect('note' in (sent.request.body as object)).toBe(false);
      sent.flush({ weight: 'watch' });
      await done;
    });
  });

  describe('removeInterest', () => {
    it('deletes the stake', async () => {
      const done = conversation.removeInterest(key);

      const sent = http.expectOne(`${base}/interest`);
      expect(sent.request.method).toBe('DELETE');
      sent.flush('', { status: 204, statusText: 'No Content' });
      await done;
    });
  });

  it('addresses every call through the tenant, the project and the number of the key', async () => {
    const done = conversation.comment('globex/COW-3', 'x', formKey);

    http.expectOne('/api/v1/tenants/globex/projects/COW/tickets/3/comments').flush({ id: 'c1' });
    await done;
  });

  describe('the comments of a comment (docs/adr/0015 D3)', () => {
    const comment = {
      id: '0199aaaa-0000-7000-8000-0000000000c1',
      version: 2,
      body: 'Before',
    } as Comment;
    const at = `${base}/comments/0199aaaa-0000-7000-8000-0000000000c1`;

    it('edits a comment over the version the editing began with', async () => {
      const done = conversation.editComment(key, comment, 'After');

      const sent = http.expectOne(at);
      expect(sent.request.method).toBe('PATCH');
      expect(sent.request.headers.get('If-Match')).toBe('"2"');
      expect(sent.request.body).toEqual({ body: 'After' });
      sent.flush({ ...comment, version: 3, body: 'After' });

      expect((await done).version).toBe(3);
    });

    it('reads the earlier texts of a comment, oldest first, as the API orders them', async () => {
      const done = conversation.commentRevisions(key, comment);

      http
        .expectOne(`${at}/revisions?limit=200`)
        .flush({ items: [{ body: 'First' }, { body: 'Before' }], next_cursor: null });

      expect((await done).map((revision) => revision.body)).toEqual(['First', 'Before']);
    });

    it('withdraws a comment, which takes no version', async () => {
      const done = conversation.withdrawComment(key, comment);

      const sent = http.expectOne(`${at}/withdrawal`);
      expect(sent.request.method).toBe('PUT');
      expect(sent.request.headers.has('If-Match')).toBe(false);
      sent.flush({ ...comment, withdrawn: true, body: null });

      expect((await done).withdrawn).toBe(true);
    });
  });

  describe('editQuestion (docs/adr/0011 D2)', () => {
    it('changes the text of an open question over the version the editing began with', async () => {
      const done = conversation.editQuestion(key, question(), { question: 'Which way now?' });

      const sent = http.expectOne(`${base}/questions/3`);
      expect(sent.request.method).toBe('PATCH');
      expect(sent.request.headers.get('If-Match')).toBe('"4"');
      expect(sent.request.body).toEqual({ question: 'Which way now?' });
      sent.flush(question({ question: 'Which way now?', version: 5 }));

      expect((await done).version).toBe(5);
    });
  });
});
