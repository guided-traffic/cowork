import { HttpHeaders } from '@angular/common/http';
import { Component, signal, WritableSignal } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideRouter } from '@angular/router';
import { MessageService } from 'primeng/api';
import { Subject } from 'rxjs';
import { Api } from '../../api/api';
import { listMyInbox } from '../../api/functions';
import { Activity, InboxEntry, InboxList, Me } from '../../api/models';
import { EventStreamService, StreamEvent } from '../../core/event-stream.service';
import { InboxService } from '../../core/inbox.service';
import { SessionService } from '../../core/session.service';
import { actor, groupByTicket, happening, Inbox } from './inbox';

/**
 * The Api of the page's specs: `invoke` answers the body, and `invoke$Response`, which the
 * conditional loads use (docs/adr/0054 D7), the same body with no headers.
 */
function apiOf(invoke: ReturnType<typeof vi.fn>) {
  return {
    invoke,
    invoke$Response: async (fn: unknown, params: unknown) => ({
      body: await (invoke as (fn: unknown, params: unknown) => Promise<unknown>)(fn, params),
      headers: new HttpHeaders(),
    }),
  };
}

@Component({ template: '' })
class Page {}

const me: Me = {
  id: 'p1',
  display_name: 'Ada Lovelace',
  username: 'ada',
  global_admin: false,
  local: true,
  password_change_required: false,
  memberships: [],
};

function act(overrides: Partial<Activity> = {}): Activity {
  return {
    id: 'a1',
    at: '2026-10-04T10:00:00Z',
    actor: { id: 'p2', display_name: 'Sam Rivera', username: 'sam' },
    actor_system: null,
    agent: null,
    token: null,
    action: 'commented',
    entity_type: 'comment',
    entity_id: 'c1',
    before: null,
    after: null,
    reason: null,
    note: null,
    explained_by_comment: null,
    redacted: false,
    ...overrides,
  };
}

function entry(id: string, key: string, overrides: Partial<InboxEntry> = {}): InboxEntry {
  const [tenant] = key.split('/');
  return {
    id,
    tenant: { slug: tenant, name: tenant === 'acme' ? 'Acme Corp' : 'Globex' },
    ticket: { key, title: `Title of ${key}`, state: 'analysed' },
    reason: 'commented',
    act: act(),
    blocker: null,
    withdrawn: false,
    read: false,
    created_at: '2026-10-04T10:00:00Z',
    ...overrides,
  };
}

describe('groupByTicket', () => {
  it('groups the entries by ticket, the groups in the order of their newest entry, and counts the unread', () => {
    const groups = groupByTicket([
      entry('n3', 'acme/COW-1'),
      entry('n2', 'globex/OPS-4', { read: true }),
      entry('n1', 'acme/COW-1', { read: true }),
    ]);

    expect(
      groups.map((group) => [group.ticket.key, group.entries.map((e) => e.id), group.unread]),
    ).toEqual([
      ['acme/COW-1', ['n3', 'n1'], 1],
      ['globex/OPS-4', ['n2'], 0],
    ]);
    expect(groups[1].tenant.name).toBe('Globex');
  });
});

describe('happening', () => {
  it.each<[Partial<InboxEntry>, string]>([
    [{ reason: 'assigned' }, 'assigned it to you'],
    [{ reason: 'asked' }, 'asked you a question'],
    [{ reason: 'answered' }, 'answered your question'],
    [{ reason: 'commented' }, 'commented'],
    [{ reason: 'urgent' }, 'registered an urgent need'],
    [{ reason: 'state_changed', act: act({ after: { state: 'review' } }) }, 'moved it to review'],
    [{ reason: 'state_changed', act: act({ redacted: true, after: null }) }, 'changed its state'],
    [
      {
        reason: 'blocker_closed',
        act: act({ after: { state: 'done' } }),
        blocker: { key: 'acme/COW-2', title: 'The blocker', state: 'done' },
      },
      'closed COW-2, which blocks it, as done',
    ],
  ])('says what happened for %j', (overrides, said) => {
    expect(happening(entry('n1', 'acme/COW-1', overrides))).toBe(said);
  });

  it('names the person who acted, or the system actor', () => {
    expect(actor(entry('n1', 'acme/COW-1'))).toBe('Sam Rivera');
    expect(
      actor(
        entry('n1', 'acme/COW-1', {
          act: act({ actor: null, actor_system: 'system:identity-provider' }),
        }),
      ),
    ).toBe('system:identity-provider');
  });
});

