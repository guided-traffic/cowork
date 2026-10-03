import { Theme, ThemeUtils } from '@primeuix/themes';
import { meanings } from '../shared/vocabulary';
import { CoworkPreset, logoColours, logoGradient } from './cowork-preset';
import { darkClass } from './theme.service';

type Tokens = Record<string, string>;

const semantic = CoworkPreset.semantic as unknown as { primary: Tokens; surface: Tokens };
const extend = CoworkPreset.extend as unknown as Record<string, Tokens>;

const steps = ['50', '100', '200', '300', '400', '500', '600', '700', '800', '900', '950'];

/** The two halves of a light-dark() pair of plain colours. */
function pair(value: string): { light: string; dark: string } {
  const match = /^light-dark\(\s*([^,]+?)\s*,\s*([^)]+?)\s*\)$/.exec(value);
  if (!match) {
    throw new Error(`not a light-dark() pair: ${value}`);
  }
  return { light: match[1], dark: match[2] };
}

function luminance(hex: string): number {
  const channel = (offset: number) => {
    const value = parseInt(hex.slice(offset, offset + 2), 16) / 255;
    return value <= 0.03928 ? value / 12.92 : ((value + 0.055) / 1.055) ** 2.4;
  };
  return 0.2126 * channel(1) + 0.7152 * channel(3) + 0.0722 * channel(5);
}

/** The WCAG contrast ratio of two colours. */
function contrast(a: string, b: string): number {
  const [lighter, darker] = [luminance(a), luminance(b)].sort((x, y) => y - x);
  return (lighter + 0.05) / (darker + 0.05);
}

/** What PrimeNG writes into the page for the preset: the variables of its semantic tokens. */
function variables(): string {
  const common = ThemeUtils.getCommon({
    theme: { preset: CoworkPreset, options: { darkModeSelector: `.${darkClass}` } },
    defaults: Theme.defaults,
  });
  return common.global.css ?? '';
}

