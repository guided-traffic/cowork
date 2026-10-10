import {
  ChangeDetectionStrategy,
  Component,
  computed,
  inject,
  signal,
  viewChild,
} from '@angular/core';
import { ButtonDirective } from 'primeng/button';
import { Dialog } from 'primeng/dialog';
import { Skeleton } from 'primeng/skeleton';
import { SessionService } from '../../core/session.service';
import { keepOpenWhile } from '../../shared/keep-open';
import { FirstTenant } from './first-tenant';
import { TeamTiles } from './team-tiles';

/**
 * Every team of the installation (docs/adr/0023 D4 as amended 2026-10-10), which a global
 * administrator reaches through "All teams" in the person menu: a tile per team with the role they
 * hold in it or `no role`, each leading to the team's dashboard — and from there, for a team they
 * hold no role in, to its administration (docs/adr/0034 D2). *New team* opens the form of the first
 * team in a dialog (docs/adr/0005 D5); once the team exists its creator, its administrator, finds
 * it in the sidebar, and its dashboard opens. Anybody else who opens the address sees their own
 * teams.
 */
@Component({
  selector: 'app-all-teams',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [ButtonDirective, Dialog, FirstTenant, Skeleton, TeamTiles],
  template: `
    <section class="page">
      <header class="head">
        <h1>{{ globalAdmin() ? 'All teams' : 'Your teams' }}</h1>
        <span class="spacer"></span>
        @if (globalAdmin()) {
          <button pButton type="button" (click)="creating.set(true)" data-testid="new-team">
            <i class="pi pi-plus"></i>New team
          </button>
        }
      </header>
      @if (globalAdmin()) {
        <p class="muted lead">
          Every team of this installation, with your role in it. In a team where you hold no role
          you see its administration only, until you grant yourself one.
        </p>
      }
      @if (listing()) {
        <p-skeleton width="16rem" height="4.5rem" />
      } @else {
        <app-team-tiles />
      }
    </section>

    <p-dialog
      [(visible)]="creating"
      [modal]="true"
      [draggable]="false"
      [closable]="!busy()"
      [dismissableMask]="!busy()"
      header="New team"
      [style]="{ width: '36rem' }"
      data-testid="new-team-dialog"
    >
      @if (creating()) {
        <app-first-tenant [first]="false" />
      }
    </p-dialog>
  `,
  styles: `
    .page {
      display: flex;
      flex-direction: column;
      gap: 1.25rem;
      padding: 2rem;
    }
    .head {
      display: flex;
      align-items: center;
      gap: 0.625rem;
      h1 {
        font-size: 1.5rem;
      }
    }
    .spacer {
      flex: 1;
    }
    .lead {
      margin: 0;
      max-width: 48rem;
    }
  `,
})
export class AllTeams {
  protected readonly session = inject(SessionService);

  protected readonly creating = signal(false);
  private readonly form = viewChild(FirstTenant);
  /** The new team is on its way: the dialog stays open, its refusal lands in its form. */
  protected readonly busy = computed(() => this.form()?.creating() ?? false);

  protected readonly globalAdmin = computed(() => this.session.person()?.global_admin === true);
  /** A global administrator's teams are listed for the first time. */
  protected readonly listing = computed(
    () =>
      this.globalAdmin() &&
      !this.session.installation.hasValue() &&
      this.session.installation.isLoading(),
  );

  constructor() {
    keepOpenWhile(() => this.busy());
  }
}
