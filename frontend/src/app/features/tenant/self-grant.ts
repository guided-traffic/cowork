import {
  ChangeDetectionStrategy,
  Component,
  computed,
  effect,
  inject,
  signal,
  untracked,
} from '@angular/core';
import { FormsModule } from '@angular/forms';
import { ConfirmationService, MessageService } from 'primeng/api';
import { ButtonDirective } from 'primeng/button';
import { Select } from 'primeng/select';
import { Role } from '../../api/models';
import { MembersService } from '../../core/members.service';
import { ProblemService } from '../../core/problem.service';
import { SessionService } from '../../core/session.service';
import { ConfirmDialog } from '../../shared/confirm-dialog';
import { roles } from './roles';

/** How many offers the page has made: each names its section by a heading of its own id. */
let offers = 0;

/** What the confirmation says of the grant, the tenant and the role named in it. */
export function selfGrantQuestion(tenant: string, role: Role): string {
  return (
    `You will hold the role ${role} in ${tenant}. The grant is recorded in the team's audit ` +
    'record with you as its actor, and its members see it in the member list.'
  );
}

/**
 * What a global administrator is offered in a tenant in which they do not hold `admin`
 * (docs/adr/0034 D2). Without a role there — above every page of the tenant, and above the tab of the
 * dialog of its configuration, which lies over the page (`TeamConfig`) — that they see its
 * administration, the members, the group mappings, the settings, and none of its work; with a lower
 * role — on the members page — the role they hold. Either way *Grant yourself a role*, with the
 * role chosen beside it, `admin` unless they pick another above the one they hold, and the act asks
 * first, naming the tenant and the role and saying that the tenant sees the grant in its audit
 * record. The grant is `PUT …/members/{their id}/grant`; once `me` holds `admin` this goes, and with
 * a lower role the pages are a member's of it. A question belongs to the tenant it was asked in:
 * another tenant's pages close it.
 */
@Component({
  selector: 'app-self-grant',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [ButtonDirective, ConfirmDialog, FormsModule, Select],
  providers: [ConfirmationService],
  template: `
    <app-confirm-dialog />
    <section class="self-grant" [attr.aria-labelledby]="titleId" data-testid="self-grant">
      <i class="pi pi-shield mark" aria-hidden="true"></i>
      <div class="text">
        @if (held(); as role) {
          <h2 [id]="titleId">You hold the role {{ role }} in {{ name() }}</h2>
          <p class="muted">
            As a global administrator you may raise your own grant here, to administer this team or
            to do more of its work: the team sees the change in its audit record.
          </p>
        } @else {
          <h2 [id]="titleId">You have no role in {{ name() }}</h2>
          <p class="muted">
            As a global administrator you see this team's members, group mappings and settings, and
            none of its projects, tickets or time. To work in it or administer it, grant yourself a
            role: the team sees the grant in its audit record.
          </p>
        }
      </div>
      <div class="act">
        <p-select
          [options]="roles()"
          [ngModel]="role()"
          (ngModelChange)="role.set($event)"
          size="small"
          appendTo="body"
          [disabled]="granting()"
          ariaLabel="The role to grant yourself"
          data-testid="self-grant-role"
        />
        <button
          pButton
          type="button"
          size="small"
          [disabled]="granting()"
          (click)="ask()"
          data-testid="grant-yourself"
        >
          @if (granting()) {
            <i class="pi pi-spinner pi-spin"></i>
          } @else {
            <i class="pi pi-user-plus"></i>
          }
          Grant yourself a role
        </button>
      </div>
    </section>
  `,
  styles: `
    :host {
      display: block;
    }
    .self-grant {
      display: flex;
      flex-wrap: wrap;
      align-items: center;
      gap: 0.75rem 1rem;
      max-width: 64rem;
      padding: 1rem 1.25rem;
      border: 1px solid var(--p-app-border);
      border-left: 3px solid var(--p-primary-color);
      border-radius: var(--p-border-radius-xl);
      background: var(--p-app-panel);
    }
    .mark {
      color: var(--p-primary-color);
      font-size: 1.25rem;
    }
    .text {
      flex: 1 1 24rem;
      display: flex;
      flex-direction: column;
      gap: 0.25rem;
    }
    h2 {
      margin: 0;
      font-size: 1rem;
    }
    p {
      margin: 0;
    }
    .act {
      display: flex;
      align-items: center;
      gap: 0.5rem;
    }
  `,
})
export class SelfGrant {
  /**
   * The heading's id, one per offer: a team the person only oversees shows one above its dashboard
   * and another in the dialog of its configuration, which lies over it.
   */
  protected readonly titleId = `self-grant-title-${++offers}`;
  private readonly session = inject(SessionService);
  private readonly members = inject(MembersService);
  private readonly problems = inject(ProblemService);
  private readonly messages = inject(MessageService);
  private readonly confirm = inject(ConfirmationService);

  /** The role the person holds in the tenant, mapped or granted; null without one. */
  protected readonly held = computed(() => this.session.membership()?.role ?? null);
  /** The roles to choose from: every one without a role, those above the one held otherwise. */
  protected readonly roles = computed(() => {
    const held = this.held();
    return held ? roles.slice(roles.indexOf(held) + 1) : roles;
  });
  /** The role to grant, `admin` unless the administrator picks another. */
  protected readonly role = signal<Role>('admin');
  protected readonly granting = signal(false);
  protected readonly name = computed(() => this.session.shown()?.name ?? this.session.tenant());

  constructor() {
    // The question and the choice belong to the tenant they came from.
    effect(() => {
      this.session.tenant();
      untracked(() => {
        this.confirm.close();
        this.role.set('admin');
      });
    });
  }

  /** Asks first, naming the tenant and the role and saying that the tenant sees the grant. */
  protected ask(): void {
    const tenant = this.session.tenant();
    const person = this.session.person()?.id;
    const name = this.name();
    const role = this.role();
    if (!tenant || !person || !name) {
      return;
    }
    this.confirm.confirm({
      header: 'Grant yourself a role?',
      message: selfGrantQuestion(name, role),
      acceptLabel: `Grant yourself ${role}`,
      rejectLabel: 'Cancel',
      defaultFocus: 'reject',
      accept: () => void this.grant(tenant, person, name, role),
    });
  }

  private async grant(tenant: string, person: string, name: string, role: Role): Promise<void> {
    if (this.session.tenant() !== tenant) {
      return;
    }
    this.granting.set(true);
    try {
      await this.members.setGrant(person, role);
      this.messages.add({
        severity: 'success',
        summary: 'Role granted',
        detail: `You hold the role ${role} in ${name}.`,
        life: 4000,
      });
    } catch (error) {
      this.problems.report(error);
    } finally {
      this.granting.set(false);
    }
  }
}
