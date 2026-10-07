import {
  computed,
  DestroyRef,
  effect,
  inject,
  Injectable,
  InjectionToken,
  Injector,
  resource,
  ResourceRef,
  signal,
} from '@angular/core';
import { takeUntilDestroyed } from '@angular/core/rxjs-interop';
import { Api } from '../api/api';
import { getMe } from '../api/fn/me/get-me';
import { listTenants } from '../api/fn/tenants/list-tenants';
import { Me, Membership, Role, TenantSummary } from '../api/models';
import { changesMemberships, EventStreamService } from './event-stream.service';
import { RELOAD } from './hard-navigation';
import { keepShown, refresh } from './refresh';
import { SignInMemory } from './sign-in-memory';

/** The part of a `BroadcastChannel` the session uses. */
export interface SessionChannel {
  postMessage(message: unknown): void;
  addEventListener(type: 'message', listener: (event: MessageEvent) => void): void;
}

/**
 * The application's tabs in this browser, `cowork.session`; null where the browser has no
 * `BroadcastChannel`, and the other tabs then keep what they hold. Tests provide one of their own.
 */
export const SESSION_CHANNEL = new InjectionToken<SessionChannel | null>('SESSION_CHANNEL', {
  providedIn: 'root',
  factory: () => {
    if (typeof BroadcastChannel !== 'function') {
      return null;
    }
    const channel = new BroadcastChannel('cowork.session');
    inject(DestroyRef).onDestroy(() => channel.close());
    return channel;
  },
});

/** A tenant the person may open, with the role they hold in it; null where they hold none. */
export interface OpenableTenant {
  slug: string;
  name: string;
  role: Role | null;
}

/** Whether another tab says the session is no longer the person's: it signed out, or another signed in. */
function otherSession(data: unknown, person: string): boolean {
  const said = data as { person?: unknown; signedOut?: unknown } | null | undefined;
  return said?.signedOut === true || (typeof said?.person === 'string' && said.person !== person);
}

/**
 * Who is working and in which tenant (docs/adr/0053 D1): the person and their memberships from
 * `GET /api/v1/me`, and the tenant the pages show, taken from the route (docs/adr/0023 D4). The
 * tenant-scoped services follow `tenant()` and drop what they hold when it changes (D4). A role
 * that a grant, a mapping or the person's groups change (docs/adr/0030) is loaded again when the
 * stream says so — an act of the shown tenant, or one that names the person in any of their
 * tenants — or may have missed it, so the pages offer what the new role allows, and the tenant
 * switch a tenant joined, without a reload; a load again that fails keeps the person shown ({@link keepShown}).
 *
 * A global administrator sees every tenant of the installation, and in one where they hold no role
 * its administration only — the members, the group mappings, the settings — until they grant
 * themselves a role (docs/adr/0034 D2): `oversight()` says the pages show such a tenant, and the
 * services of the tenant's work follow `workTenant()`, which is null then. Where they hold a role
 * below `admin`, they raise their own grant (`mayGrantSelf()`).
 *
 * The browser's tabs share one session cookie, and a tab's services keep what its person looked at
 * — a conversation of the assistant among it. So every tab says on {@link SESSION_CHANNEL} whose
 * session it has once it knows, and a sign-out says so: a tab that has shown another person loads
 * again (`RELOAD`), for the reason a sign-in and a sign-out replace their own document. The tab a
 * sign-in ends in says it, whichever way the person came — the identity provider's way back runs
 * no code of the login page. For the same reason it is here that a tab, having a session again, may
 * let its login page sign in by itself once more when that session ends (`SignInMemory`,
 * docs/adr/0029 D6).
 */
@Injectable({ providedIn: 'root' })
export class SessionService {
  private readonly api = inject(Api);
  private readonly injector = inject(Injector);
  private readonly channel = inject(SESSION_CHANNEL);
  private readonly reload = inject(RELOAD);
  private readonly memory = inject(SignInMemory);
  /** The person this document has shown, whose state its services may hold. */
  private held: string | undefined;

  readonly me: ResourceRef<Me | undefined> = resource({
    loader: () => keepShown(this.me, () => this.api.invoke(getMe)),
  });
  readonly person = computed(() => (this.me.hasValue() ? this.me.value() : undefined));
  private readonly personId = computed(() => this.person()?.id);
  readonly memberships = computed<Membership[]>(() => this.person()?.memberships ?? []);

