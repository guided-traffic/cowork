import { HttpErrorResponse } from '@angular/common/http';
import { ChangeDetectionStrategy, Component, computed, inject, input, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { ButtonDirective } from 'primeng/button';
import { InputText } from 'primeng/inputtext';
import { InputPassword } from 'primeng/inputpassword';
import { Wordmark } from '../../brand/logo';
import { AuthService } from '../../core/auth.service';
import { HARD_NAVIGATION } from '../../core/hard-navigation';
import { ProblemService } from '../../core/problem.service';

/** Only a path of this application may be the way back after the login — never another site. */
export function safeReturn(value: string | null | undefined): string {
  return value && value.startsWith('/') && !value.startsWith('//') && !value.startsWith('/\\')
    ? value
    : '/';
}

/**
 * The login page (docs/adr/0033 D8): the local form when the installation has local accounts,
 * the identity provider's button when one is configured (phase 4), and a notice when it has
 * neither. A refusal says only that the name or the password is wrong, whichever it was. A
 * sign-in ends with a new document, not a route change, so that nothing the person before them
 * looked at in this tab is left in memory (`HARD_NAVIGATION`).
 */
@Component({
  selector: 'app-login',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [ButtonDirective, FormsModule, InputText, InputPassword, Wordmark],
  templateUrl: './login.html',
  styleUrl: './auth-page.scss',
})
export class Login {
  /** Where to go afterwards, from `?return=`. */
  readonly return = input<string>();

  protected readonly auth = inject(AuthService);
  private readonly problems = inject(ProblemService);
  private readonly navigate = inject(HARD_NAVIGATION);

  protected readonly username = signal('');
  protected readonly password = signal('');
  /** The password is hidden until the person shows it. */
  protected readonly masked = signal(true);
  protected readonly busy = signal(false);
  protected readonly error = signal('');
  protected readonly canSubmit = computed(
    () => this.username().trim() !== '' && this.password() !== '' && !this.busy(),
  );
  protected readonly local = computed(
    () => this.auth.options.hasValue() && this.auth.options.value().local,
  );
  protected readonly unavailable = computed(
    () =>
      this.auth.options.error() !== undefined ||
      (this.auth.options.hasValue() &&
        !this.auth.options.value().local &&
        !this.auth.options.value().oidc),
  );

  protected async submit(): Promise<void> {
    if (!this.canSubmit()) {
      return;
    }
    this.busy.set(true);
    this.error.set('');
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
        this.error.set('The name or the password is wrong.');
      } else if (error instanceof HttpErrorResponse && error.status === 429) {
        this.error.set('Too many attempts from here. Wait a minute and try again.');
      } else {
        this.error.set(problem.detail || problem.title);
      }
    } finally {
      this.busy.set(false);
    }
  }
}
