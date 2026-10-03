import { HttpErrorResponse, provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { TestBed } from '@angular/core/testing';
import { provideApiConfiguration } from '../api/api-configuration';
import { Me } from '../api/models';
import { AuthService } from './auth.service';
import { SessionService } from './session.service';

const hans: Me = {
  id: '0199aaaa-0000-7000-8000-000000000001',
  display_name: 'Hans',
  username: 'local:hans',
  global_admin: false,
  local: true,
  password_change_required: false,
  memberships: [{ role: 'admin', tenant: { slug: 'acme', name: 'Acme' } }],
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
  });
});
