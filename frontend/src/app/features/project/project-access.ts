import {
  ChangeDetectionStrategy,
  Component,
  computed,
  effect,
  inject,
  Injector,
  input,
  signal,
  untracked,
} from '@angular/core';
import { FormsModule } from '@angular/forms';
import { ConfirmationService } from 'primeng/api';
import { ButtonDirective } from 'primeng/button';
import { Message } from 'primeng/message';
import { Select } from 'primeng/select';
import { TableModule } from 'primeng/table';
import { ToggleSwitch } from 'primeng/toggleswitch';
import { Project, ProjectAccessEntry, ProjectAccessRole } from '../../api/models';
import { MembersService } from '../../core/members.service';
import { ProblemService } from '../../core/problem.service';
import { ProjectsService } from '../../core/projects.service';
import { refresh } from '../../core/refresh';
import { ConfirmDialog } from '../../shared/confirm-dialog';
import { AccessList } from './access-list';

/** A member the picker offers to put on the list. */
interface Candidate {
  id: string;
  label: string;
}

/** What an entry allows in the project, at most (docs/adr/0034 D3). */
export const accessMeanings: Record<ProjectAccessRole, string> = {
  viewer: 'Reads the project; changes nothing in it.',
  member: 'Works in the project as a member does.',
};

/**
 * What the confirmation of a restriction says: how many people are on the access list, who sees
 * the project then — and, while the list is not loaded, no number at all.
 */
export function restrictionNotice(people: number | undefined): string {
  if (people === undefined) {
    return (
      "Once it is restricted, only the tenant's administrators and the people on its access " +
      'list see it.'
    );
  }
  if (people === 0) {
    return (
      "Nobody is on its access list: once it is restricted, only the tenant's administrators " +
      'see it.'
    );
  }
  const who = people === 1 ? '1 person is' : `${people} people are`;
  return (
    `${who} on its access list. Once it is restricted, only they and the tenant's ` +
    'administrators see it.'
  );
}

/**
 * Who sees a project (docs/adr/0034 D3), for the tenant's administrators: the restriction, and the
 * access list — a member of the tenant with `member` or `viewer` each, added, changed and taken
 * off. The list is offered whether the project is restricted or not, because it may be filled
 * before the restriction, so that nobody on it loses the project in between; it counts while the
 * project is restricted. A restriction asks first, saying how many people are on the list, and so
 * does opening, saying that every member will see the project and its tickets; either is written
 * over the version that was read (docs/adr/0050 D3). A switch or a select shows what it was set to
 * while the question is open and the write is out, and goes back when it is refused. A row takes
 * no second change while its change or its removal is out. Each row and each member the picker
 * offers carry the e-mail address, which the access list and, for its administrators, the member
 * list have.
 */
@Component({
  selector: 'app-project-access',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [
    ButtonDirective,
    ConfirmDialog,
    FormsModule,
    Message,
    Select,
    TableModule,
    ToggleSwitch,
  ],
  providers: [AccessList, ConfirmationService],
  templateUrl: './project-access.html',
  styleUrl: './project-access.scss',
})
export class ProjectAccess {
  readonly project = input.required<Project>();

  protected readonly access = inject(AccessList);
  private readonly projects = inject(ProjectsService);
  private readonly members = inject(MembersService);
  private readonly problems = inject(ProblemService);
  private readonly confirm = inject(ConfirmationService);
  private readonly injector = inject(Injector);

  protected readonly accessRoles: ProjectAccessRole[] = ['viewer', 'member'];
  protected readonly accessMeanings = accessMeanings;
  /** The restriction on its way, shown over the project's until the answer is in the list. */
  protected readonly restricting = signal<boolean | null>(null);
  protected readonly restricted = computed(() => this.restricting() ?? this.project().restricted);
  /** The entries' roles on their way, by person. */
  protected readonly pending = signal<ReadonlyMap<string, ProjectAccessRole>>(new Map());
  /** The entries whose removal is on its way, by person. */
  protected readonly removing = signal<ReadonlySet<string>>(new Set());
  /** What the section says about the last change it could not make. */
  protected readonly notice = signal<string | null>(null);
  /** The member to put on the list, and with which role. */
  protected readonly candidate = signal<string | null>(null);
  protected readonly candidateRole = signal<ProjectAccessRole>('member');
  protected readonly adding = signal(false);
  /**
   * The tenant's members who are not on the list yet, as the picker offers them: by name, with the
   * e-mail address beside it where the member list has one, so that two persons of one name can be
   * told apart. The label is what an option shows, what a screen reader names it and what the
   * filter searches.
   */
  protected readonly candidates = computed<Candidate[]>(() => {
    const listed = new Set(this.access.list().map((entry) => entry.person.id));
    return this.members
      .list()
      .filter((member) => !listed.has(member.person.id))
      .map((member) => ({
        id: member.person.id,
        label: member.email
          ? `${member.person.display_name} (${member.email})`
          : member.person.display_name,
      }));
  });

