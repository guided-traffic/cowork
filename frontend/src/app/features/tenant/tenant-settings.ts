import { DOCUMENT } from '@angular/common';
import {
  ChangeDetectionStrategy,
  Component,
  computed,
  effect,
  inject,
  Injector,
  linkedSignal,
  resource,
  ResourceRef,
  signal,
} from '@angular/core';
import { takeUntilDestroyed } from '@angular/core/rxjs-interop';
import { FormsModule } from '@angular/forms';
import { ButtonDirective } from 'primeng/button';
import { InputText } from 'primeng/inputtext';
import { ToggleSwitch } from 'primeng/toggleswitch';
import { Api } from '../../api/api';
import { getAttachmentUsage } from '../../api/fn/attachments/get-attachment-usage';
import { AttachmentUsage } from '../../api/models';
import { AttachmentConsistencyService } from '../../core/attachment-consistency.service';
import { ConditionalPages } from '../../core/conditional';
import { EventStreamService, ofTenant, StreamEvent } from '../../core/event-stream.service';
import { exportNote, ImportsService } from '../../core/imports.service';
import { ProblemService } from '../../core/problem.service';
import { keepShown, refresh } from '../../core/refresh';
import { SessionService } from '../../core/session.service';
import { TenantService } from '../../core/tenant.service';
import { byteSize } from '../../shared/bytes';
import { AttachmentConsistencySection } from './attachment-consistency';
import { saveFile } from '../../shared/download';

/**
 * Whether an event may have moved what the tenant's attachments hold: an upload or a purge in the
 * tenant — a deletion leaves the files in the bucket, and their bytes counted, until the purge —, a
 * gap in the stream or the fallback's poll.
 */
export function changesUsage(event: StreamEvent, tenant: string | null): boolean {
  if (event.name === 'resync' || event.name === 'poll') {
    return true;
  }
  return (
    event.name === 'ticket.changed' &&
    ofTenant(event, tenant) &&
    (event.kind === 'uploaded' || event.kind === 'purged')
  );
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
 * the version read (docs/adr/0050 D3); what the tenant's attachments hold against the quota
 * of the installation (docs/adr/0016 D6), read when the page opens and again on an upload or a
 * purge in the tenant; the latest consistency check of the attachments against the bucket
 * (docs/adr/0059 D4), {@link AttachmentConsistencySection}; and the export of the whole tenant as
 * one archive (docs/adr/0051 D4), the second line of a backup (docs/adr/0059 D2).
 */
@Component({
  selector: 'app-tenant-settings',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [AttachmentConsistencySection, ButtonDirective, FormsModule, InputText, ToggleSwitch],
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
            <p class="muted small">Only the team's administrators change these.</p>
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
                The installation sets no quota per team (<code>COWORK_ATTACHMENT_TEAM_QUOTA</code>):
                the team's files are bounded only by the size of each file and their number per
                ticket.
              </p>
            }
            <p class="muted small">
              Every file of the team counts, on tickets you see and on those you do not, and on a
              deleted ticket until the purge removes it. An upload that would go above the quota is
              refused before it is stored.
            </p>
          } @else if (usageFailure(); as failure) {
            <p class="muted" data-testid="attachment-usage-failure">{{ failure }}</p>
          }
        </section>
        <app-attachment-consistency />
        <section class="card usage" data-testid="tenant-export" aria-labelledby="export-heading">
          <h2 id="export-heading">Export</h2>
          <p class="muted small">
            Every project of the team you see, archived ones too, as one archive: each ticket's
            Markdown document, with a manifest of the links and one of the attachments — their
            names, never their bytes. The export is recorded in the audit record; kept on a schedule
            of the installation's, it is the second line of a backup.
          </p>
          <div class="actions">
            @if (exported(); as note) {
              <span class="muted small" role="status" data-testid="tenant-export-note">{{
                note
              }}</span>
            }
            <button
              pButton
              type="button"
              severity="secondary"
              [outlined]="true"
              [disabled]="exporting()"
              (click)="exportTenant()"
              data-testid="tenant-export-button"
            >
              @if (exporting()) {
                <i class="pi pi-spinner pi-spin"></i>
              } @else {
                <i class="pi pi-download"></i>
              }
              Export the team
            </button>
          </div>
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
      align-items: center;
      justify-content: flex-end;
      gap: 0.75rem;
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
  private readonly injector = inject(Injector);
  private readonly imports = inject(ImportsService);
  private readonly consistency = inject(AttachmentConsistencyService);
  private readonly document = inject(DOCUMENT);
  protected readonly exporting = signal(false);
  /** What the last export of the tenant shown held; another tenant's page starts without it. */
  protected readonly exported = linkedSignal<string | null, string | null>({
    source: () => this.session.tenant(),
    computation: () => null,
  });
  /** The weak `ETag` of the usage held: a load again that finds it unchanged is a `304`. */
  private readonly conditional = new ConditionalPages(this.api);
  /** The tenant while the person is its administrator: the usage is theirs to read. */
  private readonly administered = computed(() =>
    this.tenant.isAdmin() ? (this.session.tenant() ?? undefined) : undefined,
  );
  /**
   * What the tenant's attachments hold against the quota (docs/adr/0016 D6), for its
   * administrators only — the sum counts files of tickets a member may not see. Read when the page
   * opens, for the tenant it shows, and again on what may move it ({@link changesUsage}); a load
   * again that fails keeps the usage shown.
   */
  protected readonly attachmentUsage: ResourceRef<AttachmentUsage | undefined> = resource({
    params: () => this.administered(),
    loader: ({ params: tenant }): Promise<AttachmentUsage> =>
      keepShown(this.attachmentUsage, () =>
        this.conditional.load((fetch) => fetch(getAttachmentUsage, { team: tenant })),
      ),
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
    inject(EventStreamService)
      .events.pipe(takeUntilDestroyed())
      .subscribe((event) => {
        if (changesUsage(event, this.session.tenant())) {
          refresh(this.attachmentUsage, this.injector);
        }
      });
    effect(() => {
      const tenant = this.tenant.value();
      if (tenant) {
        this.name.set(tenant.name);
        this.membersCreate.set(tenant.members_create_projects);
        this.timeVisible.set(tenant.time_visible_to_members);
      }
    });
  }

  /**
   * Downloads every project of the tenant the person sees as one archive (docs/adr/0051 D4), saved
   * under the name the server gives it, and says beside the button what it holds; the consistency
   * section then reads the time of the tenant's last export again.
   */
  protected async exportTenant(): Promise<void> {
    const tenant = this.session.tenant();
    if (!tenant || this.exporting()) {
      return;
    }
    this.exporting.set(true);
    try {
      const archive = await this.imports.exportTenant(tenant);
      saveFile(this.document, archive.blob, archive.filename);
      if (this.session.tenant() === tenant) {
        this.exported.set(exportNote(archive));
        this.consistency.exported();
      }
    } catch (error) {
      this.problems.report(error);
    } finally {
      this.exporting.set(false);
    }
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
