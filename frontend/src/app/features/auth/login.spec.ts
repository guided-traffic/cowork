import { HttpErrorResponse } from '@angular/common/http';
import { signal, WritableSignal } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { MessageService } from 'primeng/api';
import type { MockInstance } from 'vitest';
import { AuthOptions, LocalLoginResult, Problem } from '../../api/models';
import { AuthService } from '../../core/auth.service';
import { HARD_NAVIGATION, HardNavigation } from '../../core/hard-navigation';
import { Login, providerRefusals, safeReturn, unknownRefusal } from './login';
import { presenceInputs } from './presence';

describe('safeReturn', () => {
  it.each([
    ['/', '/'],
    ['/t/acme', '/t/acme'],
    ['/t/acme/tickets/COW-12', '/t/acme/tickets/COW-12'],
    ['/t/acme/p/COW/backlog?q=flicker#top', '/t/acme/p/COW/backlog?q=flicker#top'],
    ['/x', '/x'],
  ])('lets the path %j of this application stand', (value, expected) => {
    expect(safeReturn(value)).toBe(expected);
  });

  it.each<[string | null | undefined, string]>([
    ['//evil.example', 'a protocol-relative URL'],
    ['//evil.example/t/acme', 'a protocol-relative URL with a path'],
    ['/\\evil.example', 'a slash and a backslash, which browsers read as two slashes'],
    ['\\\\evil.example', 'two backslashes'],
    ['https://evil.example', 'an absolute URL'],
    ['http://evil.example/t/acme', 'an absolute URL with a path'],
    ['javascript:alert(1)', 'a script URL'],
    ['evil.example/t/acme', 'a host without a scheme'],
    ['t/acme', 'a path that is not rooted'],
    ['', 'nothing'],
    [null, 'null'],
    [undefined, 'undefined'],
    ['/t/ac\tme', 'a tab'],
    ['/t/acme\n', 'a line feed'],
    ['/t/ac\rme', 'a carriage return'],
    ['/t/\t\\acme', 'a tab and a backslash'],
    ['/t/acme\\backlog', 'a backslash'],
    ['/t/acme\u0000', 'a null character'],
    ['/t/acme\u007f', 'a delete character'],
  ])('sends %j to the start page instead: %s', (value) => {
    expect(safeReturn(value)).toBe('/');
  });

  it('lets a path of 2048 bytes stand and sends a longer one to the start page', () => {
    const longest = `/${'a'.repeat(2047)}`;

    expect(safeReturn(longest)).toBe(longest);
    expect(safeReturn(`${longest}a`)).toBe('/');
  });

  it('counts the length in UTF-8 bytes, as the backend does', () => {
    // 1 + 1024 × 2 bytes, though only 1025 characters.
    expect(safeReturn(`/${'é'.repeat(1024)}`)).toBe('/');
    expect(safeReturn(`/${'é'.repeat(1023)}`)).toBe(`/${'é'.repeat(1023)}`);
  });
});

/** What the installation offers on the login page, with the minimum password length of the default. */
const offered = (
  local: boolean,
  oidc: boolean,
  name: string | null = oidc ? 'Dex' : null,
): AuthOptions => ({
  local,
  oidc,
  oidc_name: name,
  password_min_length: 12,
  token_max_lifetime_days: 365,
});

