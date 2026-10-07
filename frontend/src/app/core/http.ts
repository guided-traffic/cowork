import { HttpContextToken, HttpErrorResponse, HttpInterceptorFn } from '@angular/common/http';
import { inject } from '@angular/core';
import { Router } from '@angular/router';
import { catchError, throwError } from 'rxjs';

/**
 * The custom header of docs/adr/0037 D4, which a cross-site page cannot send without a preflight
 * and which the backend requires on the writes of a cookie session. A request that does not go
 * through the HttpClient — the chat's `fetch` — sets it from here.
 */
export const requestedWithHeader = { 'X-Requested-With': 'cowork' } as const;

/**
 * Every request carries `X-Requested-With: cowork` (docs/adr/0037 D4). Nothing else in the
 * frontend needs to know the rule.
 */
export const requestedWith: HttpInterceptorFn = (request, next) =>
  next(request.clone({ setHeaders: requestedWithHeader }));

/**
 * The header that makes a read the person's own activity, which alone — besides a write — moves the
 * session's idle clock (docs/adr/0031 D3). A read without it moves nothing, so a tab that is only
 * open — its event stream, the reloads an event triggers, the polling fallback — reaches the idle
 * limit. Only the keep-alive's request carries it, after the person's input (`KeepAliveService`).
 */
export const activityHeader = { 'X-Cowork-Activity': 'input' } as const;

/** Marks a request as the person's activity; {@link personActivity} turns it into the header. */
export const PERSON_ACTIVITY = new HttpContextToken<boolean>(() => false);

/** Sets {@link activityHeader} on a request that carries {@link PERSON_ACTIVITY}, and on no other. */
export const personActivity: HttpInterceptorFn = (request, next) =>
  next(
    request.context.get(PERSON_ACTIVITY) ? request.clone({ setHeaders: activityHeader }) : request,
  );

/**
 * The page a `401` must not send to the login again. The password page is not one of them: a
 * wrong current password is a field error, so a `401` there means the session is gone, and the
 * login brings the person back to it.
 */
const signInPages = ['/login'];

/**
 * Sends the browser to the login page, which comes back to where it was — unless it is on the
 * login page already. What a `401` of the API means: no session, or an expired one
 * (docs/adr/0053 D5).
 */
export function toSignIn(router: Router): void {
  const here = router.url.split('?')[0];
  if (!signInPages.includes(here)) {
    void router.navigate(['/login'], { queryParams: { return: router.url } });
  }
}

/** A `401` from the API goes to the login page ({@link toSignIn}); the error still reaches the caller. */
export const signInOnUnauthorised: HttpInterceptorFn = (request, next) => {
  const router = inject(Router);
  return next(request).pipe(
    catchError((error: unknown) => {
      if (
        error instanceof HttpErrorResponse &&
        error.status === 401 &&
        request.url.startsWith('/api/')
      ) {
        toSignIn(router);
      }
      return throwError(() => error);
    }),
  );
};
