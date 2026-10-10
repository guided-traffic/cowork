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
import { AuthService } from '../../core/auth.service';
import { ProblemService } from '../../core/problem.service';
import { SessionService } from '../../core/session.service';
import { TokensService } from '../../core/tokens.service';
import { assisted, capabilityMeanings, selectableCapabilities } from '../../shared/capabilities';
import { describedBy, numberAria, selectAria } from '../../shared/field-aria';
import { keepOpenWhile } from '../../shared/keep-open';

/**
 * What each scope reaches, in a line (docs/adr/0035 D3, docs/security/tokens.md), shown beside the
 * choice and in the list. The strongest acts are named, the irreversible ones among them: a
 * person who chooses `admin` for a script that changes a setting should read that the same token
 * deactivates accounts for good.
 */
export const scopeMeanings: Record<Scope, string> = {
  read: 'Reads what you may read.',
  write:
    'Reads, and does what a member does: files and moves tickets, comments, books time, creates ' +
    'projects where you may, and revokes your other tokens.',
  admin:
    'Does what write does, and what your admin role allows: team settings and the time lock, ' +
    "archiving projects, the confidential flag, withdrawing other people's comments, and " +
    'unlocking, ending the sessions of and deactivating (for good) the local accounts of the team.',
};

/**
 * The longest lifetime the API's schema takes, in days. The form offers the installation's own
 * maximum within it once `/auth/options` names it (docs/adr/0035 D4); until then this bound, and the
 * installation shortens a longer lifetime and the token shows the expiry it got.
 */
export const maxLifetimeDays = 3650;

const scopes: Scope[] = ['read', 'write', 'admin'];

