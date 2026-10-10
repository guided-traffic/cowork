import { DOCUMENT } from '@angular/common';
import { HttpErrorResponse } from '@angular/common/http';
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
import { ButtonDirective } from 'primeng/button';
import { Divider } from 'primeng/divider';
import { InputText } from 'primeng/inputtext';
import { InputPassword } from 'primeng/inputpassword';
import { loginOidc } from '../../api/fn/auth/login-oidc';
import { Wordmark } from '../../brand/logo';
import { AuthService } from '../../core/auth.service';
import { HARD_NAVIGATION } from '../../core/hard-navigation';
import { ProblemService } from '../../core/problem.service';
import { SignInMemory } from '../../core/sign-in-memory';
import { whenPresent } from './presence';

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
    'This installation is not set up yet: until its first team exists, only its administrators can sign in.',
  oidc_failed: 'The sign-in through the identity provider did not complete. Try again.',
  oidc_unavailable: 'This installation offers no sign-in through an identity provider.',
};

/** What the page says for a code it does not know; the code itself is never shown. */
export const unknownRefusal = 'The sign-in did not complete. Try again.';

/**
 * The code of a silent sign-in the identity provider could not complete without the person
 * (docs/adr/0029 D6): no refusal — the page says calmly that the provider's session ended.
 */
export const loginRequired = 'login_required';

/** The login page itself is no way back for a person with a session: it would only come here again. */
const loginPage = /^\/login(?:[?#]|$)/;

/** The provider's name where the backend names none, as `COWORK_OIDC_DISPLAY_NAME` defaults. */
const defaultProviderName = 'single sign-on';

/**
 * The login page (docs/adr/0033 D8): the identity provider's button when one is configured and
 * admits somebody, the local form when the installation has local accounts, and a notice when it
 * has neither. A refusal of the local form says only that the name or the password is wrong,
 * whichever it was; a failed way back from the provider is a message of its own. A sign-in ends
 * with a new document, not a route change, so that nothing the person before them looked at in
 * this tab is left in memory (`HARD_NAVIGATION`) — the provider's starts with one.
 *
 * **It signs the person in again by itself** after their session ended (docs/adr/0029 D6), when the
 * provider is offered, no way back from it failed — `?error=`, whatever it says, `login_required`
 * included, so the page never tries twice in a row —, the browser remembers that the person signs
 * in through it (`SignInMemory`, written by the button, forgotten by a local sign-in and a sign-out)
 * and this tab has not tried since its last session. It says so, and waits for a sign of a person
 * ({@link whenPresent}), so that an open tab nobody looks at does not sign itself in and show
 * content again; then it asks whether another tab has signed in meanwhile, and goes back to where the
 * person was if so, or leaves for the provider's sign-in with `silent=true`. The button and the form
 * stay meanwhile, and a click on the button wins.
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
  private readonly memory = inject(SignInMemory);
  private readonly document = inject(DOCUMENT);

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
    if (!code || code === loginRequired || this.retried()) {
      return undefined;
    }
    // Only the codes of the record itself: `?error=constructor` names a property of every object.
    return Object.hasOwn(providerRefusals, code) ? providerRefusals[code] : unknownRefusal;
  });
  /** The page's own sign-in found no session at the provider: a calm word, no refusal. */
  protected readonly providerSessionEnded = computed(
    () => this.error() === loginRequired && !this.retried(),
  );

  /** A sign-in is on its way out of this page — the button's, the form's or the page's own. */
  private leaving = false;
  /**
   * The page gave up its own sign-in: nobody could say whether a session exists, nothing remembers
   * the provider any longer, or the person's own sign-in is on its way.
   */
  private readonly gaveUp = signal(false);
  /**
   * Read as the page comes: the person signs in through the provider, and this tab has not tried
   * since its last session.
   */
  private readonly remembered = this.memory.provider() && !this.memory.tried();
  /** The page signs the person in again by itself at their first sign (docs/adr/0029 D6). */
  protected readonly resuming = computed(
    () => this.remembered && this.oidc() && this.error() === undefined && !this.gaveUp(),
  );

  constructor() {
    effect((onCleanup) => {
      if (this.resuming()) {
        onCleanup(whenPresent(this.document, () => void this.resume()));
      }
    });
  }

  /**
   * A navigation, not a call: the backend answers with a redirect to the identity provider and
   * remembers the way back in a cookie of its own, which it checks again (docs/adr/0029). The browser
   * remembers that the person signs in through the provider, for the page to sign them in again by
   * itself when this session ends (D6).
   */
  protected signInWithProvider(): void {
    this.leaving = true;
    this.memory.rememberProvider();
    this.navigate(`${loginOidc.PATH}?return_to=${encodeURIComponent(safeReturn(this.return()))}`);
  }

  /**
   * A person is back. Another tab may have signed in meanwhile: then the page goes back to where
   * the person was. Otherwise it leaves for the provider's sign-in with `silent=true` — unless the
   * person signed out meanwhile, which forgets the provider, or the tab cannot note its attempt. It
   * stays as it is when the backend cannot say, and when the person's own sign-in is on its way.
   */
  private async resume(): Promise<void> {
    let signedIn: boolean;
    try {
      signedIn = await this.auth.hasSession();
    } catch {
      this.gaveUp.set(true);
      return;
    }
    if (this.leaving) {
      return;
    }
    if (this.busy()) {
      this.gaveUp.set(true);
      return;
    }
    const back = safeReturn(this.return());
    if (signedIn) {
      this.leaving = true;
      this.navigate(loginPage.test(back) ? '/' : back);
    } else if (this.memory.provider() && this.memory.markTried()) {
      this.leaving = true;
      this.navigate(`${loginOidc.PATH}?return_to=${encodeURIComponent(back)}&silent=true`);
    } else {
      this.gaveUp.set(true);
    }
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
      this.leaving = true;
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
