import { Component, signal } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { Dialog } from 'primeng/dialog';
import { keepOpenWhile } from './keep-open';

@Component({
  imports: [Dialog],
  template: `
    <p-dialog
      [visible]="open()"
      (visibleChange)="open.set($event)"
      [modal]="true"
      [closable]="!busy()"
      [dismissableMask]="!busy()"
      header="A form"
    >
      <p data-testid="content">What the person typed</p>
    </p-dialog>
  `,
})
class Host {
  readonly open = signal(true);
  readonly busy = signal(false);

  constructor() {
    keepOpenWhile(() => this.busy());
  }
}

describe('keepOpenWhile', () => {
  async function render() {
    const fixture = TestBed.createComponent(Host);
    await settle(fixture);
    return fixture;
  }

  async function settle(fixture: ComponentFixture<Host>) {
    await new Promise((resolve) => setTimeout(resolve));
    fixture.detectChanges();
    await fixture.whenStable();
  }

  /** A key press as the browser makes one: aimed at the focused element, and on its way up to the document. */
  const press = (key: string, target: EventTarget = document.body) =>
    target.dispatchEvent(new KeyboardEvent('keydown', { key, bubbles: true, cancelable: true }));

  const mask = () => document.querySelector('.p-dialog-mask') as HTMLElement | null;

  it('lets Escape close a dialog while nothing is running, which is what PrimeNG does by itself', async () => {
    const fixture = await render();
    expect(fixture.componentInstance.open()).toBe(true);

    press('Escape');
    await settle(fixture);

    expect(fixture.componentInstance.open()).toBe(false);
  });

  it('keeps the dialog open on Escape while busy, from the body and from a field of the dialog alike', async () => {
    const fixture = await render();
    fixture.componentInstance.busy.set(true);
    await settle(fixture);

    press('Escape');
    press('Escape', document.querySelector('[data-testid="content"]') as HTMLElement);
    await settle(fixture);

    expect(fixture.componentInstance.open()).toBe(true);
    expect(document.querySelector('[data-testid="content"]')).not.toBeNull();
  });

  it('closes on Escape again as soon as the request has ended', async () => {
    const fixture = await render();
    fixture.componentInstance.busy.set(true);
    await settle(fixture);
    press('Escape');
    expect(fixture.componentInstance.open()).toBe(true);

    fixture.componentInstance.busy.set(false);
    await settle(fixture);
    press('Escape');
    await settle(fixture);

    expect(fixture.componentInstance.open()).toBe(false);
  });

  it('stops only Escape: another key still reaches whoever listens for it', async () => {
    const fixture = await render();
    fixture.componentInstance.busy.set(true);
    await settle(fixture);
    const seen = vi.fn<(event: KeyboardEvent) => void>();
    document.addEventListener('keydown', seen);

    press('a');
    press('Enter');
    press('Escape');
    document.removeEventListener('keydown', seen);

    expect(seen.mock.calls.map(([event]) => event.key)).toEqual(['a', 'Enter']);
  });

  it('is what the cross and the click beside the dialog rely on to be read at each use: both are off while busy', async () => {
    const fixture = await render();
    expect(document.querySelector('.p-dialog-close-button')).not.toBeNull();
    fixture.componentInstance.busy.set(true);
    await settle(fixture);

    expect(document.querySelector('.p-dialog-close-button')).toBeNull();
    mask()?.dispatchEvent(new MouseEvent('mousedown', { bubbles: true }));
    await settle(fixture);
    expect(fixture.componentInstance.open()).toBe(true);

    fixture.componentInstance.busy.set(false);
    await settle(fixture);
    expect(document.querySelector('.p-dialog-close-button')).not.toBeNull();
    mask()?.dispatchEvent(new MouseEvent('mousedown', { bubbles: true }));
    await settle(fixture);
    expect(fixture.componentInstance.open()).toBe(false);
  });

  it('takes its listener off the document when the component goes', async () => {
    const remove = vi.spyOn(document, 'removeEventListener');
    try {
      const fixture = await render();

      fixture.destroy();

      expect(remove).toHaveBeenCalledWith('keydown', expect.any(Function), true);
    } finally {
      remove.mockRestore();
    }
  });
});
