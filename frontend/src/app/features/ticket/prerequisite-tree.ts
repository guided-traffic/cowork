import { ChangeDetectionStrategy, Component, computed, inject } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { RouterLink } from '@angular/router';
import { SelectButton } from 'primeng/selectbutton';
import { Skeleton } from 'primeng/skeleton';
import { Tooltip } from 'primeng/tooltip';
import { PrerequisiteNode } from '../../api/models';
import { ProblemService } from '../../core/problem.service';
import { splitKey } from '../../core/tickets.service';
import { StateBadge } from '../../shared/badges';
import { StageBar } from '../../shared/stage-bar';
import { Stage, stageOfState } from '../../shared/stages';
import { TicketRelations, TreeDirection } from './ticket-relations';

/** The stage a node works on, as the board's card shows it; none for one that waits or is closed. */
export function nodeStage(node: PrerequisiteNode): Stage | null {
  return stageOfState(
    node.state === 'blocked' && node.blocked_from ? node.blocked_from : node.state,
  );
}

/** A node's value of a stage, as the ticket shows it. */
export function nodeValue(node: PrerequisiteNode, stage: Stage): number {
  switch (stage) {
    case 'refinement':
      return node.progress_refinement;
    case 'implementation':
      return node.progress;
    case 'review':
      return node.progress_review;
  }
}

const directions: { label: string; value: TreeDirection }[] = [
  { label: 'Prerequisites', value: 'down' },
  { label: 'Dependents', value: 'up' },
];

/**
 * The prerequisite tree on the detail page (docs/adr/0012 D6, docs/adr/0018 D2): what has to be
 * done before the ticket can be finished — the tickets that block it, what blocks those, and so on
 * — or read upward, what waits for it. Each node with its key, title, state, assignee and the bar of
 * the stage it works on; the settled ones — done or dropped — marked, a ticket the tree holds under
 * two others marked the second time; the count of the open ones over the whole tree. What the
 * person cannot see is not in it (docs/adr/0065 D5). The tree loads again when a link of the ticket
 * or a ticket of the tree changes ({@link TicketRelations}).
 */
@Component({
  selector: 'app-prerequisite-tree',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [FormsModule, RouterLink, SelectButton, Skeleton, StageBar, StateBadge, Tooltip],
  template: `
    <div class="head">
      <h2>
        {{ down() ? 'Prerequisites' : 'Dependents' }}
        @if (relations.tree.hasValue()) {
          <span class="count muted" data-testid="tree-open">{{ open() }} open</span>
        }
      </h2>
      <p-selectbutton
        [options]="directions"
        optionLabel="label"
        optionValue="value"
        [ngModel]="relations.direction()"
        (ngModelChange)="relations.direction.set($event)"
        [allowEmpty]="false"
        size="small"
        ariaLabel="Which way the tree is read"
        data-testid="tree-direction"
      />
    </div>
    @if (relations.tree.error(); as error) {
      <p class="muted" data-testid="tree-failed">{{ failure(error) }}</p>
    } @else if (relations.tree.hasValue()) {
      <ul class="tree" data-testid="tree">
        @for (node of nodes(); track $index) {
          <li
            class="node"
            [class.settled]="node.settled"
            [class.repeated]="node.repeated"
            [style.padding-left.rem]="(node.depth - 1) * 1.25"
            [attr.data-testid]="'tree-node-' + $index"
            [attr.data-key]="node.key"
          >
            <span class="line">
              @if (node.settled) {
                <i
                  class="pi pi-check-circle settled-mark"
                  pTooltip="Settled: done or dropped"
                  aria-label="settled"
                ></i>
              }
              <a
                class="key tabular"
                [routerLink]="['/t', tenantOf(node), 'tickets', shortKey(node)]"
                >{{ shortKey(node) }}</a
              >
              <span class="title">{{ node.title }}</span>
            </span>
            <span class="facts">
              <app-state [value]="node.state" />
              <span class="muted small">{{ node.assignee?.display_name ?? 'unassigned' }}</span>
              @if (stageOf(node); as stage) {
                <app-stage-bar
                  class="bar"
                  [stage]="stage"
                  [value]="valueOf(node, stage)"
                  [derived]="node.progress_derived"
                />
              }
              @if (node.repeated) {
                <span
                  class="muted small"
                  pTooltip="It stands in the tree in full under another ticket, with what lies behind it"
                  >also above</span
                >
              }
            </span>
          </li>
        } @empty {
          <li class="muted" data-testid="tree-empty">
            {{ down() ? 'Nothing blocks this ticket.' : 'This ticket blocks nothing.' }}
          </li>
        }
      </ul>
      @if (more()) {
        <p class="muted small" data-testid="tree-more">
          The tree goes on beyond what is shown here.
        </p>
      }
    } @else {
      <p-skeleton height="3rem" />
    }
  `,
  styles: `
    .head {
      display: flex;
      flex-wrap: wrap;
      align-items: center;
      justify-content: space-between;
      gap: 0.5rem;
      margin-bottom: 0.75rem;
      h2 {
        margin: 0;
        font-size: 0.875rem;
      }
    }
    .count {
      font-weight: 400;
    }
    .tree {
      margin: 0;
      padding: 0;
      list-style: none;
    }
    .node {
      display: flex;
      flex-wrap: wrap;
      align-items: center;
      justify-content: space-between;
      gap: 0.25rem 0.75rem;
      padding-top: 0.375rem;
      padding-bottom: 0.375rem;
      border-top: 1px solid var(--p-app-border);
      font-size: 0.8125rem;
      &:first-child {
        border-top: 0;
      }
      &.settled .title {
        color: var(--p-text-muted-color);
        text-decoration: line-through;
      }
      &.repeated {
        opacity: 0.75;
      }
    }
    .line {
      display: flex;
      align-items: baseline;
      gap: 0.5rem;
      min-width: 0;
    }
    .title {
      overflow: hidden;
      text-overflow: ellipsis;
    }
    .settled-mark {
      color: var(--p-state-done);
    }
    .facts {
      display: flex;
      align-items: center;
      gap: 0.5rem;
    }
    .bar {
      width: 4rem;
    }
    .small {
      font-size: 0.75rem;
    }
  `,
})
export class PrerequisiteTree {
  protected readonly relations = inject(TicketRelations);
  private readonly problems = inject(ProblemService);

  protected readonly directions = directions;
  protected readonly down = computed(() => this.relations.direction() === 'down');
  protected readonly nodes = computed<PrerequisiteNode[]>(() =>
    this.relations.tree.hasValue() ? this.relations.tree.value().items : [],
  );
  protected readonly open = computed(() =>
    this.relations.tree.hasValue() ? this.relations.tree.value().open : 0,
  );
  protected readonly more = computed(
    () => this.relations.tree.hasValue() && this.relations.tree.value().next_cursor !== null,
  );

  protected tenantOf(node: PrerequisiteNode): string {
    return splitKey(node.key).tenant;
  }

  protected shortKey(node: PrerequisiteNode): string {
    return splitKey(node.key).key;
  }

  protected stageOf(node: PrerequisiteNode): Stage | null {
    return nodeStage(node);
  }

  protected valueOf(node: PrerequisiteNode, stage: Stage): number {
    return nodeValue(node, stage);
  }

  protected failure(error: unknown): string {
    const problem = this.problems.read(error);
    return `Could not load this: ${problem.detail || problem.title}`;
  }
}
