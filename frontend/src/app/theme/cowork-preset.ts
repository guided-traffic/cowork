import { definePreset } from '@primeuix/themes';
import Aura from '@primeuix/themes/aura';

/**
 * The colours of the logo (docs/adr/0052 D2, D8): its gradient from teal to gold and the ink it
 * is filled with. Everything else in the palette is derived from them.
 */
export const logoColours = {
  teal: '#70e6ce',
  violet: '#6e51eb',
  purple: '#a63cd8',
  magenta: '#d233b8',
  coral: '#db6a68',
  amber: '#e7a666',
  gold: '#f2d672',
  ink: '#160941',
} as const;

/** The logo's border gradient, also the one accent the UI draws with it. */
export const logoGradient =
  'linear-gradient(135deg, #70e6ce 0%, #6e51eb 22%, #a63cd8 40%, #d233b8 56%, #db6a68 72%, #e7a666 86%, #f2d672 100%)';

/** The primary scale: the logo's violet as 500 and its ink as 950. */
const violet = {
  50: '#f2f0ff',
  100: '#e6e1fe',
  200: '#cfc4fd',
  300: '#b09ff9',
  400: '#8e77f3',
  500: '#6e51eb',
  600: '#5636d9',
  700: '#4a2cba',
  800: '#3f2791',
  900: '#321e6b',
  950: '#160941',
};

/** The dark surfaces: a near-black tinted toward the logo's ink. */
const ink = {
  50: '#f6f5fa',
  100: '#eae9f1',
  200: '#d4d3df',
  300: '#b1afc0',
  400: '#89869c',
  500: '#666379',
  600: '#4a485b',
  700: '#343243',
  800: '#232131',
  900: '#161421',
  950: '#0d0b16',
};

const steps = [50, 100, 200, 300, 400, 500, 600, 700, 800, 900, 950] as const;

/** Light uses Aura's slate, dark the ink scale. */
function surfaces(): Record<number, string> {
  return {
    0: '#ffffff',
    ...Object.fromEntries(steps.map((step) => [step, `light-dark({slate.${step}}, ${ink[step]})`])),
  };
}

/**
 * The accent of each value a badge shows, for the light and the dark scheme. Every pair passes
 * WCAG AA as text: the light one on white, the dark one on the ink-900 card. A badge derives its
 * background from the accent, so one token per value is enough.
 */
const accents = {
  severity: {
    critical: 'light-dark(#c8234f, #ff7d97)',
    high: 'light-dark(#c2540f, #ff9f6e)',
    medium: 'light-dark(#9a6a00, #f2c96b)',
    low: 'light-dark(#0f7a65, #70e6ce)',
    cosmetic: 'light-dark(#666379, #89869c)',
  },
  security: {
    live: 'light-dark(#b0219b, #f07ad9)',
    boundary: 'light-dark(#7a33c4, #c39af2)',
    hardening: 'light-dark(#3f55c9, #9eacff)',
  },
  state: {
    filed: 'light-dark(#666379, #89869c)',
    analysed: 'light-dark(#1f6fb2, #7dc4ff)',
    decided: 'light-dark(#5636d9, #b09ff9)',
    inProgress: 'light-dark(#a35f00, #f5c46b)',
    review: 'light-dark(#4d7c0f, #bef264)',
    blocked: 'light-dark(#c2410c, #ff9466)',
    done: 'light-dark(#0f7a65, #70e6ce)',
    dropped: 'light-dark(#666379, #89869c)',
  },
};

/**
 * cowork's preset over Aura (docs/adr/0052 D2): the primary scale and the dark surfaces from the
 * logo, and the badge accents as tokens. Every value is a `light-dark()` pair, so the scheme is
 * the `color-scheme` the `.app-dark` class sets (D3).
 */
export const CoworkPreset = definePreset(Aura, {
  semantic: {
    primary: {
      ...violet,
      contrastColor: 'light-dark(#ffffff, {primary.950})',
    },
    surface: surfaces(),
  },
  extend: {
    /** The layers of the page: the ground, the panels on it, their borders. */
    app: {
      ground: 'light-dark({surface.50}, {surface.950})',
      panel: 'light-dark({surface.0}, {surface.900})',
      raised: 'light-dark({surface.0}, {surface.800})',
      border: 'light-dark({surface.200}, {surface.800})',
      hover: 'light-dark({surface.100}, {surface.800})',
    },
    brand: {
      gradient: logoGradient,
      ink: logoColours.ink,
      inkHighlight: '#2b1673',
      glyph: '#ffffff',
      glow: 'rgba(122, 60, 200, 0.38)',
    },
    ...accents,
  },
});
