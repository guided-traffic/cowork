import {
  ChangeDetectionStrategy,
  Component,
  computed,
  effect,
  inject,
  model,
  output,
  resource,
  signal,
  untracked,
} from '@angular/core';
import { FormsModule } from '@angular/forms';
import { ButtonDirective } from 'primeng/button';
import { Checkbox } from 'primeng/checkbox';
import { Dialog } from 'primeng/dialog';
import { InputNumber } from 'primeng/inputnumber';
import { InputText } from 'primeng/inputtext';
import { Select } from 'primeng/select';
import { Capability, Scope, TokenCreate, TokenCreated } from '../../api/models';
import { CAPABILITY } from '../../api/models/capability-array';
import { ProblemService } from '../../core/problem.service';
import { SessionService } from '../../core/session.service';
import { TokensService } from '../../core/tokens.service';

/** What each scope reaches, in a line (docs/adr/0035 D3), shown beside the choice and in the list. */
export const scopeMeanings: Record<Scope, string> = {
  read: 'Reads what you may read.',
  write: 'Reads, and does what a member does: files and moves tickets, comments, books time.',
  admin:
    'Does what write does, and what your admin role allows: tenant settings, archiving a project.',
};

/** What each capability lets an agent do beyond the baseline (docs/adr/0043 D4). */
export const capabilityMeanings: Record<Capability, string> = {
  decide: 'Move a ticket from analysed to decided.',
  close: 'Move a ticket to done; the verification note stays mandatory.',
  drop: 'Move a ticket to dropped, with a reason.',
  rank: 'Move the rank and adopt the score.',
  'override-urgency': 'Override the urgency of a ticket, with a reason.',
  interest: 'Register need and urgent interest, not only watch.',
  upload: 'Upload attachments.',
  'create-project': 'Create a project and bind a repository, where you may.',
  'record-answer': 'Record an answer you gave, marked as recorded by the agent.',
};

/** The "assisted" shortcut of docs/adr/0043 D4: a person stays the one to decide, close and rank. */
export const assisted: Capability[] = CAPABILITY.filter(
  (capability) =>
    !['decide', 'close', 'rank', 'create-project', 'record-answer'].includes(capability),
);

/** The lifetime a token starts with, and the longest the form offers (docs/adr/0035 D4). */
export const defaultLifetimeDays = 90;
export const maxLifetimeDays = 365;

const scopes: Scope[] = ['read', 'write', 'admin'];

/**
 * Creates a personal access token (docs/adr/0035 D5): its name, scope, an agent flag with the
 * capabilities (docs/adr/0043), a restriction to a tenant and to a project of it, and a lifetime.
 * An agent token has at most `write` scope, and the form says so before the server has to. The
 * answer carries the plaintext, which the dialog hands on in the event and keeps nowhere.
 */
