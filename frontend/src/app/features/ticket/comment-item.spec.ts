import { HttpErrorResponse } from '@angular/common/http';
import { signal } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { By } from '@angular/platform-browser';
import { ConfirmationService, MessageService } from 'primeng/api';
import type { MockInstance } from 'vitest';
import { Attachment, Comment, Problem } from '../../api/models';
import { Conversation } from '../../core/conversation.service';
import { TicketRecords } from '../../core/ticket-records.service';
import { Clock } from '../../shared/time';
import { CommentItem } from './comment-item';
import { FilePreview } from './file-preview';

const ada = { id: 'p1', display_name: 'Ada Lovelace' };
const sam = { id: 'p2', display_name: 'Sam Rivera' };

function comment(overrides: Partial<Comment> = {}): Comment {
  return {
    id: 'c-1',
    author: ada,
    agent: null,
    token: null,
    body: 'Reproduced on the second board.',
    body_html: null,
    edited: false,
    explains: [],
    withdrawn: false,
    withdrawn_at: null,
    created_at: '2026-10-03T10:00:00Z',
    updated_at: '2026-10-03T10:00:00Z',
    version: 2,
    ...overrides,
  };
}

const conflict = () =>
  new HttpErrorResponse({
    status: 412,
    error: {
      type: 'about:blank',
      title: 'Changed',
      status: 412,
      code: 'precondition_failed',
    } satisfies Problem,
  });

