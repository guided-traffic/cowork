import { ChangeDetectionStrategy, Component, computed, input } from '@angular/core';
import { Tooltip } from 'primeng/tooltip';
import { SecurityClass, Severity, TicketState, TicketType } from '../api/models';
import { meanings } from './vocabulary';

/** The icon of each ticket type (docs/adr/0008). */
export const typeIcons: Record<TicketType, string> = {
  task: 'pi pi-check-square',
  bug: 'pi pi-exclamation-circle',
  feature: 'pi pi-star',
  decision: 'pi pi-directions',
  question: 'pi pi-question-circle',
};

/**
 * The badges of docs/adr/0018 D1. Each value's accent is a token of the preset (docs/adr/0052
 * D2), so a badge reads the same in both schemes; the pill derives its tint from the accent. The
 * value is shown as the API spells it, its meaning in the tooltip (docs/adr/0055 D4).
 */
@Component({
  selector: 'app-state',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [Tooltip],
  template: `<span
    class="pill"
    [style.--accent]="accent()"
    [attr.data-state]="value()"
    [pTooltip]="meaning()"
    [showDelay]="400"
    ><span class="dot"></span>{{ value() }}</span
  >`,
})
export class StateBadge {
  readonly value = input.required<TicketState>();
  protected readonly accent = computed(() => `var(--p-state-${this.value()})`);
  protected readonly meaning = computed(() => meanings.state[this.value()]);
}

@Component({
  selector: 'app-severity',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [Tooltip],
  template: `<span
    class="pill outline"
    [style.--accent]="accent()"
    [attr.data-severity]="value()"
    [pTooltip]="meaning()"
    [showDelay]="400"
    >{{ value() }}</span
  >`,
})
export class SeverityBadge {
  readonly value = input.required<Severity>();
  protected readonly accent = computed(() => `var(--p-severity-${this.value()})`);
  protected readonly meaning = computed(() => meanings.severity[this.value()]);
}

/** Nothing for `none`: only a security class worth noticing gets a badge. */
@Component({
  selector: 'app-security',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [Tooltip],
  template: `@if (value() !== 'none') {
    <span
      class="pill"
      [style.--accent]="accent()"
      [attr.data-security]="value()"
      [pTooltip]="meaning()"
      [showDelay]="400"
      ><i class="pi pi-shield"></i>{{ value() }}</span
    >
  }`,
})
export class SecurityBadge {
  readonly value = input.required<SecurityClass>();
  protected readonly accent = computed(() => `var(--p-security-${this.value()})`);
  protected readonly meaning = computed(() => meanings.security[this.value()]);
}

@Component({
  selector: 'app-type',
  changeDetection: ChangeDetectionStrategy.OnPush,
  host: { '[attr.title]': 'title()' },
  template: `<i [class]="icon()" aria-hidden="true"></i>
    @if (showLabel()) {
      <span>{{ value() }}</span>
    }`,
  styles: `
    :host {
      display: inline-flex;
      align-items: center;
      gap: 0.375rem;
      color: var(--p-text-muted-color);
    }
  `,
})
export class TypeIcon {
  readonly value = input.required<TicketType>();
  readonly showLabel = input(false);
  protected readonly icon = computed(() => typeIcons[this.value()]);
  protected readonly title = computed(() => `${this.value()}: ${meanings.type[this.value()]}`);
}