describe('the colours of the logo', () => {
  it('are the teal-to-gold gradient and the ink of docs/adr/0052 D8', () => {
    expect(logoColours).toEqual({
      teal: '#70e6ce',
      violet: '#6e51eb',
      purple: '#a63cd8',
      magenta: '#d233b8',
      coral: '#db6a68',
      amber: '#e7a666',
      gold: '#f2d672',
      ink: '#160941',
    });
  });

  it('run through the gradient from teal at 0 percent to gold at 100 percent, in the order of the logo', () => {
    const { teal, violet, purple, magenta, coral, amber, gold } = logoColours;
    const stops = [...logoGradient.matchAll(/(#[0-9a-f]{6}) (\d+)%/g)].map((match) => [
      match[1],
      Number(match[2]),
    ]);

    expect(logoGradient.startsWith('linear-gradient(135deg, ')).toBe(true);
    expect(stops.map(([colour]) => colour)).toEqual([
      teal,
      violet,
      purple,
      magenta,
      coral,
      amber,
      gold,
    ]);
    expect(stops[0][1]).toBe(0);
    expect(stops[stops.length - 1][1]).toBe(100);
    expect(stops.map(([, position]) => position)).toEqual(
      [...stops.map(([, position]) => position)].sort((a, b) => Number(a) - Number(b)),
    );
  });
});

describe('the primary scale', () => {
  it('has the eleven steps of a PrimeNG palette', () => {
    expect(steps.every((step) => /^#[0-9a-f]{6}$/.test(semantic.primary[step]))).toBe(true);
  });

  it('is the violet of the logo at 500 and its ink at 950', () => {
    expect(semantic.primary['500']).toBe(logoColours.violet);
    expect(semantic.primary['950']).toBe(logoColours.ink);
  });

  it('gets darker with every step', () => {
    const lightness = steps.map((step) => luminance(semantic.primary[step]));

    expect(lightness).toEqual([...lightness].sort((a, b) => b - a));
    expect(new Set(lightness).size).toBe(steps.length);
  });

  it('puts white on the primary colour in light and its ink in dark', () => {
    expect(pair(semantic.primary['contrastColor'])).toEqual({
      light: '#ffffff',
      dark: '{primary.950}',
    });
  });
});

describe('the surfaces', () => {
  it('are white at 0 in both schemes', () => {
    expect(semantic.surface['0']).toBe('#ffffff');
  });

  it('take the slate of Aura in light and the ink-tinted scale in dark, step by step', () => {
    for (const step of steps) {
      expect(semantic.surface[step]).toMatch(
        new RegExp(`^light-dark\\(\\{slate\\.${step}\\}, #[0-9a-f]{6}\\)$`),
      );
    }
  });

  it('get darker with every step in dark too, from near-white to near-black', () => {
    const dark = steps.map((step) => luminance(pair(semantic.surface[step]).dark));

    expect(dark).toEqual([...dark].sort((a, b) => b - a));
    expect(new Set(dark).size).toBe(steps.length);
    expect(dark[dark.length - 1]).toBeLessThan(0.01);
  });
});

describe('the tokens that extend the preset', () => {
  it('are the layers of the page, the brand, and the accents of severity, security and state', () => {
    expect(Object.keys(extend).sort()).toEqual(['app', 'brand', 'security', 'severity', 'state']);
  });

  it('build every layer of the page as a light-dark() pair of surfaces', () => {
    expect(Object.keys(extend['app']).sort()).toEqual([
      'border',
      'ground',
      'hover',
      'panel',
      'raised',
    ]);
    for (const value of Object.values(extend['app'])) {
      expect(value).toMatch(/^light-dark\(\{surface\.\d+\}, \{surface\.\d+\}\)$/);
    }
  });

  it('carry the logo as the brand: its gradient, its ink, a white glyph and a violet glow', () => {
    expect(extend['brand']['gradient']).toBe(logoGradient);
    expect(extend['brand']['ink']).toBe(logoColours.ink);
    expect(extend['brand']['inkHighlight']).toMatch(/^#[0-9a-f]{6}$/);
    expect(extend['brand']['glyph']).toBe('#ffffff');
    expect(extend['brand']['glow']).toMatch(/^rgba\(\d+, \d+, \d+, 0?\.\d+\)$/);
  });

  describe.each([
    ['severity', Object.keys(meanings.severity)],
    ['security', Object.keys(meanings.security).filter((value) => value !== 'none')],
    [
      'state',
      Object.keys(meanings.state).map((value) =>
        value.replace(/-(\w)/g, (_, c: string) => c.toUpperCase()),
      ),
    ],
  ])('the %s accents', (group, values) => {
    it('name every value of the vocabulary, and none of its own (a security class of none gets no badge)', () => {
      expect(Object.keys(extend[group]).sort()).toEqual([...values].sort());
    });

    it('are a light-dark() pair of plain colours each', () => {
      for (const value of Object.values(extend[group])) {
        const { light, dark } = pair(value);
        expect(light).toMatch(/^#[0-9a-f]{6}$/);
        expect(dark).toMatch(/^#[0-9a-f]{6}$/);
      }
    });

    it('pass WCAG AA as text: the light one on white, the dark one on the card of the dark scheme', () => {
      const card = pair(semantic.surface['900']).dark;

      for (const [name, value] of Object.entries(extend[group])) {
        const { light, dark } = pair(value);
        expect(contrast(light, '#ffffff'), `${group}.${name} in light`).toBeGreaterThanOrEqual(4.5);
        expect(contrast(dark, card), `${group}.${name} in dark`).toBeGreaterThanOrEqual(4.5);
      }
    });
  });
});

describe('the CSS variables PrimeNG writes for the preset', () => {
  it('define the accent that each badge reads, spelt as the API spells the value (docs/adr/0055 D4)', () => {
    const css = variables();
    const states = Object.keys(meanings.state).map((state) => `--p-state-${state}:`);
    const severities = Object.keys(meanings.severity).map(
      (severity) => `--p-severity-${severity}:`,
    );
    const securities = Object.keys(meanings.security)
      .filter((security) => security !== 'none')
      .map((security) => `--p-security-${security}:`);

    for (const name of [...states, ...severities, ...securities]) {
      expect(css, name).toContain(name);
    }
  });

  it('write in-progress with its hyphen, which is the variable the state badge asks for', () => {
    expect(variables()).toContain('--p-state-in-progress:light-dark(#a35f00, #f5c46b)');
  });

  it('define the brand and the layers of the page', () => {
    const css = variables();

    for (const name of ['gradient', 'ink', 'ink-highlight', 'glyph', 'glow']) {
      expect(css, name).toContain(`--p-brand-${name}:`);
    }
    for (const name of ['ground', 'panel', 'raised', 'border', 'hover']) {
      expect(css, name).toContain(`--p-app-${name}:`);
    }
  });

  it('set the colour scheme that the light-dark() pairs resolve against: light on the page, dark on the class', () => {
    const css = variables();

    expect(css).toContain('color-scheme:light}');
    expect(css).toContain(`.${darkClass}{color-scheme:dark}`);
  });
});