describe('CommentItem', () => {
  let conversation: {
    editComment: MockInstance<Conversation['editComment']>;
    commentRevisions: MockInstance<Conversation['commentRevisions']>;
    withdrawComment: MockInstance<Conversation['withdrawComment']>;
  };
  let attach: MockInstance<TicketRecords['attach']>;
  let confirm: MockInstance<ConfirmationService['confirm']>;

  beforeEach(() => {
    conversation = {
      editComment: vi.fn<Conversation['editComment']>().mockResolvedValue(comment()),
      commentRevisions: vi.fn<Conversation['commentRevisions']>().mockResolvedValue([]),
      withdrawComment: vi.fn<Conversation['withdrawComment']>().mockResolvedValue(comment()),
    };
    attach = vi.fn<TicketRecords['attach']>().mockResolvedValue({} as Attachment);
    confirm = vi.fn<ConfirmationService['confirm']>();
    TestBed.configureTestingModule({
      providers: [
        MessageService,
        { provide: Conversation, useValue: conversation },
        { provide: TicketRecords, useValue: { attach } },
        { provide: ConfirmationService, useValue: { confirm } },
        { provide: Clock, useValue: { now: signal(Date.parse('2026-10-03T12:00:00Z')) } },
      ],
    });
  });

  async function render(
    current: Comment = comment(),
    inputs: { me?: string; administers?: boolean; files?: Attachment[] } = { me: 'p1' },
  ) {
    const fixture = TestBed.createComponent(CommentItem);
    fixture.componentRef.setInput('ticketKey', 'acme/COW-12');
    fixture.componentRef.setInput('comment', current);
    for (const [name, value] of Object.entries(inputs)) {
      fixture.componentRef.setInput(name, value);
    }
    await settle(fixture);
    return fixture;
  }

  async function settle(fixture: ComponentFixture<CommentItem>) {
    await new Promise((resolve) => setTimeout(resolve));
    fixture.detectChanges();
    await fixture.whenStable();
  }

  const page = (fixture: ComponentFixture<CommentItem>) => fixture.nativeElement as HTMLElement;
  const el = (fixture: ComponentFixture<CommentItem>, testId: string) =>
    page(fixture).querySelector<HTMLElement>(`[data-testid="${testId}"]`);
  const editor = (fixture: ComponentFixture<CommentItem>) =>
    el(fixture, 'comment-input-c-1') as HTMLTextAreaElement | null;

  async function edit(fixture: ComponentFixture<CommentItem>, text: string) {
    el(fixture, 'comment-edit-c-1')?.click();
    await settle(fixture);
    const field = editor(fixture) as HTMLTextAreaElement;
    field.value = text;
    field.dispatchEvent(new Event('input'));
    fixture.detectChanges();
  }

  async function save(fixture: ComponentFixture<CommentItem>) {
    (el(fixture, 'comment-save-c-1') as HTMLButtonElement).click();
    await settle(fixture);
  }

  describe('its text', () => {
    it('shows the text as the server rendered it', async () => {
      const fixture = await render(
        comment({ body: 'see `x`', body_html: '<p>see <code>x</code></p>' }),
      );

      expect(el(fixture, 'comment-text-c-1')?.querySelector('code')?.textContent).toBe('x');
    });

    it('shows the text as text where it has no rendering', async () => {
      const fixture = await render(comment({ body: '<b>x</b>', body_html: null }));

      expect(page(fixture).querySelector('p.text')?.textContent).toBe('<b>x</b>');
      expect(page(fixture).querySelector('b')).toBeNull();
    });
  });

  describe('what it offers', () => {
    it('offers its author to edit it, to attach a file to it and to withdraw it', async () => {
      const fixture = await render();

      expect(el(fixture, 'comment-edit-c-1')).not.toBeNull();
      expect(el(fixture, 'comment-attach-c-1')).not.toBeNull();
      expect(el(fixture, 'comment-withdraw-c-1')).not.toBeNull();
    });

    it('offers a tenant administrator to withdraw the comment of another, and nothing else', async () => {
      const fixture = await render(comment({ author: sam }), { me: 'p1', administers: true });

      expect(el(fixture, 'comment-edit-c-1')).toBeNull();
      expect(el(fixture, 'comment-attach-c-1')).toBeNull();
      expect(el(fixture, 'comment-withdraw-c-1')).not.toBeNull();
    });

    it('offers anybody else nothing', async () => {
      const fixture = await render(comment({ author: sam }), { me: 'p1' });

      expect(page(fixture).querySelector('.row-actions')).toBeNull();
    });

    it('offers nothing on a withdrawn comment, which shows as withdrawn', async () => {
      const fixture = await render(comment({ withdrawn: true, body: null, edited: true }));

      expect(page(fixture).querySelector('.row-actions')).toBeNull();
      expect(page(fixture).textContent).toContain('withdrawn');
      expect(el(fixture, 'comment-revisions-c-1')).toBeNull();
    });
  });

  describe('an edit (docs/adr/0015 D3)', () => {
    it('starts from the text and writes it, trimmed, over the version the editing began with', async () => {
      const fixture = await render();
      el(fixture, 'comment-edit-c-1')?.click();
      await settle(fixture);
      expect(editor(fixture)?.value).toBe('Reproduced on the second board.');

      await edit(fixture, '  Reproduced on both boards. ');
      fixture.componentRef.setInput('comment', comment({ version: 3 }));
      await settle(fixture);
      await save(fixture);

      expect(conversation.editComment).toHaveBeenCalledExactlyOnceWith(
        'acme/COW-12',
        comment(),
        'Reproduced on both boards.',
      );
      expect(editor(fixture)).toBeNull();
    });

    it('closes without a write when the text did not change', async () => {
      const fixture = await render();
      await edit(fixture, 'Reproduced on the second board.');

      await save(fixture);

      expect(conversation.editComment).not.toHaveBeenCalled();
      expect(editor(fixture)).toBeNull();
    });

    describe('that met a change made meanwhile', () => {
      async function conflicted() {
        conversation.editComment.mockRejectedValueOnce(conflict());
        const fixture = await render();
        await edit(fixture, 'Mine');
        await save(fixture);
        fixture.componentRef.setInput(
          'comment',
          comment({ version: 3, body: 'From the other tab' }),
        );
        await settle(fixture);
        return fixture;
      }

      it('says so and keeps what was typed', async () => {
        const fixture = await conflicted();

        expect(el(fixture, 'conflict')?.textContent).toContain(
          'The comment changed while you edited it.',
        );
        expect(editor(fixture)?.value).toBe('Mine');
      });

      it('writes the person own text over the comment as it is now on request', async () => {
        const fixture = await conflicted();

        el(fixture, 'conflict-overwrite')?.click();
        await settle(fixture);

        expect(conversation.editComment).toHaveBeenLastCalledWith(
          'acme/COW-12',
          comment({ version: 3, body: 'From the other tab' }),
          'Mine',
        );
      });

      it('goes on from the comment as it is now on request', async () => {
        const fixture = await conflicted();

        el(fixture, 'conflict-take-theirs')?.click();
        await settle(fixture);

        expect(editor(fixture)?.value).toBe('From the other tab');
        expect(conversation.editComment).toHaveBeenCalledOnce();
      });
    });
  });

  describe('the earlier texts', () => {
    it('are shown on request, with who replaced each, and hidden again', async () => {
      conversation.commentRevisions.mockResolvedValueOnce([
        {
          body: 'First words',
          edited_by: ada,
          agent: 'claude-code/opus/1',
          token: null,
          at: '2026-10-03T11:00:00Z',
        },
      ]);
      const fixture = await render(comment({ edited: true }));

      el(fixture, 'comment-revisions-c-1')?.click();
      await settle(fixture);

      expect(conversation.commentRevisions).toHaveBeenCalledWith(
        'acme/COW-12',
        comment({ edited: true }),
      );
      const list = el(fixture, 'comment-revision-list-c-1') as HTMLElement;
      expect(list.textContent).toContain('First words');
      expect(list.textContent).toContain('replaced by Ada Lovelace');
      expect(list.querySelector('app-agent-mark')).not.toBeNull();

      el(fixture, 'comment-revisions-c-1')?.click();
      await settle(fixture);
      expect(el(fixture, 'comment-revision-list-c-1')).toBeNull();
    });

    it('are not offered for a comment never edited', async () => {
      const fixture = await render();

      expect(el(fixture, 'comment-revisions-c-1')).toBeNull();
    });
  });

  describe('a withdrawal', () => {
    it('asks first and withdraws on request', async () => {
      const fixture = await render();

      el(fixture, 'comment-withdraw-c-1')?.click();
      expect(confirm.mock.calls[0][0]).toMatchObject({
        header: 'Withdraw the comment',
        acceptLabel: 'Withdraw',
      });
      expect(conversation.withdrawComment).not.toHaveBeenCalled();
      confirm.mock.calls[0][0].accept?.();
      await settle(fixture);

      expect(conversation.withdrawComment).toHaveBeenCalledExactlyOnceWith(
        'acme/COW-12',
        comment(),
      );
    });

    it('names the author and the administrator when it is moderation', async () => {
      const fixture = await render(comment({ author: sam }), { me: 'p1', administers: true });

      el(fixture, 'comment-withdraw-c-1')?.click();

      expect(confirm.mock.calls[0][0].message).toContain('The comment of Sam Rivera is hidden');
      expect(confirm.mock.calls[0][0].message).toContain('the activity names you');
    });
  });

  describe('its files (docs/adr/0016 D1)', () => {
    const shot = {
      id: 'a-1',
      file_name: 'shot.png',
      content_type: 'image/png',
      content_url: '/api/v1/x/content',
      comment: 'c-1',
    } as Attachment;

    it('shows its files with the preview of a raster image', async () => {
      const fixture = await render(comment(), { me: 'p1', files: [shot] });

      expect(el(fixture, 'comment-file-a-1')?.textContent).toContain('shot.png');
      expect(fixture.debugElement.query(By.directive(FilePreview))).not.toBeNull();
    });

    it('uploads a file picked to the comment', async () => {
      const fixture = await render();
      const picker = el(fixture, 'comment-attach-input-c-1') as HTMLInputElement;
      const file = new File(['png'], 'shot.png', { type: 'image/png' });
      Object.defineProperty(picker, 'files', { value: [file], configurable: true });

      picker.dispatchEvent(new Event('change'));
      await settle(fixture);

      expect(attach).toHaveBeenCalledExactlyOnceWith(
        'acme/COW-12',
        file,
        expect.stringMatching(/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/),
        'c-1',
      );
    });

    it('sends the same file again with the same Idempotency-Key after a lost answer, and a new one once it went through (docs/adr/0045 D3)', async () => {
      attach.mockRejectedValueOnce(
        new HttpErrorResponse({ status: 0, statusText: 'Unknown Error' }),
      );
      const fixture = await render();
      const picker = el(fixture, 'comment-attach-input-c-1') as HTMLInputElement;
      const pick = async (file: File) => {
        Object.defineProperty(picker, 'files', { value: [file], configurable: true });
        picker.dispatchEvent(new Event('change'));
        await settle(fixture);
      };
      const shot = new File(['png'], 'shot.png', { type: 'image/png', lastModified: 1 });

      await pick(shot);
      await pick(new File(['png'], 'shot.png', { type: 'image/png', lastModified: 1 }));
      await pick(shot);
      const keys = attach.mock.calls.map((call) => call[2]);

      expect(keys[1]).toBe(keys[0]);
      expect(keys[2]).not.toBe(keys[1]);
    });

    it('toasts a refused upload', async () => {
      attach.mockRejectedValueOnce(
        new HttpErrorResponse({
          status: 415,
          error: {
            type: 'about:blank',
            title: 'Unsupported',
            status: 415,
            code: 'unsupported_media_type',
          } satisfies Problem,
        }),
      );
      const add = vi.spyOn(TestBed.inject(MessageService), 'add');
      const fixture = await render();
      const picker = el(fixture, 'comment-attach-input-c-1') as HTMLInputElement;
      Object.defineProperty(picker, 'files', {
        value: [new File(['x'], 'x.exe')],
        configurable: true,
      });

      picker.dispatchEvent(new Event('change'));
      await settle(fixture);

      expect(add).toHaveBeenCalledOnce();
    });
  });
});
