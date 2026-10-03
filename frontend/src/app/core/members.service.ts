import { computed, inject, Injectable, resource } from '@angular/core';
import { Api } from '../api/api';
import { listMembers } from '../api/functions';
import { Member } from '../api/models';
import { SessionService } from './session.service';

/** The members of the tenant the pages show, every page of them: for pickers and the member list. */
@Injectable({ providedIn: 'root' })
export class MembersService {
  private readonly api = inject(Api);
  private readonly session = inject(SessionService);

  readonly members = resource({
    params: () => this.session.tenant() ?? undefined,
    loader: async ({ params: tenant }) => {
      const members: Member[] = [];
      let cursor: string | undefined;
      do {
        const page = await this.api.invoke(listMembers, { tenant, cursor, limit: 200 });
        members.push(...page.items);
        cursor = page.next_cursor ?? undefined;
      } while (cursor);
      return members;
    },
  });

  readonly list = computed<Member[]>(() => (this.members.hasValue() ? this.members.value() : []));
}
