import { Type } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { By } from '@angular/platform-browser';
import { LogoGlyph, LogoMark, LogoVariant, sparkle, Wordmark } from './logo';

/** Creates the component with the inputs set and lets it render. */
async function render<T>(
  component: Type<T>,
  inputs: Record<string, unknown> = {},
): Promise<ComponentFixture<T>> {
  const fixture = TestBed.createComponent(component);
  for (const [name, value] of Object.entries(inputs)) {
    fixture.componentRef.setInput(name, value);
  }
  await fixture.whenStable();
  return fixture;
}

async function setInput<T>(
  fixture: ComponentFixture<T>,
  name: string,
  value: unknown,
): Promise<void> {
  fixture.componentRef.setInput(name, value);
  await fixture.whenStable();
}

const host = (fixture: ComponentFixture<unknown>) => fixture.nativeElement as HTMLElement;

/** The glyph a mark or a wordmark hands its variant to. */
const glyphOf = (fixture: ComponentFixture<unknown>) =>
  fixture.debugElement.query(By.directive(LogoGlyph)).componentInstance as LogoGlyph;

const shapes = (root: HTMLElement) => [...root.querySelectorAll('svg > *')];
const paths = (root: HTMLElement) =>
  [...root.querySelectorAll('svg > path')].map((path) => path.getAttribute('d'));
const columns = (root: HTMLElement) => [...root.querySelectorAll('svg > rect')];

describe('sparkle', () => {
  it('draws a four-pointed star as four curves through its tips, closed', () => {
    expect(sparkle(50, 40, 20)).toBe(
      'M50 20C50 31 59 40 70 40C59 40 50 49 50 60C50 49 41 40 30 40C41 40 50 31 50 20Z',
    );
  });

  it('starts at the top tip and ends each curve on the next tip: right, bottom, left, top', () => {
    const [start, ...curves] = sparkle(10, 20, 8).replace('Z', '').split('C');

    expect(start).toBe('M10 12');
    expect(curves.map((curve) => curve.split(' ').slice(-2).join(' '))).toEqual([
      '18 20',
      '10 28',
      '2 20',
      '10 12',
    ]);
  });

  it('puts each control point in from its tip by 55 percent of the radius', () => {
    // r = 10: the first curve leaves the top tip (0, -10) straight down to (0, -4.5).
    expect(sparkle(0, 0, 10)).toBe(
      'M0 -10C0 -4.5 4.5 0 10 0C4.5 0 0 4.5 0 10C0 4.5 -4.5 0 -10 0C-4.5 0 0 -4.5 0 -10Z',
    );
  });

  it('moves with its centre', () => {
    expect(sparkle(5, 7, 10)).toBe(
      'M5 -3C5 2.5 9.5 7 15 7C9.5 7 5 11.5 5 17C5 11.5 0.5 7 -5 7C0.5 7 5 2.5 5 -3Z',
    );
  });
});

