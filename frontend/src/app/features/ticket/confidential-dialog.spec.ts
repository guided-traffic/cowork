import { HttpErrorResponse } from '@angular/common/http';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { MessageService } from 'primeng/api';
import type { MockInstance } from 'vitest';
import { Problem, Ticket } from '../../api/models';
import { StaleWrite, TicketActions } from '../../core/ticket-actions.service';
import { ConfidentialDialog } from './confidential-dialog';

function ticket(overrides: Partial<Ticket> = {}): Ticket {
  return {
    key: 'acme/COW-12',
    number: 12,
    project: 'COW',
    confidential: false,
    version: 3,
    ...overrides,
  } as Ticket;
}

describe('ConfidentialDialog', () => {
  let setConfidential: MockInstance<TicketActions['setConfidential']>;

  beforeEach(() => {
    setConfidential = vi.fn<TicketActions['setConfidential']>().mockResolvedValue(ticket());
    TestBed.configureTestingModule({
      providers: [MessageService, { provide: TicketActions, useValue: { setConfidential } }],
    });
  });

  async function render(current: Ticket = ticket(), open = true) {
    const fixture = TestBed.createComponent(ConfidentialDialog);
    fixture.componentRef.setInput('ticket', current);
    fixture.componentRef.setInput('open', open);
    await settle(fixture);
    return fixture;
  }

  async function settle(fixture: ComponentFixture<ConfidentialDialog>) {
    await new Promise((resolve) => setTimeout(resolve));
    fixture.detectChanges();
    await fixture.whenStable();
  }

  const page = () => document.body;
  const el = (testId: string) => page().querySelector<HTMLElement>(`[data-testid="${testId}"]`);
  const send = () => el('confidential-send') as HTMLButtonElement;

  function type(fixture: ComponentFixture<ConfidentialDialog>, text: string) {
    const field = el('confidential-reason') as HTMLTextAreaElement;
    field.value = text;
    field.dispatchEvent(new Event('input'));
    fixture.detectChanges();
  }

  it('is closed until it is opened', async () => {
    await render(ticket(), false);

    expect(el('confidential-send')).toBeNull();
  });

  it('says whom a confidential ticket is shown to, and sets the flag without a reason', async () => {
    const fixture = await render();
    const closed = vi.fn();
    fixture.componentInstance.closed.subscribe(closed);

    expect(page().querySelector('.p-dialog-title')?.textContent).toBe('Make it confidential');
    expect(page().querySelector('.explain')?.textContent).toContain(
      "Only the tenant's administrators, the assignee and the reporter will see COW-12",
    );
    expect(send().disabled).toBe(false);
    send().click();
    await settle(fixture);

    expect(setConfidential).toHaveBeenCalledExactlyOnceWith('acme/COW-12', true, undefined);
    expect(closed).toHaveBeenCalledOnce();
  });

  it('sends a reason given to set it', async () => {
    const fixture = await render();
    type(fixture, '  An HR matter ');

    send().click();
    await settle(fixture);

    expect(setConfidential).toHaveBeenCalledWith('acme/COW-12', true, 'An HR matter');
  });

  it('lifts the flag only with a reason', async () => {
    const fixture = await render(ticket({ confidential: true }));

    expect(page().querySelector('.p-dialog-title')?.textContent).toBe('Lift the confidential flag');
    expect(send().disabled).toBe(true);
    type(fixture, '   ');
    expect(send().disabled).toBe(true);
    type(fixture, 'Fixed and released');
    send().click();
    await settle(fixture);

    expect(setConfidential).toHaveBeenCalledExactlyOnceWith(
      'acme/COW-12',
      false,
      'Fixed and released',
    );
  });

  it('shows a refusal in its form and stays open', async () => {
    const body: Problem = {
      type: 'about:blank',
      title: 'Forbidden',
      status: 403,
      code: 'forbidden',
      detail: 'the act needs the admin role',
    };
    setConfidential.mockRejectedValueOnce(new HttpErrorResponse({ status: 403, error: body }));
    const fixture = await render();
    const closed = vi.fn();
    fixture.componentInstance.closed.subscribe(closed);

    send().click();
    await settle(fixture);

    expect(el('confidential-error')?.textContent).toBe('the act needs the admin role');
    expect(closed).not.toHaveBeenCalled();
  });

  it('says so when somebody else changed the flag meanwhile', async () => {
    setConfidential.mockRejectedValueOnce(
      new StaleWrite(
        {
          status: 412,
          code: 'precondition_failed',
          title: 'Changed',
          detail: '',
          fields: {},
          current: {},
        },
        ticket({ confidential: true, version: 4 }),
      ),
    );
    const fixture = await render();

    send().click();
    await settle(fixture);

    expect(el('confidential-error')?.textContent).toBe(
      'Someone set the flag meanwhile. Check what the ticket shows now.',
    );
  });

  it('starts empty each time it opens', async () => {
    const fixture = await render(ticket({ confidential: true }));
    type(fixture, 'Half a reason');

    fixture.componentRef.setInput('open', false);
    await settle(fixture);
    fixture.componentRef.setInput('open', true);
    await settle(fixture);

    expect((el('confidential-reason') as HTMLTextAreaElement).value).toBe('');
  });
});
