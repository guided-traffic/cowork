import {
  ChangeDetectionStrategy,
  Component,
  computed,
  effect,
  ElementRef,
  inject,
  Injector,
  signal,
  untracked,
} from '@angular/core';
import { takeUntilDestroyed } from '@angular/core/rxjs-interop';
import { FormsModule } from '@angular/forms';
import { ConfirmationService, MessageService } from 'primeng/api';
import { ButtonDirective } from 'primeng/button';
import { Message } from 'primeng/message';
import { Paginator } from 'primeng/paginator';
import { Select } from 'primeng/select';
import { TableModule } from 'primeng/table';
import { Tooltip } from 'primeng/tooltip';
import { Member, MembershipSource, Role } from '../../api/models';
import { changesMemberships, EventStreamService } from '../../core/event-stream.service';
import { MembersService } from '../../core/members.service';
import { ProblemService } from '../../core/problem.service';
import { refresh } from '../../core/refresh';
import { SessionService } from '../../core/session.service';
import { perPageOptions, tablePages } from '../../core/table-pages';
import { TenantService } from '../../core/tenant.service';
import { ConfirmDialog } from '../../shared/confirm-dialog';
import { refocus } from '../../shared/refocus';
import { AddMemberDialog } from './add-member-dialog';
import { SelfGrant } from './self-grant';
import {
  higher,
  lastAdminNotice,
  originAccents,
  originMeanings,
  roleFrom,
  roleMeanings,
  roles,
} from './roles';

/**
 * The tenant's members (docs/adr/0034 D7) in numbered pages of 25, 50 or 100 (docs/adr/0048 D2,
 * D4), loaded again on `membership.changed`, a resync and a poll: each with the effective role — the
 * higher of the mapped and the granted one (docs/adr/0030 D4) — and where it comes from, the group
 * mapping, the grant and the local account, as badges. The tenant's administrators see the e-mail
 * address under the name, which tells two persons of one name apart: the list carries it for them
 * only. They add a person by e-mail address or username, change a member's grant in the row and
 * remove it; a removal asks first and says what stays. A change that takes the administrator's own
 * administrator role away asks first as well. Anybody else sees the list without the controls
 * (D8). A global administrator who holds a role below `admin` here is offered to raise their own
 * grant above the list ({@link SelfGrant}, docs/adr/0034 D2); one without a role is offered the
 * grant above every page of the tenant. A refusal the page can explain — the tenant's last
 * administrator, a person who is gone — is its message above the list; anything else is a toast. When a dialog closes, the keyboard goes
 * back to the control it came from, or where that control went: the row's select, the row that
 * took its place, the heading.
 */
@Component({
  selector: 'app-members',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [
    AddMemberDialog,
    ButtonDirective,
    ConfirmDialog,
    FormsModule,
    Message,
    Paginator,
    Select,
    SelfGrant,
    TableModule,
    Tooltip,
  ],
  providers: [ConfirmationService],
  templateUrl: './members.html',
  styleUrl: './members.scss',
})
export class Members {
  protected readonly members = inject(MembersService);
  protected readonly session = inject(SessionService);
  protected readonly tenant = inject(TenantService);
  private readonly problems = inject(ProblemService);
  private readonly messages = inject(MessageService);
  private readonly confirm = inject(ConfirmationService);
  private readonly injector = inject(Injector);
  private readonly host = inject<ElementRef<HTMLElement>>(ElementRef);

  protected readonly roles = roles;
  protected readonly perPageOptions = perPageOptions;
  /** The page of members shown, apart from the every-member list the pickers read. */
  protected readonly table = tablePages(
    () => this.session.tenant() ?? undefined,
    (tenant, page, perPage) => this.members.page(tenant, page, perPage),
  );
  /** The sources of a membership with a badge, each explained once on the page. */
  protected readonly sources: (MembershipSource | 'local')[] = ['mapping', 'grant', 'local'];
  /** A row stays the row of its person when the list loads again, and the focus in it with it. */
  protected readonly byPerson = (_: number, member: Member) => member.person.id;
  protected readonly adding = signal(false);
  /**
   * The grants on their way, by person: a row's select shows the role it was set to until the
   * answer is in the list, and goes back to the list's when the change is refused or not confirmed.
   */
  protected readonly pending = signal<ReadonlyMap<string, Role>>(new Map());
  /** What the page says about the last change it could not make. */
  protected readonly notice = signal<string | null>(null);