describe('LogoGlyph', () => {
  it('is a drawing that is hidden from assistive technology, in the 64 by 64 box', async () => {
    const svg = host(await render(LogoGlyph)).querySelector('svg');

    expect(svg?.getAttribute('aria-hidden')).toBe('true');
    expect(svg?.getAttribute('focusable')).toBe('false');
    expect(svg?.getAttribute('viewBox')).toBe('0 0 64 64');
  });

  describe('the board variant, which is the default', () => {
    it('draws three columns of falling height and the spark that works them', async () => {
      const root = host(await render(LogoGlyph));

      expect(shapes(root).map((shape) => shape.tagName)).toEqual(['rect', 'rect', 'rect', 'path']);
      expect(columns(root).map((column) => column.getAttribute('height'))).toEqual([
        '34',
        '24',
        '13',
      ]);
      expect(columns(root).map((column) => column.getAttribute('opacity'))).toEqual([
        null,
        '0.85',
        '0.7',
      ]);
      expect(columns(root).every((column) => column.getAttribute('fill') === 'currentColor')).toBe(
        true,
      );
      expect(paths(root)).toEqual([sparkle(45, 43, 9)]);
    });

    it('is also what the variant "board" asks for', async () => {
      const root = host(await render(LogoGlyph, { variant: 'board' }));

      expect(columns(root)).toHaveLength(3);
      expect(paths(root)).toEqual([sparkle(45, 43, 9)]);
    });

    it('is what a value it does not know gets, since the board is the default branch', async () => {
      const root = host(await render(LogoGlyph, { variant: 'nonsense' as LogoVariant }));

      expect(columns(root)).toHaveLength(3);
      expect(paths(root)).toEqual([sparkle(45, 43, 9)]);
    });
  });

  describe('the twin variant', () => {
    it('draws a large and a small sparkle', async () => {
      const root = host(await render(LogoGlyph, { variant: 'twin' }));

      expect(shapes(root).map((shape) => shape.tagName)).toEqual(['path', 'path']);
      expect(paths(root)).toEqual([sparkle(37, 37, 17), sparkle(19, 19, 8)]);
      expect(shapes(root).every((shape) => shape.getAttribute('fill') === 'currentColor')).toBe(
        true,
      );
    });
  });

  describe('the spark-c variant', () => {
    it('draws an open ring with a sparkle at its mouth', async () => {
      const root = host(await render(LogoGlyph, { variant: 'spark-c' }));
      const [ring, spark] = [...root.querySelectorAll('svg > path')];

      expect(shapes(root)).toHaveLength(2);
      expect(ring.getAttribute('d')).toBe('M44.5 21.5 A15 15 0 1 0 44.5 42.5');
      expect(ring.getAttribute('fill')).toBe('none');
      expect(ring.getAttribute('stroke')).toBe('currentColor');
      expect(ring.getAttribute('stroke-width')).toBe('7');
      expect(ring.getAttribute('stroke-linecap')).toBe('round');
      expect(spark.getAttribute('d')).toBe(sparkle(47, 32, 9));
      expect(spark.getAttribute('fill')).toBe('currentColor');
    });
  });

  it('swaps the drawing when the variant changes, and leaves nothing of the old one', async () => {
    const fixture = await render(LogoGlyph, { variant: 'board' });
    expect(columns(host(fixture))).toHaveLength(3);

    await setInput(fixture, 'variant', 'twin');
    expect(columns(host(fixture))).toHaveLength(0);
    expect(paths(host(fixture))).toEqual([sparkle(37, 37, 17), sparkle(19, 19, 8)]);

    await setInput(fixture, 'variant', 'spark-c');
    expect(paths(host(fixture))).toHaveLength(2);
    expect(paths(host(fixture))[1]).toBe(sparkle(47, 32, 9));

    await setInput(fixture, 'variant', 'board');
    expect(columns(host(fixture))).toHaveLength(3);
    expect(paths(host(fixture))).toEqual([sparkle(45, 43, 9)]);
  });
});

describe('LogoMark', () => {
  it('is an image named cowork', async () => {
    const root = host(await render(LogoMark));

    expect(root.getAttribute('role')).toBe('img');
    expect(root.getAttribute('aria-label')).toBe('cowork');
  });

  it('can be named otherwise', async () => {
    const root = host(await render(LogoMark, { label: 'cowork backlog' }));

    expect(root.getAttribute('aria-label')).toBe('cowork backlog');
  });

  describe('the glyph it carries', () => {
    it('is the board unless told otherwise', async () => {
      const fixture = await render(LogoMark);

      expect(glyphOf(fixture).variant()).toBe('board');
      expect(columns(host(fixture))).toHaveLength(3);
      expect(paths(host(fixture))).toEqual([sparkle(45, 43, 9)]);
    });

    it.each([
      ['twin', [sparkle(37, 37, 17), sparkle(19, 19, 8)]],
      ['spark-c', ['M44.5 21.5 A15 15 0 1 0 44.5 42.5', sparkle(47, 32, 9)]],
    ] as const)('is the %s glyph when the variant says so', async (variant, drawn) => {
      const fixture = await render(LogoMark, { variant });

      expect(glyphOf(fixture).variant()).toBe(variant);
      expect(columns(host(fixture))).toHaveLength(0);
      expect(paths(host(fixture))).toEqual(drawn);
    });

    it('follows the variant when it changes', async () => {
      const fixture = await render(LogoMark);

      await setInput(fixture, 'variant', 'twin');

      expect(glyphOf(fixture).variant()).toBe('twin');
      expect(columns(host(fixture))).toHaveLength(0);
    });

    it('is drawn once, inside the mark', async () => {
      const fixture = await render(LogoMark);

      expect(host(fixture).querySelectorAll('app-logo-glyph')).toHaveLength(1);
      expect(host(fixture).querySelectorAll('svg')).toHaveLength(1);
    });
  });

  describe('size', () => {
    it('is 32 pixels unless told otherwise', async () => {
      const root = host(await render(LogoMark));

      expect(root.style.getPropertyValue('--logo-size')).toBe('32px');
    });

    it('sets the --logo-size the styles are written in', async () => {
      const fixture = await render(LogoMark, { size: 48 });
      expect(host(fixture).style.getPropertyValue('--logo-size')).toBe('48px');

      await setInput(fixture, 'size', 20);
      expect(host(fixture).style.getPropertyValue('--logo-size')).toBe('20px');
    });
  });

  describe('glow', () => {
    it('is on by default', async () => {
      const root = host(await render(LogoMark));

      expect(root.classList.contains('glow')).toBe(true);
    });

    it('can be switched off and on', async () => {
      const fixture = await render(LogoMark, { glow: false });
      expect(host(fixture).classList.contains('glow')).toBe(false);

      await setInput(fixture, 'glow', true);
      expect(host(fixture).classList.contains('glow')).toBe(true);
    });
  });
});

