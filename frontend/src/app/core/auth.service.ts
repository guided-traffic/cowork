import { inject, Injectable, resource } from '@angular/core';
import { Api } from '../api/api';
import { changeMyPassword, getAuthOptions, loginLocal, logout } from '../api/functions';
import { LocalLoginResult } from '../api/models';
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
   * sign-out's own way to the login: the shell replaces the document afterwards.
   */
  logout(): Promise<void> {
    return this.api.invoke(logout);
  }
}
