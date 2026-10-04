import { ChangeDetectionStrategy, Component, effect, inject, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { ButtonDirective } from 'primeng/button';
import { InputText } from 'primeng/inputtext';
import { ToggleSwitch } from 'primeng/toggleswitch';
import { ProblemService } from '../../core/problem.service';
import { TenantService } from '../../core/tenant.service';

/**
 * The tenant's settings, for its administrators: the name, whether members create projects
 * (docs/adr/0034 D9) and whether members see each other's time (docs/adr/0017), written with
 * the version read (docs/adr/0050 D3).
 */
@Component({
  selector: 'app-tenant-settings',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [ButtonDirective, FormsModule, InputText, ToggleSwitch],
  template: `
    <section class="page">
      <h1>Settings</h1>
      @if (tenant.value(); as t) {
        <form class="card form" (ngSubmit)="save()">
          <label class="field">
            <span>Name</span>
            <input
              pInputText
              name="name"
              [ngModel]="name()"
              (ngModelChange)="name.set($event)"
              [disabled]="!tenant.isAdmin()"
              data-testid="tenant-name-input"
            />
          </label>
          <label class="toggle">
            <p-toggleswitch
              name="membersCreate"
              [ngModel]="membersCreate()"
              (ngModelChange)="membersCreate.set($event)"
              [disabled]="!tenant.isAdmin()"
              data-testid="members-create"
            />
            <span
              >Members may create projects
              <span class="muted">— administrators always may</span></span
            >
          </label>
          <label class="toggle">
            <p-toggleswitch
              name="timeVisible"
              [ngModel]="timeVisible()"
              (ngModelChange)="timeVisible.set($event)"
              [disabled]="!tenant.isAdmin()"
              data-testid="time-visible"
            />
            <span>Members see each other's time entries</span>
          </label>
          @if (tenant.isAdmin()) {
            <div class="actions">
              <button pButton type="submit" data-testid="tenant-save" [disabled]="saving()">
                @if (saving()) {
                  <i class="pi pi-spinner pi-spin"></i>
                }
                Save
              </button>
            </div>
          } @else {
            <p class="muted small">Only the tenant's administrators change these.</p>
          }
        </form>
        <p class="muted small">
          Slug <code>{{ t.slug }}</code
          >, which never changes.
        </p>
      }
    </section>
  `,
  styles: `
    .page {
      display: flex;
      flex-direction: column;
      gap: 1rem;
      max-width: 44rem;
      padding: 2rem;
    }
    h1 {
      font-size: 1.5rem;
    }
    .card {
      padding: 1.25rem;
      border: 1px solid var(--p-app-border);
      border-radius: var(--p-border-radius-xl);
      background: var(--p-app-panel);
    }
    .form {
      display: flex;
      flex-direction: column;
      gap: 1rem;
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
    .toggle {
      display: flex;
      align-items: center;
      gap: 0.75rem;
    }
    .actions {
      display: flex;
      justify-content: flex-end;
    }
    .small {
      font-size: 0.8125rem;
    }
  `,
})
export class TenantSettings {
  protected readonly tenant = inject(TenantService);
  private readonly problems = inject(ProblemService);
  protected readonly name = signal('');
  protected readonly membersCreate = signal(false);
  protected readonly timeVisible = signal(false);
  protected readonly saving = signal(false);

  constructor() {
    effect(() => {
      const tenant = this.tenant.value();
      if (tenant) {
        this.name.set(tenant.name);
        this.membersCreate.set(tenant.members_create_projects);
        this.timeVisible.set(tenant.time_visible_to_members);
      }
    });
  }

  protected async save(): Promise<void> {
    this.saving.set(true);
    try {
      await this.tenant.update({
        name: this.name().trim(),
        members_create_projects: this.membersCreate(),
        time_visible_to_members: this.timeVisible(),
      });
    } catch (error) {
      this.problems.report(error);
    } finally {
      this.saving.set(false);
    }
  }
}
