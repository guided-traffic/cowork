import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { TestBed } from '@angular/core/testing';
import { provideApiConfiguration } from '../api/api-configuration';
import { keepAliveEvery, KeepAliveService, workInputs } from './keep-alive.service';

function setVisibility(state: DocumentVisibilityState): void {
  Object.defineProperty(document, 'visibilityState', { configurable: true, get: () => state });
  document.dispatchEvent(new Event('visibilitychange'));
}

describe('KeepAliveService (docs/adr/0031 D3)', () => {
  let service: KeepAliveService;
  let http: HttpTestingController;

  const minutes = (n: number) => vi.advanceTimersByTimeAsync(n * 60_000);
  /** The person works in the page: a key, unless the test says what. */
  const work = (name = 'keydown') =>
    document.body.dispatchEvent(new Event(name, { bubbles: true }));
  /** The one request the keep-alive makes, answered as a live session answers it. */
  const asked = () => {
    const request = http.expectOne('/api/v1/me');
    expect(request.request.method).toBe('GET');
    request.flush({});
  };

  beforeEach(() => {
    vi.useFakeTimers();
    TestBed.configureTestingModule({
      providers: [provideHttpClient(), provideHttpClientTesting(), provideApiConfiguration('')],
    });
    service = TestBed.inject(KeepAliveService);
    http = TestBed.inject(HttpTestingController);
  });

  afterEach(() => {
    try {
      http.verify();
    } finally {
      TestBed.resetTestingModule();
      Reflect.deleteProperty(document, 'visibilityState');
      vi.restoreAllMocks();
      vi.useRealTimers();
    }
  });

  it('asks at most every five minutes', () => {
    expect(keepAliveEvery).toBe(5 * 60_000);
  });

  it('takes a pointer pressed, a key, the wheel and a touch for work', () => {
    expect(workInputs).toEqual(['pointerdown', 'keydown', 'wheel', 'touchstart']);
  });

  it('asks nothing while nobody works in the page: an open tab reaches the idle limit', async () => {
    service.start();

    await minutes(60);

    http.expectNone('/api/v1/me');
  });

  it('asks once at the end of the interval in which the person worked, not at once', async () => {
    service.start();
    await minutes(1);
    work();
    http.expectNone('/api/v1/me');

    await minutes(3);
    http.expectNone('/api/v1/me');
    await minutes(1);

    asked();
  });

  it('asks once per interval however much the person works, and stops asking when they stop', async () => {
    service.start();
    for (let minute = 0; minute < 10; minute++) {
      work();
      work('pointerdown');
      await minutes(1);
      if (minute === 4 || minute === 9) {
        asked();
      }
    }

    await minutes(15);

    http.expectNone('/api/v1/me');
  });

  it.each(workInputs)('takes %s for work', async (name) => {
    service.start();

    work(name);
    await minutes(5);

    asked();
  });

  it('takes a pointer that only moves for no work', async () => {
    service.start();

    work('pointermove');
    await minutes(5);

    http.expectNone('/api/v1/me');
  });

  it('takes no input from before its start for work', async () => {
    work();

    service.start();
    await minutes(5);

    http.expectNone('/api/v1/me');
  });

  it('asks nothing while the document is hidden, and at the next interval once it is visible again', async () => {
    service.start();
    work();
    setVisibility('hidden');

    await minutes(5);
    http.expectNone('/api/v1/me');

    setVisibility('visible');
    await minutes(5);
    asked();
  });

  it('takes a failed answer calmly and asks again after the next work', async () => {
    service.start();
    work();
    await minutes(5);
    http.expectOne('/api/v1/me').flush('gone', { status: 401, statusText: 'Unauthorized' });
    await minutes(5);
    http.expectNone('/api/v1/me');

    work();
    await minutes(5);

    asked();
  });

  it('runs once, however often it is started', async () => {
    const timers = vi.getTimerCount();

    service.start();
    service.start();
    work();
    await minutes(5);

    expect(vi.getTimerCount()).toBe(timers + 1);
    asked();
  });

  it('stops asking and listening when it is stopped', async () => {
    const timers = vi.getTimerCount();
    const removed = vi.spyOn(document, 'removeEventListener');
    service.start();
    work();

    service.stop();
    await minutes(10);

    http.expectNone('/api/v1/me');
    expect(vi.getTimerCount()).toBe(timers);
    for (const name of workInputs) {
      expect(removed).toHaveBeenCalledWith(name, expect.any(Function), true);
    }
  });

  it('may start again after it stopped', async () => {
    service.start();
    service.stop();

    service.start();
    work();
    await minutes(5);

    asked();
  });

  it('stops when its injector is destroyed', () => {
    const timers = vi.getTimerCount();
    service.start();
    expect(vi.getTimerCount()).toBe(timers + 1);

    TestBed.resetTestingModule();

    expect(vi.getTimerCount()).toBe(timers);
  });
});
