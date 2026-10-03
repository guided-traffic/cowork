import {
  ChangeDetectionStrategy,
  Component,
  computed,
  effect,
  inject,
  input,
  signal,
} from '@angular/core';
import { FormsModule } from '@angular/forms';
import { Router } from '@angular/router';
import { ConfirmationService } from 'primeng/api';
import { ButtonDirective } from 'primeng/button';
import { ConfirmDialog } from 'primeng/confirmdialog';
import { InputNumber } from 'primeng/inputnumber';
import { InputText } from 'primeng/inputtext';
import { Textarea } from 'primeng/textarea';
import { WipLimits } from '../../api/models';
import { ProblemService } from '../../core/problem.service';
import { ProjectsService } from '../../core/projects.service';
import { SessionService } from '../../core/session.service';
import { TenantService } from '../../core/tenant.service';

/** The columns a WIP limit can hold (docs/adr/0019). */
const limited: (keyof WipLimits)[] = ['analysed', 'decided', 'in-progress', 'blocked'];

/**
 * A project's settings: its name, description and WIP limits (docs/adr/0019), written with the
 * version read (docs/adr/0050 D3), and its archive (docs/adr/0006 D4), an administrator's act.
 */
@Component({
  selector: 'app-project-settings',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [ButtonDirective, ConfirmDialog, FormsModule, InputNumber, InputText, Textarea],
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

  protected readonly current = computed(() => this.projects.byKey(this.project()));
  protected readonly columns = limited;
  protected readonly name = signal('');
  protected readonly description = signal('');
  protected readonly limits = signal<WipLimits>({});
  protected readonly saving = signal(false);

  constructor() {
    effect(() => {
      const project = this.current();
      if (project) {
        this.name.set(project.name);
        this.description.set(project.description);
        this.limits.set({ ...project.wip_limits });
      }
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
