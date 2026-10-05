import { inject, Injectable, resource } from '@angular/core';
import { Api } from '../api/api';
import { getAuthOptions } from '../api/fn/auth/get-auth-options';
import { loginLocal } from '../api/fn/auth/login-local';
import { logout } from '../api/fn/auth/logout';
import { changeMyPassword } from '../api/fn/me/change-my-password';
import { LocalLoginResult, LogoutResult } from '../api/models';
import { SessionService } from './session.service';

/**
 * The browser's login (docs/adr/0031, 0033): the session cookie is the backend's — `HttpOnly`,
 * so no script here ever sees it — and these calls only start, change and end it. The flows live
 * under `/auth/`, beside the API's `/api/v1`, and go through the generated client like every call.
 */
@Injectable({ providedIn: 'root' })
export class AuthService {
  private readonly api = inject(Api);
  private readonly session = inject(SessionService);

  /** What the login page offers (docs/adr/0033 D8): the local form, the identity provider, or neither. */
  readonly options = resource({ loader: () => this.api.invoke(getAuthOptions) });

  /**
   * Starts the session. Who is working is not asked again here: the login page replaces the
   * document afterwards (`HARD_NAVIGATION`), and the new one asks.
   */
  login(username: string, password: string): Promise<LocalLoginResult> {
    return this.api.invoke(loginLocal, { body: { username, password } });
  }

  /** Changes the person's password; every other session of theirs ends (docs/adr/0033 D4). */
  async changePassword(current: string, next: string): Promise<void> {
    await this.api.invoke(changeMyPassword, {
      body: { current_password: current, new_password: next },
    });
    this.session.me.reload();
  }

  /**
   * Ends the session. Who is working is not asked again — the answer would be a `401` racing the
   * sign-out's own way to the login: the shell replaces the document afterwards. A session the
   * identity provider made may answer with the provider's own logout (docs/adr/0031 D4), which
   * this returns for the browser to go on to; `null` means the login page. Only a web address is
   * followed: a `javascript:` URL from a provider's discovery would otherwise run in this page.
   */
  async logout(): Promise<string | null> {
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
