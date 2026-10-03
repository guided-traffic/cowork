import { ChangeDetectionStrategy, Component, computed, input } from '@angular/core';
import { Tooltip } from 'primeng/tooltip';
import { StreamStatus } from '../core/event-stream.service';

const texts: Record<StreamStatus, { label: string; hint: string }> = {
  idle: { label: '', hint: '' },
  connecting: { label: 'Connecting', hint: 'Opening the live stream' },
  live: { label: 'Live', hint: 'Changes by anyone arrive here within a second' },
  polling: {
    label: 'Polling',
    hint: 'The live stream is unavailable: refreshing every 15 seconds and retrying the stream every minute',
  },
};

/** How fresh the page is (docs/adr/0054): live through the stream, or polled as the fallback. */
@Component({
  selector: 'app-live-indicator',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [Tooltip],
  template: `@if (status() !== 'idle') {
    <span
      class="indicator"
      [attr.data-status]="status()"
      [pTooltip]="text().hint"
      tooltipPosition="bottom"
      data-testid="live-indicator"
      ><span class="dot"></span>{{ text().label }}</span
    >
  }`,
  styles: `
    .indicator {
      --tone: var(--p-state-done);
      display: inline-flex;
      align-items: center;
      gap: 0.5rem;
      height: 1.75rem;
      padding: 0 0.625rem;
      border-radius: 999px;
      font-size: 0.75rem;
      font-weight: 550;
      color: var(--p-text-muted-color);
      background: var(--p-app-hover);
    }
    .indicator[data-status='connecting'],
    .indicator[data-status='polling'] {
      --tone: var(--p-state-in-progress);
    }
    .dot {
      width: 0.5rem;
      height: 0.5rem;
      border-radius: 50%;
      background: var(--tone);
      box-shadow: 0 0 0 0 color-mix(in srgb, var(--tone) 60%, transparent);
    }
    .indicator[data-status='live'] .dot {
      animation: pulse 2.4s ease-out infinite;
    }
    @keyframes pulse {
      0% {
        box-shadow: 0 0 0 0 color-mix(in srgb, var(--tone) 55%, transparent);
      }
      70%,
      100% {
        box-shadow: 0 0 0 0.45rem transparent;
      }
    }
    @media (prefers-reduced-motion: reduce) {
      .indicator[data-status='live'] .dot {
        animation: none;
      }
    }
  `,
})
export class LiveIndicator {
  readonly status = input.required<StreamStatus>();
  protected readonly text = computed(() => texts[this.status()]);
}
