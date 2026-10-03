import { HttpErrorResponse, HttpInterceptorFn } from '@angular/common/http';
import { inject } from '@angular/core';
import { Router } from '@angular/router';
import { catchError, throwError } from 'rxjs';

/**
 * Every request carries `X-Requested-With: cowork` (docs/adr/0037 D4) — the custom header a
 * cross-site page cannot send without a preflight, which the backend requires on the writes of a
 * cookie session. Nothing else in the frontend needs to know the rule.
 */
export const requestedWith: HttpInterceptorFn = (request, next) =>
  next(request.clone({ setHeaders: { 'X-Requested-With': 'cowork' } }));

/**
 * The page a `401` must not send to the login again. The password page is not one of them: a
 * wrong current password is a field error, so a `401` there means the session is gone, and the
 * login brings the person back to it.
 */
const signInPages = ['/login'];

/**
 * A `401` from the API means no session, or an expired one: the browser goes to the login page
 * and comes back afterwards (docs/adr/0053 D5). The error still reaches the caller.
 */
export const signInOnUnauthorised: HttpInterceptorFn = (request, next) => {
  const router = inject(Router);
  return next(request).pipe(
    catchError((error: unknown) => {
      const here = router.url.split('?')[0];
      if (
        error instanceof HttpErrorResponse &&
        error.status === 401 &&
        request.url.startsWith('/api/') &&
        !signInPages.includes(here)
      ) {
        void router.navigate(['/login'], { queryParams: { return: router.url } });
      }
      return throwError(() => error);
    }),
  );
};