@Component({
  selector: 'app-new-token-dialog',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [ButtonDirective, Checkbox, Dialog, FormsModule, InputNumber, InputText, Select],
  template: `
    <p-dialog
      [visible]="visible()"
      (visibleChange)="visible.set($event)"
      [modal]="true"
      [draggable]="false"
      [dismissableMask]="true"
      [style]="{ width: '38rem' }"
      header="New token"
      data-testid="new-token-dialog"
    >
      <form class="form" (ngSubmit)="save()">
        <label class="field">
          <span>Name</span>
          <input
            pInputText
            name="name"
            maxlength="100"
            autocomplete="off"
            placeholder="claude on my laptop"
            [ngModel]="name()"
            (ngModelChange)="name.set($event)"
            data-testid="token-name"
          />
          @if (errors()['name']; as error) {
            <small class="error" data-testid="token-name-error">{{ error }}</small>
          }
        </label>

        <div class="field">
          <span id="token-scope-label">Scope</span>
          <p-select
            [options]="scopeOptions()"
            optionLabel="value"
            optionValue="value"
            optionDisabled="disabled"
            [ngModel]="scope()"
            (ngModelChange)="scope.set($event)"
            name="scope"
            size="small"
            ariaLabelledBy="token-scope-label"
            data-testid="token-scope"
          />
          <small class="muted" data-testid="token-scope-meaning">
            {{ scopeMeanings[scope()] }}
            @if (agent()) {
              An agent token has at most write scope.
            }
          </small>
          @if (errors()['scope']; as error) {
            <small class="error" data-testid="token-scope-error">{{ error }}</small>
          }
        </div>

        <div class="field">
          <label class="check">
            <p-checkbox
              [binary]="true"
              [ngModel]="agent()"
              (ngModelChange)="setAgent($event)"
              name="agent"
              data-testid="token-agent"
            />
            <span>This token is for an agent</span>
          </label>
          <small class="muted">
            Every request of an agent token counts as an agent's and is marked so in the audit
            trail. It cannot be changed afterwards.
          </small>
        </div>

        @if (agent()) {
          <div class="field">
            <span id="token-capabilities-label">Capabilities</span>
            <div class="capabilities" role="group" aria-labelledby="token-capabilities-label">
              <p-select
                class="grow"
                [options]="capabilityOptions"
                [multiple]="true"
                optionLabel="value"
                optionValue="value"
                [ngModel]="capabilities()"
                (ngModelChange)="capabilities.set($event ?? [])"
                name="capabilities"
                placeholder="Choose at least one"
                size="small"
                ariaLabelledBy="token-capabilities-label"
                data-testid="token-capabilities"
              >
                <ng-template #item let-option>
                  <span class="capability"
                    ><span>{{ option.value }}</span
                    ><small class="muted">{{ option.meaning }}</small></span
                  >
                </ng-template>
              </p-select>
              <button
                pButton
                type="button"
                size="small"
                severity="secondary"
                [outlined]="true"
                (click)="capabilities.set(everything)"
                data-testid="token-capabilities-full"
              >
                Full
              </button>
              <button
                pButton
                type="button"
                size="small"
                severity="secondary"
                [outlined]="true"
                (click)="capabilities.set(assistedSet)"
                data-testid="token-capabilities-assisted"
              >
                Assisted
              </button>
            </div>
            <small class="muted" data-testid="token-capabilities-count">
              {{ capabilities().length }} of {{ everything.length }} chosen. Full is everything;
              assisted leaves deciding, closing, ranking, creating projects and recording answers to
              you.
            </small>
            @if (capabilities().length === 0) {
              <small class="error" data-testid="token-capabilities-none">
                Choose at least one. A token that names none is given every capability.
              </small>
            }
            @if (errors()['capabilities']; as error) {
              <small class="error" data-testid="token-capabilities-error">{{ error }}</small>
            }
          </div>
        }

        <div class="row">
          <div class="field">
            <span id="token-tenant-label">Restrict to a tenant</span>
            <p-select
              [options]="tenantOptions()"
              optionLabel="label"
              optionValue="slug"
              [ngModel]="tenant()"
              (ngModelChange)="setTenant($event)"
              name="tenant"
              placeholder="Any tenant of yours"
              [showClear]="true"
              size="small"
              ariaLabelledBy="token-tenant-label"
              data-testid="token-tenant"
            />
            @if (errors()['tenant']; as error) {
              <small class="error" data-testid="token-tenant-error">{{ error }}</small>
            }
          </div>
          @if (tenant()) {
            <div class="field">
              <span id="token-project-label">Restrict to a project</span>
              <p-select
                [options]="projectOptions()"
                optionLabel="label"
                optionValue="key"
                [ngModel]="project()"
                (ngModelChange)="project.set($event)"
                name="project"
                placeholder="Any project"
                [showClear]="true"
                [loading]="projects.isLoading()"
                size="small"
                ariaLabelledBy="token-project-label"
                data-testid="token-project"
              />
              @if (projects.error()) {
                <small class="error" data-testid="token-project-failed"
                  >The projects of this tenant could not be loaded.</small
                >
              }
              @if (errors()['project']; as error) {
                <small class="error" data-testid="token-project-error">{{ error }}</small>
              }
            </div>
          }
        </div>
        <small class="muted">
          A token that is restricted is useless anywhere else. An unrestricted one reaches every
          tenant you belong to.
        </small>

        <div class="field">
          <span id="token-lifetime-label">Lifetime</span>
          <p-inputnumber
            [ngModel]="days()"
            (ngModelChange)="days.set($event)"
            name="lifetime"
            [min]="1"
            [max]="maxDays"
            [maxFractionDigits]="0"
            [useGrouping]="false"
            [showButtons]="false"
            suffix=" days"
            size="small"
            ariaLabelledBy="token-lifetime-label"
            data-testid="token-lifetime"
          />
          <small class="muted">
            1 to {{ maxDays }} days. The installation may shorten it; the token shows its expiry
            once it exists.
          </small>
          @if (errors()['lifetime_days']; as error) {
            <small class="error" data-testid="token-lifetime-error">{{ error }}</small>
          }
        </div>

        <div class="actions">
          <button
            pButton
            type="button"
            [text]="true"
            severity="secondary"
            (click)="visible.set(false)"
            data-testid="token-cancel"
          >
            Cancel
          </button>
          <button pButton type="submit" data-testid="token-save" [disabled]="!canSave()">
            @if (saving()) {
              <i class="pi pi-spinner pi-spin"></i>
            }
            Create token
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
      grid-template-columns: repeat(auto-fit, minmax(14rem, 1fr));
      gap: 0.75rem;
    }
    .field {
      display: flex;
      flex-direction: column;
      gap: 0.375rem;
      min-width: 0;
      > span:first-child {
        font-size: 0.8125rem;
        font-weight: 550;
      }
    }
    .check {
      display: flex;
      align-items: center;
      gap: 0.625rem;
      font-weight: 550;
    }
    .capabilities {
      display: flex;
      gap: 0.5rem;
      align-items: center;
      .grow {
        flex: 1;
        min-width: 0;
      }
    }
    .capability {
      display: flex;
      flex-direction: column;
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
      gap: 0.5rem;
    }
  `,
})
export class NewTokenDialog {
  readonly visible = model(false);
  /** The token that was made, with its plaintext, for the page to show once. */
  readonly created = output<TokenCreated>();

