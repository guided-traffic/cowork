import { ChangeDetectionStrategy, Component, computed, inject, input, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { Router } from '@angular/router';
import { ButtonDirective } from 'primeng/button';
import { InputPassword } from 'primeng/inputpassword';
import { Wordmark } from '../../brand/logo';
import { AuthService } from '../../core/auth.service';
import { ProblemService } from '../../core/problem.service';
import { SessionService } from '../../core/session.service';
import { safeReturn } from './login';

/**
 * The person changes their password (docs/adr/0033 D4): after a temporary one, before anything
 * else, or whenever they like. The current password counts like a login attempt, the new one is
 * held to the length policy by the server, and every other session of the person ends.
 */
@Component({
  selector: 'app-change-password',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [ButtonDirective, FormsModule, InputPassword, Wordmark],
  templateUrl: './change-password.html',
  styleUrl: './auth-page.scss',
})
export class ChangePassword {
  /** Where to go afterwards, from `?return=`. */
  readonly return = input<string>();

  private readonly auth = inject(AuthService);
  private readonly problems = inject(ProblemService);
  private readonly router = inject(Router);
  protected readonly session = inject(SessionService);

  protected readonly current = signal('');
  protected readonly next = signal('');
  protected readonly again = signal('');
  protected readonly masked = signal(true);
  protected readonly busy = signal(false);
  protected readonly errors = signal<Record<string, string>>({});
  protected readonly required = computed(
    () => this.session.person()?.password_change_required ?? false,
  );
  protected readonly mismatch = computed(() => this.again() !== '' && this.again() !== this.next());
  protected readonly canSubmit = computed(
    () =>
      this.current() !== '' && this.next() !== '' && this.again() === this.next() && !this.busy(),
  );

  protected async submit(): Promise<void> {
    if (!this.canSubmit()) {
      return;
    }
    this.busy.set(true);
    this.errors.set({});
    try {
      await this.auth.changePassword(this.current(), this.next());
      this.current.set('');
      this.next.set('');
      this.again.set('');
      await this.router.navigateByUrl(safeReturn(this.return()));
    } catch (error) {
      // A wrong current password is the field error of `current_password`, a short new one that
      // of `new_password`; a `401` means the session is gone, and the login takes over.
      this.errors.set(this.problems.report(error, { fields: true }).fields);
    } finally {
      this.busy.set(false);
    }
  }
}
