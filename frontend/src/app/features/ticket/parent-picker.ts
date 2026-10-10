import {
  ChangeDetectionStrategy,
  Component,
  computed,
  DestroyRef,
  inject,
  input,
  linkedSignal,
  output,
  signal,
} from '@angular/core';
import { FormsModule } from '@angular/forms';
import { RouterLink } from '@angular/router';
import { ButtonDirective } from 'primeng/button';
import { Select } from 'primeng/select';
import { Tooltip } from 'primeng/tooltip';
import { Ticket, TicketHead } from '../../api/models';
import { ProblemService } from '../../core/problem.service';
import { TicketsService } from '../../core/tickets.service';
import {
  headLabel,
  headOfHit,
  headOfKey,
  headOfTicket,
  headRoute,
  placeholderText,
  TicketChoice,
} from '../../shared/ticket-head';

/**
 * A ticket the picker offers: its canonical key, the label a screen reader hears, its head, and
 * the text the person typed that found it.
 */
export interface ParentOption {
  /** The canonical key the picker sends; {@link confidentialParent} for a parent not to be seen. */
  key: string;
  label: string;
  head: TicketHead;
  /** The text typed that found the ticket; empty for what the picker offers before any is typed. */
  query: string;
}

/** What the picker holds for a parent the reader may not see: no key names it (docs/adr/0065 D5). */
export const confidentialParent = '(confidential)';

/** How long the picker waits after the last keystroke before it searches. */
export const searchDelay = 250;

/** The states a parent is not chosen from: the picker offers open tickets. */
const closed: ReadonlySet<string> = new Set(['done', 'dropped']);

/**
 * Chooses a ticket's parent among the tickets the person reads, of any project or team
 * (docs/adr/0008 D2): before the person types, the open tickets of the ticket's project, every page
 * of them, loaded when the picker first opens and not before; once they type, the open tickets of
 * every team of the person that the person-level search finds for the words or the key typed — a
 * union under each team's rules (docs/adr/0023 D2, docs/adr/0025) — 250 ms after the last
 * keystroke, an option standing only under the text that found it. Each choice shows its type, its
 * key — with its team where it is of another team —, its title and its state; the picker takes and
 * sends canonical keys. The ticket itself is not offered. The parent the ticket has stays shown by
 * its head (`head`): a closed one, one of another team, unlinked where the reader may not open it,
 * and the placeholder `<team> [Confidential]` for one they may not see, which the person may still
 * remove (docs/adr/0065 D5); beside a parent they may open, a link to it. Whether the parent closes
 * a cycle, and whether the person may set it, is the server's to say (`409 parent_cycle`, a key
 * they cannot read answered as a missing one).
 */
@Component({
  selector: 'app-parent-picker',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [ButtonDirective, FormsModule, RouterLink, Select, TicketChoice, Tooltip],
  template: `
    <p-select
      [options]="options()"
      optionLabel="label"
      optionValue="key"
      [ngModel]="selected()"
      (ngModelChange)="pick($event)"
      [filter]="true"
      filterBy="query"
      filterMatchMode="equals"
      filterPlaceholder="Search your teams"
      ariaFilterLabel="Search the tickets of your teams"
      (onFilter)="type($event.filter)"
      (onClear)="type('')"
      [showClear]="true"
      [loading]="loading() || searching()"
      (onShow)="load()"
      placeholder="No parent"
      emptyMessage="No other open ticket in the project"
      [emptyFilterMessage]="searching() ? 'Searching…' : 'No open ticket you can read matches'"
      appendTo="body"
      size="small"
      [variant]="variant()"
      [ariaLabel]="label()"
      [ariaLabelledBy]="labelledBy()"
      [disabled]="disabled()"
      data-testid="parent-picker"
    >
      <ng-template #selectedItem let-option>
        <app-ticket-choice [head]="option.head" [here]="tenant()" data-testid="parent-chosen" />
      </ng-template>
      <ng-template #item let-option>
        <app-ticket-choice [head]="option.head" [here]="tenant()" />
      </ng-template>
    </p-select>
    @if (route(); as route) {
      <a
        pButton
        [text]="true"
        [rounded]="true"
        size="small"
        severity="secondary"
        [iconOnly]="true"
        [routerLink]="route"
        aria-label="Open the parent"
        pTooltip="Open the parent"
        [showDelay]="400"
        data-testid="parent-open"
        ><i class="pi pi-arrow-up-right"></i
      ></a>
    }
  `,
  styles: `
    :host {
      display: flex;
      align-items: center;
      gap: 0.25rem;
      min-width: 0;
    }
    :host ::ng-deep .p-select {
      flex: 1;
      width: 100%;
      min-width: 0;
    }
  `,
})
export class ParentPicker {
  readonly tenant = input.required<string>();
  readonly project = input.required<string>();
  /** The parent's canonical key, or null for none and for one the reader may not see. */
  readonly value = input<string | null>(null);
  /**
   * The parent as the reader sees it (`Ticket.parent_head`), where the page shows a ticket's own:
   * what the picker shows it by, and whether the reader may open it.
   */
  readonly head = input<TicketHead | null>(null);
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
  /** The text the person typed into the search, trimmed; empty before they type. */
  private readonly query = signal('');
  /** The open tickets the last search found, each under the text that found it. */
  private readonly hits = signal<ParentOption[]>([]);
  protected readonly searching = signal(false);
  /** The choice the person made last, which keeps its label while the list offers others. */
  private readonly chosen = signal<ParentOption | null>(null);
  private timer: ReturnType<typeof setTimeout> | null = null;