  protected readonly failure = computed(() => {
    const error = this.access.entries.error();
    if (!error) {
      return undefined;
    }
    const problem = this.problems.read(error);
    return `The access list could not be loaded: ${problem.detail || problem.title}`;
  });

  /**
   * The key of the project of the page. The projects load again on events and hand in a new object
   * for the same project each time; only another key starts the section again.
   */
  private readonly key = computed(() => this.project().key);

  constructor() {
    // The list follows the project of the page; what was chosen, asked or said belongs to the one
    // before, and a restriction confirmed now would be another project's.
    effect(() => {
      const key = this.key();
      untracked(() => {
        this.confirm.close();
        this.restricting.set(null);
        this.access.project.set(key);
        this.notice.set(null);
        this.candidate.set(null);
      });
    });
  }

  /**
   * Restricts or opens the project once the administrator confirmed what that means: a restriction
   * hides it from every member off the list, and opening shows it and its tickets to every member —
   * a confidential ticket excepted, which stays with the administrators, its assignee and its
   * reporter (docs/adr/0065 D1). The switch shows the change, and holds it, while the question is
   * open.
   */
  protected restrict(restricted: boolean): void {
    const key = this.project().key;
    this.restricting.set(restricted);
    this.confirm.confirm({
      ...(restricted
        ? {
            header: `Restrict ${key}?`,
            message: restrictionNotice(
              this.access.entries.hasValue() ? this.access.list().length : undefined,
            ),
            acceptLabel: 'Restrict',
            rejectLabel: 'Keep it open',
          }
        : {
            header: `Open ${key} to every member?`,
            message:
              `${key} and its tickets become visible to every member of the tenant. A ` +
              'confidential ticket stays visible only to the administrators, its assignee and ' +
              'its reporter.',
            acceptLabel: 'Open it',
            rejectLabel: 'Keep it restricted',
          }),
      defaultFocus: 'reject',
      accept: () => void this.write(restricted),
      reject: () => this.restricting.set(null),
    });
  }

  private async write(restricted: boolean): Promise<void> {
    const project = this.project();
    this.restricting.set(restricted);
    this.notice.set(null);
    try {
      await this.projects.restrict(project, restricted);
    } catch (error) {
      if (this.problems.read(error).code === 'precondition_failed') {
        this.notice.set(
          `${project.key} changed meanwhile: the page shows it as it is now. Set it again if you still want it.`,
        );
        refresh(this.projects.projects, this.injector);
      } else {
        this.problems.report(error);
      }
    } finally {
      this.restricting.set(null);
    }
  }

  /** The role the entry's select shows: the one on its way, else the list's. */
  protected shownRole(entry: ProjectAccessEntry): ProjectAccessRole {
    return this.pending().get(entry.person.id) ?? entry.role;
  }

  /** Whether the entry's change or its removal is on its way: its row takes no other meanwhile. */
  protected busy(entry: ProjectAccessEntry): boolean {
    return this.pending().has(entry.person.id) || this.removing().has(entry.person.id);
  }

  protected async changeRole(entry: ProjectAccessEntry, role: ProjectAccessRole): Promise<void> {
    if (role === entry.role || this.busy(entry)) {
      return;
    }
    const id = entry.person.id;
    this.pending.update((held) => new Map(held).set(id, role));
    this.notice.set(null);
    try {
      await this.access.set(id, role);
    } catch (error) {
      this.refused(error, entry.person.display_name);
    } finally {
      this.pending.update((held) => {
        const next = new Map(held);
        next.delete(id);
        return next;
      });
    }
  }

  protected async remove(entry: ProjectAccessEntry): Promise<void> {
    if (this.busy(entry)) {
      return;
    }
    const id = entry.person.id;
    this.removing.update((held) => new Set(held).add(id));
    this.notice.set(null);
    try {
      await this.access.remove(id);
    } catch (error) {
      this.problems.report(error);
    } finally {
      this.removing.update((held) => {
        const next = new Set(held);
        next.delete(id);
        return next;
      });
    }
  }

  protected async add(): Promise<void> {
    const id = this.candidate();
    const member = this.members.list().find((each) => each.person.id === id);
    if (!member || this.adding()) {
      return;
    }
    this.adding.set(true);
    this.notice.set(null);
    try {
      await this.access.set(member.person.id, this.candidateRole());
      this.candidate.set(null);
    } catch (error) {
      this.refused(error, member.person.display_name);
    } finally {
      this.adding.set(false);
    }
  }

  /** A person who left the tenant meanwhile is the section's message; anything else a toast. */
  private refused(error: unknown, name: string): void {
    if (this.problems.read(error).code === 'person_not_found') {
      this.notice.set(`${name} is no longer a member of this tenant.`);
      refresh(this.members.members, this.injector);
      refresh(this.access.entries, this.injector);
    } else {
      this.problems.report(error);
    }
  }
}
