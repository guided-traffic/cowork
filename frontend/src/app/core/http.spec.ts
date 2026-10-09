import {
  HttpClient,
  HttpErrorResponse,
  HttpInterceptorFn,
  provideHttpClient,
  withInterceptors,
} from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { Component } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { provideRouter, Router } from '@angular/router';
import { catchError, firstValueFrom, throwError } from 'rxjs';
import { Api } from '../api/api';
import { provideApiConfiguration } from '../api/api-configuration';
import { getMe } from '../api/fn/me/get-me';
import { requestedWith, signInOnUnauthorised } from './http';

describe('requestedWith', () => {
  let client: HttpClient;
  let http: HttpTestingController;

  beforeEach(() => {
    TestBed.configureTestingModule({
      providers: [
        provideHttpClient(withInterceptors([requestedWith])),
        provideHttpClientTesting(),
        provideApiConfiguration(''),
      ],
    });
    client = TestBed.inject(HttpClient);
    http = TestBed.inject(HttpTestingController);
  });

  afterEach(() => {
    try {
      http.verify();
    } finally {
      TestBed.resetTestingModule();
    }
  });

  it.each(['GET', 'POST', 'PUT', 'PATCH', 'DELETE'])(
    'sets X-Requested-With: cowork on a %s request (docs/adr/0037 D4)',
    (method) => {
      client.request(method, '/api/v1/anything').subscribe();

      const request = http.expectOne('/api/v1/anything');

      expect(request.request.method).toBe(method);
      expect(request.request.headers.get('X-Requested-With')).toBe('cowork');
      request.flush(null);
    },
  );

  it('leaves the url, the body and the other headers of the request alone', () => {
    client
      .post('/api/v1/tickets', { title: 'Plain' }, { headers: { 'Idempotency-Key': 'k1' } })
      .subscribe();

    const request = http.expectOne('/api/v1/tickets');

    expect(request.request.body).toEqual({ title: 'Plain' });
    expect(request.request.headers.get('Idempotency-Key')).toBe('k1');
    expect(request.request.headers.get('X-Requested-With')).toBe('cowork');
    request.flush(null);
  });

  it('replaces a different value that the caller set, so the header cannot be spoofed away', () => {
    client.get('/api/v1/anything', { headers: { 'X-Requested-With': 'other' } }).subscribe();

    const request = http.expectOne('/api/v1/anything');

    expect(request.request.headers.getAll('X-Requested-With')).toEqual(['cowork']);
    request.flush(null);
  });

  it('is on the requests of the generated client, which no caller has to know about', async () => {
    const answer = TestBed.inject(Api).invoke(getMe);

    const request = http.expectOne('/api/v1/me');
    expect(request.request.headers.get('X-Requested-With')).toBe('cowork');
    request.flush({ id: 'p1', display_name: 'Hans', memberships: [] });

    expect((await answer).display_name).toBe('Hans');
  });
});

@Component({ template: '' })
class Page {}

