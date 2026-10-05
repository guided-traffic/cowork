import { provideLocationMocks } from '@angular/common/testing';
import { reflectComponentType, Type } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import {
  ActivatedRouteSnapshot,
  provideRouter,
  Route,
  Router,
  withComponentInputBinding,
} from '@angular/router';
import { routes } from './app.routes';
import { DesignPreview } from './dev/design-preview';
import { ChangePassword } from './features/auth/change-password';
import { Login } from './features/auth/login';
import { Home } from './features/home/home';
import { NotFound } from './features/home/not-found';
import { Decisions } from './features/me/decisions';
import { Inbox } from './features/me/inbox';
import { MyTickets } from './features/me/my-tickets';
import { Tokens } from './features/me/tokens';
import { Backlog } from './features/project/backlog';
import { Board } from './features/project/board';
import { ProjectSettings } from './features/project/project-settings';
import { SearchResults } from './features/search/search';
import { Accounts } from './features/tenant/accounts';
import { Audit } from './features/tenant/audit';
import { DeletedTickets } from './features/tenant/deleted-tickets';
import { GroupMappings } from './features/tenant/group-mappings';
import { Members } from './features/tenant/members';
import { TenantOverview } from './features/tenant/overview';
import { TenantBoard } from './features/tenant/tenant-board';
import { TenantSettings } from './features/tenant/tenant-settings';
import { TicketDetail } from './features/ticket/ticket-detail';
import { TimeReport } from './features/time/time-report';
import { Shell } from './layout/shell';
import { TenantScope } from './layout/tenant-scope';

interface Placed {
  /** The path from the root, such as `t/:tenant/members`. */
  path: string;
  route: Route;
}

function place(tree: Route[], parent = ''): Placed[] {
  return tree.flatMap((route) => {
    const path = [parent, route.path].filter(Boolean).join('/');
    return [{ path, route }, ...place(route.children ?? [], path)];
  });
}

/** Every route that loads its page on demand, with the page it has to load. */
const pages: [string, Type<unknown>][] = [
  ['login', Login],
  ['password', ChangePassword],
  ['', Home],
  ['me/tokens', Tokens],
  ['me/next', MyTickets],
  ['me/inbox', Inbox],
  ['me/assigned', MyTickets],
  ['me/decisions', Decisions],
  ['me/search', SearchResults],
  ['t/:tenant', TenantOverview],
  ['t/:tenant/board', TenantBoard],
  ['t/:tenant/p/:project/backlog', Backlog],
  ['t/:tenant/p/:project/board', Board],
  ['t/:tenant/p/:project/settings', ProjectSettings],
  ['t/:tenant/settings', TenantSettings],
  ['t/:tenant/tickets/:key', TicketDetail],
  ['t/:tenant/search', SearchResults],
  ['t/:tenant/members', Members],
  ['t/:tenant/accounts', Accounts],
  ['t/:tenant/audit', Audit],
  ['t/:tenant/group-mappings', GroupMappings],
  ['t/:tenant/time', TimeReport],
  ['t/:tenant/deleted-tickets', DeletedTickets],
  ['dev/design', DesignPreview],
  ['**', NotFound],
];