  protected readonly failure = computed(() => {
    const error = this.table.rows.error();
    if (!error) {
      return undefined;
    }
    const problem = this.problems.read(error);
    return `The members could not be loaded: ${problem.detail || problem.title}`;
  });

  constructor() {
    inject(EventStreamService)
      .events.pipe(takeUntilDestroyed())
      .subscribe((event) => {
        if (changesMemberships(event, this.session.tenant())) {
          refresh(this.table.rows, this.injector);
        }
      });
    // A dialog, a question and a message belong to the tenant they came from; another tenant's page
    // starts clean. The page is reused when only the tenant of the path changes: a question
    // answered then would act on the person of the same id in the tenant shown now, and the grant
    // it holds would stay on that person's select.
    effect(() => {
      this.session.tenant();
      untracked(() => {
        this.adding.set(false);
        this.confirm.close();
        this.pending.set(new Map());
        this.notice.set(null);
      });
    });
  }

  protected roleMeaning(role: Role): string {
    return roleMeanings[role];
  }

  protected originMeaning(source: MembershipSource | 'local'): string {
    return originMeanings[source];
  }

  protected originAccent(source: MembershipSource | 'local'): string {
    return originAccents[source];
  }

  protected isOwn(member: Member): boolean {
    return member.person.id === this.session.person()?.id;
  }

  protected grantOf(member: Member): Role | null {
    return roleFrom(member.origins, 'grant');
  }

  /** The grant the row's select shows: the one on its way, else the list's. */
  protected shownGrant(member: Member): Role | null {
    return this.pending().get(member.person.id) ?? this.grantOf(member);
  }

  /** The add dialog opens or closes; closed, it gives the keyboard back to Add member. */
  protected addingChange(open: boolean): void {
    this.adding.set(open);
    if (!open) {
      this.refocus('[data-testid="add-member"]');
    }
  }

  protected added(member: Member): void {
    refresh(this.table.rows, this.injector);
    this.messages.add({
      severity: 'success',
      summary: 'Member added',
      detail: `${member.person.display_name} holds the role ${member.role} here.`,
      life: 4000,
    });
  }

  /**
   * Gives the member a grant with the role, or changes theirs; asks first where it would take the
   * administrator's own administrator role away.
   */
  protected changeGrant(member: Member, role: Role): void {
    if (role === this.grantOf(member)) {
      return;
    }
    this.hold(member, role);
    const after = higher(roleFrom(member.origins, 'mapping'), role);
    if (!this.losesOwnAdmin(member, after)) {
      void this.writeGrant(member, role);
      return;
    }
    this.confirm.confirm({
      header: 'Give up your administrator role?',
      message:
        `Your role here would be ${after}: you could no longer administer this team, and ` +
        'only another administrator could give the role back.',
      acceptLabel: 'Change my grant',
      rejectLabel: 'Keep it',
      acceptButtonProps: { severity: 'danger' },
      defaultFocus: 'reject',
      accept: () => {
        // The role goes, and the controls with it: the keyboard waits on the heading.
        this.refocus('h1');
        void this.writeGrant(member, role).then((written) => {
          if (!written) {
            this.refocus(this.select(member.person.id), 'h1');
          }
        });
      },
      reject: () => {
        this.release(member);
        this.refocus(this.select(member.person.id), 'h1');
      },
    });
  }

