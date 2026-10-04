import { HttpErrorResponse } from '@angular/common/http';
import { ChangeDetectionStrategy, Component, computed, inject, input, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { ButtonDirective } from 'primeng/button';
import { Divider } from 'primeng/divider';
import { InputText } from 'primeng/inputtext';
import { InputPassword } from 'primeng/inputpassword';
import { loginOidc } from '../../api/functions';
import { Wordmark } from '../../brand/logo';
import { AuthService } from '../../core/auth.service';
import { HARD_NAVIGATION } from '../../core/hard-navigation';
import { ProblemService } from '../../core/problem.service';

/** The longest way back, in UTF-8 bytes as the backend counts it (`maxReturnTo`). */
const maxReturn = 2048;

/**
 * Only a path of this application may be the way back after the login: one that starts with a
 * single slash, holds no control character and no backslash anywhere, and is at most 2048 bytes
 * long — the rule of the backend's `safeReturnTo` (backend/internal/api/oidc.go). Anything else is
 * the start page.
 */
export function safeReturn(value: string | null | undefined): string {
  if (
    !value ||
    !value.startsWith('/') ||
    value.startsWith('//') ||
    new TextEncoder().encode(value).length > maxReturn
  ) {
    return '/';
  }
  for (let at = 0; at < value.length; at++) {
    const code = value.charCodeAt(at);
    if (code < 0x20 || code === 0x7f || code === 0x5c) {
      return '/';
    }
  }
  return value;
}

/**
 * What the page says when the identity provider's way back failed: the backend sends the browser
 * to `/login?error=<code>` (docs/adr/0029, docs/adr/0030 D1, docs/adr/0032 D5). The reason of a
 * refusal stays in the backend's log; the page says what the person can do about it.
 */
export const providerRefusals: Record<string, string> = {
  not_allowed:
    'This installation does not admit your account. Ask the people who run cowork for access.',
  not_initialised:
    'This installation is not set up yet: until its first tenant exists, only its administrators can sign in.',
  oidc_failed: 'The sign-in through the identity provider did not complete. Try again.',
  oidc_unavailable: 'This installation offers no sign-in through an identity provider.',
};

/** What the page says for a code it does not know; the code itself is never shown. */
export const unknownRefusal = 'The sign-in did not complete. Try again.';

/** The provider's name where the backend names none, as `COWORK_OIDC_DISPLAY_NAME` defaults. */
const defaultProviderName = 'single sign-on';

/**
 * The login page (docs/adr/0033 D8): the identity provider's button when one is configured and
 * admits somebody, the local form when the installation has local accounts, and a notice when it
 * has neither. A refusal of the local form says only that the name or the password is wrong,
 * whichever it was; a failed way back from the provider is a message of its own. A sign-in ends
 * with a new document, not a route change, so that nothing the person before them looked at in
 * this tab is left in memory (`HARD_NAVIGATION`) — the provider's starts with one.
 */
@Component({
  selector: 'app-login',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [ButtonDirective, Divider, FormsModule, InputText, InputPassword, Wordmark],
  templateUrl: './login.html',
  styleUrl: './auth-page.scss',
})
export class Login {
  /** Where to go afterwards, from `?return=`. */
  readonly return = input<string>();
  /** Why the identity provider's way back failed, from `?error=`. */
  readonly error = input<string>();

  protected readonly auth = inject(AuthService);
  private readonly problems = inject(ProblemService);
  private readonly navigate = inject(HARD_NAVIGATION);

  protected readonly username = signal('');
  protected readonly password = signal('');
  /** The password is hidden until the person shows it. */
  protected readonly masked = signal(true);
  protected readonly busy = signal(false);
  /** Why the local form was refused. */
  protected readonly refusal = signal('');
  /** The person tried the local form since the page came: the provider's message is old news. */
  private readonly retried = signal(false);
  protected readonly canSubmit = computed(
    () => this.username().trim() !== '' && this.password() !== '' && !this.busy(),
  );
  protected readonly local = computed(
    () => this.auth.options.hasValue() && this.auth.options.value().local,
  );
  protected readonly oidc = computed(
    () => this.auth.options.hasValue() && this.auth.options.value().oidc,
  );
  protected readonly providerName = computed(
    () =>
      (this.auth.options.hasValue() && this.auth.options.value().oidc_name) || defaultProviderName,
  );
  protected readonly unavailable = computed(
    () =>
      this.auth.options.error() !== undefined ||
      (this.auth.options.hasValue() &&
        !this.auth.options.value().local &&
        !this.auth.options.value().oidc),
  );
  protected readonly providerRefusal = computed(() => {
    const code = this.error();
    if (!code || this.retried()) {
      return undefined;
    }
    // Only the codes of the record itself: `?error=constructor` names a property of every object.
    return Object.hasOwn(providerRefusals, code) ? providerRefusals[code] : unknownRefusal;
  });

  /**
   * A navigation, not a call: the backend answers with a redirect to the identity provider and
   * remembers the way back in a cookie of its own, which it checks again (docs/adr/0029).
   */
  protected signInWithProvider(): void {
    this.navigate(`${loginOidc.PATH}?return_to=${encodeURIComponent(safeReturn(this.return()))}`);
  }

  protected async submit(): Promise<void> {
    if (!this.canSubmit()) {
      return;
    }
    this.busy.set(true);
    this.refusal.set('');
    this.retried.set(true);
    try {
      const result = await this.auth.login(this.username().trim(), this.password());
      this.password.set('');
      const back = safeReturn(this.return());
      this.navigate(
        result.password_change_required ? `/password?return=${encodeURIComponent(back)}` : back,
      );
    } catch (error) {
      const problem = this.problems.read(error);
      if (error instanceof HttpErrorResponse && error.status === 401) {
        this.refusal.set('The name or the password is wrong.');
      } else if (error instanceof HttpErrorResponse && error.status === 429) {
        this.refusal.set('Too many attempts from here. Wait a minute and try again.');
      } else {
        this.refusal.set(problem.detail || problem.title);
      }
    } finally {
      this.busy.set(false);
    }
  }
}