describe('the routes', () => {
  const placed = place(routes);

  describe('the table', () => {
    it('has a lazy route for each page and for no other', () => {
      const lazy = placed.filter(({ route }) => route.loadComponent).map(({ path }) => path);

      expect(lazy.sort()).toEqual(pages.map(([path]) => path).sort());
    });

    it.each(pages)('loads the page of %j', async (path, page) => {
      const route = placed.find((each) => each.path === path && each.route.loadComponent)?.route;

      expect(await route?.loadComponent?.()).toBe(page);
    });

    it('frames every page in the shell, except the login page', () => {
      const framed = routes.filter((route) => route.component === Shell);
      const unframed = routes.filter((route) => route.component !== Shell);

      expect(framed).toHaveLength(1);
      expect(framed[0].path).toBe('');
      expect(unframed.map((route) => route.path)).toEqual(['login', 'password']);
    });

    it('puts the pages of a tenant under the tenant scope', () => {
      const scope = placed.find(({ route }) => route.component === TenantScope);

      expect(scope?.path).toBe('t/:tenant');
      expect(scope?.route.children?.length).toBeGreaterThan(0);
      // Every child is a page that loads on demand, or the redirect of a project to its board.
      expect(scope?.route.children?.every((child) => child.loadComponent || child.redirectTo)).toBe(
        true,
      );
    });

    it('puts the pages of the person, which name no tenant, in the shell beside the tenant scope', () => {
      const shell = routes.find((route) => route.component === Shell);
      const scope = placed.find(({ route }) => route.component === TenantScope);

      expect(
        shell?.children?.find((child) => child.path === 'me/tokens')?.loadComponent,
      ).toBeDefined();
      expect(scope?.route.children?.some((child) => child.path?.startsWith('me/'))).toBe(false);
    });

    it('answers a path that matches nothing with the not-found page, as the last route of the shell', () => {
      const shell = routes.find((route) => route.component === Shell);

      expect(shell?.children?.at(-1)?.path).toBe('**');
    });
  });

  describe('the URLs of the UI (docs/adr/0023 D4)', () => {
    /** Navigates the real router over the real table and returns the matched routes, outermost first. */
    async function navigate(url: string): Promise<ActivatedRouteSnapshot[]> {
      TestBed.configureTestingModule({
        providers: [provideRouter(routes, withComponentInputBinding()), provideLocationMocks()],
      });
      const router = TestBed.inject(Router);
      await router.navigateByUrl(url);
      const matched: ActivatedRouteSnapshot[] = [];
      for (let step = router.routerState.snapshot.root.firstChild; step; step = step.firstChild) {
        matched.push(step);
      }
      return matched;
    }

    const named = (url: string, ...components: Type<unknown>[]) =>
      [
        url,
        components.map((component) => reflectComponentType(component)?.selector).join(' > '),
        components,
      ] as const;

    it.each([
      named('/login', Login),
      named('/', Shell, Home),
      named('/me/tokens', Shell, Tokens),
      named('/me/next', Shell, MyTickets),
      named('/me/assigned', Shell, MyTickets),
      named('/me/search?q=gate', Shell, SearchResults),
      named('/t/acme/search?q=gate', Shell, TenantScope, SearchResults),
      named('/t/acme', Shell, TenantScope, TenantOverview),
      named('/t/acme/board', Shell, TenantScope, TenantBoard),
      named('/t/acme/members', Shell, TenantScope, Members),
      named('/t/acme/accounts', Shell, TenantScope, Accounts),
      named('/t/acme/group-mappings', Shell, TenantScope, GroupMappings),
      named('/t/acme/time', Shell, TenantScope, TimeReport),
      named('/t/acme/settings', Shell, TenantScope, TenantSettings),
      named('/t/acme/p/COW/backlog', Shell, TenantScope, Backlog),
      named('/t/acme/p/COW/board', Shell, TenantScope, Board),
      named('/t/acme/p/COW/settings', Shell, TenantScope, ProjectSettings),
      named('/t/acme/tickets/COW-12', Shell, TenantScope, TicketDetail),
      named('/dev/design', Shell, DesignPreview),
      named('/t', Shell, NotFound),
      named('/t/acme/p/COW', Shell, TenantScope, Board),
      named('/t/acme/unknown', Shell, NotFound),
      named('/nothing/here', Shell, NotFound),
    ])('takes %s to %s', async (url, _, components) => {
      const matched = await navigate(url);

      expect(matched.map((step) => step.component)).toEqual(components);
    });

    it.each([
      ['/t/acme/p/COW', '/t/acme/p/COW/board'],
      ['/t/acme/p/OPS', '/t/acme/p/OPS/board'],
      ['/t/globex/p/COW?q=flicker', '/t/globex/p/COW/board?q=flicker'],
    ])('sends the project without a view, %s, on to its board, %s', async (url, target) => {
      TestBed.configureTestingModule({
        providers: [provideRouter(routes, withComponentInputBinding()), provideLocationMocks()],
      });
      const router = TestBed.inject(Router);

      await router.navigateByUrl(url);

      expect(router.url).toBe(target);
    });

    it.each([
      ['/t/acme', 1, { tenant: 'acme' }],
      ['/t/acme/p/COW/backlog', 2, { project: 'COW' }],
      ['/t/acme/p/COW/board', 2, { project: 'COW' }],
      ['/t/acme/p/COW/settings', 2, { project: 'COW' }],
      ['/t/acme/tickets/COW-12', 2, { key: 'COW-12' }],
    ] as [string, number, Record<string, string>][])(
      'reads the parameters of %s',
      async (url, depth, params) => {
        const matched = await navigate(url);

        expect(matched[depth].params).toMatchObject(params);
      },
    );

    it.each([
      '/t/acme',
      '/t/acme/p/COW/backlog',
      '/t/acme/p/COW/board',
      '/t/acme/p/COW/settings',
      '/t/acme/tickets/COW-12',
    ])('has an input for each parameter of the path of every page of %s', async (url) => {
      for (const step of await navigate(url)) {
        const own = [...(step.routeConfig?.path ?? '').matchAll(/:(\w+)/g)].map(
          (param) => param[1],
        );
        const inputs = reflectComponentType(step.component as Type<unknown>)?.inputs.map(
          (input) => input.templateName,
        );

        expect(inputs, `the page of ${step.routeConfig?.path}`).toEqual(
          expect.arrayContaining(own),
        );
      }
    });

    it('hands the way back, which the login page takes from ?return=, to an input of the page', async () => {
      const [login] = await navigate('/login?return=%2Ft%2Facme%2Ftickets%2FCOW-12');

      expect(login.queryParams['return']).toBe('/t/acme/tickets/COW-12');
      const inputs = reflectComponentType(Login)?.inputs.map((input) => input.templateName);
      expect(inputs).toContain('return');
    });

    it('hands the project filter of the tenant board, ?project= repeated, to an input of the page', async () => {
      const [, , board] = await navigate('/t/acme/board?project=COW&project=OPS');

      expect(board.component).toBe(TenantBoard);
      expect(board.queryParams['project']).toEqual(['COW', 'OPS']);
      const inputs = reflectComponentType(TenantBoard)?.inputs.map((input) => input.templateName);
      expect(inputs).toContain('project');
    });

    it("hands why the identity provider's way back failed, ?error=, to an input of the login page", async () => {
      const [login] = await navigate('/login?error=not_allowed');

      expect(login.queryParams['error']).toBe('not_allowed');
      const inputs = reflectComponentType(Login)?.inputs.map((input) => input.templateName);
      expect(inputs).toContain('error');
    });

    it.each([
      ['/me/next', 'next'],
      ['/me/assigned', 'assigned'],
    ])('names the list of %s in the data the page takes as an input', async (url, list) => {
      const [, page] = await navigate(url);

      expect(page.data['list']).toBe(list);
      const inputs = reflectComponentType(MyTickets)?.inputs.map((input) => input.templateName);
      expect(inputs).toContain('list');
    });

    it.each([
      ['/t/acme/p/COW/backlog', '/t/acme/p/OPS/backlog'],
      ['/t/acme/p/COW/board', '/t/acme/p/OPS/board'],
      ['/t/acme/p/COW/settings', '/t/acme/p/OPS/settings'],
      ['/t/acme/tickets/COW-12', '/t/acme/tickets/COW-13'],
      ['/t/acme/board', '/t/globex/board'],
      ['/t/acme/board', '/t/acme/board?project=COW'],
      ['/t/acme/members', '/t/globex/members'],
      ['/t/acme/accounts', '/t/globex/accounts'],
      ['/t/acme/group-mappings', '/t/globex/group-mappings'],
    ])('keeps the page when only a parameter changes from %s to %s', async (from, to) => {
      TestBed.configureTestingModule({
        providers: [provideRouter(routes, withComponentInputBinding()), provideLocationMocks()],
      });
      const router = TestBed.inject(Router);
      const page = () => {
        let route = router.routerState.root;
        while (route.firstChild) {
          route = route.firstChild;
        }
        return route;
      };
      await router.navigateByUrl(from);
      const before = page();

      await router.navigateByUrl(to);

      // The page is not created again: what it holds outlives the parameter, so a page that has
      // state of its own has to follow its inputs.
      expect(page()).toBe(before);
    });
  });
});
