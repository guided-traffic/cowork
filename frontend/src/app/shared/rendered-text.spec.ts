import { TestBed } from '@angular/core/testing';
import { RenderedText } from './rendered-text';

/**
 * The second line of docs/adr/0011 D6: what the server rendered is bound as a string, so Angular's
 * own sanitiser runs over it; nothing marks it trusted. HTML the server would never send shows what
 * that line still removes.
 */
describe('RenderedText', () => {
  function render(html: string): HTMLElement {
    const fixture = TestBed.createComponent(RenderedText);
    fixture.componentRef.setInput('html', html);
    fixture.detectChanges();
    return (fixture.nativeElement as HTMLElement).querySelector('.rendered') as HTMLElement;
  }

  it('shows what the server allows', () => {
    const shown = render(
      '<h2>State</h2><p><strong>a</strong> <a href="https://e.com" rel="noopener noreferrer nofollow" target="_blank">b</a></p>' +
        '<table><thead><tr><th align="left">x</th></tr></thead></table><img src="/api/v1/x/content" alt="shot">',
    );

    expect(shown.querySelector('h2')?.textContent).toBe('State');
    const link = shown.querySelector('a') as HTMLAnchorElement;
    expect(link.getAttribute('rel')).toBe('noopener noreferrer nofollow');
    expect(link.getAttribute('target')).toBe('_blank');
    expect(shown.querySelector('th')?.getAttribute('align')).toBe('left');
    expect(shown.querySelector('img')?.getAttribute('alt')).toBe('shot');
  });

  it("keeps Angular's sanitiser on: a script, a handler and a javascript: address do not run", () => {
    const shown = render(
      '<p onclick="alert(1)">a</p><script>alert(2)</script><a href="javascript:alert(3)">c</a>' +
        '<img src="x" onerror="alert(4)"><iframe src="https://evil.example"></iframe>',
    );

    expect(shown.querySelector('p')?.getAttribute('onclick')).toBeNull();
    expect(shown.querySelector('script')).toBeNull();
    expect(shown.querySelector('a')?.getAttribute('href')).toMatch(/^unsafe:/);
    expect(shown.querySelector('img')?.getAttribute('onerror')).toBeNull();
    expect(shown.querySelector('iframe')).toBeNull();
  });
});
