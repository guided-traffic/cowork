import { ChangeDetectionStrategy, Component, ElementRef, inject, Injector } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { refocus } from './refocus';

@Component({
  selector: 'app-host',
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <h1 tabindex="-1">Heading</h1>
    <button type="button" class="first">First</button>
    <button type="button" class="second">Second</button>
  `,
})
class Host {
  readonly element = inject<ElementRef<HTMLElement>>(ElementRef).nativeElement;
  readonly injector = inject(Injector);
}

describe('refocus', () => {
  let fixture: ComponentFixture<Host>;

  beforeEach(() => {
    fixture = TestBed.createComponent(Host);
    fixture.detectChanges();
  });

  async function rendered() {
    fixture.detectChanges();
    await fixture.whenStable();
  }

  const go = (...targets: string[]) =>
    refocus(fixture.componentInstance.element, fixture.componentInstance.injector, ...targets);

  it('puts the keyboard on the first target that is there, once the page has rendered', async () => {
    go('.gone', '.second', '.first');
    expect(document.activeElement).toBe(document.body);

    await rendered();

    expect(document.activeElement?.textContent).toBe('Second');
  });

  it('takes a heading the page made focusable as the last resort', async () => {
    go('.gone', 'h1');

    await rendered();

    expect(document.activeElement?.tagName).toBe('H1');
  });

  it('leaves the focus where it is when no target is there', async () => {
    fixture.nativeElement.querySelector('.first').focus();

    go('.gone', '.also-gone');
    await rendered();

    expect(document.activeElement?.textContent).toBe('First');
  });

  it('looks only inside the host', async () => {
    const outside = document.createElement('button');
    outside.className = 'outside';
    document.body.prepend(outside);

    go('.outside', '.second');
    await rendered();

    expect(document.activeElement?.textContent).toBe('Second');
    outside.remove();
  });
});
