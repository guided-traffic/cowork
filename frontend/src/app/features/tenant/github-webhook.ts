import { DOCUMENT } from '@angular/common';
import {
  ChangeDetectionStrategy,
  Component,
  computed,
  inject,
  Injector,
  resource,
  ResourceRef,
  signal,
} from '@angular/core';
import { ConfirmationService } from 'primeng/api';
import { ButtonDirective } from 'primeng/button';
import { InputText } from 'primeng/inputtext';
import { Api } from '../../api/api';
import { createGitHubSecret } from '../../api/fn/integrations/create-git-hub-secret';
import { getGitHubIntegration } from '../../api/fn/integrations/get-git-hub-integration';
import { revokeGitHubSecret } from '../../api/fn/integrations/revoke-git-hub-secret';
import { GitHubIntegration } from '../../api/models';
import { ProblemService } from '../../core/problem.service';
import { keepShown, refresh } from '../../core/refresh';
import { SessionService } from '../../core/session.service';
import { ConfirmDialog } from '../../shared/confirm-dialog';
import { SecretDialog } from '../../shared/secret-dialog';
import { ago, Clock, dateTime } from '../../shared/time';

/** The events to choose at GitHub, as its webhook form names them (docs/adr/0071 D4). */
const eventNames: Readonly<Record<string, string>> = {
  pull_request: 'Pull requests',
  push: 'Pushes',
};

/** What GitHub's form calls an event cowork reads, or the event's own name. */
export function gitHubEventName(event: string): string {
  return eventNames[event] ?? event;
}

/**
 * GitHub's inbound webhook, for the tenant's administrators (docs/adr/0071 D1, D7): whether the
 * tenant takes it, the endpoint GitHub posts to and what to set at GitHub, and the secret — made,
 * rotated, revoked. A new secret is shown once in the {@link SecretDialog}, with a copy button and
 * the endpoint beside it; the page keeps it in one signal and nowhere else. Making and rotating
 * take the browser session, which the page is (docs/adr/0035 D5); both ask first where a secret
 * exists, because GitHub refuses nothing it sends until its settings hold the new one.
 */
@Component({
  selector: 'app-github-webhook',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [ButtonDirective, ConfirmDialog, InputText, SecretDialog],
  providers: [ConfirmationService],
  template: `
    <app-confirm-dialog />
    <app-secret-dialog [(secret)]="secret" header="The webhook's secret" label="Secret">
      <strong>Shown once.</strong> Whoever holds this secret can link pull requests to this tenant's
      tickets and notify their watchers — never change a ticket's state. Put it into the webhook's
      settings at GitHub now, for every repository: cowork keeps it sealed and cannot show it again.
      @if (replaced()) {
        The secret before it is refused from now on: every webhook that holds it fails until it
        holds this one.
      }
      <span class="url-line"
        >Payload URL: <code data-testid="secret-url">{{ url() }}</code></span
      >
    </app-secret-dialog>

    <section class="card github" aria-labelledby="github-heading" data-testid="github-webhook">
      <h2 id="github-heading"><i class="pi pi-github" aria-hidden="true"></i>GitHub</h2>
      @if (integration(); as i) {
        @if (i.secret; as s) {
          <p data-testid="github-state">
            The tenant takes GitHub's webhook. Its secret was made
            <span [title]="dateTime(s.created_at)">{{ ago(s.created_at) }}</span> by
            {{ s.created_by.display_name || s.created_by.username || 'a former member' }}.
          </p>
        } @else {
          <p data-testid="github-state">
            The tenant takes no webhook: GitHub's deliveries are answered like an unknown tenant
            until a secret exists.
          </p>
        }
        <label class="field">
          <span>Payload URL</span>
          <span class="copy">
            <input
              pInputText
              readonly
              [value]="url()"
              (focus)="selectAll($event)"
              aria-label="Payload URL"
              data-testid="github-url"
            />
            <button
              pButton
              type="button"
              severity="secondary"
              (click)="copyUrl()"
              data-testid="github-url-copy"
            >
              <i [class]="copied() ? 'pi pi-check' : 'pi pi-copy'"></i>
              {{ copied() ? 'Copied' : 'Copy' }}
            </button>
          </span>
        </label>
        <ol class="steps small">
          <li>
            Bind the repository to a project of this tenant; a delivery of another is passed over.
          </li>
          <li>
            At GitHub, in the repository's <em>Settings → Webhooks → Add webhook</em>: the payload
            URL above, content type <code>application/json</code>, and the secret shown when it is
            made.
          </li>
          <li>
            Under <em>Let me select individual events</em>: {{ events(i) }}. Keep the webhook
            active; GitHub's ping should answer 202.
          </li>
        </ol>
        <p class="muted small">
          A pull request is linked by the keys of its title and body, its commits once a push brings
          them to the default branch. Nothing changes a ticket's state; a merge tells its assignee
          and its watchers.
        </p>
        <div class="actions">
          @if (i.secret) {
            <button
              pButton
              type="button"
              severity="danger"
              [outlined]="true"
              [disabled]="busy()"
              (click)="revoke()"
              data-testid="github-revoke"
            >
              Revoke
            </button>
          }
          <button
            pButton
            type="button"
            [disabled]="busy()"
            (click)="make()"
            data-testid="github-make"
          >
            @if (busy()) {
              <i class="pi pi-spinner pi-spin"></i>
            }
            {{ i.secret ? 'Rotate the secret' : 'Make the secret' }}
          </button>
        </div>
      } @else if (failure(); as f) {
        <p class="muted" data-testid="github-failure">{{ f }}</p>
      }
    </section>
  `,
  styles: `
    .github {
      display: flex;
      flex-direction: column;
      gap: 0.75rem;
      h2 {
        display: flex;
        align-items: center;
        gap: 0.5rem;
        margin: 0;
        font-size: 1rem;
      }
      p {
        margin: 0;
      }
    }
    .card {
      padding: 1.25rem;
      border: 1px solid var(--p-app-border);
      border-radius: var(--p-border-radius-xl);
      background: var(--p-app-panel);
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
    .copy {
      display: flex;
      gap: 0.5rem;
      input {
        flex: 1;
        min-width: 0;
        font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
      }
    }
    .steps {
      margin: 0;
      padding-left: 1.25rem;
      display: flex;
      flex-direction: column;
      gap: 0.25rem;
    }
    .actions {
      display: flex;
      justify-content: flex-end;
      gap: 0.5rem;
    }
    .small {
      font-size: 0.8125rem;
    }
    .url-line {
      display: block;
      margin-top: 0.5rem;
      overflow-wrap: anywhere;
    }
  `,
})
export class GitHubWebhook {
  private readonly api = inject(Api);
  private readonly session = inject(SessionService);
  private readonly problems = inject(ProblemService);
  private readonly confirm = inject(ConfirmationService);
  private readonly clock = inject(Clock);
  private readonly injector = inject(Injector);
  private readonly document = inject(DOCUMENT);
  protected readonly dateTime = dateTime;

