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
import { FormsModule } from '@angular/forms';
import { ConfirmationService, MessageService } from 'primeng/api';
import { ButtonDirective } from 'primeng/button';
import { Message } from 'primeng/message';
import { Select } from 'primeng/select';
import { Skeleton } from 'primeng/skeleton';
import { TableModule } from 'primeng/table';
import { Tooltip } from 'primeng/tooltip';
import { GroupMapping, Role } from '../../api/models';
import { GroupMappingsService } from '../../core/group-mappings.service';
import { ProblemService } from '../../core/problem.service';
import { refresh } from '../../core/refresh';
import { SessionService } from '../../core/session.service';
import { TenantService } from '../../core/tenant.service';
import { ConfirmDialog } from '../../shared/confirm-dialog';
import { refocus } from '../../shared/refocus';
import { dateTime } from '../../shared/time';
import { NewMappingDialog } from './new-mapping-dialog';
import { lastAdminNotice, roleFrom, roles } from './roles';

/** What the mark of the editor's own groups means. */
export const includesYouMeaning =
  'Your groups at the identity provider include this group, as of your last sign-in: its role is yours here.';

/** What the confirmation says when the change takes the editor's own administrator role away. */
const ownAdminWarning = (group: string) =>
  `Your administrator role here comes only from the group ${group}: you lose it, and only another ` +
  'administrator could give it back.';

/**
 * The tenant's group mappings, for its administrators (docs/adr/0030 D2, D7): each gives everyone
 * whose identity-provider groups include its group a role in the tenant, the highest where several
 * match. An administrator adds one, changes its role in the row — over the version that was read
 * (docs/adr/0050 D3) — and removes one, which asks first. A change or a removal that would take
 * the editor's own administrator role away — it comes from this mapping and from no grant and no
 * other mapping of theirs — asks first with a warning (ADR 0030, Consequences). A refusal the page
 * can explain is its message above the list; anything else is a toast. Anybody else is told that
 * the administrators manage the mappings. When a dialog closes, the keyboard goes back to the
 * control it came from, or where that control went: the row that took its place, the heading.
 */
@Component({
  selector: 'app-group-mappings',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [
    ButtonDirective,
    ConfirmDialog,
    FormsModule,
    Message,
    NewMappingDialog,
    Select,
    Skeleton,
    TableModule,
    Tooltip,
  ],
  providers: [ConfirmationService],
  templateUrl: './group-mappings.html',
  styleUrl: './group-mappings.scss',
})
export class GroupMappings {
  protected readonly mappings = inject(GroupMappingsService);
  protected readonly session = inject(SessionService);
  protected readonly tenant = inject(TenantService);
  private readonly problems = inject(ProblemService);
  private readonly messages = inject(MessageService);
  private readonly confirm = inject(ConfirmationService);
  private readonly injector = inject(Injector);
  private readonly host = inject<ElementRef<HTMLElement>>(ElementRef);

  protected readonly roles = roles;
  protected readonly dateTime = dateTime;
  protected readonly includesYouMeaning = includesYouMeaning;
  /** A row stays the row of its mapping when the list loads again, and the focus in it with it. */
  protected readonly byId = (_: number, mapping: GroupMapping) => mapping.id;
  protected readonly creating = signal(false);
  /**
   * The roles on their way, by mapping: a row's select shows the role it was set to until the
   * answer is in the list, and goes back to the list's when the change is refused or not confirmed.
   */
  protected readonly pending = signal<ReadonlyMap<string, Role>>(new Map());
  /** What the page says about the last change it could not make. */
  protected readonly notice = signal<string | null>(null);

  protected readonly failure = computed(() => {
    const error = this.mappings.mappings.error();
    if (!error) {
      return undefined;
    }
    const problem = this.problems.read(error);
    return `The group mappings could not be loaded: ${problem.detail || problem.title}`;
  });

  constructor() {
    // A dialog, a question and a message belong to the tenant they came from; another tenant's page
    // starts clean. The page is reused when only the tenant of the path changes: a question
    // answered then would send the mapping it names to the tenant shown now, and the role it holds
    // would stay on the select of that mapping's id.
    effect(() => {
      this.session.tenant();
      untracked(() => {
        this.creating.set(false);
        this.confirm.close();
        this.pending.set(new Map());
        this.notice.set(null);
      });
    });
  }

  /** The role the row's select shows: the one on its way, else the list's. */
  protected shownRole(mapping: GroupMapping): Role {
    return this.pending().get(mapping.id) ?? mapping.role;
  }

  /** The dialog of a new mapping opens or closes; closed, it gives the keyboard back to New mapping. */
  protected creatingChange(open: boolean): void {
    this.creating.set(open);
    if (!open) {
      this.refocus('[data-testid="new-mapping"]');
    }
  }

  protected created(mapping: GroupMapping): void {
    this.messages.add({
      severity: 'success',
      summary: 'Group mapped',
      detail: `Members of ${mapping.group} hold the role ${mapping.role} here.`,
      life: 4000,
    });
  }