  /** Removes the member's grant, after saying what stays: the mapped role, or nothing. */
  protected removeGrant(member: Member): void {
    const name = member.person.display_name;
    const mapped = roleFrom(member.origins, 'mapping');
    const own = this.isOwn(member);
    const stays = mapped
      ? own
        ? `You keep the role ${mapped} through a group mapping.`
        : `${name} keeps the role ${mapped} through a group mapping: a mapped membership stays until the mapping or their groups change.`
      : own
        ? 'You have no other membership here and leave this team.'
        : `${name} has no other membership here and is no longer a member of this team.`;
    const id = member.person.id;
    const lost = this.losesOwnAdmin(member, mapped);
    const warning = lost
      ? ' You could no longer administer this team, and only another administrator could give the role back.'
      : '';
    this.confirm.confirm({
      header: own ? 'Remove your grant?' : `Remove the grant of ${name}?`,
      message: stays + warning,
      acceptLabel: 'Remove grant',
      rejectLabel: 'Keep it',
      acceptButtonProps: { severity: 'danger' },
      defaultFocus: 'reject',
      accept: async () => {
        // The keyboard goes where the grant leaves something to work on: the row, where a mapping
        // keeps the member; the row that takes its place, where the member leaves; the heading,
        // where the administrator's own controls go.
        this.refocus(...(lost ? [] : mapped ? [this.select(id)] : this.neighbours(id)), 'h1');
        this.notice.set(null);
        try {
          await this.members.removeGrant(id);
          refresh(this.table.rows, this.injector);
          this.messages.add({
            severity: 'success',
            summary: 'Grant removed',
            detail: mapped
              ? `${name} keeps the role ${mapped} through a group mapping.`
              : `${name} is no longer a member of this team.`,
            life: 4000,
          });
        } catch (error) {
          this.refused(error, member);
          this.refocus(this.removeButton(id), this.select(id), 'h1');
        }
      },
      reject: () => this.refocus(this.removeButton(id), 'h1'),
    });
  }

  private refocus(...targets: string[]): void {
    refocus(this.host.nativeElement, this.injector, ...targets);
  }

  /** The grant's select in the row of the person. */
  private select(personId: string): string {
    return `tr[data-row="${personId}"] [role="combobox"]`;
  }

  /** The removal of the grant in the row of the person. */
  private removeButton(personId: string): string {
    return `tr[data-row="${personId}"] .remove`;
  }

  /** The selects of the rows beside the person's on the page, the next one first. */
  private neighbours(personId: string): string[] {
    const list = this.table.items();
    const at = list.findIndex((each) => each.person.id === personId);
    return [list[at + 1], list[at - 1]].flatMap((each) =>
      each ? [this.select(each.person.id)] : [],
    );
  }

  /** Whether the administrator's own administrator role would go: their row, and a role after it below admin. */
  private losesOwnAdmin(member: Member, after: Role | null): boolean {
    return this.isOwn(member) && member.role === 'admin' && after !== 'admin';
  }

  /** Writes the grant; says whether it went through. */
  private async writeGrant(member: Member, role: Role): Promise<boolean> {
    this.notice.set(null);
    try {
      this.shown(await this.members.setGrant(member.person.id, role));
      return true;
    } catch (error) {
      this.refused(error, member);
      return false;
    } finally {
      this.release(member);
    }
  }

  /**
   * Puts the member as the answer has them into the page at once — the row's select must not jump
   * back while the page loads again — and loads the page again.
   */
  private shown(member: Member): void {
    if (this.table.rows.hasValue()) {
      const held = this.table.rows.value();
      this.table.rows.set({
        ...held,
        items: held.items.map((each) => (each.person.id === member.person.id ? member : each)),
      });
    }
    refresh(this.table.rows, this.injector);
  }

  private hold(member: Member, role: Role): void {
    this.pending.update((held) => new Map(held).set(member.person.id, role));
  }

  private release(member: Member): void {
    this.pending.update((held) => {
      const next = new Map(held);
      next.delete(member.person.id);
      return next;
    });
  }

  private refused(error: unknown, member: Member): void {
    const problem = this.problems.read(error);
    if (problem.code === 'last_admin') {
      this.notice.set(lastAdminNotice);
    } else if (problem.code === 'person_not_found') {
      this.notice.set(`${member.person.display_name} is no longer a member of this team.`);
      refresh(this.members.members, this.injector);
      refresh(this.table.rows, this.injector);
    } else {
      this.problems.report(error);
    }
  }
}