  /** The secret just made, shown once and forgotten when its dialog closes. */
  protected readonly secret = signal<string | null>(null);
  protected readonly replaced = signal(false);
  protected readonly busy = signal(false);
  protected readonly copied = signal(false);

  /** The tenant the page shows; the section is its administrators'. */
  private readonly tenant = computed(() => this.session.tenant() ?? undefined);
  protected readonly state: ResourceRef<GitHubIntegration | undefined> = resource({
    params: () => this.tenant(),
    loader: ({ params: tenant }) =>
      keepShown(this.state, () => this.api.invoke(getGitHubIntegration, { tenant })),
  });
  protected readonly integration = computed(() =>
    this.state.hasValue() ? this.state.value() : undefined,
  );
  protected readonly failure = computed(() => {
    const error = this.state.error();
    if (!error) {
      return undefined;
    }
    const problem = this.problems.read(error);
    return `GitHub's webhook could not be loaded: ${problem.detail || problem.title}`;
  });
  /** The endpoint under the address the page is served from, which is COWORK_BASE_URL. */
  protected readonly url = computed(() => {
    const path = this.integration()?.webhook_path ?? '';
    return `${this.document.location.origin}${path}`;
  });

  protected ago(iso: string): string {
    return ago(iso, this.clock.now());
  }

  protected events(i: GitHubIntegration): string {
    return i.events.map(gitHubEventName).join(' and ');
  }

  protected selectAll(event: Event): void {
    (event.target as HTMLInputElement).select();
  }

  protected async copyUrl(): Promise<void> {
    try {
      await navigator.clipboard.writeText(this.url());
      this.copied.set(true);
    } catch {
      this.copied.set(false);
    }
  }

  /** Makes the secret, or rotates it after the question; the answer is the one sight of it. */
  protected make(): void {
    if (!this.integration()?.secret) {
      void this.create();
      return;
    }
    this.confirm.confirm({
      header: 'Rotate the secret',
      message:
        'A new secret replaces the one GitHub holds at once: every delivery signed with the old one is refused until the webhook at GitHub holds the new one.',
      acceptLabel: 'Rotate',
      rejectLabel: 'Keep it',
      accept: () => void this.create(),
    });
  }

  protected revoke(): void {
    const tenant = this.tenant();
    if (!tenant) {
      return;
    }
    this.confirm.confirm({
      header: 'Revoke the secret',
      message:
        "GitHub's deliveries are answered like an unknown tenant from now on, and nothing is linked any more. The links made stay.",
      acceptLabel: 'Revoke',
      rejectLabel: 'Keep it',
      accept: () => void this.guard(() => this.api.invoke(revokeGitHubSecret, { tenant })),
    });
  }

  private async create(): Promise<void> {
    const tenant = this.tenant();
    if (!tenant) {
      return;
    }
    await this.guard(async () => {
      const made = await this.api.invoke(createGitHubSecret, { tenant });
      this.replaced.set(made.replaced);
      this.secret.set(made.secret);
    });
  }

  private async guard(act: () => Promise<unknown>): Promise<void> {
    this.busy.set(true);
    try {
      await act();
    } catch (error) {
      this.problems.report(error);
    } finally {
      this.busy.set(false);
      refresh(this.state, this.injector);
    }
  }
}
