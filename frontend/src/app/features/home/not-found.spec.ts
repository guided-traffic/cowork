import { TestBed } from '@angular/core/testing';
import { provideRouter } from '@angular/router';
import { NotFound } from './not-found';

describe('NotFound', () => {
  it('says the page does not exist and leads back to the start', async () => {
    TestBed.configureTestingModule({ providers: [provideRouter([])] });
    const fixture = TestBed.createComponent(NotFound);
    await fixture.whenStable();
    const page = (fixture.nativeElement as HTMLElement).querySelector('[data-testid="not-found"]');

    expect(page?.querySelector('h1')?.textContent).toBe('Nothing here');
    expect(page?.textContent).toContain('This page does not exist, or you cannot see it.');
    const back = page?.querySelector('a');
    expect(back?.textContent).toBe('Back to the start');
    expect(back?.getAttribute('href')).toBe('/');
  });
});
