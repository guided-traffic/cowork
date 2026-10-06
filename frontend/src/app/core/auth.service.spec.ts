import { HttpErrorResponse, provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { TestBed } from '@angular/core/testing';
import { provideApiConfiguration } from '../api/api-configuration';
import { Me } from '../api/models';
import { AuthService, webAddress } from './auth.service';
import { SessionService } from './session.service';

const hans: Me = {
  id: '0199aaaa-0000-7000-8000-000000000001',
  display_name: 'Hans',
  username: 'local:hans',
  global_admin: false,
  local: true,
  password_change_required: false,
  memberships: [
    {
      role: 'admin',
      tenant: { slug: 'acme', name: 'Acme' },
      origins: [{ source: 'grant', role: 'admin' }],
    },
  ],
};

const unauthenticated = {
  type: 'about:blank',
  title: 'Unauthenticated',
  status: 401,
  code: 'unauthenticated',
};
const unauthorized = { status: 401, statusText: 'Unauthorized' };

describe('AuthService', () => {
  let service: AuthService;
  let session: SessionService;
  let http: HttpTestingController;

  /**
   * Starts the loads that are due, lets the promise chains of answered requests finish, and runs the
   * effects they feed. Fake timers yield the macrotask for that, so this never waits for a request the
   * test has not answered yet, which ApplicationRef.whenStable() would.
   */
  const settle = async () => {
    TestBed.tick();
    await vi.advanceTimersByTimeAsync(0);
    TestBed.tick();
  };
  /** The first tick starts both loads: who is working, and what the login page offers. */
  const start = () => {
    TestBed.tick();
    return { me: http.expectOne('/api/v1/me'), options: http.expectOne('/auth/options') };
  };
  const startSignedOut = async () => {
    const { me, options } = start();
    me.flush(unauthenticated, unauthorized);
    options.flush({ local: true, oidc: false });
    await settle();
  };
  const startSignedIn = async () => {
    const { me, options } = start();
    me.flush(hans);
    options.flush({ local: true, oidc: false });
    await settle();
  };
  /** The request that asks who is working once a login or a logout has changed the session. */
  const meAgain = async () => {
    await settle();
    return http.expectOne('/api/v1/me');
  };

  beforeEach(() => {
    vi.useFakeTimers();
    TestBed.configureTestingModule({
      providers: [provideHttpClient(), provideHttpClientTesting(), provideApiConfiguration('')],
    });
    service = TestBed.inject(AuthService);
    session = TestBed.inject(SessionService);
    http = TestBed.inject(HttpTestingController);
  });

  afterEach(() => {
    try {
      http.verify();
    } finally {
      vi.useRealTimers();
      TestBed.resetTestingModule();
      localStorage.clear();
      sessionStorage.clear();
    }
  });

  describe('options', () => {
    it('asks what the login page offers at /auth/options, outside the API document', async () => {
      const { me, options } = start();

      expect(options.request.method).toBe('GET');
      expect(service.options.status()).toBe('loading');
      me.flush(unauthenticated, unauthorized);
      options.flush({ local: true, oidc: false });
      await settle();

      expect(service.options.status()).toBe('resolved');
      expect(service.options.value()).toEqual({ local: true, oidc: false });
    });

    it('offers whatever the backend says, an identity provider included', async () => {
      const { me, options } = start();
      me.flush(unauthenticated, unauthorized);
      options.flush({ local: false, oidc: true });
      await settle();

      expect(service.options.value()).toEqual({ local: false, oidc: true });
    });

    it('fails with the HTTP error when the backend cannot say', async () => {
      const { me, options } = start();
      me.flush(unauthenticated, unauthorized);
      options.flush('down', { status: 503, statusText: 'Service Unavailable' });
      await settle();

      expect(service.options.status()).toBe('error');
      expect((service.options.error() as HttpErrorResponse).status).toBe(503);
    });
  });

  describe('login', () => {
    beforeEach(startSignedOut);

    it('posts the username and the password to /auth/local', async () => {
      const done = service.login('hans', 's3cret');

      const request = http.expectOne('/auth/local');
      expect(request.request.method).toBe('POST');
      expect(request.request.body).toEqual({ username: 'hans', password: 's3cret' });
      request.flush({ password_change_required: false });
      await done;
    });

    it('hands on whether the password has to be changed', async () => {
      const done = service.login('hans', 'initial');

      http.expectOne('/auth/local').flush({ password_change_required: true });

      expect(await done).toEqual({ password_change_required: true });
    });

    it('does not ask who is working: the login page replaces the document, and the new one asks', async () => {
      const done = service.login('hans', 's3cret');
      http.expectOne('/auth/local').flush({ password_change_required: false });
      await done;
      await settle();

      http.expectNone('/api/v1/me');
      expect(session.person()).toBeUndefined();
    });

    it('rejects with the HTTP error of a wrong password and does not ask who is working', async () => {
      const done = service.login('hans', 'wrong');
      const outcome = done.then(
        () => null,
        (error: unknown) => error,
      );

      http.expectOne('/auth/local').flush(unauthenticated, unauthorized);
      const error = await outcome;
      await settle();

      expect(error).toBeInstanceOf(HttpErrorResponse);
      expect((error as HttpErrorResponse).status).toBe(401);
      expect(session.me.status()).toBe('error');
      http.expectNone('/api/v1/me');
    });

    it('forgets that the person signs in through the identity provider once it succeeds (docs/adr/0029 D6)', async () => {
      localStorage.setItem('cowork.sign-in', 'oidc');
      const done = service.login('hans', 's3cret');

      http.expectOne('/auth/local').flush({ password_change_required: false });
      await done;

      expect(localStorage.getItem('cowork.sign-in')).toBeNull();
    });

    it('remembers the identity provider still when the local sign-in is refused', async () => {
      localStorage.setItem('cowork.sign-in', 'oidc');
      const done = service.login('hans', 'wrong').catch(() => undefined);

      http.expectOne('/auth/local').flush(unauthenticated, unauthorized);
      await done;

      expect(localStorage.getItem('cowork.sign-in')).toBe('oidc');
    });
  });

  describe('hasSession', () => {
    beforeEach(startSignedOut);

    it('asks GET /api/v1/me anew, and says yes to an answer', async () => {
      const done = service.hasSession();

      const request = http.expectOne('/api/v1/me');
      expect(request.request.method).toBe('GET');
      request.flush(hans);

      expect(await done).toBe(true);
    });

    it('says no to a 401', async () => {
      const done = service.hasSession();

      http.expectOne('/api/v1/me').flush(unauthenticated, unauthorized);

      expect(await done).toBe(false);
    });

    it.each([
      ['a 500', { status: 500, statusText: 'Internal Server Error' }],
      ['a 403', { status: 403, statusText: 'Forbidden' }],
      ['no answer', { status: 0, statusText: 'Unknown Error' }],
    ])('rejects with the HTTP error of %s, since nobody can say then', async (_what, status) => {
      const done = service.hasSession();
      const outcome = done.then(
        () => null,
        (error: unknown) => error,
      );

      http.expectOne('/api/v1/me').flush('down', status);

      expect(((await outcome) as HttpErrorResponse).status).toBe(status.status);
    });
  });

  describe('changePassword', () => {
    beforeEach(startSignedIn);

    it('puts the current and the new password to /api/v1/me/password', async () => {
      const done = service.changePassword('old horse', 'correct horse battery');

      const request = http.expectOne('/api/v1/me/password');
      expect(request.request.method).toBe('PUT');
      expect(request.request.body).toEqual({
        current_password: 'old horse',
        new_password: 'correct horse battery',
      });
      request.flush(null, { status: 204, statusText: 'No Content' });
      await done;
      (await meAgain()).flush(hans);
    });

    it('asks who is working again, so that a required change is no longer required', async () => {
      session.me.set({ ...hans, password_change_required: true });
      const done = service.changePassword('initial', 'correct horse battery');
      http.expectOne('/api/v1/me/password').flush(null, { status: 204, statusText: 'No Content' });
      await done;

      (await meAgain()).flush({ ...hans, password_change_required: false });
      await settle();

      expect(session.person()?.password_change_required).toBe(false);
    });

    it('ends the requirement at once, before who is working has answered again', async () => {
      session.me.set({ ...hans, password_change_required: true });
      const done = service.changePassword('initial', 'correct horse battery');
      http.expectOne('/api/v1/me/password').flush(null, { status: 204, statusText: 'No Content' });
      await done;

      // The shell decides where the next page goes now, while /api/v1/me has not answered yet.
      expect(session.person()?.password_change_required).toBe(false);
      (await meAgain()).flush({ ...hans, password_change_required: false });
      await settle();
      expect(session.person()?.password_change_required).toBe(false);
    });

    it('rejects with the HTTP error of a refusal and does not ask who is working', async () => {
      const done = service.changePassword('wrong', 'correct horse battery');
      const outcome = done.then(
        () => null,
        (error: unknown) => error,
      );

      http.expectOne('/api/v1/me/password').flush(
        {
          type: 'about:blank',
          title: 'Invalid request',
          status: 422,
          code: 'validation_failed',
          errors: [{ pointer: '/current_password', message: 'the current password is wrong' }],
        },
        { status: 422, statusText: 'Unprocessable Content' },
      );
      const error = await outcome;
      await settle();

      expect((error as HttpErrorResponse).status).toBe(422);
      http.expectNone('/api/v1/me');
    });
  });

  describe('logout', () => {
    beforeEach(startSignedIn);

    it('posts to /auth/logout without a body', async () => {
      const done = service.logout();

      const request = http.expectOne('/auth/logout');
      expect(request.request.method).toBe('POST');
      expect(request.request.body).toBeNull();
      request.flush(null, { status: 204, statusText: 'No Content' });
      await done;
    });

    it('hands back nothing to go on to after a 204, which leaves the way to the login page', async () => {
      const done = service.logout();

      http.expectOne('/auth/logout').flush(null, { status: 204, statusText: 'No Content' });

      expect(await done).toBeNull();
    });

    it("hands back the identity provider's logout when the backend names one (docs/adr/0031 D4)", async () => {
      const done = service.logout();
      const url =
        'https://login.example.com/oidc/logout?client_id=cowork&post_logout_redirect_uri=https%3A%2F%2Fcowork.example.com%2Flogin';

      http.expectOne('/auth/logout').flush({ end_session_url: url });

      expect(await done).toBe(url);
    });

    it.each([
      ['a script URL', 'javascript:alert(document.cookie)'],
      ['a data URL', 'data:text/html,<script>alert(1)</script>'],
      ['a path, which is no address of a provider', '/login'],
      ['nothing', ''],
    ])('hands back nothing for %s, which the browser would run or misread', async (_what, url) => {
      const done = service.logout();

      http.expectOne('/auth/logout').flush({ end_session_url: url });

      expect(await done).toBeNull();
    });

    it('hands back nothing for an answer without the address', async () => {
      const done = service.logout();

      http.expectOne('/auth/logout').flush({});

      expect(await done).toBeNull();
    });

    it('does not ask who is working: that answer would be a 401 racing the way to the login', async () => {
      const done = service.logout();
      http.expectOne('/auth/logout').flush(null, { status: 204, statusText: 'No Content' });
      await done;
      await settle();

      http.expectNone('/api/v1/me');
    });

    it('rejects with the HTTP error when the backend refuses, and keeps the person', async () => {
      const done = service.logout();
      const outcome = done.then(
        () => null,
        (error: unknown) => error,
      );

      http
        .expectOne('/auth/logout')
        .flush('down', { status: 500, statusText: 'Internal Server Error' });
      const error = await outcome;
      await settle();

      expect((error as HttpErrorResponse).status).toBe(500);
      expect(session.person()?.display_name).toBe('Hans');
      http.expectNone('/api/v1/me');
    });

    it('forgets the identity provider before it asks the backend, so that the sign-out stays one (docs/adr/0029 D6)', async () => {
      localStorage.setItem('cowork.sign-in', 'oidc');

      const done = service.logout();

      expect(localStorage.getItem('cowork.sign-in')).toBeNull();
      http.expectOne('/auth/logout').flush({ end_session_url: 'https://login.example.com/logout' });
      expect(await done).toBe('https://login.example.com/logout');
      expect(localStorage.getItem('cowork.sign-in')).toBeNull();
    });

    it('forgets it also when the backend refuses the sign-out', async () => {
      localStorage.setItem('cowork.sign-in', 'oidc');
      const done = service.logout().catch(() => undefined);

      http
        .expectOne('/auth/logout')
        .flush('down', { status: 500, statusText: 'Internal Server Error' });
      await done;

      expect(localStorage.getItem('cowork.sign-in')).toBeNull();
    });
  });
});

describe('webAddress', () => {
  it.each([
    'https://login.example.com/logout?client_id=cowork',
    'http://localhost:5556/dex/logout',
  ])('lets the web address %s stand as it is', (value) => {
    expect(webAddress(value)).toBe(value);
  });

  it.each<[string | undefined, string]>([
    ['javascript:alert(1)', 'a script URL'],
    ['JavaScript:alert(1)', 'a script URL in mixed case'],
    ['data:text/html,x', 'a data URL'],
    ['vbscript:x', 'another script URL'],
    ['ftp://files.example.com/', 'an address that is no web page'],
    ['/login', 'a path'],
    ['//evil.example/logout', 'a protocol-relative URL'],
    ['not a url', 'text'],
    ['', 'nothing'],
    [undefined, 'undefined'],
  ])('refuses %j, %s', (value) => {
    expect(webAddress(value)).toBeNull();
  });
});
