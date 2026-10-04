import { Routes } from '@angular/router';
import { devRoutes } from './dev/dev.routes';
import { Shell } from './layout/shell';
import { TenantScope } from './layout/tenant-scope';

/** The UI mirrors the API (docs/adr/0023 D4); the login page stands outside the shell. */
export const routes: Routes = [
  {
    path: 'login',
    loadComponent: () => import('./features/auth/login').then((m) => m.Login),
  },
  {
    path: 'password',
    loadComponent: () => import('./features/auth/change-password').then((m) => m.ChangePassword),
  },
  {
    path: '',
    component: Shell,
    children: [
      {
        path: '',
        pathMatch: 'full',
        loadComponent: () => import('./features/home/home').then((m) => m.Home),
      },
      {
        path: 'me/tokens',
        loadComponent: () => import('./features/me/tokens').then((m) => m.Tokens),
      },
      {
        path: 't/:tenant',
        component: TenantScope,
        children: [
          {
            path: '',
            pathMatch: 'full',
            loadComponent: () => import('./features/tenant/overview').then((m) => m.TenantOverview),
          },
          // A project's address without a view opens its board (docs/adr/0018 D1).
          { path: 'p/:project', pathMatch: 'full', redirectTo: 'p/:project/board' },
          {
            path: 'p/:project/backlog',
            loadComponent: () => import('./features/project/backlog').then((m) => m.Backlog),
          },
          {
            path: 'p/:project/board',
            loadComponent: () => import('./features/project/board').then((m) => m.Board),
          },
          {
            path: 'p/:project/settings',
            loadComponent: () =>
              import('./features/project/project-settings').then((m) => m.ProjectSettings),
          },
          {
            path: 'settings',
            loadComponent: () =>
              import('./features/tenant/tenant-settings').then((m) => m.TenantSettings),
          },
          {
            path: 'tickets/:key',
            loadComponent: () =>
              import('./features/ticket/ticket-detail').then((m) => m.TicketDetail),
          },
          {
            path: 'members',
            loadComponent: () => import('./features/tenant/members').then((m) => m.Members),
          },
          {
            path: 'accounts',
            loadComponent: () => import('./features/tenant/accounts').then((m) => m.Accounts),
          },
          {
            path: 'group-mappings',
            loadComponent: () =>
              import('./features/tenant/group-mappings').then((m) => m.GroupMappings),
          },
          {
            path: 'time',
            loadComponent: () => import('./features/time/time-report').then((m) => m.TimeReport),
          },
        ],
      },
      ...devRoutes,
      {
        path: '**',
        loadComponent: () => import('./features/home/not-found').then((m) => m.NotFound),
      },
    ],
  },
];
