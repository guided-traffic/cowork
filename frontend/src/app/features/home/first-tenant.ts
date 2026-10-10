import {
  ChangeDetectionStrategy,
  Component,
  computed,
  inject,
  linkedSignal,
  signal,
} from '@angular/core';
import { FormsModule } from '@angular/forms';
import { Router } from '@angular/router';
import { ButtonDirective } from 'primeng/button';
import { InputText } from 'primeng/inputtext';
import { ProblemService } from '../../core/problem.service';
import { TenantsService } from '../../core/tenants.service';
import { describedBy } from '../../shared/field-aria';

/** A tenant slug: lower case, two to 63 characters, a letter or a digit first (docs/adr/0005 D4). */
export const tenantSlug = /^[a-z0-9][a-z0-9-]{1,62}$/;

/**
 * The start of an installation that has no tenant for the person to work in (docs/adr/0032 D5):
 * a global administrator makes the first one, and becomes its administrator (D7). The slug is
 * checked as it is typed, because it goes into every URL and never changes (docs/adr/0005 D4).
 * When the tenant exists the person is taken into it. A retry of the same slug and name — after
 * an answer was lost — sends the same Idempotency-Key, so the server answers the first attempt
 * again instead of refusing the slug as taken (docs/adr/0045).
 */
@Component({
  selector: 'app-first-tenant',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [ButtonDirective, FormsModule, InputText],
  template: `
    <section class="first" aria-labelledby="first-tenant-title" data-testid="first-tenant">
      <h1 id="first-tenant-title">Create the first team</h1>
      <p class="muted lead">
        A team holds its own projects and their tickets, apart from every other team of this
        installation; a person may belong to several. You are a global administrator and are not a
        member of any team yet. The team you create makes you its administrator.
      </p>
      <form class="card form" (ngSubmit)="create()">
        <div class="field">
          <label for="first-tenant-slug-input">Slug</label>
          <input
            pInputText
            id="first-tenant-slug-input"
            name="slug"
            maxlength="63"
            autocomplete="off"
            spellcheck="false"
            placeholder="acme"
            [ngModel]="slug()"
            (ngModelChange)="slug.set($event.toLowerCase())"
            [attr.aria-invalid]="slugInvalid() || !!errors()['slug']"
            [attr.aria-describedby]="
              describedBy('first-tenant-slug-hint', errors()['slug'] && 'first-tenant-slug-error')
            "
            data-testid="first-tenant-slug"
          />
          <small
            id="first-tenant-slug-hint"
            [class.error]="slugInvalid()"
            [class.muted]="!slugInvalid()"
            data-testid="first-tenant-slug-hint"
          >
            2 to 63 lower-case letters, digits or hyphens, starting with a letter or a digit. It is
            in every URL and never changes.
          </small>
          @if (errors()['slug']; as error) {
            <small
              class="error"
              id="first-tenant-slug-error"
              role="alert"
              data-testid="first-tenant-slug-error"
              >{{ error }}</small
            >
          }
        </div>
        <div class="field">
          <label for="first-tenant-name-input">Name</label>
          <input
            pInputText
            id="first-tenant-name-input"
            name="name"
            maxlength="200"
            autocomplete="off"
            placeholder="Acme Corp"
            [ngModel]="name()"
            (ngModelChange)="name.set($event)"
            [attr.aria-invalid]="!!errors()['name']"
            [attr.aria-describedby]="describedBy(errors()['name'] && 'first-tenant-name-error')"
            data-testid="first-tenant-name"
          />
          @if (errors()['name']; as error) {
            <small
              class="error"
              id="first-tenant-name-error"
              role="alert"
              data-testid="first-tenant-name-error"
              >{{ error }}</small
            >
          }
        </div>
        <div class="actions">
          <button
            pButton
            type="submit"
            data-testid="first-tenant-create"
            [disabled]="!canCreate() || creating()"
          >
            @if (creating()) {
              <i class="pi pi-spinner pi-spin"></i>
            }
            Create team
          </button>
        </div>
      </form>
    </section>
  `,
  styles: `
    .first {
      display: flex;
      flex-direction: column;
      gap: 1rem;
      max-width: 34rem;
    }
    .lead {
      margin: 0;
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
      > label {
        font-size: 0.8125rem;
        font-weight: 550;
      }
    }
    small {
      font-size: 0.75rem;
    }
    .error {
      color: var(--p-severity-critical);
    }
    .actions {
      display: flex;
      justify-content: flex-end;
    }
  `,
})
export class FirstTenant {
  private readonly tenants = inject(TenantsService);
  private readonly problems = inject(ProblemService);
  private readonly router = inject(Router);

  protected readonly describedBy = describedBy;
  protected readonly slug = signal('');
  protected readonly name = signal('');
  protected readonly creating = signal(false);
  protected readonly errors = signal<Record<string, string>>({});
  protected readonly slugInvalid = computed(
    () => this.slug() !== '' && !tenantSlug.test(this.slug()),
  );
  protected readonly canCreate = computed(
    () => tenantSlug.test(this.slug()) && this.name().trim() !== '',
  );

  /**
   * The Idempotency-Key of the act this form is making: one for each content it holds. The same
   * slug and name sent again send the same key; any change of either makes a new one.
   */
  private readonly key = linkedSignal(() => {
    this.slug();
    this.name();
    return crypto.randomUUID();
  });

  protected async create(): Promise<void> {
    if (!this.canCreate() || this.creating()) {
      return;
    }
    this.creating.set(true);
    this.errors.set({});
    try {
      const tenant = await this.tenants.create(
        { slug: this.slug(), name: this.name().trim() },
        this.key(),
      );
      await this.router.navigate(['/t', tenant.slug]);
    } catch (error) {
      this.errors.set(this.problems.report(error, { fields: true }).fields);
    } finally {
      this.creating.set(false);
    }
  }
}
