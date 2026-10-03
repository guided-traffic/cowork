import { DOCUMENT } from '@angular/common';
import { TestBed } from '@angular/core/testing';
import { darkClass, ThemePreference, ThemeService } from './theme.service';

const storageKey = 'cowork.theme';

/** The part of a MediaQueryList the service uses, with a way to flip it as the operating system does. */
class FakeMediaQueryList {
  readonly listeners: ((event: { matches: boolean }) => void)[] = [];

  constructor(public matches: boolean) {}

  addEventListener(type: string, listener: (event: { matches: boolean }) => void): void {
    if (type === 'change') {
      this.listeners.push(listener);
    }
  }

  /** The operating system switched its colour scheme. */
  switchTo(dark: boolean): void {
    this.matches = dark;
    this.listeners.forEach((listener) => listener({ matches: dark }));
  }
}

/** jsdom has no matchMedia; this one answers every query with the given list. */
function stubMatchMedia(media: FakeMediaQueryList): string[] {
  const queries: string[] = [];
  Object.defineProperty(window, 'matchMedia', {
    configurable: true,
    writable: true,
    value: (query: string) => {
      queries.push(query);
      return media;
    },
  });
  return queries;
}

describe('ThemeService', () => {
  const html = () => document.documentElement;
  const isDark = () => html().classList.contains(darkClass);
  const stored = () => localStorage.getItem(storageKey);

  /** Creates the service, and with it the effect that follows the scheme. */
  const create = () => {
    const service = TestBed.inject(ThemeService);
    TestBed.tick();
    return service;
  };

  afterEach(() => {
    html().classList.remove(darkClass);
    localStorage.clear();
    Reflect.deleteProperty(window, 'matchMedia');
    vi.restoreAllMocks();
  });

  it('puts the dark class where docs/adr/0052 D3 names it', () => {
    expect(darkClass).toBe('app-dark');
  });

  describe('the stored preference', () => {
    it('is system when nothing is stored', () => {
      expect(create().preference()).toBe('system');
    });

    it.each(['system', 'light', 'dark'] as const)(
      'is %s when that is what is stored',
      (preference) => {
        localStorage.setItem(storageKey, preference);

        expect(create().preference()).toBe(preference);
      },
    );

    it.each(['purple', '', 'DARK', 'null', ' dark'])(
      'is system when the stored value is %j',
      (value) => {
        localStorage.setItem(storageKey, value);

        expect(create().preference()).toBe('system');
      },
    );

    it('is system when the storage refuses to be read', () => {
      vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
        throw new DOMException('denied', 'SecurityError');
      });

      expect(create().preference()).toBe('system');
    });
  });

  describe('set', () => {
    it('takes the preference and keeps it in the browser storage', () => {
      const service = create();

      service.set('dark');

      expect(service.preference()).toBe('dark');
      expect(stored()).toBe('dark');
    });

    it.each(['light', 'dark', 'system'] as const)(
      'stores %s under the key cowork.theme',
      (preference) => {
        create().set(preference);

        expect(stored()).toBe(preference);
      },
    );

    it('is read again by the next page that starts', () => {
      create().set('light');
      TestBed.resetTestingModule();

      expect(create().preference()).toBe('light');
    });

    it('holds for this page when the storage refuses to be written', () => {
      vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
        throw new DOMException('quota', 'QuotaExceededError');
      });
      const service = create();

      expect(() => service.set('dark')).not.toThrow();
      TestBed.tick();

      expect(service.preference()).toBe('dark');
      expect(service.scheme()).toBe('dark');
      expect(isDark()).toBe(true);
    });
  });

  describe('cycle', () => {
    it('goes system, light, dark, and round again, storing each step', () => {
      const service = create();
      const seen: (ThemePreference | null)[] = [];

      for (let i = 0; i < 4; i++) {
        service.cycle();
        seen.push(service.preference());
        expect(stored()).toBe(service.preference());
      }

      expect(seen).toEqual(['light', 'dark', 'system', 'light']);
    });

    it('goes on from the stored preference', () => {
      localStorage.setItem(storageKey, 'dark');
      const service = create();

      service.cycle();

      expect(service.preference()).toBe('system');
    });
  });

  describe('the dark class on <html>', () => {
    it('is not there for light', () => {
      localStorage.setItem(storageKey, 'light');

      create();

      expect(isDark()).toBe(false);
    });

    it('is there for dark', () => {
      localStorage.setItem(storageKey, 'dark');

      create();

      expect(isDark()).toBe(true);
    });

    it('is set as soon as the service exists, before any effect has run, so the page does not flash', () => {
      localStorage.setItem(storageKey, 'dark');

      TestBed.inject(ThemeService);

      expect(isDark()).toBe(true);
    });

    it('follows the preference as it changes', () => {
      const service = create();
      expect(isDark()).toBe(false);

      service.set('dark');
      TestBed.tick();
      expect(isDark()).toBe(true);

      service.set('light');
      TestBed.tick();
      expect(isDark()).toBe(false);
    });

    it('leaves other classes of <html> alone', () => {
      html().classList.add('keep-me');
      try {
        const service = create();

        service.set('dark');
        TestBed.tick();
        service.set('light');
        TestBed.tick();

        expect(html().classList.contains('keep-me')).toBe(true);
      } finally {
        html().classList.remove('keep-me');
      }
    });
  });

  describe('the scheme', () => {
    it('is the one the person fixed', () => {
      const service = create();

      service.set('dark');
      expect(service.scheme()).toBe('dark');

      service.set('light');
      expect(service.scheme()).toBe('light');
    });

    describe('while the preference is system', () => {
      it('is light when the browser cannot tell, as in a browser without matchMedia', () => {
        const service = create();

        expect(service.preference()).toBe('system');
        expect(service.scheme()).toBe('light');
        expect(isDark()).toBe(false);
      });

      it('asks the browser for prefers-color-scheme: dark', () => {
        const queries = stubMatchMedia(new FakeMediaQueryList(false));

        create();

        expect(queries).toEqual(['(prefers-color-scheme: dark)']);
      });

      it('is dark when the operating system is dark', () => {
        stubMatchMedia(new FakeMediaQueryList(true));

        const service = create();

        expect(service.scheme()).toBe('dark');
        expect(isDark()).toBe(true);
      });

      it('is light when the operating system is light', () => {
        stubMatchMedia(new FakeMediaQueryList(false));

        const service = create();

        expect(service.scheme()).toBe('light');
        expect(isDark()).toBe(false);
      });

      it('follows the operating system live', () => {
        const media = new FakeMediaQueryList(false);
        stubMatchMedia(media);
        const service = create();

        media.switchTo(true);
        TestBed.tick();
        expect(service.scheme()).toBe('dark');
        expect(isDark()).toBe(true);

        media.switchTo(false);
        TestBed.tick();
        expect(service.scheme()).toBe('light');
        expect(isDark()).toBe(false);
      });

      it('follows the operating system again after a fixed choice was given up', () => {
        const media = new FakeMediaQueryList(true);
        stubMatchMedia(media);
        const service = create();
        service.set('light');
        TestBed.tick();
        expect(isDark()).toBe(false);

        service.set('system');
        TestBed.tick();

        expect(service.scheme()).toBe('dark');
        expect(isDark()).toBe(true);
      });
    });

    describe('while the person fixed a scheme', () => {
      it.each([
        ['light', true, 'light'],
        ['dark', false, 'dark'],
      ] as const)(
        'is %s whatever the operating system says (it says dark: %s)',
        (preference, osDark, scheme) => {
          localStorage.setItem(storageKey, preference);
          const media = new FakeMediaQueryList(osDark);
          stubMatchMedia(media);
          const service = create();

          expect(service.scheme()).toBe(scheme);

          media.switchTo(!osDark);
          TestBed.tick();
          expect(service.scheme()).toBe(scheme);
          expect(isDark()).toBe(scheme === 'dark');
        },
      );
    });
  });

  describe('without a window', () => {
    it('still works, with the system preference and no storage', () => {
      const detached = document.implementation.createHTMLDocument('detached');
      TestBed.configureTestingModule({ providers: [{ provide: DOCUMENT, useValue: detached }] });
      const service = TestBed.inject(ThemeService);

      expect(detached.defaultView).toBeNull();
      expect(service.preference()).toBe('system');
      expect(service.scheme()).toBe('light');

      expect(() => service.set('dark')).not.toThrow();
      TestBed.tick();

      expect(service.scheme()).toBe('dark');
      expect(detached.documentElement.classList.contains(darkClass)).toBe(true);
    });
  });
});
