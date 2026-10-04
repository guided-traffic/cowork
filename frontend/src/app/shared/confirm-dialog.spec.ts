import { ChangeDetectionStrategy, Component, inject } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { ConfirmationService } from 'primeng/api';
import { ConfirmDialog } from './confirm-dialog';

/** A page as the pages have it: its own ConfirmationService, and the dialog in its template. */
@Component({
  selector: 'app-host',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [ConfirmDialog],
  providers: [ConfirmationService],
  template: `<app-confirm-dialog />`,
})
class Host {
  readonly confirmation = inject(ConfirmationService);
}

describe('ConfirmDialog', () => {
  let fixture: ComponentFixture<Host>;

  beforeEach(async () => {
    fixture = TestBed.createComponent(Host);
    await settle();
  });

  async function settle() {
    for (let round = 0; round < 2; round++) {
      await new Promise((resolve) => setTimeout(resolve));
      fixture.detectChanges();
    }
    await fixture.whenStable();
  }

  const dialog = () => document.body.querySelector('.p-confirmdialog');
  const press = (label: string) =>
    [...(dialog()?.querySelectorAll('button') ?? [])]
      .find((button) => button.textContent?.trim() === label)
      ?.dispatchEvent(new MouseEvent('click', { bubbles: true }));

  async function ask(message: string, accept = vi.fn(), reject = vi.fn()) {
    fixture.componentInstance.confirmation.confirm({
      header: 'Remove it?',
      message,
      acceptLabel: 'Remove',
      rejectLabel: 'Keep it',
      accept,
      reject,
    });
    await settle();
    return { accept, reject };
  }

  it('shows the header and the message of a confirmation', async () => {
    await ask('Everything it gave goes.');

    expect(dialog()?.querySelector('.p-dialog-title')?.textContent).toBe('Remove it?');
    expect(dialog()?.querySelector('.p-confirmdialog-message')?.textContent).toBe(
      'Everything it gave goes.',
    );
  });

  it('shows a message as text, never as markup', async () => {
    await ask('<a href="x">y</a> and <img src="x"> stay text');

    expect(dialog()?.querySelector('.p-confirmdialog-message')?.textContent).toBe(
      '<a href="x">y</a> and <img src="x"> stay text',
    );
    expect(dialog()?.querySelector('a')).toBeNull();
    expect(dialog()?.querySelector('img')).toBeNull();
  });

  it('answers with what the person pressed', async () => {
    const first = await ask('Sure?');
    press('Remove');
    await settle();
    expect(first.accept).toHaveBeenCalledOnce();
    expect(first.reject).not.toHaveBeenCalled();

    const second = await ask('Sure?');
    press('Keep it');
    await settle();
    expect(second.reject).toHaveBeenCalledOnce();
    expect(second.accept).not.toHaveBeenCalled();
  });
});
