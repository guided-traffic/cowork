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
      // The person-level pages, across the person's tenants (docs/adr/0018 D3).
      {
        path: 'me/next',
        loadComponent: () => import('./features/me/my-tickets').then((m) => m.MyTickets),
        data: { list: 'next' },
      },
      {
        path: 'me/inbox',
        loadComponent: () => import('./features/me/inbox').then((m) => m.Inbox),
      },
      {
        path: 'me/assigned',
        loadComponent: () => import('./features/me/my-tickets').then((m) => m.MyTickets),
        data: { list: 'assigned' },
      },
      {
        path: 'me/decisions',
        loadComponent: () => import('./features/me/decisions').then((m) => m.Decisions),
      },
      // The search of every tenant of the person (docs/adr/0023 D2, docs/adr/0025).
      {
        path: 'me/search',
        data: { scope: 'me' },
        loadComponent: () => import('./features/search/search').then((m) => m.SearchResults),
      },
      {
        path: 't/:tenant',
        component: TenantScope,
        children: [
          // The tenant's front page is its dashboard (docs/adr/0018 D6).
          {
            path: '',
            pathMatch: 'full',
            loadComponent: () =>
              import('./features/tenant/dashboard').then((m) => m.TenantDashboard),
          },
          // The tenant's board, a swimlane per project (docs/adr/0018 D4).
          {
            path: 'board',
            loadComponent: () =>
              import('./features/tenant/tenant-board').then((m) => m.TenantBoard),
          },
          // The tenant's tickets across its projects, mirroring GET …/tickets (docs/adr/0023 D4,
          // docs/adr/0018 D5); a ticket of it is tickets/:key below.
          {
            path: 'tickets',
            loadComponent: () =>
              import('./features/tenant/tenant-tickets').then((m) => m.TenantTickets),
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
          // The tenant's search, what the search box does inside a tenant (docs/adr/0018 D7).
          {
            path: 'search',
            data: { scope: 'tenant' },
            loadComponent: () => import('./features/search/search').then((m) => m.SearchResults),
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
            path: 'audit',
            loadComponent: () => import('./features/tenant/audit').then((m) => m.Audit),
          },
          {
            path: 'tokens',
            loadComponent: () =>
              import('./features/tenant/tenant-tokens').then((m) => m.TenantTokens),
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
          // The tenant's bin, mirroring GET …/deleted-tickets (docs/adr/0024 D1, docs/adr/0023 D4).
          {
            path: 'deleted-tickets',
            loadComponent: () =>
              import('./features/tenant/deleted-tickets').then((m) => m.DeletedTickets),
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
