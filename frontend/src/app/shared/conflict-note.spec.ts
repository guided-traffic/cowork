import { TestBed } from '@angular/core/testing';
import { ConflictNote } from './conflict-note';

describe('ConflictNote', () => {
  function render(busy = false) {
    const fixture = TestBed.createComponent(ConflictNote);
    fixture.componentRef.setInput('what', 'The description');
    fixture.componentRef.setInput('busy', busy);
    fixture.detectChanges();
    const page = fixture.nativeElement as HTMLElement;
    const button = (testId: string) =>
      page.querySelector<HTMLButtonElement>(`[data-testid="${testId}"]`) as HTMLButtonElement;
    return { fixture, page, button };
  }

  it('says what changed while it was edited, and offers the two ways on', () => {
    const { page, button } = render();

    expect(page.textContent).toContain('The description changed while you edited it.');
    expect(button('conflict-overwrite').textContent?.trim()).toBe('Write mine over it');
    expect(button('conflict-take-theirs').textContent?.trim()).toBe('Take the new version');
  });

  it('says which way the person chose', () => {
    const { fixture, button } = render();
    const overwrite = vi.fn();
    const takeTheirs = vi.fn();
    fixture.componentInstance.overwrite.subscribe(overwrite);
    fixture.componentInstance.takeTheirs.subscribe(takeTheirs);

    button('conflict-overwrite').click();
    button('conflict-take-theirs').click();

    expect(overwrite).toHaveBeenCalledOnce();
    expect(takeTheirs).toHaveBeenCalledOnce();
  });

  it('takes no choice while a write is on its way', () => {
    const { button } = render(true);

    expect(button('conflict-overwrite').disabled).toBe(true);
    expect(button('conflict-take-theirs').disabled).toBe(true);
  });
});
