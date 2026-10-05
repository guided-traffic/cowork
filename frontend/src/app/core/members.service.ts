import { computed, inject, Injectable, Injector, resource, ResourceRef } from '@angular/core';
import { takeUntilDestroyed } from '@angular/core/rxjs-interop';
import { Api } from '../api/api';
import { addMember, listMembers, removeMemberGrant, setMemberGrant } from '../api/functions';
import { Member, MemberList, Role } from '../api/models';
import { ConditionalPages } from './conditional';
import { changesMemberships, EventStreamService } from './event-stream.service';
import { keepShown, refresh } from './refresh';
import { SessionService } from './session.service';
import { PerPage } from './table-pages';

/**
 * The members of the tenant the pages show, every page of them: for pickers and the member list
 * (docs/adr/0034 D7), each with the effective role and where it comes from. The administrators'
 * acts on the grants (docs/adr/0030 D3) are here too. `membership.changed` loads them again, and
 * so do a resync and the fallback's poll, which may have missed one (docs/adr/0054); a load again
 * that fails keeps the list shown ({@link keepShown}).
 */
@Injectable({ providedIn: 'root' })
export class MembersService {
  private readonly api = inject(Api);
  private readonly session = inject(SessionService);
  private readonly injector = inject(Injector);
  private readonly pages = new ConditionalPages(this.api);
  /** The weak `ETag`s of the numbered pages the member list shows, apart from the pickers'. */
  private readonly numbered = new ConditionalPages(this.api);

  readonly members: ResourceRef<Member[] | undefined> = resource({
    params: () => this.session.tenant() ?? undefined,
    loader: ({ params: tenant }) =>
      keepShown(this.members, () =>
        this.pages.load(async (page) => {
          const members: Member[] = [];
          let cursor: string | undefined;
          do {
            const next = await page(listMembers, { tenant, cursor, limit: 200 });
            members.push(...next.items);
            cursor = next.next_cursor ?? undefined;
          } while (cursor);
          return members;
        }),
      ),
  });

  readonly list = computed<Member[]>(() => (this.members.hasValue() ? this.members.value() : []));

  constructor() {
    inject(EventStreamService)
      .events.pipe(takeUntilDestroyed())
      .subscribe((event) => {
        if (changesMemberships(event, this.session.tenant())) {
          refresh(this.members, this.injector);
        }
      });
  }

  /**
   * One numbered page of the tenant's members with the total (docs/adr/0048 D2, D4), for the member
   * list; the pickers keep reading every member. A load again that finds the page unchanged is a
   * `304` (docs/adr/0054 D7).
   */
  page(tenant: string, page: number, perPage: PerPage): Promise<MemberList> {
    return this.numbered.load((fetch) => fetch(listMembers, { tenant, page, per_page: perPage }));
  }

  /**
   * Grants a role to a person who is not a member yet, by the e-mail address the identity
   * provider gave or a local account's username (docs/adr/0030 D3). The key is the form's, one
   * for each content it holds, so a retry of a lost answer is answered again instead of being
   * refused as a grant that exists (docs/adr/0045).
   */
  async add(person: string, role: Role, idempotencyKey: string): Promise<Member> {
    const tenant = this.session.tenant() as string;
    const member = await this.api.invoke(addMember, {
      tenant,
      'Idempotency-Key': idempotencyKey,
      body: { person, role },
    });
    this.changed(tenant, member);
    return member;
  }

  /** Gives a member a grant with the role, or changes the role of theirs; a mapped role stays. */
  async setGrant(personId: string, role: Role): Promise<Member> {
    const tenant = this.session.tenant() as string;
    const member = await this.api.invoke(setMemberGrant, {
      tenant,
      person_id: personId,
      body: { role },
    });
    this.changed(tenant, member);
    return member;
  }

  /** Removes a member's grant: a mapped membership stays, and without one the person leaves the tenant. */
  async removeGrant(personId: string): Promise<void> {
    await this.api.invoke(removeMemberGrant, {
      tenant: this.session.tenant() as string,
      person_id: personId,
    });
    refresh(this.members, this.injector);
    this.reloadOwn(personId);
  }

  /**
   * Puts the member as the answer has them into the list at once — a select that shows the new
   * grant must not jump back while the list loads — and loads the list again. An answer that
   * arrives after the pages turned to another tenant is not put into that tenant's list.
   */
  private changed(tenant: string, member: Member): void {
    if (this.session.tenant() === tenant && this.members.hasValue()) {
      const held = this.members.value();
      const at = held.findIndex((each) => each.person.id === member.person.id);
      this.members.set(
        at === -1 ? [...held, member] : held.map((each, index) => (index === at ? member : each)),
      );
    }
    refresh(this.members, this.injector);
    this.reloadOwn(member.person.id);
  }

  /** The person's own role changed: what the pages offer follows `GET /api/v1/me`. */
  private reloadOwn(personId: string): void {
    if (personId === this.session.person()?.id) {
      refresh(this.session.me, this.injector);
    }
  }
}