  protected changeRole(mapping: GroupMapping, role: Role): void {
    if (role === mapping.role) {
      return;
    }
    this.hold(mapping, role);
    if (!this.losesOwnAdmin(mapping, role)) {
      void this.write(mapping, role);
      return;
    }
    this.confirm.confirm({
      header: 'Give up your administrator role?',
      message: ownAdminWarning(mapping.group),
      acceptLabel: `Make it ${role}`,
      rejectLabel: 'Keep admin',
      acceptButtonProps: { severity: 'danger' },
      defaultFocus: 'reject',
      accept: () => {
        // The role goes, and the page's controls with it: the keyboard waits on the heading.
        this.refocus('h1');
        void this.write(mapping, role).then((written) => {
          if (!written) {
            this.refocus(this.select(mapping.id), 'h1');
          }
        });
      },
      reject: () => {
        this.release(mapping);
        this.refocus(this.select(mapping.id), 'h1');
      },
    });
  }

  protected remove(mapping: GroupMapping): void {
    const own = this.losesOwnAdmin(mapping, null);
    this.confirm.confirm({
      header: `Remove the mapping of ${mapping.group}?`,
      message:
        'Everyone whose role here comes from this group loses it at once, or keeps the highest ' +
        'role another of their mapped groups gives. Grants stay.' +
        (own ? ` ${ownAdminWarning(mapping.group)}` : ''),
      acceptLabel: 'Remove mapping',
      rejectLabel: 'Keep it',
      acceptButtonProps: { severity: 'danger' },
      defaultFocus: 'reject',
      accept: async () => {
        // The row goes: the keyboard goes to the row that takes its place, or to the heading where
        // the editor's own role, and the page's controls with it, go.
        this.refocus(...(own ? [] : this.neighbours(mapping.id)), 'h1');
        this.notice.set(null);
        try {
          await this.mappings.remove(mapping);
          this.messages.add({
            severity: 'success',
            summary: 'Mapping removed',
            detail: `${mapping.group} gives no role here any more.`,
            life: 4000,
          });
        } catch (error) {
          this.refused(error, mapping);
          this.refocus(this.removeButton(mapping.id), this.select(mapping.id), 'h1');
        }
      },
      reject: () => this.refocus(this.removeButton(mapping.id), 'h1'),
    });
  }

  private refocus(...targets: string[]): void {
    refocus(this.host.nativeElement, this.injector, ...targets);
  }

  /** The role's select in the row of the mapping. */
  private select(id: string): string {
    return `tr[data-row="${id}"] [role="combobox"]`;
  }

  /** The removal in the row of the mapping. */
  private removeButton(id: string): string {
    return `tr[data-row="${id}"] .remove`;
  }

  /** The selects of the rows beside the mapping's, the next one first. */
  private neighbours(id: string): string[] {
    const list = this.mappings.list();
    const at = list.findIndex((each) => each.id === id);
    return [list[at + 1], list[at - 1]].flatMap((each) => (each ? [this.select(each.id)] : []));
  }

  /**
   * Whether the editor's own administrator role would go with the mapping's role: the mapping is
   * theirs and gives `admin`, the role after it does not, and neither a grant of theirs nor
   * another mapping of theirs gives `admin`.
   */
  private losesOwnAdmin(mapping: GroupMapping, after: Role | null): boolean {
    return (
      mapping.includes_caller &&
      mapping.role === 'admin' &&
      after !== 'admin' &&
      roleFrom(this.session.membership()?.origins ?? [], 'grant') !== 'admin' &&
      !this.mappings
        .list()
        .some((other) => other.id !== mapping.id && other.includes_caller && other.role === 'admin')
    );
  }

  /** Writes the role; says whether it went through. */
  private async write(mapping: GroupMapping, role: Role): Promise<boolean> {
    this.notice.set(null);
    try {
      await this.mappings.changeRole(mapping, role);
      return true;
    } catch (error) {
      this.refused(error, mapping);
      return false;
    } finally {
      this.release(mapping);
    }
  }

  private hold(mapping: GroupMapping, role: Role): void {
    this.pending.update((held) => new Map(held).set(mapping.id, role));
  }

  private release(mapping: GroupMapping): void {
    this.pending.update((held) => {
      const next = new Map(held);
      next.delete(mapping.id);
      return next;
    });
  }

  private refused(error: unknown, mapping: GroupMapping): void {
    const problem = this.problems.read(error);
    if (problem.code === 'last_admin') {
      this.notice.set(lastAdminNotice);
    } else if (problem.code === 'precondition_failed' || problem.code === 'not_found') {
      this.notice.set(
        `The mapping of ${mapping.group} changed meanwhile: the list shows it as it is now.`,
      );
      refresh(this.mappings.mappings, this.injector);
    } else {
      this.problems.report(error);
    }
  }
}
