import {
  ChangeDetectionStrategy,
  Component,
  computed,
  effect,
  inject,
  input,
  signal,
  untracked,
} from '@angular/core';
import { FormsModule } from '@angular/forms';
import { Router } from '@angular/router';
import { ConfirmationService } from 'primeng/api';
import { ButtonDirective } from 'primeng/button';
import { InputNumber } from 'primeng/inputnumber';
import { InputText } from 'primeng/inputtext';
import { Textarea } from 'primeng/textarea';
import { WipLimits } from '../../api/models';
import { ProblemService } from '../../core/problem.service';
import { ProjectsService } from '../../core/projects.service';
import { SessionService } from '../../core/session.service';
import { TenantService } from '../../core/tenant.service';
import { ConfirmDialog } from '../../shared/confirm-dialog';
import { ProjectAccess } from './project-access';

/**
 * The limits a project can set (docs/adr/0019 D3), in the order of the board's columns and named
 * as the board names them (docs/adr/0018 D1), with the states each counts: the Refinement column
 * holds `filed` and `analysed` and counts against the `analysed` limit.
 */
const limited: { key: keyof WipLimits; column: string; counts: string }[] = [
  { key: 'analysed', column: 'Refinement', counts: 'filed and analysed' },
  { key: 'decided', column: 'Ready', counts: 'decided' },
  { key: 'in-progress', column: 'In Progress', counts: 'in-progress' },
  { key: 'blocked', column: 'Blocked', counts: 'blocked' },
  { key: 'review', column: 'Review', counts: 'review' },
];

/**
 * A project's settings: its name, description and WIP limits (docs/adr/0019), written with the
 * version read (docs/adr/0050 D3); who sees it (docs/adr/0034 D3) and its archive
 * (docs/adr/0006 D4), an administrator's acts. The archive asks first, and its question goes when
 * the page turns to another tenant or another project.
 */
@Component({
  selector: 'app-project-settings',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [
    ButtonDirective,
    ConfirmDialog,
    FormsModule,
    InputNumber,
    InputText,
    ProjectAccess,
    Textarea,
  ],
  providers: [ConfirmationService],
  templateUrl: './project-settings.html',
  styleUrl: './project-settings.scss',
})
export class ProjectSettings {
  readonly project = input.required<string>();

  private readonly projects = inject(ProjectsService);
  private readonly problems = inject(ProblemService);
  private readonly confirm = inject(ConfirmationService);
  private readonly router = inject(Router);
  protected readonly session = inject(SessionService);
  protected readonly tenant = inject(TenantService);

  /** The project as the list has it, whose version a write sends. */
  protected readonly current = computed(() => this.projects.byKey(this.project()));
  /**
   * What the form edits, as the list has it. The list loads again on events that change nothing in
   * it (docs/adr/0054), and the restriction raises the version without touching these fields: only
   * another project or another value of theirs starts the form again, so that neither takes back
   * what is being typed.
   */
  private readonly edited = computed(() => this.current(), {
    equal: (a, b) =>
      a?.key === b?.key &&
      a?.name === b?.name &&
      a?.description === b?.description &&
      limited.every(({ key }) => a?.wip_limits[key] === b?.wip_limits[key]),
  });
  protected readonly columns = limited;
  protected readonly name = signal('');
  protected readonly description = signal('');
  protected readonly limits = signal<WipLimits>({});
  protected readonly saving = signal(false);

  constructor() {
    effect(() => {
      const project = this.edited();
      if (project) {
        this.name.set(project.name);
        this.description.set(project.description);
        this.limits.set({ ...project.wip_limits });
      }
    });
    // The archive's question belongs to the project it names. The page is reused when only the
    // tenant or the project of the path changes: answered then, it would archive a project the
    // page no longer shows — after a tenant switch, the one of the same key in the tenant shown
    // now.
    effect(() => {
      this.session.tenant();
      this.project();
      untracked(() => this.confirm.close());
    });
  }

  protected setLimit(column: keyof WipLimits, value: number | null): void {
    this.limits.update((limits) => {
      const next = { ...limits };
      if (value === null || value === undefined) {
        delete next[column];
      } else {
        next[column] = value;
      }
      return next;
    });
  }

  protected async save(): Promise<void> {
    const project = this.current();
    if (!project) {
      return;
    }
    this.saving.set(true);
    try {
      await this.projects.update(project, {
        name: this.name().trim(),
        description: this.description().trim(),
        wip_limits: this.limits(),
      });
    } catch (error) {
      this.problems.report(error);
    } finally {
      this.saving.set(false);
    }
  }

  protected archive(): void {
    const project = this.current();
    if (!project) {
      return;
    }
    this.confirm.confirm({
      header: `Archive ${project.key}?`,
      message: 'Its tickets and keys stay; it leaves the lists and takes no new ticket.',
      acceptLabel: 'Archive',
      rejectLabel: 'Keep it',
      acceptButtonProps: { severity: 'danger' },
      accept: async () => {
        try {
          await this.projects.archive(project);
          await this.router.navigate(['/t', this.session.tenant()]);
        } catch (error) {
          this.problems.report(error);
        }
      },
    });
  }
}