describe('Inbox', () => {
  let stream: Subject<StreamEvent>;
  let pages: InboxList[];
  let invoke: ReturnType<typeof vi.fn>;
  let markRead: ReturnType<typeof vi.fn>;
  let markAllRead: ReturnType<typeof vi.fn>;
  let unread: WritableSignal<number>;

  async function render(): Promise<{ fixture: ComponentFixture<Inbox>; page: HTMLElement }> {
    TestBed.configureTestingModule({
      providers: [
        provideRouter([{ path: '**', component: Page }]),
        MessageService,
        { provide: Api, useValue: apiOf(invoke) },
        { provide: SessionService, useValue: { person: signal(me) } },
        { provide: InboxService, useValue: { count: () => unread(), markRead, markAllRead } },
        { provide: EventStreamService, useValue: { events: stream.asObservable() } },
      ],
    });
    const fixture = TestBed.createComponent(Inbox);
    await fixture.whenStable();
    return { fixture, page: fixture.nativeElement as HTMLElement };
  }

  const byTestId = (page: HTMLElement, id: string) => page.querySelector(`[data-testid="${id}"]`);

  beforeEach(() => {
    stream = new Subject<StreamEvent>();
    unread = signal(2);
    pages = [
      {
        items: [
          entry('n3', 'acme/COW-1'),
          entry('n2', 'globex/OPS-4'),
          entry('n1', 'acme/COW-1', { read: true }),
        ],
        next_cursor: null,
        unread: 2,
      },
    ];
    invoke = vi.fn(async (fn: unknown, params: { cursor?: string }) => {
      if (fn !== listMyInbox) {
        throw new Error('unexpected call');
      }
      return params.cursor === 'c1' ? pages[1] : pages[0];
    });
    markRead = vi.fn().mockResolvedValue(undefined);
    markAllRead = vi.fn().mockResolvedValue(undefined);
  });

  it('shows the notifications grouped by ticket, each beside its tenant and linked to its ticket', async () => {
    const { page } = await render();

    const groups = [...page.querySelectorAll('article.group')];
    expect(groups.map((group) => group.getAttribute('data-testid'))).toEqual([
      'group-acme/COW-1',
      'group-globex/OPS-4',
    ]);
    expect(groups[0].querySelector('[data-testid="tenant"]')?.textContent?.trim()).toBe(
      'Acme Corp',
    );
    expect(byTestId(page, 'open-acme/COW-1')?.getAttribute('href')).toBe('/t/acme/tickets/COW-1');
    expect(groups[0].querySelectorAll('li.entry')).toHaveLength(2);
    expect(groups[0].querySelectorAll('li.entry.unread')).toHaveLength(1);
    expect(byTestId(page, 'inbox-unread')?.textContent?.trim()).toBe('2 unread notifications');
    expect(invoke).toHaveBeenCalledWith(listMyInbox, { cursor: undefined, limit: 50 });
  });

  it('marks one notification read and loads the inbox again', async () => {
    const { fixture, page } = await render();
    invoke.mockClear();

    (
      byTestId(page, 'entry-n3')?.querySelector('[data-testid="mark-read"]') as HTMLButtonElement
    ).click();
    await fixture.whenStable();

    expect(markRead).toHaveBeenCalledExactlyOnceWith('n3');
    expect(invoke).toHaveBeenCalledWith(listMyInbox, { cursor: undefined, limit: 50 });
  });

  it('marks every notification up to the newest shown read', async () => {
    const { fixture, page } = await render();

    (byTestId(page, 'mark-all-read') as HTMLButtonElement).click();
    await fixture.whenStable();

    expect(markAllRead).toHaveBeenCalledExactlyOnceWith('n3');
  });

  it('offers no marking of everything while nothing is unread', async () => {
    unread.set(0);

    const { page } = await render();

    expect((byTestId(page, 'mark-all-read') as HTMLButtonElement).disabled).toBe(true);
  });

  it("reads a ticket's unread notifications when it is opened from here", async () => {
    const { fixture, page } = await render();

    (byTestId(page, 'open-acme/COW-1') as HTMLAnchorElement).click();
    await fixture.whenStable();

    expect(markRead).toHaveBeenCalledExactlyOnceWith('n3');
  });

  it('loads again when the person-level stream says the inbox changed, and on a resync', async () => {
    const { fixture } = await render();
    invoke.mockClear();

    stream.next({ name: 'inbox.changed', unread: 3 });
    await fixture.whenStable();
    stream.next({ name: 'resync' });
    await fixture.whenStable();
    stream.next({
      name: 'comment.changed',
      id: 'e',
      key: 'acme/COW-1',
      version: 1,
      kind: 'commented',
    });
    await fixture.whenStable();

    expect(invoke).toHaveBeenCalledTimes(2);
  });

  it('loads one page more on request, asking for every page again', async () => {
    pages[0] = { ...pages[0], next_cursor: 'c1' };
    pages[1] = {
      items: [entry('n0', 'globex/OPS-9', { read: true })],
      next_cursor: null,
      unread: 2,
    };
    const { fixture, page } = await render();

    (byTestId(page, 'load-more') as HTMLButtonElement).click();
    await fixture.whenStable();

    expect(byTestId(page, 'group-globex/OPS-9')).not.toBeNull();
    expect(byTestId(page, 'load-more')).toBeNull();
  });

  it('marks what was withdrawn since', async () => {
    pages[0] = { ...pages[0], items: [entry('n3', 'acme/COW-1', { withdrawn: true })] };

    const { page } = await render();

    expect(byTestId(page, 'withdrawn')).not.toBeNull();
  });

  it('says when the inbox is empty', async () => {
    pages[0] = { items: [], next_cursor: null, unread: 0 };

    const { page } = await render();

    expect(byTestId(page, 'inbox-empty')).not.toBeNull();
  });
});