describe('Wordmark', () => {
  it('shows the glyph before the name', async () => {
    const root = host(await render(Wordmark));
    const glyph = root.querySelector('app-logo-glyph');
    const name = root.querySelector('.name');

    expect(name?.textContent).toBe('cowork');
    expect(glyph).not.toBeNull();
    expect(glyph?.compareDocumentPosition(name as Node)).toBe(Node.DOCUMENT_POSITION_FOLLOWING);
  });

  it('hides the drawing from assistive technology and leaves the name to read', async () => {
    const root = host(await render(Wordmark));

    expect(root.querySelector('svg')?.getAttribute('aria-hidden')).toBe('true');
  });

  describe('variant', () => {
    it('is the board unless told otherwise, and hands it to its glyph', async () => {
      const fixture = await render(Wordmark);

      expect(glyphOf(fixture).variant()).toBe('board');
      expect(columns(host(fixture))).toHaveLength(3);
      expect(paths(host(fixture))).toEqual([sparkle(45, 43, 9)]);
    });

    it.each(['twin', 'spark-c'] as const)(
      'hands the %s variant on to its glyph',
      async (variant) => {
        const fixture = await render(Wordmark, { variant });

        expect(glyphOf(fixture).variant()).toBe(variant);
        expect(columns(host(fixture))).toHaveLength(0);
      },
    );

    it('follows the variant when it changes', async () => {
      const fixture = await render(Wordmark, { variant: 'twin' });

      await setInput(fixture, 'variant', 'board');

      expect(glyphOf(fixture).variant()).toBe('board');
      expect(columns(host(fixture))).toHaveLength(3);
    });
  });

  describe('compact', () => {
    it('is the pill by default', async () => {
      const root = host(await render(Wordmark));

      expect(root.classList.contains('compact')).toBe(false);
    });

    it('drops the pill for narrow places and back', async () => {
      const fixture = await render(Wordmark, { compact: true });
      expect(host(fixture).classList.contains('compact')).toBe(true);

      await setInput(fixture, 'compact', false);
      expect(host(fixture).classList.contains('compact')).toBe(false);
    });

    it('still shows the glyph and the name', async () => {
      const root = host(await render(Wordmark, { compact: true }));

      expect(root.querySelector('.name')?.textContent).toBe('cowork');
      expect(root.querySelector('app-logo-glyph svg')).not.toBeNull();
    });
  });

  describe('height', () => {
    it('is 32 pixels unless told otherwise', async () => {
      const root = host(await render(Wordmark));

      expect(root.style.getPropertyValue('--wordmark-height')).toBe('32px');
    });

    it('sets the --wordmark-height the styles are written in', async () => {
      const root = host(await render(Wordmark, { height: 44 }));

      expect(root.style.getPropertyValue('--wordmark-height')).toBe('44px');
    });
  });
});
