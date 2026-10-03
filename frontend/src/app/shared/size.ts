import { ChangeDetectionStrategy, Component, computed, input } from '@angular/core';
import { Tooltip } from 'primeng/tooltip';
import { Effort } from '../api/models';

/**
 * A ticket's effort (docs/adr/0010 D1: `XS`, `S`, `M` or `L`, a size and no time) as a small
 * T-shirt, the letter on it, and "Effort M" in the tooltip. The backlog shows it in a column; the
 * board's cards show it too (docs/adr/0018 D1). The outline and the letter take the text colours
 * of the theme, so the glyph reads the same in both schemes.
 */
@Component({
  selector: 'app-size',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [Tooltip],
  host: { role: 'img', '[attr.aria-label]': 'label()', '[attr.data-size]': 'value()' },
  template: `<span class="glyph" [pTooltip]="label()" [showDelay]="400">
    <svg viewBox="0 0 32 28" aria-hidden="true" focusable="false">
      <path
        d="M11 3 4.5 5.5 1.5 11 7 13.5V25h18V13.5l5.5-2.5-3-5.5L21 3c-1 2.2-2.8 3.5-5 3.5S12 5.2 11 3Z"
      />
    </svg>
    <span class="letter" aria-hidden="true">{{ value() }}</span>
  </span>`,
  styles: `
    :host {
      display: inline-flex;
      vertical-align: middle;
    }

    .glyph {
      position: relative;
      display: inline-block;
      width: 2.25rem;
      height: 1.96875rem;
      color: var(--p-text-muted-color);
    }

    svg {
      display: block;
      width: 100%;
      height: 100%;
      fill: none;
      stroke: currentColor;
      stroke-width: 1.5;
      stroke-linejoin: round;
    }

    .letter {
      position: absolute;
      left: 0;
      right: 0;
      top: 58%;
      transform: translateY(-50%);
      font-size: 0.6875rem;
      font-weight: 650;
      line-height: 1;
      text-align: center;
      color: var(--p-text-color);
    }
  `,
})
export class SizeIcon {
  readonly value = input.required<Effort>();
  protected readonly label = computed(() => `Effort ${this.value()}`);
}
