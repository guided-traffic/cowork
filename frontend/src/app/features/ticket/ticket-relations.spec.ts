import { provideHttpClient } from '@angular/common/http';
import {
  HttpTestingController,
  provideHttpClientTesting,
  TestRequest,
} from '@angular/common/http/testing';
import { ApplicationRef, ResourceRef } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { Subject } from 'rxjs';
import { provideApiConfiguration } from '../../api/api-configuration';
import { Relation, TicketHead } from '../../api/models';
import { EventStreamService, StreamEvent, TicketEventName } from '../../core/event-stream.service';
import { address, childPageSize, TicketAddress, TicketRelations } from './ticket-relations';

describe('address', () => {
  it.each([
    ['COW-12', 'COW', 12],
    ['VKO-1', 'VKO', 1],
    ['AB-100', 'AB', 100],
    ['A1-7', 'A1', 7],
    ['ABCDEFGHIJ-5', 'ABCDEFGHIJ', 5],
  ])('reads %s as the project %s and the number %i', (key, project, number) => {
    expect(address('acme', key)).toEqual({ team: 'acme', project, number });
  });

  it.each([
    ['cow-12', 'a lower-case project key'],
    ['COW-0', 'the number zero'],
    ['COW-012', 'a number with a leading zero'],
    ['COW', 'a key without a number'],
    ['COW-', 'a key with an empty number'],
    ['COW-x', 'a number that is not a number'],
    ['COW-1.5', 'a fractional number'],
    ['C-1', 'a project key of one character'],
    ['ABCDEFGHIJK-1', 'a project key of eleven characters'],
    ['1COW-2', 'a project key that starts with a digit'],
    ['COW-12-3', 'a second number'],
    [' COW-12', 'leading whitespace'],
    ['COW-12 ', 'trailing whitespace'],
    ['acme/COW-12', 'the canonical key, which the path never carries'],
    ['', 'an empty key'],
  ])('rejects %j: %s', (key) => {
    expect(address('acme', key)).toBeUndefined();
  });

  it('rejects a key outside a tenant', () => {
    expect(address(null, 'COW-12')).toBeUndefined();
    expect(address('', 'COW-12')).toBeUndefined();
  });
});

