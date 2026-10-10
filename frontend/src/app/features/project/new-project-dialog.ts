import {
  ChangeDetectionStrategy,
  Component,
  computed,
  inject,
  input,
  linkedSignal,
  model,
  signal,
} from '@angular/core';
import { FormsModule } from '@angular/forms';
import { Router } from '@angular/router';
import { ButtonDirective } from 'primeng/button';
import { Dialog } from 'primeng/dialog';
import { InputText } from 'primeng/inputtext';
import { Textarea } from 'primeng/textarea';
import { ProblemService } from '../../core/problem.service';
import { ProjectsService } from '../../core/projects.service';
import { SessionService } from '../../core/session.service';

/** A project key: upper case, no hyphen, two to ten characters (docs/adr/0007 D1). */
export const projectKey = /^[A-Z][A-Z0-9]{1,9}$/;

/**
 * Creates a project in a team (docs/adr/0006) — the team of the sidebar's plus it was opened from,
 * which need not be the team the pages show (docs/adr/0023 D4 as amended 2026-10-10): its key,
 * which no ticket key can contain a hyphen of and which never changes (docs/adr/0007 D5), its name
 * and description. Created, the project's board opens.
 */
@Component({
  selector: 'app-new-project-dialog',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [ButtonDirective, Dialog, FormsModule, InputText, Textarea],
  template: `
    <p-dialog
      [visible]="visible()"
      (visibleChange)="visible.set($event)"
      [modal]="true"
      [draggable]="false"
      [dismissableMask]="true"
      [style]="{ width: '32rem' }"
      [header]="header()"
      data-testid="new-project-dialog"
    >
      <form class="form" (ngSubmit)="save()">
        <div class="row">
          <label class="field key">
            <span>Key</span>
            <input
              pInputText
              name="key"
              maxlength="10"
              placeholder="COW"
              autocomplete="off"
              [ngModel]="key()"
              (ngModelChange)="key.set($event.toUpperCase())"
              data-testid="project-key"
            />
          </label>
          <label class="field">
            <span>Name</span>
            <input
              pInputText
              name="name"
              maxlength="200"
              placeholder="cowork"
              autocomplete="off"
              [ngModel]="name()"
              (ngModelChange)="name.set($event)"
              data-testid="project-name"
            />
          </label>
        </div>
        <small class="muted hint"
          >The key starts every ticket key (COW-12) and never changes.</small
        >
        @if (errors()['key']; as error) {
          <small class="error">{{ error }}</small>
        }
        <label class="field">
          <span>Description</span>
          <textarea
            pTextarea
            name="description"
            rows="3"
            [ngModel]="description()"
            (ngModelChange)="description.set($event)"
            data-testid="project-description"
          ></textarea>
        </label>
        <div class="actions">
          <button
            pButton
            type="button"
            [text]="true"
            severity="secondary"
            (click)="visible.set(false)"
          >
            Cancel
          </button>
          <button
            pButton
            type="submit"
            data-testid="project-save"
            [disabled]="!canSave() || saving()"
          >
            @if (saving()) {
              <i class="pi pi-spinner pi-spin"></i>
            }
            Create project
          </button>
        </div>
      </form>
    </p-dialog>
  `,
  styles: `
    .form {
      display: flex;
      flex-direction: column;
      gap: 0.875rem;
    }
    .row {
      display: grid;
      grid-template-columns: 7rem 1fr;
      gap: 0.75rem;
    }
    .field {
      display: flex;
      flex-direction: column;
      gap: 0.375rem;
      > span:first-child {
        font-size: 0.8125rem;
        font-weight: 550;
      }
    }
    .key input {
      text-transform: uppercase;
      letter-spacing: 0.04em;
    }
    .hint {
      margin-top: -0.375rem;
      font-size: 0.75rem;
    }
    .error {
      color: var(--p-severity-critical);
    }
    .actions {
      display: flex;
      justify-content: flex-end;
      gap: 0.5rem;
    }
  `,
})
export class NewProjectDialog {
  readonly visible = model(false);
  /** The slug of the team the project is created in; null while no team is chosen. */
  readonly team = input<string | null>(null);
  private readonly projects = inject(ProjectsService);
  private readonly problems = inject(ProblemService);
  private readonly session = inject(SessionService);
  private readonly router = inject(Router);

  protected readonly key = signal('');
  protected readonly name = signal('');
  protected readonly description = signal('');
  protected readonly saving = signal(false);
  protected readonly errors = signal<Record<string, string>>({});
  protected readonly canSave = computed(
    () =>
      this.team() !== null && projectKey.test(this.key()) && this.name().trim() !== '' && !this.saving(),
  );
  /** The dialog names the team, which may be another than the one the pages show. */
  protected readonly header = computed(() => {
    const team = this.team();
    const name = this.session.memberships().find((m) => m.team.slug === team)?.team.name ?? team;
    return name ? `New project in ${name}` : 'New project';
  });

  /**
   * The Idempotency-Key of the project this form is creating: one for each content it holds and
   * each team it creates in, so a retry of a lost answer is answered again instead of being
   * refused as a key that is taken; any change, and a project created, make a new one
   * (docs/adr/0045 D3).
   */
  private readonly idempotencyKey = linkedSignal(() => {
    this.team();
    this.key();
    this.name();
    this.description();
    return crypto.randomUUID();
  });

  protected async save(): Promise<void> {
    const team = this.team();
    if (!this.canSave() || team === null) {
      return;
    }
    this.saving.set(true);
    this.errors.set({});
    try {
      const project = await this.projects.create(
        team,
        {
          key: this.key(),
          name: this.name().trim(),
          ...(this.description().trim() ? { description: this.description().trim() } : {}),
        },
        this.idempotencyKey(),
      );
      this.visible.set(false);
      this.key.set('');
      this.name.set('');
      this.description.set('');
      await this.router.navigate(['/t', team, 'p', project.key, 'board']);
    } catch (error) {
      this.errors.set(this.problems.report(error, { fields: true }).fields);
    } finally {
      this.saving.set(false);
    }
  }
}