  private readonly tokens = inject(TokensService);
  private readonly problems = inject(ProblemService);
  private readonly session = inject(SessionService);

  protected readonly scopeMeanings = scopeMeanings;
  protected readonly everything = [...CAPABILITY];
  protected readonly assistedSet = assisted;
  protected readonly maxDays = maxLifetimeDays;
  protected readonly capabilityOptions = CAPABILITY.map((value) => ({
    value,
    meaning: capabilityMeanings[value],
  }));

  protected readonly name = signal('');
  protected readonly scope = signal<Scope>('read');
  protected readonly agent = signal(false);
  protected readonly capabilities = signal<Capability[]>([...CAPABILITY]);
  protected readonly tenant = signal<string | null>(null);
  protected readonly project = signal<string | null>(null);
  protected readonly days = signal<number | null>(defaultLifetimeDays);
  protected readonly saving = signal(false);
  protected readonly errors = signal<Record<string, string>>({});

  /** `admin` is not on offer to an agent token: the schema refuses the pair. */
  protected readonly scopeOptions = computed(() =>
    scopes.map((value) => ({ value, disabled: value === 'admin' && this.agent() })),
  );
  protected readonly tenantOptions = computed(() =>
    this.session.memberships().map(({ tenant }) => ({
      slug: tenant.slug,
      label: `${tenant.name} (${tenant.slug})`,
    })),
  );
  /** The projects of the tenant chosen, which only exists while there is one. */
  protected readonly projects = resource({
    params: () => this.tenant() ?? undefined,
    loader: ({ params: tenant }) => this.tokens.projectsOf(tenant),
  });
  protected readonly projectOptions = computed(() =>
    (this.projects.hasValue() ? this.projects.value() : []).map((project) => ({
      key: project.key,
      label: `${project.key} · ${project.name}`,
    })),
  );
  protected readonly canSave = computed(() => {
    const days = this.days();
    return (
      this.name().trim() !== '' &&
      days !== null &&
      Number.isInteger(days) &&
      days >= 1 &&
      days <= maxLifetimeDays &&
      (!this.agent() || this.capabilities().length > 0) &&
      !this.saving()
    );
  });

  constructor() {
    // However the dialog closes, the next one starts clean.
    effect(() => {
      if (!this.visible()) {
        untracked(() => this.reset());
      }
    });
  }

  /** An agent token has at most `write` scope: asking for the flag lowers an `admin` choice to it. */
  protected setAgent(agent: boolean): void {
    this.agent.set(agent);
    if (agent && this.scope() === 'admin') {
      this.scope.set('write');
    }
  }

  /** A project belongs to its tenant: another tenant, or none, leaves no project chosen. */
  protected setTenant(tenant: string | null): void {
    this.tenant.set(tenant);
    this.project.set(null);
  }

  protected async save(): Promise<void> {
    if (!this.canSave()) {
      return;
    }
    const tenant = this.tenant();
    const project = this.project();
    const body: TokenCreate = {
      name: this.name().trim(),
      scope: this.scope(),
      // In the order of the vocabulary, not of the clicks.
      ...(this.agent()
        ? {
            agent: true,
            capabilities: CAPABILITY.filter((each) => this.capabilities().includes(each)),
          }
        : {}),
      ...(tenant ? { tenant, ...(project ? { project } : {}) } : {}),
      lifetime_days: this.days() as number,
    };
    this.saving.set(true);
    this.errors.set({});
    try {
      const token = await this.tokens.create(body);
      this.visible.set(false);
      if (token.token === undefined) {
        // An answer without a plaintext is a repeated one; there is nothing to show.
        this.problems.report(
          new Error(
            'The token was made, but its secret did not come with the answer. Revoke it in the list and make another.',
          ),
        );
        return;
      }
      this.created.emit(token);
    } catch (error) {
      this.errors.set(this.problems.report(error, { fields: true }).fields);
    } finally {
      this.saving.set(false);
    }
  }

  private reset(): void {
    this.name.set('');
    this.scope.set('read');
    this.agent.set(false);
    this.capabilities.set([...CAPABILITY]);
    this.tenant.set(null);
    this.project.set(null);
    this.days.set(defaultLifetimeDays);
    this.errors.set({});
  }
}
