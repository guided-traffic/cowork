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
import { getMe } from '../api/functions';
import { Me, Membership } from '../api/models';
import { changesMemberships, EventStreamService } from './event-stream.service';
import { RELOAD } from './hard-navigation';
import { keepShown, refresh } from './refresh';

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
 * tenant's stream says so, or may have missed it, so the pages offer what the new role allows
 * without a reload; a load again that fails keeps the person shown ({@link keepShown}).
 *
 * The browser's tabs share one session cookie, and a tab's services keep what its person looked at
 * — a conversation of the assistant among it. So every tab says on {@link SESSION_CHANNEL} whose
 * session it has once it knows, and a sign-out says so: a tab that has shown another person loads
 * again (`RELOAD`), for the reason a sign-in and a sign-out replace their own document. The tab a
 * sign-in ends in says it, whichever way the person came — the identity provider's way back runs
 * no code of the login page.
 */
@Injectable({ providedIn: 'root' })
export class SessionService {
  private readonly api = inject(Api);
  private readonly injector = inject(Injector);
  private readonly channel = inject(SESSION_CHANNEL);
  private readonly reload = inject(RELOAD);
  /** The person this document has shown, whose state its services may hold. */
  private held: string | undefined;

  readonly me: ResourceRef<Me | undefined> = resource({
    loader: () => keepShown(this.me, () => this.api.invoke(getMe)),
  });
  readonly person = computed(() => (this.me.hasValue() ? this.me.value() : undefined));
  private readonly personId = computed(() => this.person()?.id);
  readonly memberships = computed<Membership[]>(() => this.person()?.memberships ?? []);

  /** The tenant of the current page; null on the person-level pages. */
  readonly tenant = signal<string | null>(null);
  readonly membership = computed(() =>
    this.memberships().find((membership) => membership.tenant.slug === this.tenant()),
  );
  /** The only tenant when there is exactly one; it gets no switcher (docs/adr/0023 D4). */
  readonly soleTenant = computed(() => {
    const memberships = this.memberships();
    return memberships.length === 1 ? memberships[0].tenant.slug : null;
  });

  constructor() {
    inject(EventStreamService)
      .events.pipe(takeUntilDestroyed())
      .subscribe((event) => {
        if (changesMemberships(event)) {
          refresh(this.me, this.injector);
        }
      });
    effect(() => {
      const person = this.personId();
      if (person !== undefined) {
        this.held = person;
        this.channel?.postMessage({ person });
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
