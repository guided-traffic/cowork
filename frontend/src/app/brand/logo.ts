import { ChangeDetectionStrategy, Component, input } from '@angular/core';

/**
 * The glyphs the mark can carry (docs/adr/0052 D8). `board` is the owner's pick: three board
 * columns and the spark that works them. `twin` and `spark-c` stay for the design preview.
 */
export type LogoVariant = 'twin' | 'spark-c' | 'board';

/** A four-pointed sparkle centred on (cx, cy) with radius r, in the 64×64 glyph box. */
export function sparkle(cx: number, cy: number, r: number): string {
  const k = r * 0.55;
  return [
    `M${cx} ${cy - r}`,
    `C${cx} ${cy - r + k} ${cx + r - k} ${cy} ${cx + r} ${cy}`,
    `C${cx + r - k} ${cy} ${cx} ${cy + r - k} ${cx} ${cy + r}`,
    `C${cx} ${cy + r - k} ${cx - r + k} ${cy} ${cx - r} ${cy}`,
    `C${cx - r + k} ${cy} ${cx} ${cy - r + k} ${cx} ${cy - r}Z`,
  ].join('');
}

/** The glyph alone, white on whatever carries it; its host sets the size. */
@Component({
  selector: 'app-logo-glyph',
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <svg viewBox="0 0 64 64" aria-hidden="true" focusable="false">
      @switch (variant()) {
        @case ('spark-c') {
          <path
            d="M44.5 21.5 A15 15 0 1 0 44.5 42.5"
            fill="none"
            stroke="currentColor"
            stroke-width="7"
            stroke-linecap="round"
          />
          <path [attr.d]="sparkC" fill="currentColor" />
        }
        @case ('twin') {
          <path [attr.d]="sparkLarge" fill="currentColor" />
          <path [attr.d]="sparkSmall" fill="currentColor" />
        }
        @default {
          <rect x="13" y="14" width="9" height="34" rx="3.5" fill="currentColor" />
          <rect x="27.5" y="14" width="9" height="24" rx="3.5" fill="currentColor" opacity="0.85" />
          <rect x="42" y="14" width="9" height="13" rx="3.5" fill="currentColor" opacity="0.7" />
          <path [attr.d]="sparkBoard" fill="currentColor" />
        }
      }
    </svg>
  `,
  styles: `
    :host {
      display: block;
    }
    svg {
      display: block;
      width: 100%;
      height: 100%;
    }
  `,
})
export class LogoGlyph {
  readonly variant = input<LogoVariant>('board');

  protected readonly sparkLarge = sparkle(37, 37, 17);
  protected readonly sparkSmall = sparkle(19, 19, 8);
  protected readonly sparkC = sparkle(47, 32, 9);
  protected readonly sparkBoard = sparkle(45, 43, 9);
}

/**
 * The cowork mark: the logo's ink inside a border of its gradient, a soft violet glow and a white
 * glyph — the style of the owner's reference, built with the same `padding-box`/`border-box`
 * gradient, so it needs no SVG gradient reference (which a `<base href>` would break).
 */
@Component({
  selector: 'app-logo-mark',
  changeDetection: ChangeDetectionStrategy.OnPush,
  host: {
    role: 'img',
    '[attr.aria-label]': 'label()',
    '[style.--logo-size.px]': 'size()',
    '[class.glow]': 'glow()',
  },
  imports: [LogoGlyph],
  template: `<app-logo-glyph [variant]="variant()" />`,
  styles: `
    :host {
      --logo-size: 32px;
      display: inline-grid;
      place-items: center;
      flex: none;
      width: var(--logo-size);
      height: var(--logo-size);
      box-sizing: border-box;
      border-radius: calc(var(--logo-size) * 0.3);
      border: max(1.5px, calc(var(--logo-size) * 0.055)) solid transparent;
      background:
        radial-gradient(circle at 50% 30%, var(--p-brand-ink-highlight), var(--p-brand-ink) 72%)
          padding-box,
        var(--p-brand-gradient) border-box;
      color: var(--p-brand-glyph);
    }
    :host(.glow) {
      box-shadow: 0 0 calc(var(--logo-size) * 0.75) var(--p-brand-glow);
    }
    app-logo-glyph {
      width: 74%;
      height: 74%;
    }
  `,
})
export class LogoMark {
  readonly variant = input<LogoVariant>('board');
  readonly size = input(32);
  readonly glow = input(true);
  readonly label = input('cowork');
}

/**
 * The wordmark: the reference's pill — the gradient border, the ink, the glyph and the name.
 * The shell shows it in the top bar; `compact` drops the pill for narrow places.
 */
@Component({
  selector: 'app-wordmark',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [LogoGlyph],
  host: { '[style.--wordmark-height.px]': 'height()', '[class.compact]': 'compact()' },
  template: `
    <app-logo-glyph [variant]="variant()" />
    <span class="name">cowork</span>
  `,
  styles: `
    :host {
      --wordmark-height: 32px;
      display: inline-flex;
      align-items: center;
      gap: calc(var(--wordmark-height) * 0.22);
      height: var(--wordmark-height);
      box-sizing: border-box;
      padding: 0 calc(var(--wordmark-height) * 0.55) 0 calc(var(--wordmark-height) * 0.36);
      border-radius: 999px;
      border: max(1.5px, calc(var(--wordmark-height) * 0.07)) solid transparent;
      background:
        linear-gradient(var(--p-brand-ink), var(--p-brand-ink)) padding-box,
        var(--p-brand-gradient) border-box;
      box-shadow: 0 0 calc(var(--wordmark-height) * 1.2) var(--p-brand-glow);
      color: var(--p-brand-glyph);
    }
    app-logo-glyph {
      width: calc(var(--wordmark-height) * 0.6);
      height: calc(var(--wordmark-height) * 0.6);
    }
    .name {
      font-size: calc(var(--wordmark-height) * 0.5);
      font-weight: 600;
      letter-spacing: -0.01em;
      line-height: 1;
    }
    :host(.compact) {
      padding: 0;
      border: 0;
      background: none;
      box-shadow: none;
      color: inherit;
    }
  `,
})
export class Wordmark {
  readonly height = input(32);
  readonly compact = input(false);
  readonly variant = input<LogoVariant>('board');
}
