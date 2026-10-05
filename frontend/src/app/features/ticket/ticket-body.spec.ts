import { HttpErrorResponse } from '@angular/common/http';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { MessageService } from 'primeng/api';
import type { MockInstance } from 'vitest';
import { Problem, Ticket } from '../../api/models';
import { StaleWrite, TicketActions } from '../../core/ticket-actions.service';
import { TicketBody } from './ticket-body';

function ticket(overrides: Partial<Ticket> = {}): Ticket {
  return {
    key: 'acme/COW-12',
    number: 12,
    project: 'COW',
    title: 'The board flickers',
    body: '## Current state\n\nIt flickers.',
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
    ticket({ version: 5, ...current }),
  );

describe('TicketBody', () => {
  let replaceBody: MockInstance<TicketActions['replaceBody']>;

  beforeEach(() => {
    replaceBody = vi.fn<TicketActions['replaceBody']>().mockResolvedValue(ticket());
    TestBed.configureTestingModule({
      providers: [MessageService, { provide: TicketActions, useValue: { replaceBody } }],
    });
  });

  async function render(current: Ticket = ticket()) {
    const fixture = TestBed.createComponent(TicketBody);
    fixture.componentRef.setInput('ticket', current);
    await settle(fixture);
    return fixture;
  }

  async function settle(fixture: ComponentFixture<TicketBody>) {
    await new Promise((resolve) => setTimeout(resolve));
    fixture.detectChanges();
    await fixture.whenStable();
  }

  const el = (fixture: ComponentFixture<TicketBody>, testId: string) =>
    (fixture.nativeElement as HTMLElement).querySelector<HTMLElement>(`[data-testid="${testId}"]`);

  const editor = (fixture: ComponentFixture<TicketBody>) =>
    el(fixture, 'body-input') as HTMLTextAreaElement | null;

  async function edit(fixture: ComponentFixture<TicketBody>, text?: string) {
    el(fixture, 'body-edit')?.click();
    await settle(fixture);
    if (text !== undefined) {
      const field = editor(fixture) as HTMLTextAreaElement;
      field.value = text;
      field.dispatchEvent(new Event('input'));
      fixture.detectChanges();
    }
  }

  async function save(fixture: ComponentFixture<TicketBody>) {
    (el(fixture, 'body-save') as HTMLButtonElement).click();
    await settle(fixture);
  }

  it('shows the body as text, never as markup', async () => {
    const fixture = await render(ticket({ body: '<b>bold</b> and **strong**' }));

    expect(el(fixture, 'body')?.textContent).toBe('<b>bold</b> and **strong**');
    expect(el(fixture, 'body')?.querySelector('b')).toBeNull();
  });

  it('says so when there is no body', async () => {
    const fixture = await render(ticket({ body: '' }));

    expect(el(fixture, 'body')).toBeNull();
    expect((fixture.nativeElement as HTMLElement).textContent).toContain('No description.');
  });

  it('edits the body as Markdown, starting from the one the ticket has', async () => {
    const fixture = await render();

    await edit(fixture);

    expect(editor(fixture)?.value).toBe('## Current state\n\nIt flickers.');
    expect(el(fixture, 'body')).toBeNull();
    expect(el(fixture, 'body-edit')).toBeNull();
  });

  it('replaces the body as a whole over the version the editing began with, and closes', async () => {
    const fixture = await render();
    await edit(fixture, '## Current state\n\nFixed.');

    fixture.componentRef.setInput('ticket', ticket({ version: 4, state: 'analysed' }));
    await settle(fixture);
    await save(fixture);

    expect(replaceBody).toHaveBeenCalledExactlyOnceWith(
      'acme/COW-12',
      '## Current state\n\nFixed.',
      ticket(),
    );
    expect(editor(fixture)).toBeNull();
  });

  it('may empty the body', async () => {
    const fixture = await render();
    await edit(fixture, '');

    await save(fixture);

    expect(replaceBody).toHaveBeenCalledWith('acme/COW-12', '', ticket());
  });

  it('closes without a write when nothing changed, and on Cancel', async () => {
    const fixture = await render();
    await edit(fixture);
    await save(fixture);
    expect(editor(fixture)).toBeNull();

    await edit(fixture, 'Something else');
    el(fixture, 'body-cancel')?.click();
    await settle(fixture);

    expect(editor(fixture)).toBeNull();
    expect(replaceBody).not.toHaveBeenCalled();
  });

  describe('a body somebody else changed meanwhile', () => {
    async function conflicted() {
      replaceBody.mockRejectedValueOnce(stale({ body: 'Their body' }));
      const fixture = await render();
      await edit(fixture, 'My body');
      await save(fixture);
      return fixture;
    }

    it('says so in the editor, which keeps what was typed and saves nothing until the person decides', async () => {
      const fixture = await conflicted();

      expect(el(fixture, 'conflict')?.textContent).toContain(
        'The description changed while you edited it.',
      );
      expect(editor(fixture)?.value).toBe('My body');
      expect((el(fixture, 'body-save') as HTMLButtonElement).disabled).toBe(true);
    });

    it('writes the person own text over the new version on request', async () => {
      const fixture = await conflicted();

      el(fixture, 'conflict-overwrite')?.click();
      await settle(fixture);

      expect(replaceBody).toHaveBeenLastCalledWith(
        'acme/COW-12',
        'My body',
        expect.objectContaining({ version: 5, body: 'Their body' }),
      );
      expect(editor(fixture)).toBeNull();
    });

    it('goes on from the new body on request, without a write', async () => {
      const fixture = await conflicted();

      el(fixture, 'conflict-take-theirs')?.click();
      await settle(fixture);

      expect(editor(fixture)?.value).toBe('Their body');
      expect(el(fixture, 'conflict')).toBeNull();
      expect(replaceBody).toHaveBeenCalledOnce();
    });
  });

  it('toasts a refusal and keeps the editor with what was typed', async () => {
    const body: Problem = {
      type: 'about:blank',
      title: 'Validation failed',
      status: 400,
      code: 'validation_failed',
    };
    replaceBody.mockRejectedValueOnce(new HttpErrorResponse({ status: 400, error: body }));
    const add = vi.spyOn(TestBed.inject(MessageService), 'add');
    const fixture = await render();
    await edit(fixture, 'Mine');

    await save(fixture);

    expect(add).toHaveBeenCalledOnce();
    expect(editor(fixture)?.value).toBe('Mine');
  });

  it('closes when the page turns to another ticket, which nothing is written to', async () => {
    const fixture = await render();
    await edit(fixture, 'Meant for COW-12');

    fixture.componentRef.setInput(
      'ticket',
      ticket({ key: 'acme/COW-13', number: 13, body: 'The next one' }),
    );
    await settle(fixture);

    expect(editor(fixture)).toBeNull();
    expect(el(fixture, 'body')?.textContent).toBe('The next one');
    expect(replaceBody).not.toHaveBeenCalled();
  });
});