/**
 * Creates a personal access token (docs/adr/0035 D5): its name — which shows on every act made
 * through it, and says so (docs/adr/0036 D6) —, scope, an agent flag with the capabilities
 * (docs/adr/0043), a restriction to a tenant and to a project of it, and a lifetime.
 * An agent token has at most `write` scope, and the form says so before the server has to; one
 * with no capability keeps the baseline and nothing more, which is a choice like any other. The
 * lifetime is left empty unless the person fills it, which is the installation's default, and goes
 * up to the installation's maximum that `/auth/options` names. The
 * answer carries the plaintext, which the dialog hands on in the event and keeps nowhere. While
 * the request is out nothing closes the dialog, so that a refusal always lands in the form that
 * was sent.
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
      [closable]="!saving()"
      [dismissableMask]="!saving()"
      [style]="{ width: '38rem' }"
      header="New token"
      data-testid="new-token-dialog"
    >
      <form class="form" (ngSubmit)="save()">
        <div class="field">
          <label for="token-name-input">Name</label>
          <input
            pInputText
            id="token-name-input"
            name="name"
            maxlength="100"
            autocomplete="off"
            placeholder="claude on my laptop"
            [ngModel]="name()"
            (ngModelChange)="name.set($event)"
            [attr.aria-invalid]="!!errors()['name']"
            [attr.aria-describedby]="
              describedBy('token-name-hint', errors()['name'] && 'token-name-error')
            "
            data-testid="token-name"
          />
          <small class="muted" id="token-name-hint" data-testid="token-name-hint">
            The name shows on what the token does: everyone who can read a ticket sees it beside the
            acts made through this token — changes, comments, questions, answers, files, time — also
            after you revoke it.
          </small>
          @if (errors()['name']; as error) {
            <small
              class="error"
              id="token-name-error"
              role="alert"
              data-testid="token-name-error"
              >{{ error }}</small
            >
          }
        </div>

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
            [invalid]="!!errors()['scope']"
            [pt]="scopePt()"
            ariaLabelledBy="token-scope-label"
            data-testid="token-scope"
          />
          <small class="muted" id="token-scope-meaning" data-testid="token-scope-meaning">
            {{ scopeMeanings[scope()] }}
            @if (agent()) {
              An agent token has at most write scope, and what stays with a person — booking time,
              revoking tokens, administration — is never its own.
            }
          </small>
          @if (errors()['scope']; as error) {
            <small
              class="error"
              id="token-scope-error"
              role="alert"
              data-testid="token-scope-error"
              >{{ error }}</small
            >
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
                placeholder="None: the baseline only"
                size="small"
                [invalid]="!!errors()['capabilities']"
                [pt]="capabilitiesPt()"
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
            <small
              class="muted"
              id="token-capabilities-count"
              data-testid="token-capabilities-count"
            >
              {{ capabilities().length }} of {{ everything.length }} chosen. Full is everything;
              assisted leaves deciding, closing, ranking, creating projects and recording answers to
              you.
            </small>
            @if (capabilities().length === 0) {
              <small
                class="muted"
                id="token-capabilities-none"
                data-testid="token-capabilities-none"
              >
                No capability: the baseline only — filing and editing tickets, comments, links,
                questions, progress and a watch stake.
              </small>
            }
            @if (errors()['capabilities']; as error) {
              <small
                class="error"
                id="token-capabilities-error"
                role="alert"
                data-testid="token-capabilities-error"
                >{{ error }}</small
              >
            }
          </div>
        }

        <div class="row">
          <div class="field">
            <span id="token-tenant-label">Restrict to a team</span>
            <p-select
              [options]="tenantOptions()"
              optionLabel="label"
              optionValue="slug"
              [ngModel]="tenant()"
              (ngModelChange)="setTenant($event)"
              name="tenant"
              placeholder="Any team of yours"
              [showClear]="true"
              size="small"
              [invalid]="!!teamError()"
              [pt]="tenantPt()"
              ariaLabelledBy="token-tenant-label"
              data-testid="token-tenant"
            />
            @if (teamError(); as error) {
              <small
                class="error"
                id="token-tenant-error"
                role="alert"
                data-testid="token-tenant-error"
                >{{ error }}</small
              >
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
                [invalid]="!!errors()['project']"
                [pt]="projectPt()"
                ariaLabelledBy="token-project-label"
                data-testid="token-project"
              />
              @if (projects.error()) {
                <small
                  class="error"
                  id="token-project-failed"
                  role="alert"
                  data-testid="token-project-failed"
                  >The projects of this team could not be loaded.</small
                >
              }
              @if (errors()['project']; as error) {
                <small
                  class="error"
                  id="token-project-error"
                  role="alert"
                  data-testid="token-project-error"
                  >{{ error }}</small
                >
              }
            </div>
          }
        </div>
        <small class="muted">
          A token that is restricted is useless anywhere else. An unrestricted one reaches every
          team you belong to, now and later.
        </small>

        <div class="field">
          <span id="token-lifetime-label">Lifetime</span>
          <p-inputnumber
            [ngModel]="days()"
            (ngModelChange)="days.set($event)"
            name="lifetime"
            [min]="1"
            [max]="maxDays()"
            [disabled]="maxDays() < 1"
            [maxFractionDigits]="0"
            [useGrouping]="false"
            [showButtons]="false"
            suffix=" days"
            placeholder="Installation default"
            size="small"
            [invalid]="!!errors()['lifetime_days']"
            [pt]="lifetimePt()"
            ariaLabelledBy="token-lifetime-label"
            [ariaDescribedBy]="
              describedBy(
                'token-lifetime-hint',
                errors()['lifetime_days'] && 'token-lifetime-error'
              ) ?? undefined
            "
            data-testid="token-lifetime"
          />
          <small class="muted" id="token-lifetime-hint" data-testid="token-lifetime-hint">
            @if (installationMax() === undefined) {
              Empty is the installation's default. Up to {{ maxDays() }} days; the installation may
              shorten it, and the token shows its expiry once it exists.
            } @else if (maxDays() < 1) {
              This installation gives a token less than a day: leave it empty for its default.
            } @else {
              Empty is the installation's default. Up to {{ maxDays() }} days, the longest this
              installation gives a token.
            }
          </small>
          @if (errors()['lifetime_days']; as error) {
            <small
              class="error"
              id="token-lifetime-error"
              role="alert"
              data-testid="token-lifetime-error"
              >{{ error }}</small
            >
          }
        </div>

        <div class="actions">
          <button
            pButton
            type="button"
            [text]="true"
            severity="secondary"
            [disabled]="saving()"
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
      > label:not(.check),
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
  private readonly auth = inject(AuthService);

  protected readonly scopeMeanings = scopeMeanings;
  protected readonly everything = [...selectableCapabilities];
  protected readonly assistedSet = assisted;
  /**
   * The installation's longest lifetime in whole days, `COWORK_TOKEN_MAX_LIFETIME` as
   * `/auth/options` names it; undefined until it answers, or where it cannot be read.
   */
  protected readonly installationMax = computed(() =>
    this.auth.options.hasValue() ? this.auth.options.value().token_max_lifetime_days : undefined,
  );
  /** The longest lifetime the form takes: the installation's, within the schema's bound. */
  protected readonly maxDays = computed(() =>
    Math.min(maxLifetimeDays, this.installationMax() ?? maxLifetimeDays),
  );
  protected readonly describedBy = describedBy;
  protected readonly capabilityOptions = selectableCapabilities.map((value) => ({
    value,
    meaning: capabilityMeanings[value],
  }));

  protected readonly name = signal('');
  protected readonly scope = signal<Scope>('read');
  protected readonly agent = signal(false);
  protected readonly capabilities = signal<Capability[]>([...selectableCapabilities]);
  protected readonly tenant = signal<string | null>(null);
  protected readonly project = signal<string | null>(null);
  /** Empty is the installation's default: the request then leaves `lifetime_days` out. */
  protected readonly days = signal<number | null>(null);
  protected readonly saving = signal(false);
  protected readonly errors = signal<Record<string, string>>({});

  /** `admin` is not on offer to an agent token: the schema refuses the pair. */
  protected readonly scopeOptions = computed(() =>
    scopes.map((value) => ({ value, disabled: value === 'admin' && this.agent() })),
  );
  protected readonly tenantOptions = computed(() =>
    this.session.memberships().map(({ team }) => ({
      slug: team.slug,
      label: `${team.name} (${team.slug})`,
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
      (days === null || (Number.isInteger(days) && days >= 1 && days <= this.maxDays())) &&
      !this.saving()
    );
  });

  /**
   * What PrimeNG leaves out of a field the server refused: `aria-invalid` on the element a person
   * tabs to, and the text that says why, with the hint that is always there.
   */
  protected readonly scopePt = computed(() =>
    selectAria(
      !!this.errors()['scope'],
      describedBy('token-scope-meaning', this.errors()['scope'] && 'token-scope-error'),
    ),
  );
  protected readonly capabilitiesPt = computed(() =>
    selectAria(
      !!this.errors()['capabilities'],
      describedBy(
        'token-capabilities-count',
        this.capabilities().length === 0 && 'token-capabilities-none',
        this.errors()['capabilities'] && 'token-capabilities-error',
      ),
    ),
  );
  /**
   * The refusal of the team restriction: the API points at `/team`, the property the form sends,
   * and a server before it at `/tenant`, the property's name before (docs/adr/0005 D1).
   */
  protected readonly teamError = computed(() => this.errors()['team'] ?? this.errors()['tenant']);
  protected readonly tenantPt = computed(() =>
    selectAria(!!this.teamError(), describedBy(this.teamError() && 'token-tenant-error')),
  );
  protected readonly projectPt = computed(() =>
    selectAria(
      !!this.errors()['project'],
      describedBy(
        this.projects.error() && 'token-project-failed',
        this.errors()['project'] && 'token-project-error',
      ),
    ),
  );
  protected readonly lifetimePt = computed(() => numberAria(!!this.errors()['lifetime_days']));

  constructor() {
    keepOpenWhile(() => this.saving());
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
    const days = this.days();
    const body: TokenCreate = {
      name: this.name().trim(),
      scope: this.scope(),
      // In the order of the vocabulary, not of the clicks; none chosen is an empty list, which
      // the API reads as the baseline only.
      ...(this.agent()
        ? {
            agent: true,
            capabilities: selectableCapabilities.filter((each) => this.capabilities().includes(each)),
          }
        : {}),
      ...(tenant ? { team: tenant, ...(project ? { project } : {}) } : {}),
      ...(days !== null ? { lifetime_days: days } : {}),
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
      if (this.visible()) {
        this.errors.set(this.problems.report(error, { fields: true }).fields);
      } else {
        // The page closed the dialog while the request was out, and the form is empty: a field
        // error would sit under nothing and come back with the next token. A toast says it.
        this.problems.report(error);
      }
    } finally {
      this.saving.set(false);
    }
  }

  private reset(): void {
    this.name.set('');
    this.scope.set('read');
    this.agent.set(false);
    this.capabilities.set([...selectableCapabilities]);
    this.tenant.set(null);
    this.project.set(null);
    this.days.set(null);
    this.errors.set({});
  }
}
