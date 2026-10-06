import { HttpErrorResponse } from '@angular/common/http';
import { inject, Injectable, resource } from '@angular/core';
import { Api } from '../api/api';
import { getAuthOptions } from '../api/fn/auth/get-auth-options';
import { loginLocal } from '../api/fn/auth/login-local';
import { logout } from '../api/fn/auth/logout';
import { changeMyPassword } from '../api/fn/me/change-my-password';
import { getMe } from '../api/fn/me/get-me';
import { LocalLoginResult, LogoutResult } from '../api/models';
import { SessionService } from './session.service';
import { SignInMemory } from './sign-in-memory';

/**
 * The browser's login (docs/adr/0031, 0033): the session cookie is the backend's — `HttpOnly`,
 * so no script here ever sees it — and these calls only start, change and end it. The flows live
 * under `/auth/`, beside the API's `/api/v1`, and go through the generated client like every call.
 */
@Injectable({ providedIn: 'root' })
export class AuthService {
  private readonly api = inject(Api);
  private readonly session = inject(SessionService);
  private readonly memory = inject(SignInMemory);

  /** What the login page offers (docs/adr/0033 D8): the local form, the identity provider, or neither. */
  readonly options = resource({ loader: () => this.api.invoke(getAuthOptions) });

  /**
   * Starts the session. Who is working is not asked again here: the login page replaces the
   * document afterwards (`HARD_NAVIGATION`), and the new one asks. A local sign-in that succeeds
   * forgets that the person signs in through the identity provider: the login page no longer signs
   * this browser in through it by itself (docs/adr/0029 D6).
   */
  async login(username: string, password: string): Promise<LocalLoginResult> {
    const result = await this.api.invoke(loginLocal, { body: { username, password } });
    this.memory.forgetProvider();
    return result;
  }

  /**
   * Whether this browser holds a session now — `GET /api/v1/me`, asked anew, since another tab may
   * have signed in meanwhile: a `401` is no, an answer yes, and any other failure is thrown, since
   * then nobody can say.
   */
  async hasSession(): Promise<boolean> {
    try {
      await this.api.invoke(getMe);
      return true;
    } catch (error) {
      if (error instanceof HttpErrorResponse && error.status === 401) {
        return false;
      }
      throw error;
    }
  }

  /** Changes the person's password; every other session of theirs ends (docs/adr/0033 D4). */
  async changePassword(current: string, next: string): Promise<void> {
    await this.api.invoke(changeMyPassword, {
      body: { current_password: current, new_password: next },
    });
    // The answer ends the requirement (docs/adr/0033 D4). Said here before the page goes on: the
    // shell sends a person who must change it to the password page, and while `me` loads again it
    // shows the answer before — a page reached in that moment was sent back to change it again.
    this.session.me.update((me) => (me ? { ...me, password_change_required: false } : me));
    this.session.me.reload();
  }

  /**
   * Ends the session. Who is working is not asked again — the answer would be a `401` racing the
   * sign-out's own way to the login: the shell replaces the document afterwards. A session the
   * identity provider made may answer with the provider's own logout (docs/adr/0031 D4), which
   * this returns for the browser to go on to; `null` means the login page. Only a web address is
   * followed: a `javascript:` URL from a provider's discovery would otherwise run in this page.
   *
   * The browser forgets that the person signs in through the identity provider first, before the
   * backend is asked and whatever it answers: an explicit sign-out is never undone by the login
   * page signing in again by itself (docs/adr/0029 D6).
   */
  async logout(): Promise<string | null> {
    this.memory.forgetProvider();
    // A 204 has no body, which the generated client hands on as null.
    const answer = (await this.api.invoke(logout)) as LogoutResult | null;
    return webAddress(answer?.end_session_url);
  }
}

/** An absolute `https:` or `http:` URL as it is, and null for anything else. */
export function webAddress(value: string | undefined): string | null {
  if (!value || !URL.canParse(value)) {
    return null;
  }
  const { protocol } = new URL(value);
  return protocol === 'https:' || protocol === 'http:' ? value : null;
}