describe('signInOnUnauthorised', () => {
  let client: HttpClient;
  let http: HttpTestingController;
  let router: Router;
  let navigate: ReturnType<typeof vi.spyOn>;
  let seenByTheBackend: unknown;

  /** Sits below the interceptor under test and keeps the error exactly as the backend made it. */
  const recorder: HttpInterceptorFn = (request, next) =>
    next(request).pipe(
      catchError((error: unknown) => {
        seenByTheBackend = error;
        return throwError(() => error);
      }),
    );

  /** Sends a GET, answers it with the status, and returns what the caller of the client got. */
  async function answered(url: string, status: number): Promise<unknown> {
    const outcome = firstValueFrom(client.get(url)).then(
      () => 'resolved',
      (error: unknown) => error,
    );
    http.expectOne(url).flush({ detail: 'no' }, { status, statusText: 'no' });
    return outcome;
  }

  beforeEach(() => {
    seenByTheBackend = undefined;
    TestBed.configureTestingModule({
      providers: [
        provideHttpClient(withInterceptors([signInOnUnauthorised, recorder])),
        provideHttpClientTesting(),
        provideRouter([{ path: '**', component: Page }]),
      ],
    });
    client = TestBed.inject(HttpClient);
    http = TestBed.inject(HttpTestingController);
    router = TestBed.inject(Router);
    navigate = vi.spyOn(router, 'navigate').mockResolvedValue(true);
  });

  afterEach(() => {
    try {
      http.verify();
    } finally {
      TestBed.resetTestingModule();
    }
  });

  it('sends a 401 of the API to the login page and names the page to come back to', async () => {
    await router.navigateByUrl('/t/acme/p/VKO/backlog?state=filed&q=a%20b');

    await answered('/api/v1/me', 401);

    expect(navigate).toHaveBeenCalledTimes(1);
    expect(navigate).toHaveBeenCalledWith(['/login'], {
      queryParams: { return: '/t/acme/p/VKO/backlog?state=filed&q=a%20b' },
    });
  });

  it('does so for any API address, with or without a query string', async () => {
    await router.navigateByUrl('/t/acme');

    await answered('/api/v1/tenants/acme/tickets?state=filed', 401);

    expect(navigate).toHaveBeenCalledWith(['/login'], { queryParams: { return: '/t/acme' } });
  });

  it('still hands the 401 on to the caller, as the very error the backend made', async () => {
    const error = await answered('/api/v1/me', 401);

    expect(error).toBeInstanceOf(HttpErrorResponse);
    expect((error as HttpErrorResponse).status).toBe(401);
    expect(error).toBe(seenByTheBackend);
  });

  it('leaves a 401 from outside the API alone, as the login flows of /auth/ answer it themselves', async () => {
    await router.navigateByUrl('/t/acme');

    const error = await answered('/auth/local', 401);

    expect(navigate).not.toHaveBeenCalled();
    expect((error as HttpErrorResponse).status).toBe(401);
  });

  it('does not take a path that merely starts like the API for the API', async () => {
    await answered('/apidocs/index.html', 401);

    expect(navigate).not.toHaveBeenCalled();
  });

  it('sends the browser from the password page to the login, which brings it back', async () => {
    await router.navigateByUrl('/password');

    await answered('/api/v1/me/password', 401);

    expect(navigate).toHaveBeenCalledWith(['/login'], { queryParams: { return: '/password' } });
  });

  it.each(['/login', '/login?return=%2Ft%2Facme'])(
    'does not send the browser to the login page again while it is on %s',
    async (page) => {
      await router.navigateByUrl(page);

      const error = await answered('/api/v1/me', 401);

      expect(navigate).not.toHaveBeenCalled();
      expect((error as HttpErrorResponse).status).toBe(401);
      expect(error).toBe(seenByTheBackend);
    },
  );

  it.each([400, 403, 404, 409, 412, 500, 503])(
    'does not sign out on a %i, and the caller still gets the error',
    async (status) => {
      const error = await answered('/api/v1/me', status);

      expect(navigate).not.toHaveBeenCalled();
      expect((error as HttpErrorResponse).status).toBe(status);
      expect(error).toBe(seenByTheBackend);
    },
  );

  it('does not sign out when the backend cannot be reached', async () => {
    const outcome = firstValueFrom(client.get('/api/v1/me')).catch((error: unknown) => error);
    http.expectOne('/api/v1/me').error(new ProgressEvent('error'));

    const error = await outcome;

    expect(navigate).not.toHaveBeenCalled();
    expect((error as HttpErrorResponse).status).toBe(0);
  });

  it('lets a successful answer through untouched', async () => {
    const body = { id: 'p1' };
    const outcome = firstValueFrom(client.get('/api/v1/me'));
    http.expectOne('/api/v1/me').flush(body);

    expect(await outcome).toEqual(body);
    expect(navigate).not.toHaveBeenCalled();
  });
});

describe('signInOnUnauthorised below an interceptor that fails with something else', () => {
  it('rethrows an error that is not an HTTP error response without a thought of the login', async () => {
    const failing: HttpInterceptorFn = () => throwError(() => new Error('interceptor broke'));
    TestBed.configureTestingModule({
      providers: [
        provideHttpClient(withInterceptors([signInOnUnauthorised, failing])),
        provideHttpClientTesting(),
        provideRouter([{ path: '**', component: Page }]),
      ],
    });
    const navigate = vi.spyOn(TestBed.inject(Router), 'navigate').mockResolvedValue(true);

    await expect(firstValueFrom(TestBed.inject(HttpClient).get('/api/v1/me'))).rejects.toThrow(
      'interceptor broke',
    );

    expect(navigate).not.toHaveBeenCalled();
  });
});
