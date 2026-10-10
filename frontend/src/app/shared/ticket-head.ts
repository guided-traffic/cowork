import { ChangeDetectionStrategy, Component, computed, input } from '@angular/core';
import { RouterLink } from '@angular/router';
import { Tooltip } from 'primeng/tooltip';
import { SearchHit, TeamRef, Ticket, TicketHead } from '../api/models';
import { StateBadge, TypeIcon } from './badges';

/**
 * A ticket at the other end of a relation — a parent, a child, a link end, a node of the
 * prerequisite tree — as the reader sees it (docs/adr/0005 D3, docs/adr/0065 D5): readable, by its
 * head where the reader holds no role in its team or its project is closed to them
 * (docs/adr/0034 D4), or the placeholder `<team> [Confidential]` where they may not see it, in
 * another team or their own. The page says no more of it than the head holds.
 */

/** `acme/COW-12` → `acme` and `COW-12`. */
function parts(key: string): { team: string; short: string } {
  const slash = key.indexOf('/');
  return { team: key.slice(0, slash), short: key.slice(slash + 1) };
}

/** `<team> [Confidential]`: a ticket the reader may not see, by its team's name alone (D5). */
export function placeholderText(head: TicketHead): string {
  return `${head.team.name} [Confidential]`;
}

/** The head's short key, `<PROJECT>-<number>`; null for the placeholder, which shows no key. */
export function shortKeyOf(head: TicketHead): string | null {
  return head.placeholder || head.key === null ? null : parts(head.key).short;
}

/** Whether the head's ticket is of another team than `here`, the team the page shows. */
export function ofAnotherTeam(head: TicketHead, here: string | null): boolean {
  return head.team.slug !== here;
}

/**
 * Where the head opens — its ticket's page in its team — or null where the reader may not open it:
 * a head they read without a role in its team, or the placeholder.
 */
export function headRoute(head: TicketHead): string[] | null {
  if (head.placeholder || !head.readable || head.key === null) {
    return null;
  }
  const { team, short } = parts(head.key);
  return ['/t', team, 'tickets', short];
}

/**
 * The head in a line of text: the placeholder, the short key of a ticket of the team the page shows,
 * and the team's name before the short key of a ticket of another team.
 */
export function headName(head: TicketHead, here: string | null): string {
  const short = shortKeyOf(head);
  if (short === null) {
    return placeholderText(head);
  }
  return ofAnotherTeam(head, here) ? `${head.team.name} · ${short}` : short;
}

/** The name and the title, as a choice offers the ticket and a chip's label reads. */
export function headLabel(head: TicketHead, here: string | null): string {
  const name = headName(head, here);
  return head.title && !head.placeholder ? `${name} ${head.title}` : name;
}

/** What a chip that names a ticket's parent shows, where it opens, and what it says. */
export interface ParentChip {
  /** The parent's name ({@link headName}). */
  text: string;
  /** The parent's page, or null where the reader may not open it. */
  route: string[] | null;
  /** What a screen reader hears: `Parent <name>`. */
  label: string;
  /** The tooltip: the parent's title, and that the reader cannot open it where they cannot. */
  tip: string;
}

/**
 * The chip of the backlog's row and of the board's card that names the ticket's parent by its head
 * (docs/adr/0005 D3, docs/adr/0008 D2): its title in the tooltip, linked where the reader may open
 * it, and the placeholder `<team> [Confidential]` for one they may not see (docs/adr/0065 D5).
 */
export function parentChip(head: TicketHead, here: string | null): ParentChip {
  const text = headName(head, here);
  const route = headRoute(head);
  let tip = 'Its parent is confidential: you may not see it';
  if (shortKeyOf(head) !== null) {
    tip = route ? `Its parent: ${head.title}` : `Its parent, which you cannot open: ${head.title}`;
  }
  return { text, route, label: `Parent ${text}`, tip };
}

/**
 * The prerequisite a `prerequisite_settled` act names (docs/adr/0012 D5): a ticket of another team
 * that blocked the act's ticket and reached done or dropped, by its team and short key — or
 * `<team> [Confidential]` where it is confidential — and the state it reached; null where the act
 * carries none.
 */
export function settledPrerequisite(after: unknown): { name: string; state: string | null } | null {
  const head = (after as { prerequisite?: TicketHead } | null)?.prerequisite;
  if (!head?.team || (!head.placeholder && typeof head.key !== 'string')) {
    return null;
  }
  return { name: headName(head, null), state: head.placeholder ? null : head.state };
}

/** A ticket the person reads, of the team `team`, as its head. */
export function headOfTicket(ticket: Ticket, team: TeamRef): TicketHead {
  return {
    team,
    key: ticket.key,
    title: ticket.title,
    type: ticket.type,
    state: ticket.state,
    placeholder: false,
    readable: true,
  };
}

