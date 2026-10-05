import {
  ChangeDetectionStrategy,
  Component,
  computed,
  inject,
  input,
  linkedSignal,
  output,
  signal,
} from '@angular/core';
import { FormsModule } from '@angular/forms';
import { Select } from 'primeng/select';
import { Ticket } from '../../api/models';
import { ProblemService } from '../../core/problem.service';
import { splitKey, TicketsService } from '../../core/tickets.service';

/** A ticket the picker offers: its canonical key, and the short key with the title to choose by. */
export interface ParentOption {
  key: string;
  label: string;
}

/**
 * Chooses a ticket's parent among the open tickets of its project (docs/adr/0008 D2), filtered by
 * the short key or the title as the person types. The list loads when the picker first opens, every
 * page of it, and not before: a page that only shows the parent asks for nothing. The ticket itself
 * is not offered; a parent that is not open stays shown by its key. Whether the parent closes a
 * cycle is the server's to say (`409 parent_cycle`).
 */
@Component({
  selector: 'app-parent-picker',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [FormsModule, Select],
  template: `
    <p-select
      [options]="options()"
      optionLabel="label"
      optionValue="key"
      [ngModel]="value()"
      (ngModelChange)="picked.emit($event ?? null)"
      [filter]="true"
      filterBy="label"
      [showClear]="true"
      [loading]="loading()"
      (onShow)="load()"
      placeholder="No parent"
      emptyMessage="No other open ticket in the project"
      emptyFilterMessage="No open ticket matches"
      appendTo="body"
      size="small"
      [variant]="variant()"
      [ariaLabel]="label()"
      [ariaLabelledBy]="labelledBy()"
      [disabled]="disabled()"
      data-testid="parent-picker"
    />
  `,
  styles: `
    :host {
      display: block;
    }
    :host ::ng-deep .p-select {
      width: 100%;
    }
  `,
})
export class ParentPicker {
  readonly tenant = input.required<string>();
  readonly project = input.required<string>();
  /** The parent's canonical key, or null for none. */
  readonly value = input<string | null>(null);
  /** The ticket whose parent is chosen, which is not offered; null while it is being filed. */
  readonly exclude = input<string | null>(null);
  readonly label = input<string | undefined>(undefined);
  readonly labelledBy = input<string | undefined>(undefined);
  readonly variant = input<'filled' | 'outlined'>('outlined');
  readonly disabled = input(false);
  readonly picked = output<string | null>();

  private readonly tickets = inject(TicketsService);
  private readonly problems = inject(ProblemService);

  private readonly place = computed(() => `${this.tenant()}/${this.project()}`);
  /** The project's open tickets once loaded; another project starts without them. */
  private readonly loaded = linkedSignal<string, Ticket[] | null>({
    source: this.place,
    computation: () => null,
  });
  protected readonly loading = signal(false);

  protected readonly options = computed<ParentOption[]>(() => {
    const options = (this.loaded() ?? [])
      .filter((ticket) => ticket.key !== this.exclude())
      .map((ticket) => ({ key: ticket.key, label: `${splitKey(ticket.key).key} ${ticket.title}` }));
    const value = this.value();
    if (value && !options.some((option) => option.key === value)) {
      options.unshift({ key: value, label: splitKey(value).key });
    }
    return options;
  });

  /** Loads the project's open tickets the first time the picker opens. */
  async load(): Promise<void> {
    if (this.loaded() !== null || this.loading()) {
      return;
    }
    const tenant = this.tenant();
    const project = this.project();
    this.loading.set(true);
    try {
      const tickets = await this.tickets.openTickets(tenant, project);
      if (tenant === this.tenant() && project === this.project()) {
        this.loaded.set(tickets);
      }
    } catch (error) {
      this.problems.report(error);
    } finally {
      this.loading.set(false);
    }
  }
}