describe('Login', () => {
  let options: {
    isLoading: WritableSignal<boolean>;
    error: WritableSignal<unknown>;
    value: WritableSignal<AuthOptions | undefined>;
    hasValue: () => boolean;
  };
  let login: MockInstance<AuthService['login']>;
  let hasSession: MockInstance<AuthService['hasSession']>;
  let navigate: MockInstance<HardNavigation>;

  beforeEach(() => {
    const value = signal<AuthOptions | undefined>(offered(true, false));
    options = {
      isLoading: signal(false),
      error: signal<unknown>(undefined),
      value,
      hasValue: () => value() !== undefined,
    };
    login = vi
      .fn<AuthService['login']>()
      .mockResolvedValue({ password_change_required: false } satisfies LocalLoginResult);
    hasSession = vi.fn<AuthService['hasSession']>().mockResolvedValue(false);
    navigate = vi.fn<HardNavigation>();
    TestBed.configureTestingModule({
      providers: [
        MessageService,
        { provide: AuthService, useValue: { options, login, hasSession } },
        { provide: HARD_NAVIGATION, useValue: navigate },
      ],
    });
  });

  afterEach(() => {
    vi.restoreAllMocks();
    localStorage.clear();
    sessionStorage.clear();
    Reflect.deleteProperty(document, 'visibilityState');
  });

  async function render(back?: string, error?: string) {
    const fixture = TestBed.createComponent(Login);
    if (back !== undefined) {
      fixture.componentRef.setInput('return', back);
    }
    if (error !== undefined) {
      fixture.componentRef.setInput('error', error);
    }
    await settle(fixture);
    return fixture;
  }

  /** Lets what a submit started finish, and shows it; fields register with the form a moment later. */
  async function settle(fixture: ComponentFixture<Login>) {
    await new Promise((resolve) => setTimeout(resolve));
    fixture.detectChanges();
    await fixture.whenStable();
  }

  const host = (fixture: ComponentFixture<Login>) => fixture.nativeElement as HTMLElement;

  const el = (fixture: ComponentFixture<Login>, testId: string) =>
    host(fixture).querySelector<HTMLElement>(`[data-testid="${testId}"]`);

  const username = (fixture: ComponentFixture<Login>) =>
    el(fixture, 'login-username') as HTMLInputElement;

  const password = (fixture: ComponentFixture<Login>) =>
    el(fixture, 'login-password') as HTMLInputElement;

  const submitButton = (fixture: ComponentFixture<Login>) =>
    el(fixture, 'login-submit') as HTMLButtonElement;

  const reveal = (fixture: ComponentFixture<Login>) =>
    el(fixture, 'login-reveal') as HTMLButtonElement;

  function typeInto(field: HTMLInputElement, value: string, fixture: ComponentFixture<Login>) {
    field.value = value;
    field.dispatchEvent(new Event('input'));
    fixture.detectChanges();
  }

  function fill(fixture: ComponentFixture<Login>, name = 'ada', secret = 'correct horse') {
    typeInto(username(fixture), name, fixture);
    typeInto(password(fixture), secret, fixture);
  }

  const submit = (fixture: ComponentFixture<Login>) =>
    host(fixture)
      .querySelector('form')
      ?.dispatchEvent(new Event('submit', { cancelable: true }));

  function refusal(status: number, title: string, detail?: string) {
    const body: Problem = {
      type: 'about:blank',
      title,
      status,
      code: status === 401 ? 'unauthenticated' : 'internal',
      ...(detail ? { detail } : {}),
    };
    return new HttpErrorResponse({ status, statusText: title, error: body });
  }

  describe('what the page offers', () => {
    it('shows the form of the local login with the name of the product above it', async () => {
      const fixture = await render();

      expect(host(fixture).querySelector('h1')?.textContent).toBe('Sign in');
      expect(host(fixture).querySelector('app-wordmark')).not.toBeNull();
      expect(username(fixture)).not.toBeNull();
      expect(password(fixture)).not.toBeNull();
      expect(el(fixture, 'login-unavailable')).toBeNull();
      expect(el(fixture, 'login-error')).toBeNull();
    });

    it('says that it loads while it asks what to offer', async () => {
      options.isLoading.set(true);

      const fixture = await render();

      expect(host(fixture).querySelector('.card > p.muted')?.textContent).toBe('Loading…');
      expect(host(fixture).querySelector('form')).toBeNull();
    });

    it('offers a notice and no form when the installation has no login at all', async () => {
      options.value.set(offered(false, false));

      const fixture = await render();

      expect(el(fixture, 'login-unavailable')?.textContent).toContain(
        'This installation offers no sign-in yet.',
      );
      expect(host(fixture).querySelector('form')).toBeNull();
    });

    it('offers the same notice when the backend cannot say what it offers', async () => {
      options.value.set(undefined);
      options.error.set(new HttpErrorResponse({ status: 0, statusText: 'Unknown Error' }));

      const fixture = await render();

      expect(el(fixture, 'login-unavailable')).not.toBeNull();
      expect(host(fixture).querySelector('form')).toBeNull();
    });

    it("offers the identity provider's button and no local form when only an identity provider is configured", async () => {
      options.value.set(offered(false, true));

      const fixture = await render();

      expect(el(fixture, 'login-oidc')?.textContent?.trim()).toBe('Sign in with Dex');
      expect(host(fixture).querySelector('form')).toBeNull();
      expect(el(fixture, 'login-divider')).toBeNull();
      expect(el(fixture, 'login-unavailable')).toBeNull();
    });

    it("offers the provider's button above the form, with a line between them, when the installation has both", async () => {
      options.value.set(offered(true, true));

      const fixture = await render();

      expect(host(fixture).querySelector('form')).not.toBeNull();
      expect(el(fixture, 'login-oidc')).not.toBeNull();
      expect(el(fixture, 'login-divider')?.textContent?.trim()).toBe('or with a local account');
      expect(
        el(fixture, 'login-oidc')!.compareDocumentPosition(host(fixture).querySelector('form')!) &
          Node.DOCUMENT_POSITION_FOLLOWING,
      ).toBeTruthy();
      expect(el(fixture, 'login-unavailable')).toBeNull();
    });

    it('offers no button of an identity provider when none admits anybody', async () => {
      const fixture = await render();

      expect(el(fixture, 'login-oidc')).toBeNull();
      expect(el(fixture, 'login-divider')).toBeNull();
    });

    it("names the provider as the installation's default does when the backend names none", async () => {
      options.value.set(offered(true, true, null));

      const fixture = await render();

      expect(el(fixture, 'login-oidc')?.textContent?.trim()).toBe('Sign in with single sign-on');
    });

    it('follows what the backend says when the answer arrives', async () => {
      options.value.set(undefined);
      options.isLoading.set(true);
      const fixture = await render();
      expect(host(fixture).querySelector('form')).toBeNull();

      options.value.set(offered(true, false));
      options.isLoading.set(false);
      await settle(fixture);

      expect(host(fixture).querySelector('form')).not.toBeNull();
    });

    it('lets the browser and the password managers know which field is which', async () => {
      const fixture = await render();

      expect(username(fixture).getAttribute('autocomplete')).toBe('username');
      expect(password(fixture).getAttribute('autocomplete')).toBe('current-password');
    });

    it('hides the password while it is typed', async () => {
      const fixture = await render();

      expect(password(fixture).type).toBe('password');
      expect(reveal(fixture).getAttribute('aria-label')).toBe('Show the password');
      expect(reveal(fixture).querySelector('i')?.classList).toContain('pi-eye');
    });

    it('shows the password only while the person has asked for it', async () => {
      const fixture = await render();

      reveal(fixture).click();
      await settle(fixture);
      expect(password(fixture).type).toBe('text');
      expect(reveal(fixture).getAttribute('aria-label')).toBe('Hide the password');
      expect(reveal(fixture).querySelector('i')?.classList).toContain('pi-eye-slash');

      reveal(fixture).click();
      await settle(fixture);
      expect(password(fixture).type).toBe('password');
      expect(reveal(fixture).getAttribute('aria-label')).toBe('Show the password');
    });

    it('does not submit the form when the password is revealed', async () => {
      const fixture = await render();
      fill(fixture);

      reveal(fixture).click();
      await settle(fixture);

      expect(reveal(fixture).type).toBe('button');
      expect(login).not.toHaveBeenCalled();
    });
  });

  describe('what a person needs to sign in', () => {
    it('cannot sign in without a name and a password, or with a name of spaces only', async () => {
      const fixture = await render();
      expect(submitButton(fixture).disabled).toBe(true);

      typeInto(username(fixture), 'ada', fixture);
      expect(submitButton(fixture).disabled).toBe(true);
      typeInto(password(fixture), 'correct horse', fixture);
      expect(submitButton(fixture).disabled).toBe(false);
      typeInto(username(fixture), '   ', fixture);
      expect(submitButton(fixture).disabled).toBe(true);
    });

    it('sends nothing when the form is submitted incomplete', async () => {
      const fixture = await render();
      typeInto(username(fixture), 'ada', fixture);

      submit(fixture);
      await settle(fixture);

      expect(login).not.toHaveBeenCalled();
    });
  });

  describe('signing in', () => {
    it('sends the name without its spaces and the password as typed', async () => {
      const fixture = await render();
      fill(fixture, '  ada  ', '  spaces count  ');

      submit(fixture);
      await settle(fixture);

      expect(login).toHaveBeenCalledExactlyOnceWith('ada', '  spaces count  ');
    });

    it('goes to the start page afterwards unless the person came from somewhere', async () => {
      const fixture = await render();
      fill(fixture);

      submit(fixture);
      await settle(fixture);

      expect(navigate).toHaveBeenCalledExactlyOnceWith('/');
    });

    it('goes back to the page the person came from', async () => {
      const fixture = await render('/t/acme/tickets/COW-12');
      fill(fixture);

      submit(fixture);
      await settle(fixture);

      expect(navigate).toHaveBeenCalledExactlyOnceWith('/t/acme/tickets/COW-12');
    });

    it.each(['//evil.example', '/\\evil.example', 'https://evil.example', 'javascript:alert(1)'])(
      'never goes back to %s, which is no page of this application',
      async (back) => {
        const fixture = await render(back);
        fill(fixture);

        submit(fixture);
        await settle(fixture);

        expect(navigate).toHaveBeenCalledExactlyOnceWith('/');
      },
    );

    it('empties the password field once the person is signed in', async () => {
      const fixture = await render();
      fill(fixture);

      submit(fixture);
      await settle(fixture);

      expect(password(fixture).value).toBe('');
    });

    it('is told to choose a new password first when the account requires it, and goes on from there', async () => {
      login.mockResolvedValue({ password_change_required: true });
      const fixture = await render('/t/acme/tickets/COW-12?tab=activity');
      fill(fixture);

      submit(fixture);
      await settle(fixture);

      expect(navigate).toHaveBeenCalledExactlyOnceWith(
        `/password?return=${encodeURIComponent('/t/acme/tickets/COW-12?tab=activity')}`,
      );
    });

    it('passes on only a safe way back to the password page, too', async () => {
      login.mockResolvedValue({ password_change_required: true });
      const fixture = await render('//evil.example');
      fill(fixture);

      submit(fixture);
      await settle(fixture);

      expect(navigate).toHaveBeenCalledExactlyOnceWith(
        `/password?return=${encodeURIComponent('/')}`,
      );
    });

    it('shows its button as busy, and signs in once, while the backend answers', async () => {
      let finish: (result: LocalLoginResult) => void = () => undefined;
      login.mockReturnValue(
        new Promise<LocalLoginResult>((resolve) => {
          finish = resolve;
        }),
      );
      const fixture = await render();
      fill(fixture);

      submit(fixture);
      submit(fixture);
      await settle(fixture);
      expect(submitButton(fixture).disabled).toBe(true);
      expect(submitButton(fixture).querySelector('i.pi-spinner')).not.toBeNull();
      expect(login).toHaveBeenCalledOnce();
      finish({ password_change_required: false });
      await settle(fixture);

      expect(navigate).toHaveBeenCalledOnce();
    });
  });

  describe('a sign-in that is refused', () => {
    async function refused(error: HttpErrorResponse) {
      login.mockRejectedValue(error);
      const fixture = await render();
      fill(fixture);
      submit(fixture);
      await settle(fixture);
      return fixture;
    }

    it('says that the name or the password is wrong, whichever it was, on a 401', async () => {
      const fixture = await refused(refusal(401, 'Unauthenticated', 'No such user.'));

      expect(el(fixture, 'login-error')?.textContent).toBe('The name or the password is wrong.');
      expect(el(fixture, 'login-error')?.getAttribute('role')).toBe('alert');
      expect(navigate).not.toHaveBeenCalled();
    });

    it('does not repeat what the server said about the account on a 401', async () => {
      const fixture = await refused(
        refusal(401, 'Unauthenticated', 'The password of ada is wrong.'),
      );

      expect(el(fixture, 'login-error')?.textContent).not.toContain('ada');
    });

    it('asks the person to wait when there were too many attempts, on a 429', async () => {
      const fixture = await refused(refusal(429, 'Too many requests', 'Slow down.'));

      expect(el(fixture, 'login-error')?.textContent).toBe(
        'Too many attempts from here. Wait a minute and try again.',
      );
    });

    it('says what the problem says for any other refusal', async () => {
      const fixture = await refused(refusal(500, 'Internal error', 'The session store is down.'));

      expect(el(fixture, 'login-error')?.textContent).toBe('The session store is down.');
    });

    it('says what the title says when the problem has no detail', async () => {
      const fixture = await refused(refusal(403, 'Forbidden'));

      expect(el(fixture, 'login-error')?.textContent).toBe('Forbidden');
    });

    it('says that the backend cannot be reached when it cannot', async () => {
      const fixture = await refused(
        new HttpErrorResponse({ status: 0, statusText: 'Unknown Error' }),
      );

      expect(el(fixture, 'login-error')?.textContent).toBe(
        'The connection failed; cowork tries again on its own.',
      );
    });

    it('keeps the name and the password so that the person can correct them', async () => {
      const fixture = await refused(refusal(401, 'Unauthenticated'));

      expect(username(fixture).value).toBe('ada');
      expect(password(fixture).value).toBe('correct horse');
      expect(submitButton(fixture).disabled).toBe(false);
    });

    it('shows no old message once the next attempt is on its way, and signs in when it succeeds', async () => {
      const fixture = await refused(refusal(401, 'Unauthenticated'));
      expect(el(fixture, 'login-error')).not.toBeNull();
      login.mockResolvedValue({ password_change_required: false });

      submit(fixture);
      await settle(fixture);

      expect(el(fixture, 'login-error')).toBeNull();
      expect(navigate).toHaveBeenCalledExactlyOnceWith('/');
    });
  });

  describe('signing in through the identity provider (docs/adr/0029)', () => {
    beforeEach(() => options.value.set(offered(true, true)));

    it('leaves for the start of the login at the backend, with the start page as the way back', async () => {
      const fixture = await render();

      el(fixture, 'login-oidc')?.click();

      expect(navigate).toHaveBeenCalledExactlyOnceWith('/auth/oidc/login?return_to=%2F');
      expect(login).not.toHaveBeenCalled();
    });

    it('hands on the page the person came from as the way back', async () => {
      const fixture = await render('/t/acme/tickets/COW-12?tab=activity');

      el(fixture, 'login-oidc')?.click();

      expect(navigate).toHaveBeenCalledExactlyOnceWith(
        `/auth/oidc/login?return_to=${encodeURIComponent('/t/acme/tickets/COW-12?tab=activity')}`,
      );
    });

    it.each(['//evil.example', '/\\evil.example', 'https://evil.example', 'javascript:alert(1)'])(
      'never hands on %s, which is no page of this application',
      async (back) => {
        const fixture = await render(back);

        el(fixture, 'login-oidc')?.click();

        expect(navigate).toHaveBeenCalledExactlyOnceWith('/auth/oidc/login?return_to=%2F');
      },
    );

    it('is a button that submits nothing', async () => {
      const fixture = await render();

      expect((el(fixture, 'login-oidc') as HTMLButtonElement).type).toBe('button');
    });
  });

  describe('a way back from the identity provider that failed, from ?error=', () => {
    it.each(Object.entries(providerRefusals))('says what %s means', async (code, message) => {
      const fixture = await render(undefined, code);

      expect(el(fixture, 'login-provider-error')?.textContent?.trim()).toBe(message);
      expect(el(fixture, 'login-provider-error')?.getAttribute('role')).toBe('alert');
    });

    it.each([
      ['not_allowed', 'does not admit your account'],
      ['not_initialised', 'until its first tenant exists'],
      ['oidc_failed', 'did not complete'],
      ['oidc_unavailable', 'offers no sign-in through an identity provider'],
    ])('explains %s in words', async (code, words) => {
      const fixture = await render(undefined, code);

      expect(el(fixture, 'login-provider-error')?.textContent).toContain(words);
    });

    it('says only that the sign-in did not complete for a code it does not know, and never shows the code', async () => {
      const fixture = await render(undefined, '<b>forged</b>');

      expect(el(fixture, 'login-provider-error')?.textContent?.trim()).toBe(unknownRefusal);
      expect(host(fixture).innerHTML).not.toContain('forged');
    });

    it.each(['constructor', 'toString', '__proto__', 'hasOwnProperty'])(
      'takes %s, which every object has, for a code it does not know',
      async (code) => {
        const fixture = await render(undefined, code);

        expect(el(fixture, 'login-provider-error')?.textContent?.trim()).toBe(unknownRefusal);
      },
    );

    it('says nothing without one', async () => {
      const fixture = await render();

      expect(el(fixture, 'login-provider-error')).toBeNull();
    });

    it('says it while the page still asks what to offer, and when nothing is offered', async () => {
      options.isLoading.set(true);
      const fixture = await render(undefined, 'not_allowed');
      expect(el(fixture, 'login-provider-error')).not.toBeNull();

      options.isLoading.set(false);
      options.value.set(offered(false, false));
      await settle(fixture);

      expect(el(fixture, 'login-provider-error')).not.toBeNull();
      expect(el(fixture, 'login-unavailable')).not.toBeNull();
    });

    it('stops saying it once the person tries the local form instead', async () => {
      login.mockRejectedValue(refusal(401, 'Unauthenticated'));
      const fixture = await render(undefined, 'not_allowed');
      fill(fixture);

      submit(fixture);
      await settle(fixture);

      expect(el(fixture, 'login-provider-error')).toBeNull();
      expect(el(fixture, 'login-error')?.textContent).toBe('The name or the password is wrong.');
    });
  });

  describe('the button remembers the identity provider (docs/adr/0029 D6)', () => {
    beforeEach(() => options.value.set(offered(true, true)));

    it('remembers in the browser that the person signs in through the identity provider', async () => {
      const fixture = await render();

      el(fixture, 'login-oidc')?.click();

      expect(localStorage.getItem('cowork.sign-in')).toBe('oidc');
      expect(sessionStorage.getItem('cowork.sign-in.attempt')).toBeNull();
    });

    it('signs in as ever where the browser refuses its storage', async () => {
      vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
        throw new DOMException('denied', 'SecurityError');
      });
      const fixture = await render();

      el(fixture, 'login-oidc')?.click();

      expect(navigate).toHaveBeenCalledExactlyOnceWith('/auth/oidc/login?return_to=%2F');
    });
  });

  describe('signing in again by itself (docs/adr/0029 D6)', () => {
    /** A sign of a person: the pointer moved over the page, unless the test names another input. */
    const sign = (name = 'pointermove') =>
      document.body.dispatchEvent(new Event(name, { bubbles: true }));

    function setVisibility(state: DocumentVisibilityState): void {
      Object.defineProperty(document, 'visibilityState', { configurable: true, get: () => state });
      document.dispatchEvent(new Event('visibilitychange'));
    }

    const silent = (back: string) =>
      `/auth/oidc/login?return_to=${encodeURIComponent(back)}&silent=true`;

    beforeEach(() => {
      options.value.set(offered(true, true));
      localStorage.setItem('cowork.sign-in', 'oidc');
    });

    describe('when the provider is offered, remembered, and no way back failed', () => {
      it('says that it signs the person in again, and still offers the button and the form', async () => {
        const fixture = await render('/t/acme/board');

        expect(el(fixture, 'login-resuming')?.textContent?.trim()).toBe(
          'Your session ended. Signing you in again through Dex…',
        );
        expect(el(fixture, 'login-resuming')?.getAttribute('role')).toBe('status');
        expect(el(fixture, 'login-oidc')).not.toBeNull();
        expect(host(fixture).querySelector('form')).not.toBeNull();
        expect(el(fixture, 'login-provider-error')).toBeNull();
      });

      it('waits for a sign of a person: an open tab nobody looks at does not sign itself in', async () => {
        const fixture = await render('/t/acme/board');
        await settle(fixture);

        expect(hasSession).not.toHaveBeenCalled();
        expect(navigate).not.toHaveBeenCalled();
      });

      it("leaves for the provider's sign-in with silent=true and the way back at the first sign", async () => {
        const fixture = await render('/t/acme/board?q=flicker');

        sign();
        await settle(fixture);

        expect(hasSession).toHaveBeenCalledOnce();
        expect(navigate).toHaveBeenCalledExactlyOnceWith(silent('/t/acme/board?q=flicker'));
        expect(sessionStorage.getItem('cowork.sign-in.attempt')).not.toBeNull();
        expect(login).not.toHaveBeenCalled();
      });

      it.each(presenceInputs)('takes %s for a sign', async (name) => {
        const fixture = await render();

        sign(name);
        await settle(fixture);

        expect(navigate).toHaveBeenCalledExactlyOnceWith(silent('/'));
      });

      it('takes the window taking the focus for a sign', async () => {
        const fixture = await render();

        window.dispatchEvent(new Event('focus'));
        await settle(fixture);

        expect(navigate).toHaveBeenCalledExactlyOnceWith(silent('/'));
      });

      it('takes the tab coming back into view for a sign, and its going for none', async () => {
        const fixture = await render();

        setVisibility('hidden');
        await settle(fixture);
        expect(hasSession).not.toHaveBeenCalled();
        setVisibility('visible');
        await settle(fixture);

        expect(navigate).toHaveBeenCalledExactlyOnceWith(silent('/'));
      });

      it('asks once and leaves once, however many signs come', async () => {
        const fixture = await render();

        sign();
        sign('keydown');
        window.dispatchEvent(new Event('focus'));
        await settle(fixture);
        sign();
        await settle(fixture);

        expect(hasSession).toHaveBeenCalledOnce();
        expect(navigate).toHaveBeenCalledOnce();
      });

      it('goes back to where the person was when another tab has signed in meanwhile', async () => {
        hasSession.mockResolvedValue(true);
        const fixture = await render('/t/acme/tickets/COW-12');

        sign();
        await settle(fixture);

        expect(navigate).toHaveBeenCalledExactlyOnceWith('/t/acme/tickets/COW-12');
        expect(sessionStorage.getItem('cowork.sign-in.attempt')).toBeNull();
      });

      it.each(['/login', '/login?return=%2F', '/login#top'])(
        'goes to the start page, not back to itself, when the way back is %s and a session exists',
        async (back) => {
          hasSession.mockResolvedValue(true);
          const fixture = await render(back);

          sign();
          await settle(fixture);

          expect(navigate).toHaveBeenCalledExactlyOnceWith('/');
        },
      );

      it.each(['//evil.example', '/\\evil.example', 'https://evil.example', 'javascript:alert(1)'])(
        'never hands on %s, which is no page of this application',
        async (back) => {
          const fixture = await render(back);

          sign();
          await settle(fixture);

          expect(navigate).toHaveBeenCalledExactlyOnceWith(silent('/'));
        },
      );

      it('stays as it is when the backend cannot say whether a session exists', async () => {
        hasSession.mockRejectedValue(new HttpErrorResponse({ status: 0, statusText: 'Unknown' }));
        const fixture = await render();

        sign();
        await settle(fixture);
        sign();
        await settle(fixture);

        expect(hasSession).toHaveBeenCalledOnce();
        expect(navigate).not.toHaveBeenCalled();
        expect(el(fixture, 'login-resuming')).toBeNull();
        expect(el(fixture, 'login-oidc')).not.toBeNull();
        expect(sessionStorage.getItem('cowork.sign-in.attempt')).toBeNull();
      });

      it('stays signed out when the person signed out in another tab meanwhile', async () => {
        const fixture = await render();
        localStorage.removeItem('cowork.sign-in');

        sign();
        await settle(fixture);

        expect(navigate).not.toHaveBeenCalled();
        expect(el(fixture, 'login-resuming')).toBeNull();
      });

      it('does not leave for the provider when the tab cannot note its attempt', async () => {
        const fixture = await render();
        vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
          throw new DOMException('quota', 'QuotaExceededError');
        });

        sign();
        await settle(fixture);

        expect(navigate).not.toHaveBeenCalled();
        expect(el(fixture, 'login-resuming')).toBeNull();
      });

      it("lets the person's click on the button win over its own sign-in", async () => {
        let answer: (signedIn: boolean) => void = () => undefined;
        hasSession.mockReturnValue(new Promise<boolean>((resolve) => (answer = resolve)));
        const fixture = await render();
        const button = el(fixture, 'login-oidc')!;

        button.dispatchEvent(new Event('pointerdown', { bubbles: true }));
        button.click();
        answer(false);
        await settle(fixture);

        expect(navigate).toHaveBeenCalledExactlyOnceWith('/auth/oidc/login?return_to=%2F');
      });

      it("leaves the person's own local sign-in alone while it is on its way", async () => {
        let answer: (signedIn: boolean) => void = () => undefined;
        hasSession.mockReturnValue(new Promise<boolean>((resolve) => (answer = resolve)));
        login.mockReturnValue(new Promise<LocalLoginResult>(() => undefined));
        const fixture = await render();
        sign('keydown');
        fill(fixture);

        submit(fixture);
        answer(false);
        await settle(fixture);

        expect(navigate).not.toHaveBeenCalled();
        expect(el(fixture, 'login-resuming')).toBeNull();
      });

      it('stops waiting when the page goes', async () => {
        const fixture = await render();

        fixture.destroy();
        sign();
        await new Promise((resolve) => setTimeout(resolve));

        expect(hasSession).not.toHaveBeenCalled();
        expect(navigate).not.toHaveBeenCalled();
      });

      it('waits for the answer of what the page offers before it waits for the person', async () => {
        options.value.set(undefined);
        options.isLoading.set(true);
        const fixture = await render();
        sign();
        await settle(fixture);
        expect(hasSession).not.toHaveBeenCalled();

        options.value.set(offered(true, true));
        options.isLoading.set(false);
        await settle(fixture);
        sign();
        await settle(fixture);

        expect(navigate).toHaveBeenCalledExactlyOnceWith(silent('/'));
      });
    });

    describe('when it waits for the button instead', () => {
      async function waitsForTheButton(fixture: ComponentFixture<Login>) {
        sign();
        sign('keydown');
        window.dispatchEvent(new Event('focus'));
        await settle(fixture);
        expect(el(fixture, 'login-resuming')).toBeNull();
        expect(hasSession).not.toHaveBeenCalled();
        expect(navigate).not.toHaveBeenCalled();
      }

      it('waits without the remembered provider: never chosen, a local sign-in since, or a sign-out', async () => {
        localStorage.removeItem('cowork.sign-in');

        await waitsForTheButton(await render());
      });

      it('waits where the installation offers no identity provider', async () => {
        options.value.set(offered(true, false));

        await waitsForTheButton(await render());
      });

      it.each([
        'login_required',
        'oidc_failed',
        'not_allowed',
        'not_initialised',
        'oidc_unavailable',
        'anything',
        '',
      ])(
        'waits after a way back with ?error=%j, so that it never tries twice in a row',
        async (code) => {
          await waitsForTheButton(await render('/t/acme/board', code));
        },
      );

      it('waits in a tab that tried since its last session', async () => {
        sessionStorage.setItem('cowork.sign-in.attempt', '1');

        await waitsForTheButton(await render());
      });

      it('waits where the browser refuses its storage', async () => {
        vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
          throw new DOMException('denied', 'SecurityError');
        });

        await waitsForTheButton(await render());
      });
    });
  });

  describe('a sign-in of its own the provider could not complete, from ?error=login_required', () => {
    beforeEach(() => options.value.set(offered(true, true)));

    it('says calmly that the session at the provider ended, as no refusal', async () => {
      const fixture = await render('/t/acme/board', 'login_required');

      expect(el(fixture, 'login-provider-ended')?.textContent?.trim()).toBe(
        'Your session at Dex has ended. Sign in again.',
      );
      expect(el(fixture, 'login-provider-ended')?.getAttribute('role')).toBe('status');
      expect(el(fixture, 'login-provider-error')).toBeNull();
      expect(host(fixture).innerHTML).not.toContain('login_required');
    });

    it("signs in with the button as ever, the provider's pages and all, back to where the person was", async () => {
      const fixture = await render('/t/acme/board', 'login_required');

      el(fixture, 'login-oidc')?.click();

      expect(navigate).toHaveBeenCalledExactlyOnceWith(
        `/auth/oidc/login?return_to=${encodeURIComponent('/t/acme/board')}`,
      );
    });

    it('stops saying it once the person tries the local form instead', async () => {
      login.mockRejectedValue(refusal(401, 'Unauthenticated'));
      const fixture = await render(undefined, 'login_required');
      fill(fixture);

      submit(fixture);
      await settle(fixture);

      expect(el(fixture, 'login-provider-ended')).toBeNull();
    });
  });
});