  /** The value the select holds: the parent's key, or the placeholder's for one not to be seen. */
  protected readonly selected = computed(() => {
    const value = this.value();
    return value === null && this.head()?.placeholder ? confidentialParent : value;
  });

  protected readonly options = computed<ParentOption[]>(() => {
    const team = { slug: this.tenant(), name: this.tenant() };
    const offered = this.query()
      ? this.hits()
      : (this.loaded() ?? []).map((ticket) => this.option(headOfTicket(ticket, team), ''));
    const options = offered.filter((option) => option.key !== this.exclude());
    const selected = this.selected();
    if (selected !== null && !options.some((option) => option.key === selected)) {
      options.unshift(this.current(selected));
    }
    return options;
  });

  /** Where the parent opens, beside the picker: only one the reader may open. */
  protected readonly route = computed(() => {
    const head = this.head();
    return head && head.key !== null && head.key === this.value() ? headRoute(head) : null;
  });

  constructor() {
    inject(DestroyRef).onDestroy(() => {
      if (this.timer) {
        clearTimeout(this.timer);
      }
    });
  }

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

  /** The person typed into the search: it searches once they rest, an empty text offers the project's. */
  protected type(text: string | null | undefined): void {
    const query = (text ?? '').trim();
    this.query.set(query);
    if (this.timer) {
      clearTimeout(this.timer);
      this.timer = null;
    }
    this.searching.set(query !== '');
    if (query !== '') {
      this.timer = setTimeout(() => void this.search(query), searchDelay);
    }
  }

  /** The open tickets the person-level search finds for the text, kept while it is still the one typed. */
  private async search(query: string): Promise<void> {
    this.timer = null;
    try {
      const hits = await this.tickets.search(query);
      if (query === this.query()) {
        this.hits.set(
          hits
            .filter((hit) => !closed.has(hit.state))
            .map((hit) => this.option(headOfHit(hit), query)),
        );
      }
    } catch (error) {
      if (query === this.query()) {
        this.problems.report(error);
      }
    } finally {
      if (query === this.query()) {
        this.searching.set(false);
      }
    }
  }

  protected pick(value: string | null | undefined): void {
    if (value === confidentialParent) {
      return;
    }
    const key = value ?? null;
    this.chosen.set(this.options().find((option) => option.key === key) ?? null);
    this.picked.emit(key);
  }

  /**
   * The parent the ticket has, which the list may not offer — closed, of another project or team,
   * or one the reader may not see —, by its head, or by the choice the person just made.
   */
  private current(selected: string): ParentOption {
    const head = this.head();
    if (selected === confidentialParent && head) {
      return { key: selected, label: placeholderText(head), head, query: '' };
    }
    const chosen = this.chosen();
    if (chosen?.key === selected) {
      return { ...chosen, query: '' };
    }
    return this.option(head?.key === selected ? head : headOfKey(selected), '');
  }

  private option(head: TicketHead, query: string): ParentOption {
    return {
      key: head.key ?? confidentialParent,
      label: headLabel(head, this.tenant()),
      head,
      query,
    };
  }
}
