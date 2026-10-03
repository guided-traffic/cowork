import { ChangeDetectionStrategy, Component, computed, input } from '@angular/core';
import { Tooltip } from 'primeng/tooltip';
import { Stage, stageNames } from './stages';

/**
 * The bar of one progress stage (docs/adr/0017 D2): a track filled to the stage's value, named
 * `Implementation 40%` as a progress bar and in its tooltip, with `, from its children` where a
 * parent's stage is derived (D3). The board's cards, the backlog and the detail page draw their
 * bars with it (docs/adr/0018 D1, D2). The track is a tint of the muted text, not the hover layer,
 * so that it stays visible on a row under the pointer, whose background is that layer.
 */
@Component({
  selector: 'app-stage-bar',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [Tooltip],
  host: {
    role: 'progressbar',
    'aria-valuemin': '0',
    'aria-valuemax': '100',
    '[attr.aria-valuenow]': 'value()',
    '[attr.aria-label]': 'label()',
    '[attr.data-stage]': 'stage()',
  },
  template: `<span class="track" [pTooltip]="label()" [showDelay]="400"
    ><span class="fill" [style.width.%]="value()"></span
  ></span>`,
  styles: `
    :host {
      display: block;
      min-width: 2.5rem;
    }

    .track {
      display: block;
      height: 0.5rem;
      border-radius: 999px;
      background: color-mix(in srgb, var(--p-text-muted-color) 22%, transparent);
      overflow: hidden;
    }

    .fill {
      display: block;
      height: 100%;
      border-radius: 999px;
      background: var(--p-primary-color);
      transition: width 0.3s ease;
    }

    @media (prefers-reduced-motion: reduce) {
      .fill {
        transition: none;
      }
    }
  `,
})
export class StageBar {
  readonly stage = input.required<Stage>();
  /** 0 to 100. */
  readonly value = input.required<number>();
  /** The stage is a parent's, derived from its children. */
  readonly derived = input(false);

  protected readonly label = computed(
    () =>
      `${stageNames[this.stage()]} ${this.value()}%${this.derived() ? ', from its children' : ''}`,
  );
}