describe('TicketRelations', () => {
  const cow12: TicketAddress = { team: 'acme', project: 'COW', number: 12 };
  const base = '/api/v1/teams/acme/projects/COW/tickets/12';
  const urls = {
    comments: `${base}/comments?limit=200`,
    activity: `${base}/activity?order=desc&limit=100`,
    questions: `${base}/questions?limit=200`,
    links: `${base}/relations?kind=link&limit=200`,
    children: `${base}/relations?kind=child&limit=${childPageSize}`,
    interest: `${base}/interest?limit=200`,
    attachments: `${base}/attachments?limit=200`,
    time: `${base}/time-entries?limit=200`,
    tree: `${base}/prerequisite-tree?direction=down&limit=200`,
  };
  const empty = { items: [], next_cursor: null };

  /** The head of a ticket at the other end of a relation, which the reader reads. */
  function head(key: string, overrides: Partial<TicketHead> = {}): TicketHead {
    return {
      team: { slug: key.slice(0, key.indexOf('/')), name: 'Team' },
      key,
      title: `Ticket ${key}`,
      type: 'task',
      state: 'filed',
      placeholder: false,
      readable: true,
      ...overrides,
    };
  }
  const child = (key: string): Relation => ({
    kind: 'child',
    id: `handle-${key.replace(/[^A-Za-z0-9]/g, '')}`,
    link: null,
    head: head(key),
  });
  const linked = (key: string): Relation => ({
    kind: 'link',
    id: `l-${key}`,
    link: {
      id: `l-${key}`,
      type: 'blocks',
      direction: 'outgoing',
      name: 'blocks',
      created_by: { id: 'p1', display_name: 'Ada' },
      created_at: '2026-10-01T09:00:00Z',
    },
    head: head(key),
  });

  let events: Subject<StreamEvent>;
  let http: HttpTestingController;
  let relations: TicketRelations;

  beforeEach(() => {
    events = new Subject<StreamEvent>();
    TestBed.configureTestingModule({
      providers: [
        TicketRelations,
        provideHttpClient(),
        provideHttpClientTesting(),
        provideApiConfiguration(''),
        { provide: EventStreamService, useValue: { events: events.asObservable() } },
      ],
    });
    http = TestBed.inject(HttpTestingController);
    relations = TestBed.inject(TicketRelations);
  });

  function ticketEvent(name: TicketEventName, key = 'acme/COW-12', kind = 'changed'): StreamEvent {
    return { name, id: 'e-1', key, version: 3, kind };
  }

  /** The resources' `reload`, replaced so that no request goes out. */
  function spyOnReloads() {
    return {
      comments: vi.spyOn(relations.comments, 'reload').mockReturnValue(true),
      activity: vi.spyOn(relations.activity, 'reload').mockReturnValue(true),
      questions: vi.spyOn(relations.questions, 'reload').mockReturnValue(true),
      links: vi.spyOn(relations.links, 'reload').mockReturnValue(true),
      children: vi.spyOn(relations.children, 'reload').mockReturnValue(true),
      interest: vi.spyOn(relations.interest, 'reload').mockReturnValue(true),
      attachments: vi.spyOn(relations.attachments, 'reload').mockReturnValue(true),
      time: vi.spyOn(relations.time, 'reload').mockReturnValue(true),
      tree: vi.spyOn(relations.tree, 'reload').mockReturnValue(true),
    };
  }

  function reloaded(spies: ReturnType<typeof spyOnReloads>): string[] {
    return Object.entries(spies)
      .filter(([, spy]) => spy.mock.calls.length > 0)
      .map(([name]) => name);
  }

  describe('loading', () => {
    it('loads nothing while no ticket is shown', () => {
      TestBed.tick();

      http.expectNone(() => true);
      expect(relations.comments.status()).toBe('idle');
    });

    it('loads the comments, the activity, the questions, the links, the children, the interest, the files, the time and the prerequisite tree of the ticket that is set', async () => {
      relations.at.set(cow12);
      TestBed.tick();

      const bodies = {
        comments: { items: [], next_cursor: null },
        activity: { items: [], next_cursor: null },
        questions: { items: [], next_cursor: null },
        links: { items: [linked('globex/API-7')], next_cursor: null },
        interest: { items: [], next_cursor: null },
        attachments: { items: [], next_cursor: null },
        time: { items: [], next_cursor: null, total_minutes: 0 },
        tree: { items: [], next_cursor: null, open: 0 },
      };
      for (const [part, body] of Object.entries(bodies)) {
        http.expectOne(urls[part as keyof typeof urls]).flush(body);
      }
      http.expectOne(urls.children).flush({ items: [child('globex/API-8')], next_cursor: null });
      await TestBed.inject(ApplicationRef).whenStable();

      for (const part of Object.keys(bodies) as (keyof typeof bodies)[]) {
        expect(relations[part].value(), part).toEqual(bodies[part]);
      }
      expect(relations.children.value()).toEqual({
        items: [child('globex/API-8')],
        nextCursor: null,
      });
      http.verify();
    });

    // docs/adr/0008 D2, docs/adr/0048 D3: the children, page after page.
    it('loads the children a page at a time, one more on request, and every page held again on a reload', async () => {
      relations.at.set(cow12);
      TestBed.tick();
      for (const request of http.match((each) => !each.url.endsWith('/relations'))) {
        request.flush({ ...empty, open: 0, total_minutes: 0 });
      }
      for (const request of http.match((each) => each.urlWithParams === urls.links)) {
        request.flush(empty);
      }
      http.expectOne(urls.children).flush({ items: [child('acme/COW-20')], next_cursor: 'p2' });
      await TestBed.inject(ApplicationRef).whenStable();
      expect(relations.children.value()?.nextCursor).toBe('p2');

      relations.childPages.update((pages) => pages + 1);
      TestBed.tick();
      http.expectOne(urls.children).flush({ items: [child('acme/COW-20')], next_cursor: 'p2' });
      await new Promise((resolve) => setTimeout(resolve));
      http
        .expectOne(`${base}/relations?kind=child&cursor=p2&limit=${childPageSize}`)
        .flush({ items: [child('globex/API-9')], next_cursor: null });
      await TestBed.inject(ApplicationRef).whenStable();

      expect(relations.children.value()).toEqual({
        items: [child('acme/COW-20'), child('globex/API-9')],
        nextCursor: null,
      });
      http.verify();
    });

    it('starts the children of another ticket at one page', () => {
      relations.at.set(cow12);
      relations.childPages.set(3);

      relations.at.set({ team: 'acme', project: 'OPS', number: 3 });

      expect(relations.childPages()).toBe(1);
    });

    it('loads again for the next ticket when another one is set', () => {
      relations.at.set(cow12);
      TestBed.tick();
      http.match(() => true);

      relations.at.set({ team: 'acme', project: 'OPS', number: 3 });
      TestBed.tick();

      expect(http.match(() => true).map((request) => request.request.url)).toEqual([
        '/api/v1/teams/acme/projects/OPS/tickets/3/comments',
        '/api/v1/teams/acme/projects/OPS/tickets/3/activity',
        '/api/v1/teams/acme/projects/OPS/tickets/3/questions',
        '/api/v1/teams/acme/projects/OPS/tickets/3/relations',
        '/api/v1/teams/acme/projects/OPS/tickets/3/relations',
        '/api/v1/teams/acme/projects/OPS/tickets/3/interest',
        '/api/v1/teams/acme/projects/OPS/tickets/3/attachments',
        '/api/v1/teams/acme/projects/OPS/tickets/3/time-entries',
        '/api/v1/teams/acme/projects/OPS/tickets/3/prerequisite-tree',
      ]);
    });

    it('reads the tree the other way, the dependents, when the page asks for them', () => {
      relations.at.set(cow12);
      TestBed.tick();
      http.match(() => true);

      relations.direction.set('up');
      TestBed.tick();

      http.expectOne(`${base}/prerequisite-tree?direction=up&limit=200`);
      http.verify();
    });
  });

  describe('the rendered body', () => {
    const bodyUrl = `${base}/body`;
    const rendered = (version: number) => ({ body: 'x', body_html: '<p>x</p>', version });

    it('loads once the version of the ticket is known, and again for a newer one', () => {
      relations.at.set(cow12);
      TestBed.tick();
      http.expectNone(bodyUrl);

      relations.version.set(3);
      TestBed.tick();
      http.expectOne(bodyUrl).flush(rendered(3));

      relations.version.set(4);
      TestBed.tick();
      http.expectOne(bodyUrl);
    });

    it('loads again on a change that moves no version, an upload, and leaves a newer one to the version', async () => {
      relations.at.set(cow12);
      relations.version.set(3);
      TestBed.tick();
      http.expectOne(bodyUrl).flush(rendered(3));
      // The other parts stay unanswered, so the application is not stable: wait for the answer.
      await new Promise((resolve) => setTimeout(resolve));
      expect(relations.body.value()).toEqual(rendered(3));
      const reload = vi.spyOn(relations.body, 'reload').mockReturnValue(true);

      events.next({
        name: 'ticket.changed',
        id: 'e-1',
        key: 'acme/COW-12',
        version: 3,
        kind: 'uploaded',
      });
      expect(reload).toHaveBeenCalledOnce();

      reload.mockClear();
      events.next({
        name: 'ticket.changed',
        id: 'e-2',
        key: 'acme/COW-12',
        version: 4,
        kind: 'edited',
      });
      expect(reload).not.toHaveBeenCalled();
    });
  });

  describe('events of the ticket', () => {
    beforeEach(() => {
      relations.at.set(cow12);
    });

    it.each([
      ['comment.changed', ['comments', 'activity']],
      ['question.changed', ['questions', 'activity']],
      ['link.changed', ['links', 'tree', 'activity']],
      ['interest.changed', ['interest', 'activity']],
      ['ticket.changed', ['attachments', 'activity']],
    ] as const)('reload only the parts %s changes: %j', (name, parts) => {
      const spies = spyOnReloads();

      events.next(ticketEvent(name));

      expect(reloaded(spies).sort()).toEqual([...parts].sort());
      for (const part of parts) {
        expect(spies[part]).toHaveBeenCalledOnce();
      }
    });

    it.each([
      ['another number of the same project', 'acme/COW-13'],
      ['another project', 'acme/OPS-12'],
      ['another tenant', 'globex/COW-12'],
    ])('change nothing for %s', (_, key) => {
      const spies = spyOnReloads();

      for (const name of [
        'ticket.changed',
        'comment.changed',
        'question.changed',
        'link.changed',
        'interest.changed',
      ] as const) {
        events.next(ticketEvent(name, key));
      }

      expect(reloaded(spies)).toEqual([]);
    });

    it('change nothing for a membership event, which names no ticket', () => {
      const spies = spyOnReloads();

      events.next({ name: 'membership.changed', id: 'e1', personId: 'p1', projectId: 'j1' });

      expect(reloaded(spies)).toEqual([]);
    });

    it.each(['resync', 'poll'] as const)('a %s reloads everything', (name) => {
      const spies = spyOnReloads();

      events.next({ name });

      expect(reloaded(spies).sort()).toEqual([
        'activity',
        'attachments',
        'children',
        'comments',
        'interest',
        'links',
        'questions',
        'time',
        'tree',
      ]);
    });

    // docs/adr/0017 D3, docs/adr/0054 D2: a child of another team moved the ticket's stages.
    it('reload the children alone on the ticket’s own derived event, which records no act', () => {
      const spies = spyOnReloads();

      events.next(ticketEvent('ticket.changed', 'acme/COW-12', 'derived'));

      expect(reloaded(spies)).toEqual(['children']);
    });

    // docs/adr/0008 D2 as amended again 2026-10-10: a child left the ticket from its side.
    it('reload the children and the activity alone on the ticket’s own detached event', () => {
      const spies = spyOnReloads();

      events.next(ticketEvent('ticket.changed', 'acme/COW-12', 'detached'));

      expect(reloaded(spies).sort()).toEqual(['activity', 'children']);
    });

    it('reload the links or the children when the page asks, after a removal', () => {
      const spies = spyOnReloads();

      relations.reloadLinks();
      expect(reloaded(spies)).toEqual(['links']);
      relations.reloadChildren();
      expect(reloaded(spies).sort()).toEqual(['children', 'links']);
    });

    it.each(['created', 'updated', 'restored'])(
      'reload the children on another ticket’s %s, which may have made it a child of this one',
      (kind) => {
        const spies = spyOnReloads();

        events.next(ticketEvent('ticket.changed', 'globex/API-3', kind));

        expect(reloaded(spies)).toEqual(['children']);
      },
    );

    describe('of a ticket at the other end of a link or a child (docs/adr/0005 D3)', () => {
      async function loaded() {
        TestBed.tick();
        for (const request of http.match(() => true)) {
          const url = request.request.urlWithParams;
          request.flush(
            url === urls.links
              ? { items: [linked('globex/API-7')], next_cursor: null }
              : url === urls.children
                ? { items: [child('globex/API-8')], next_cursor: null }
                : { ...empty, open: 0, total_minutes: 0 },
          );
        }
        await TestBed.inject(ApplicationRef).whenStable();
      }

      it('reload the links alone when the ticket at the other end of a link changes, of any team', async () => {
        await loaded();
        const spies = spyOnReloads();

        events.next(ticketEvent('ticket.changed', 'globex/API-7'));

        expect(reloaded(spies)).toEqual(['links']);
      });

      it('reload the children alone when a child changes, of any team', async () => {
        await loaded();
        const spies = spyOnReloads();

        events.next(ticketEvent('ticket.changed', 'globex/API-8'));

        expect(reloaded(spies)).toEqual(['children']);
      });

      it.each(['comment.changed', 'question.changed', 'interest.changed', 'link.changed'] as const)(
        'leave them alone on %s of such a ticket, which changes nothing its head shows',
        async (name) => {
          await loaded();
          const spies = spyOnReloads();

          events.next(ticketEvent(name, 'globex/API-7'));
          events.next(ticketEvent(name, 'globex/API-8'));

          expect(reloaded(spies)).toEqual([]);
        },
      );
    });

    describe('of a ticket of the prerequisite tree', () => {
      const node = {
        depth: 1,
        settled: false,
        repeated: false,
        blocked_from: null,
        head: head('acme/COW-7', { title: 'Pick the format' }),
        assignee: null,
        progress: { refinement: 0, implementation: 0, review: 0, derived: false },
      };

      async function loaded() {
        TestBed.tick();
        for (const request of http.match(() => true)) {
          request.flush(
            request.request.url.endsWith('/prerequisite-tree')
              ? { items: [node], next_cursor: null, open: 1 }
              : empty,
          );
        }
        await TestBed.inject(ApplicationRef).whenStable();
      }

      it.each(['ticket.changed', 'link.changed'] as const)(
        'reload the tree alone on %s, because the tree shows that ticket',
        async (name) => {
          await loaded();
          const spies = spyOnReloads();

          events.next(ticketEvent(name, 'acme/COW-7'));

          expect(reloaded(spies)).toEqual(['tree']);
        },
      );

      it.each(['comment.changed', 'question.changed', 'interest.changed'] as const)(
        'leave the tree alone on %s, which changes nothing it shows',
        async (name) => {
          await loaded();
          const spies = spyOnReloads();

          events.next(ticketEvent(name, 'acme/COW-7'));

          expect(reloaded(spies)).toEqual([]);
        },
      );
    });

    it('follow the ticket that is shown when it changes', () => {
      const spies = spyOnReloads();
      relations.at.set({ team: 'acme', project: 'OPS', number: 3 });

      events.next(ticketEvent('comment.changed', 'acme/COW-12'));
      expect(reloaded(spies)).toEqual([]);

      events.next(ticketEvent('comment.changed', 'acme/OPS-3'));
      expect(reloaded(spies).sort()).toEqual(['activity', 'comments']);
    });

    it.each([
      ['comment.changed', 'comments'],
      ['question.changed', 'questions'],
      ['link.changed', 'links'],
      ['interest.changed', 'interest'],
      ['ticket.changed', 'attachments'],
    ] as const)(
      'fetch only the part %s changes and the activity again, from the API',
      async (name, part) => {
        TestBed.tick();
        for (const request of http.match(() => true)) {
          request.flush(empty);
        }
        await TestBed.inject(ApplicationRef).whenStable();
        const changed = { items: [], next_cursor: null };

        events.next(ticketEvent(name));
        TestBed.tick();

        http.expectOne(urls[part]).flush(changed);
        http.expectOne(urls.activity).flush(empty);
        if (name === 'link.changed') {
          // A link of the ticket changes its prerequisite tree as well.
          http.expectOne(urls.tree).flush({ ...empty, open: 0 });
        }
        http.verify();
        await TestBed.inject(ApplicationRef).whenStable();
        expect(relations[part].value()).toBe(changed);
      },
    );

    describe('that arrive while a part is still loading for the first time', () => {
      /** Lets the answer of a request reach the resource and its watcher run. */
      async function settle() {
        await new Promise((resolve) => setTimeout(resolve));
        TestBed.tick();
      }

      it('fetch the part again once the first answer is in, which may predate the change', async () => {
        TestBed.tick();
        const [first] = http.match(urls.comments);

        events.next(ticketEvent('comment.changed'));
        TestBed.tick();
        expect(http.match(urls.comments)).toHaveLength(0);
        first.flush(empty);
        await settle();

        expect(http.match(urls.comments)).toHaveLength(1);
      });

      it('fetch it once, however many events arrived meanwhile', async () => {
        TestBed.tick();
        const [first] = http.match(urls.comments);

        events.next(ticketEvent('comment.changed'));
        events.next(ticketEvent('comment.changed'));
        events.next({ name: 'poll' });
        TestBed.tick();
        first.flush(empty);
        await settle();

        expect(http.match(urls.comments)).toHaveLength(1);
      });

      it('leave the parts that are loaded alone until their own event comes', async () => {
        TestBed.tick();
        for (const request of http.match(() => true)) {
          if (!request.request.urlWithParams.includes('/comments')) {
            request.flush(empty);
          }
        }
        await settle();

        events.next(ticketEvent('comment.changed'));
        TestBed.tick();

        expect(http.match(urls.questions)).toHaveLength(0);
        expect(http.match(urls.links)).toHaveLength(0);
        expect(http.match(urls.interest)).toHaveLength(0);
        expect(http.match(urls.attachments)).toHaveLength(0);
        expect(http.match(urls.time)).toHaveLength(0);
        expect(http.match(urls.activity)).toHaveLength(1);
      });
    });

    describe('the time of the ticket', () => {
      it.each([
        'ticket.changed',
        'comment.changed',
        'question.changed',
        'link.changed',
        'interest.changed',
      ] as const)(
        'is not reloaded by %s, because no event is published for time entries',
        (name) => {
          const spies = spyOnReloads();

          events.next(ticketEvent(name));

          expect(spies.time).not.toHaveBeenCalled();
        },
      );

      it('is reloaded when the page asks for it, after the person booked or corrected time', async () => {
        TestBed.tick();
        for (const request of http.match(() => true)) {
          request.flush(empty);
        }
        await TestBed.inject(ApplicationRef).whenStable();

        relations.reloadTime();
        TestBed.tick();

        http.expectOne(urls.time).flush({ ...empty, total_minutes: 90 });
        http.verify();
        await TestBed.inject(ApplicationRef).whenStable();
        expect(relations.time.value()?.total_minutes).toBe(90);
      });

      it('is reloaded once the first load is in when the page asks for it during that load', async () => {
        TestBed.tick();
        const [first] = http.match(urls.time);

        relations.reloadTime();
        TestBed.tick();
        expect(http.match(urls.time)).toHaveLength(0);
        first.flush(empty);
        await new Promise((resolve) => setTimeout(resolve));
        TestBed.tick();

        expect(http.match(urls.time)).toHaveLength(1);
      });
    });
  });

  describe('a part that loads again and fails (docs/adr/0054 D7)', () => {
    const parts = [
      'comments',
      'activity',
      'questions',
      'links',
      'children',
      'interest',
      'attachments',
      'time',
      'tree',
    ] as const;

    /** Lets the answers reach the resources and the effects they feed run. */
    async function settle() {
      await new Promise((resolve) => setTimeout(resolve));
      TestBed.tick();
    }

    /** Fails a request: without an answer at all (status 0), or with a problem of the status. */
    function fail(request: TestRequest, status: number) {
      if (status === 0) {
        request.error(new ProgressEvent('error'));
      } else {
        request.flush(
          { type: 'about:blank', title: 'Refused', status, code: 'internal' },
          { status, statusText: `Status ${status}` },
        );
      }
    }

    /** Every part of the ticket loaded once, each with its own answer, and loading again on a poll. */
    async function reloading() {
      relations.at.set(cow12);
      TestBed.tick();
      for (const request of http.match(() => true)) {
        request.flush({ items: [], next_cursor: null, total_minutes: 0 });
      }
      await settle();
      const shown = Object.fromEntries(parts.map((part) => [part, relations[part].value()]));
      events.next({ name: 'poll' });
      TestBed.tick();
      return shown;
    }

    it.each([0, 500, 503])(
      'keeps every part shown when the poll finds the backend failing with a status of %i',
      async (status) => {
        const shown = await reloading();

        const requests = http.match(() => true);
        expect(requests).toHaveLength(parts.length);
        for (const request of requests) {
          fail(request, status);
        }
        await settle();

        for (const part of parts) {
          expect(relations[part].status(), part).toBe('resolved');
          expect(relations[part].value(), part).toBe(shown[part]);
        }
      },
    );

    it("sends each part's weak ETag on a poll and keeps every part on a 304 (docs/adr/0054 D7)", async () => {
      relations.at.set(cow12);
      TestBed.tick();
      for (const request of http.match(() => true)) {
        request.flush(
          { items: [], next_cursor: null, total_minutes: 0 },
          { headers: { ETag: `W/"${request.request.url.split('/').pop()}"` } },
        );
      }
      await settle();
      const shown = Object.fromEntries(parts.map((part) => [part, relations[part].value()]));

      events.next({ name: 'poll' });
      TestBed.tick();
      const requests = http.match(() => true);
      expect(requests).toHaveLength(parts.length);
      for (const request of requests) {
        expect(request.request.headers.get('If-None-Match')).toBe(
          `W/"${request.request.url.split('/').pop()}"`,
        );
        request.flush(null, { status: 304, statusText: 'Not Modified' });
      }
      await settle();

      for (const part of parts) {
        expect(relations[part].status(), part).toBe('resolved');
        expect(relations[part].value(), part).toBe(shown[part]);
      }
    });

    it.each([401, 403, 404])(
      'shows no part when the poll answers %i: the ticket is gone for the person',
      async (status) => {
        await reloading();

        for (const request of http.match(() => true)) {
          fail(request, status);
        }
        await settle();

        for (const part of parts) {
          const ref: ResourceRef<unknown> = relations[part];
          expect(ref.status(), part).toBe('error');
          expect(ref.hasValue(), part).toBe(false);
        }
      },
    );
  });

  describe('while no ticket is shown', () => {
    it('has no time to reload, and asks for none when the page asks', () => {
      relations.reloadTime();
      TestBed.tick();

      http.expectNone(() => true);
      expect(relations.time.status()).toBe('idle');
    });

    it.each([
      ticketEvent('comment.changed'),
      { name: 'resync' },
      { name: 'poll' },
    ] as StreamEvent[])('change nothing: %j', (event) => {
      const spies = spyOnReloads();

      events.next(event);

      expect(reloaded(spies)).toEqual([]);
    });
  });

  it('stops listening to the event stream when the page that provides it is gone', () => {
    expect(events.observed).toBe(true);

    TestBed.resetTestingModule();

    expect(events.observed).toBe(false);
  });
});