  /**
   * The person's id while they are a global administrator, for whom alone the installation's
   * tenants are asked: anybody else would get a `403`. A primitive, so that the person loaded again
   * leaves the list alone.
   */
  private readonly globalAdmin = computed(() => {
    const person = this.person();
    return person?.global_admin ? person.id : undefined;
  });
  /** Every tenant of the installation, every page of them, for a global administrator. */
  readonly installation: ResourceRef<TenantSummary[] | undefined> = resource({
    params: () => this.globalAdmin(),
    loader: () =>
      keepShown(this.installation, async () => {
        const tenants: TenantSummary[] = [];
        let cursor: string | undefined;
        do {
          const page = await this.api.invoke(listTenants, { cursor, limit: 200 });
          tenants.push(...page.items);
          cursor = page.next_cursor ?? undefined;
        } while (cursor);
        return tenants;
      }),
  });
  /**
   * The tenants the person may open, by slug: their memberships with their roles, and for a global
   * administrator every other tenant of the installation, without a role. The roles are the
   * memberships', which `me` keeps current.
   */
  readonly tenants = computed<OpenableTenant[]>(() => {
    const own: OpenableTenant[] = this.memberships().map(({ tenant, role }) => ({
      slug: tenant.slug,
      name: tenant.name,
      role,
    }));
    const others: OpenableTenant[] = (this.installation.hasValue() ? this.installation.value() : [])
      .filter((tenant) => !own.some((mine) => mine.slug === tenant.slug))
      .map(({ slug, name }) => ({ slug, name, role: null }));
    return [...own, ...others].sort((a, b) => a.slug.localeCompare(b.slug));
  });

  /** The tenant of the current page; null on the person-level pages. */
  readonly tenant = signal<string | null>(null);
  readonly membership = computed(() =>
    this.memberships().find((membership) => membership.tenant.slug === this.tenant()),
  );
  /** The tenant of the current page with its name, whether the person holds a role in it or not. */
  readonly shown = computed(() => this.tenants().find((tenant) => tenant.slug === this.tenant()));
  /**
   * The pages show a tenant in which the person, a global administrator, holds no role: its
   * administration is theirs to see, its work is not (docs/adr/0034 D2). False until `me` answered.
   */
  readonly oversight = computed(
    () =>
      this.tenant() !== null &&
      this.person()?.global_admin === true &&
      this.membership() === undefined,
  );
  /**
   * The tenant of the current page while the person may work in it: what the projects, the event
   * stream and the assistant follow. Null on the person-level pages and under {@link oversight}.
   */
  readonly workTenant = computed(() => (this.oversight() ? null : this.tenant()));
  /**
   * The pages show a tenant in which the person, a global administrator, does not hold `admin`: they
   * may set their own grant there — grant themselves a role under {@link oversight}, raise the one
   * they hold otherwise (docs/adr/0034 D2). False until `me` answered.
   */
  readonly mayGrantSelf = computed(
    () =>
      this.tenant() !== null &&
      this.person()?.global_admin === true &&
      this.membership()?.role !== 'admin',
  );
  /**
   * The only tenant when there is exactly one; it gets no switcher (docs/adr/0023 D4). A global
   * administrator's tenants are known once the installation's list has answered.
   */
  readonly soleTenant = computed(() => {
    if (this.globalAdmin() !== undefined && !this.installation.hasValue()) {
      return null;
    }
    const tenants = this.tenants();
    return tenants.length === 1 ? tenants[0].slug : null;
  });

  constructor() {
    inject(EventStreamService)
      .events.pipe(takeUntilDestroyed())
      .subscribe((event) => {
        // The person's memberships of every tenant are in `me`: an act that names them, in any
        // tenant — one they join or leave included —, and any membership act of the tenant shown.
        const theirs = event.name === 'membership.changed' && event.personId === this.personId();
        if (theirs || changesMemberships(event, this.tenant())) {
          refresh(this.me, this.injector);
        }
      });
    effect(() => {
      const person = this.personId();
      if (person !== undefined) {
        this.held = person;
        this.channel?.postMessage({ person });
        this.memory.clearTried();
      }
    });
    this.channel?.addEventListener('message', (event) => {
      if (this.held !== undefined && otherSession(event.data, this.held)) {
        this.reload();
      }
    });
  }

  /** Tells the application's other tabs that the session ended here: they load again. */
  signedOut(): void {
    this.channel?.postMessage({ signedOut: true });
  }

  enter(tenant: string | null): void {
    if (this.tenant() !== tenant) {
      this.tenant.set(tenant);
    }
  }
}
