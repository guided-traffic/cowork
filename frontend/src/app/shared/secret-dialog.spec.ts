import { Component, signal } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { By } from '@angular/platform-browser';
import { Dialog } from 'primeng/dialog';
import { SecretDialog } from './secret-dialog';

const secret = `cwk_${'a1B2'.repeat(10)}xyz`;

@Component({
  imports: [SecretDialog],
  template: `
    <app-secret-dialog [(secret)]="value" header="Your new token" label="Token">
      <strong data-testid="warning">Shown once.</strong> Whoever holds it acts as you.
    </app-secret-dialog>
  `,
})
class Host {
  readonly value = signal<string | null>(null);
}

describe('SecretDialog', () => {
  let writeText: ReturnType<typeof vi.fn<(text: string) => Promise<void>>>;

  function provideClipboard(clipboard: unknown) {
    Object.defineProperty(navigator, 'clipboard', { value: clipboard, configurable: true });
  }

  beforeEach(() => {
    writeText = vi.fn<(text: string) => Promise<void>>().mockResolvedValue(undefined);
  });

  afterEach(() => {
    // jsdom has no clipboard of its own: leave it as it was found.
    delete (navigator as unknown as Record<string, unknown>)['clipboard'];
  });

  async function render(value: string | null = secret) {
    const fixture = TestBed.createComponent(Host);
    fixture.componentInstance.value.set(value);
    await settle(fixture);
    return fixture;
  }

  /** Lets what a click started finish, and shows it. */
  async function settle(fixture: ComponentFixture<Host>) {
    await new Promise((resolve) => setTimeout(resolve));
    fixture.detectChanges();
    await fixture.whenStable();
  }

  const el = (fixture: ComponentFixture<Host>, testId: string) =>
    (fixture.nativeElement as HTMLElement).querySelector<HTMLElement>(`[data-testid="${testId}"]`);
  const field = (fixture: ComponentFixture<Host>) =>
    el(fixture, 'secret-value') as HTMLInputElement | null;
  const copyButton = (fixture: ComponentFixture<Host>) =>
    el(fixture, 'secret-copy') as HTMLButtonElement;

  describe('the dialog', () => {
    it('is closed while there is no secret', async () => {
      const fixture = await render(null);

      expect(field(fixture)).toBeNull();
    });

    it('opens with the secret, under the header and the label it is given', async () => {
      const fixture = await render();

      const host = fixture.nativeElement as HTMLElement;
      expect(host.querySelector('.p-dialog-title')?.textContent).toBe('Your new token');
      expect(host.querySelector('.label')?.textContent).toBe('Token');
      expect(field(fixture)?.getAttribute('aria-label')).toBe('Token');
      expect(field(fixture)?.value).toBe(secret);
    });

    it('shows the secret in a field that cannot be edited, and without a spell check', async () => {
      const fixture = await render();

      expect(field(fixture)?.readOnly).toBe(true);
      expect(field(fixture)?.getAttribute('spellcheck')).toBe('false');
      expect(field(fixture)?.getAttribute('autocomplete')).toBe('off');
    });

    it('shows the warning it is given as an alert', async () => {
      const fixture = await render();

      const alert = (fixture.nativeElement as HTMLElement).querySelector('[role="alert"]');
      expect(alert?.textContent).toContain('Shown once. Whoever holds it acts as you.');
      expect(el(fixture, 'warning')?.textContent).toBe('Shown once.');
    });

    it('is not closed by a click beside it, because what it shows cannot be shown again', async () => {
      const fixture = await render();

      const dialog = fixture.debugElement.query(By.directive(Dialog)).componentInstance as Dialog;
      expect(dialog.dismissableMask()).toBe(false);
      expect(dialog.modal()).toBe(true);
    });

    it('follows the secret when a new one is set while it is open', async () => {
      const fixture = await render();

      fixture.componentInstance.value.set('another-secret');
      await settle(fixture);

      expect(field(fixture)?.value).toBe('another-secret');
    });

    it('selects the whole secret when the field is focused, so that it can be copied by hand', async () => {
      const fixture = await render();
      const select = vi.spyOn(field(fixture) as HTMLInputElement, 'select');

      field(fixture)?.dispatchEvent(new FocusEvent('focus'));

      expect(select).toHaveBeenCalledOnce();
    });
  });

  describe('closing', () => {
    it('forgets the secret with the button that says it is stored, and closes', async () => {
      const fixture = await render();

      el(fixture, 'secret-done')?.click();
      await settle(fixture);

      expect(fixture.componentInstance.value()).toBeNull();
      expect(field(fixture)).toBeNull();
    });

    it('forgets the secret when the dialog asks to be closed, such as with Escape or the cross', async () => {
      const fixture = await render();

      fixture.debugElement.query(By.css('p-dialog')).triggerEventHandler('visibleChange', false);
      await settle(fixture);

      expect(fixture.componentInstance.value()).toBeNull();
      expect(field(fixture)).toBeNull();
    });

    it('keeps the secret when the dialog tells that it is visible', async () => {
      const fixture = await render();

      fixture.debugElement.query(By.css('p-dialog')).triggerEventHandler('visibleChange', true);
      await settle(fixture);

      expect(fixture.componentInstance.value()).toBe(secret);
    });

    it('leaves no copy of the secret in the page once it is closed', async () => {
      const fixture = await render();

      el(fixture, 'secret-done')?.click();
      await settle(fixture);

      expect(document.body.innerHTML).not.toContain(secret);
    });

    it('stores the secret nowhere', async () => {
      const setItem = vi.spyOn(Storage.prototype, 'setItem');
      const fixture = await render();
      provideClipboard({ writeText });
      copyButton(fixture).click();
      await settle(fixture);
      el(fixture, 'secret-done')?.click();
      await settle(fixture);

      expect(setItem).not.toHaveBeenCalled();
      expect(JSON.stringify({ ...localStorage })).not.toContain(secret);
      expect(JSON.stringify({ ...sessionStorage })).not.toContain(secret);
      expect(location.href).not.toContain(secret);
      setItem.mockRestore();
    });
  });

  describe('copying', () => {
    it('writes the secret to the clipboard and says it was copied', async () => {
      provideClipboard({ writeText });
      const fixture = await render();
      expect(copyButton(fixture).textContent?.trim()).toBe('Copy');

      copyButton(fixture).click();
      await settle(fixture);

      expect(writeText).toHaveBeenCalledExactlyOnceWith(secret);
      expect(copyButton(fixture).textContent?.trim()).toBe('Copied');
      expect(copyButton(fixture).querySelector('i')?.classList).toContain('pi-check');
      expect(el(fixture, 'secret-copy-failed')).toBeNull();
    });

    it('says that copying is not available, and selects the text, where the page has no clipboard', async () => {
      // A page served over plain HTTP has no navigator.clipboard at all.
      const fixture = await render();
      const select = vi.spyOn(field(fixture) as HTMLInputElement, 'select');

      copyButton(fixture).click();
      await settle(fixture);

      expect(el(fixture, 'secret-copy-failed')?.textContent?.trim()).toBe(
        'Copying is not available here. Select the text and copy it yourself.',
      );
      expect(select).toHaveBeenCalledOnce();
      expect(copyButton(fixture).textContent?.trim()).toBe('Copy');
    });

    it('says the same when the browser refuses the clipboard, such as without permission', async () => {
      writeText.mockRejectedValue(new DOMException('Not allowed', 'NotAllowedError'));
      provideClipboard({ writeText });
      const fixture = await render();
      const select = vi.spyOn(field(fixture) as HTMLInputElement, 'select');

      copyButton(fixture).click();
      await settle(fixture);

      expect(el(fixture, 'secret-copy-failed')).not.toBeNull();
      expect(select).toHaveBeenCalledOnce();
    });

    it('starts again with the next secret: nothing is copied and nothing has failed', async () => {
      provideClipboard({ writeText });
      const fixture = await render();
      copyButton(fixture).click();
      await settle(fixture);
      expect(copyButton(fixture).textContent?.trim()).toBe('Copied');

      fixture.componentInstance.value.set('another-secret');
      await settle(fixture);

      expect(copyButton(fixture).textContent?.trim()).toBe('Copy');
      expect(el(fixture, 'secret-copy-failed')).toBeNull();
    });

    it('does not show a failure of an earlier secret with the next one', async () => {
      const fixture = await render();
      copyButton(fixture).click();
      await settle(fixture);
      expect(el(fixture, 'secret-copy-failed')).not.toBeNull();

      fixture.componentInstance.value.set('another-secret');
      await settle(fixture);

      expect(el(fixture, 'secret-copy-failed')).toBeNull();
    });

    it('copies nothing when there is no secret to copy', async () => {
      provideClipboard({ writeText });
      const fixture = await render();
      const dialog = fixture.debugElement.query(By.directive(SecretDialog));

      fixture.componentInstance.value.set(null);
      await settle(fixture);
      await dialog.componentInstance['copy']();

      expect(writeText).not.toHaveBeenCalled();
    });
  });
});
