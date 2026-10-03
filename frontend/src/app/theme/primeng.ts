import { EnvironmentProviders } from '@angular/core';
import { providePrimeNG } from 'primeng/config';
import { CoworkPreset } from './cowork-preset';
import { darkClass } from './theme.service';

/**
 * The PrimeUI Community License key (docs/adr/0052 D9), put in at build time by
 * `ng build --define` and `ng serve --define` from a secret and never kept in the repository.
 * Without it PrimeNG works and shows its license notice.
 */
declare const PRIMEUI_LICENSE: string | undefined;

export function primeuiLicense(): string | undefined {
  return typeof PRIMEUI_LICENSE === 'string' && PRIMEUI_LICENSE !== ''
    ? PRIMEUI_LICENSE
    : undefined;
}

/** PrimeNG with cowork's preset; the dark scheme is the `.app-dark` class (docs/adr/0052 D2, D3). */
export function provideCoworkPrimeNG(): EnvironmentProviders {
  return providePrimeNG({
    theme: { preset: CoworkPreset, options: { darkModeSelector: `.${darkClass}` } },
    ripple: false,
    license: primeuiLicense(),
  });
}
