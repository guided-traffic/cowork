import {
  ApplicationConfig,
  inject,
  provideAppInitializer,
  provideBrowserGlobalErrorListeners,
} from '@angular/core';
import { provideHttpClient, withFetch, withInterceptors } from '@angular/common/http';
import { provideRouter, withComponentInputBinding } from '@angular/router';
import { MessageService } from 'primeng/api';
import { provideApiConfiguration } from './api/api-configuration';
import { routes } from './app.routes';
import { requestedWith, signInOnUnauthorised } from './core/http';
import { provideCoworkPrimeNG } from './theme/primeng';
import { ThemeService } from './theme/theme.service';

export const appConfig: ApplicationConfig = {
  providers: [
    provideBrowserGlobalErrorListeners(),
    provideRouter(routes, withComponentInputBinding()),
    provideHttpClient(withFetch(), withInterceptors([requestedWith, signInOnUnauthorised])),
    // The generated paths start with /api/v1; the root URL must not add a second slash.
    provideApiConfiguration(''),
    provideCoworkPrimeNG(),
    MessageService,
    // The scheme is applied before the first render, so the page does not flash light.
    provideAppInitializer(() => {
      inject(ThemeService);
    }),
  ],
};
