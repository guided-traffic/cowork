import { createEnvironmentInjector, EnvironmentInjector } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { Theme } from '@primeuix/themes';
import { PRIME_NG_CONFIG, PrimeNG, PrimeNGConfigType } from 'primeng/config';
import { CoworkPreset } from './cowork-preset';
import { primeuiLicense, provideCoworkPrimeNG } from './primeng';
import { darkClass } from './theme.service';

/** The configuration the providers hand PrimeNG, read without starting an application. */
function configOf(): PrimeNGConfigType {
  const injector = createEnvironmentInjector(
    [provideCoworkPrimeNG()],
    TestBed.inject(EnvironmentInjector),
  );
  try {
    return injector.get(PRIME_NG_CONFIG);
  } finally {
    injector.destroy();
  }
}

describe('primeuiLicense', () => {
  afterEach(() => vi.unstubAllGlobals());

  it('is undefined when the build put no license in, as in the tests', () => {
    expect(primeuiLicense()).toBeUndefined();
  });

  it('is the key that ng build --define put in (docs/adr/0052 D9)', () => {
    vi.stubGlobal('PRIMEUI_LICENSE', 'the-key');

    expect(primeuiLicense()).toBe('the-key');
  });

  it.each([
    ['empty', ''],
    ['not a string', 42],
    ['undefined', undefined],
  ])('is undefined when the defined key is %s', (_description, value) => {
    vi.stubGlobal('PRIMEUI_LICENSE', value);

    expect(primeuiLicense()).toBeUndefined();
  });
});

describe('provideCoworkPrimeNG', () => {
  afterEach(() => vi.unstubAllGlobals());

  it('hands PrimeNG cowork preset, with dark mode on the class the theme service sets', () => {
    const config = configOf();
    const theme = config.theme as { preset: unknown; options: { darkModeSelector: string } };

    expect(theme.preset).toBe(CoworkPreset);
    expect(theme.options.darkModeSelector).toBe(`.${darkClass}`);
    expect(theme.options.darkModeSelector).toBe('.app-dark');
  });

  it('switches the ripple off', () => {
    expect(configOf().ripple).toBe(false);
  });

  it('carries no license when the build put none in', () => {
    expect(configOf().license).toBeUndefined();
  });

  it('carries the license the build put in', () => {
    vi.stubGlobal('PRIMEUI_LICENSE', 'the-key');

    expect(configOf().license).toBe('the-key');
  });

  describe('when the application starts', () => {
    afterEach(() => {
      document.getElementById('p-license-host')?.remove();
      vi.restoreAllMocks();
    });

    it('applies the configuration to PrimeNG and to the theme, and without a license shows its notice', async () => {
      const warn = vi.spyOn(console, 'warn').mockImplementation(() => undefined);
      TestBed.configureTestingModule({ providers: [provideCoworkPrimeNG()] });

      const primeng = TestBed.inject(PrimeNG);
      // PrimeNG hands the theme on in an effect.
      TestBed.tick();

      expect(primeng.ripple()).toBe(false);
      expect(Theme.getPreset()).toBe(CoworkPreset);
      expect(Theme.getOptions().darkModeSelector).toBe(`.${darkClass}`);
      // The check of the license is a promise; its notice is the warning and the banner (docs/adr/0052 D9).
      await vi.waitFor(() =>
        expect(warn).toHaveBeenCalledWith(expect.stringContaining('[PrimeUI]')),
      );
    });
  });
});