/** A search hit as its head: the search finds only what its reader reads (docs/adr/0025). */
export function headOfHit(hit: SearchHit): TicketHead {
  return {
    team: hit.team,
    key: hit.key,
    title: hit.title,
    type: hit.type,
    state: hit.state,
    placeholder: false,
    readable: true,
  };
}

/** A ticket known by its canonical key alone, which the reader reads: a key typed or chosen. */
export function headOfKey(key: string): TicketHead {
  const { team } = parts(key);
  return {
    team: { slug: team, name: team },
    key,
    title: null,
    type: null,
    state: null,
    placeholder: false,
    readable: true,
  };
}

/**
 * The key of a head: the short key, a link to its page where the reader may open it and the key
 * alone where they may not — a screen reader hears that they cannot open it —, the team's name
 * before it where the ticket is of another team than the page's, and for a ticket the reader may
 * not see the placeholder `<team> [Confidential]` and nothing else. `linked` false shows the key
 * without its link, as an option of a choice does.
 */
@Component({
  selector: 'app-head-key',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [RouterLink, Tooltip],
  template: `
    @let shown = head();
    @if (short(); as short) {
      @if (elsewhere()) {
        <span class="team" data-testid="head-team">{{ shown.team.name }}</span>
      }
      @if (route(); as route) {
        <a class="key tabular" [routerLink]="route" data-testid="head-key">{{ short }}</a>
      } @else if (shown.readable) {
        <span class="key tabular" data-testid="head-key">{{ short }}</span>
      } @else {
        <span
          class="key tabular unlinked"
          data-testid="head-key"
          pTooltip="You cannot open it: you see its key, title, type and state only"
          [showDelay]="400"
          >{{ short }}<span class="sr-only">, which you cannot open</span></span
        >
      }
    } @else {
      <span
        class="placeholder"
        data-testid="head-placeholder"
        pTooltip="A confidential ticket you may not see"
        [showDelay]="400"
        ><i class="pi pi-lock" aria-hidden="true"></i>{{ placeholder() }}</span
      >
    }
  `,
  styles: `
    :host {
      display: inline-flex;
      align-items: center;
      gap: 0.375rem;
      min-width: 0;
    }
    .team {
      flex: none;
      padding: 0 0.375rem;
      border-radius: 999px;
      font-size: 0.6875rem;
      line-height: 1.25rem;
      white-space: nowrap;
      color: var(--p-text-muted-color);
      box-shadow: inset 0 0 0 1px var(--p-app-border);
    }
    .key {
      white-space: nowrap;
    }
    .unlinked {
      color: var(--p-text-muted-color);
    }
    .placeholder {
      display: inline-flex;
      align-items: center;
      gap: 0.375rem;
      color: var(--p-text-muted-color);
      .pi {
        font-size: 0.75rem;
      }
    }
    .sr-only {
      position: absolute;
      width: 1px;
      height: 1px;
      overflow: hidden;
      clip-path: inset(50%);
      white-space: nowrap;
    }
  `,
})
export class HeadKey {
  readonly head = input.required<TicketHead>();
  /** The team the page shows: a head of another team names its team. */
  readonly here = input<string | null>(null);
  readonly linked = input(true);

  protected readonly short = computed(() => shortKeyOf(this.head()));
  protected readonly elsewhere = computed(() => ofAnotherTeam(this.head(), this.here()));
  protected readonly route = computed(() => (this.linked() ? headRoute(this.head()) : null));
  protected readonly placeholder = computed(() => placeholderText(this.head()));
}

/**
 * A ticket a choice offers — a parent, the other end of a link —: its type, its key, with its team
 * where it is of another team than `here`, its title and its state; the placeholder for a ticket
 * the reader may not see. Never a link: picking it is the choice's.
 */
@Component({
  selector: 'app-ticket-choice',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [HeadKey, StateBadge, TypeIcon],
  template: `
    @let shown = head();
    @if (shown.type; as type) {
      <app-type [value]="type" />
    }
    <app-head-key [head]="shown" [here]="here()" [linked]="false" />
    @if (shown.title !== null) {
      <span class="title">{{ shown.title }}</span>
    }
    @if (shown.state; as state) {
      <app-state [value]="state" />
    }
  `,
  styles: `
    :host {
      display: inline-flex;
      align-items: center;
      gap: 0.375rem;
      min-width: 0;
      max-width: 100%;
    }
    .title {
      min-width: 0;
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
    }
  `,
})
export class TicketChoice {
  readonly head = input.required<TicketHead>();
  /** The team the page shows: a ticket of another team names its team. */
  readonly here = input<string | null>(null);
}
