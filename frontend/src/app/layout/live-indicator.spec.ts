import { TestBed } from '@angular/core/testing';
import { LiveIndicator } from './live-indicator';

describe('LiveIndicator', () => {
  async function render(status: 'idle' | 'connecting' | 'live' | 'polling') {
    const fixture = TestBed.createComponent(LiveIndicator);
    fixture.componentRef.setInput('status', status);
    await fixture.whenStable();
    return fixture.nativeElement as HTMLElement;
  }

  it('shows nothing outside a tenant', async () => {
    expect((await render('idle')).querySelector('[data-testid="live-indicator"]')).toBeNull();
  });

  it.each([
    ['live', 'Live'],
    ['connecting', 'Connecting'],
    ['polling', 'Polling'],
  ] as const)('names the %s state', async (status, text) => {
    const indicator = (await render(status)).querySelector('[data-testid="live-indicator"]');
    expect(indicator?.textContent?.trim()).toBe(text);
    expect(indicator?.getAttribute('data-status')).toBe(status);
  });
});
