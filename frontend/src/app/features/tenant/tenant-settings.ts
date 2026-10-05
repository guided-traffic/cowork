import {
  ChangeDetectionStrategy,
  Component,
  computed,
  effect,
  inject,
  resource,
  signal,
} from '@angular/core';
import { FormsModule } from '@angular/forms';
import { ButtonDirective } from 'primeng/button';
import { InputText } from 'primeng/inputtext';
import { ToggleSwitch } from 'primeng/toggleswitch';
import { Api } from '../../api/api';
import { getAttachmentUsage } from '../../api/functions';
import { AttachmentUsage } from '../../api/models';
import { ProblemService } from '../../core/problem.service';
import { SessionService } from '../../core/session.service';
import { TenantService } from '../../core/tenant.service';

/** A count of bytes as people read it, in the binary units the configuration takes: `1.5 MiB`. */
export function byteSize(bytes: number): string {
  const units = ['bytes', 'KiB', 'MiB', 'GiB', 'TiB'];
  let value = bytes;
  let unit = 0;
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024;
    unit++;
  }
  if (unit === 0) {
    return `${value} ${value === 1 ? 'byte' : 'bytes'}`;
  }
  return `${value.toFixed(value < 10 ? 1 : 0)} ${units[unit]}`;
}

/** The share of the quota the attachments hold, whole percent, at most 100; null without a quota. */
export function quotaShare(usage: AttachmentUsage): number | null {
  if (usage.quota_bytes === null || usage.quota_bytes <= 0) {
    return null;
  }
  return Math.min(100, Math.floor((usage.used_bytes / usage.quota_bytes) * 100));
}

/**
 * The tenant's settings, for its administrators: the name, whether members create projects
 * (docs/adr/0034 D9) and whether members see each other's time (docs/adr/0017), written with
 * the version read (docs/adr/0050 D3); and what the tenant's attachments hold against the quota
 * of the installation (docs/adr/0016 D6), read again each time the page opens.
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
      @if (tenant.isAdmin()) {
        <section class="card usage" data-testid="attachment-usage" aria-labelledby="usage-heading">
          <h2 id="usage-heading">Attachments</h2>
          @if (usage(); as u) {
            <p data-testid="attachment-usage-text">
              {{ byteSize(u.used_bytes) }} in {{ u.attachments }}
              {{ u.attachments === 1 ? 'file' : 'files' }}
              @if (u.quota_bytes !== null) {
                of a quota of {{ byteSize(u.quota_bytes) }}
              }
            </p>
            @if (share(); as percent) {
              <div
                class="meter"
                role="meter"
                aria-valuemin="0"
                aria-valuemax="100"
                [attr.aria-valuenow]="percent"
                aria-label="Share of the attachment quota used"
                data-testid="attachment-usage-meter"
              >
                <span [style.width.%]="percent" [class.full]="percent >= 90"></span>
              </div>
            } @else if (u.quota_bytes === null) {
              <p class="muted small" data-testid="attachment-usage-no-quota">
                The installation sets no quota per tenant
                (<code>COWORK_ATTACHMENT_TENANT_QUOTA</code>): the tenant's files are bounded only
                by the size of each file and their number per ticket.
              </p>
            }
            <p class="muted small">
              Every file of the tenant counts, on tickets you see and on those you do not. An upload
              that would go above the quota is refused before it is stored.
            </p>
          } @else if (usageFailure(); as failure) {
            <p class="muted" data-testid="attachment-usage-failure">{{ failure }}</p>
          }
        </section>
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
    .usage {
      display: flex;
      flex-direction: column;
      gap: 0.5rem;
      h2 {
        margin: 0;
        font-size: 1rem;
      }
      p {
        margin: 0;
      }
    }
    .meter {
      height: 0.5rem;
      border-radius: 999px;
      background: var(--p-app-hover);
      overflow: hidden;
      span {
        display: block;
        height: 100%;
        background: var(--p-primary-color);
        &.full {
          background: var(--p-severity-high);
        }
      }
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
  protected readonly byteSize = byteSize;
  private readonly api = inject(Api);
  private readonly session = inject(SessionService);
  /** The tenant while the person is its administrator: the usage is theirs to read. */
  private readonly administered = computed(() =>
    this.tenant.isAdmin() ? (this.session.tenant() ?? undefined) : undefined,
  );
  /**
   * What the tenant's attachments hold against the quota (docs/adr/0016 D6), for its
   * administrators only — the sum counts files of tickets a member may not see. Read when the page
   * opens, for the tenant it shows.
   */
  protected readonly attachmentUsage = resource({
    params: () => this.administered(),
    loader: ({ params: tenant }) => this.api.invoke(getAttachmentUsage, { tenant }),
  });
  protected readonly usage = computed(() =>
    this.attachmentUsage.hasValue() ? this.attachmentUsage.value() : undefined,
  );
  protected readonly share = computed(() => {
    const usage = this.usage();
    return usage ? quotaShare(usage) : null;
  });
  protected readonly usageFailure = computed(() => {
    const error = this.attachmentUsage.error();
    if (!error) {
      return undefined;
    }
    const problem = this.problems.read(error);
    return `The usage could not be loaded: ${problem.detail || problem.title}`;
  });

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
