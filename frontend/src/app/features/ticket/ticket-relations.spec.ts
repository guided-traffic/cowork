import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { ApplicationRef } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { Subject } from 'rxjs';
import { provideApiConfiguration } from '../../api/api-configuration';
import { EventStreamService, StreamEvent, TicketEventName } from '../../core/event-stream.service';
import { address, TicketAddress, TicketRelations } from './ticket-relations';

describe('address', () => {
  it.each([
    ['COW-12', 'COW', 12],
    ['VKO-1', 'VKO', 1],
    ['AB-100', 'AB', 100],
    ['A1-7', 'A1', 7],
    ['ABCDEFGHIJ-5', 'ABCDEFGHIJ', 5],
  ])('reads %s as the project %s and the number %i', (key, project, number) => {
    expect(address('acme', key)).toEqual({ tenant: 'acme', project, number });
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
  const cow12: TicketAddress = { tenant: 'acme', project: 'COW', number: 12 };
  const base = '/api/v1/tenants/acme/projects/COW/tickets/12';
  const urls = {
    comments: `${base}/comments?limit=200`,
    activity: `${base}/activity?order=desc&limit=100`,
    questions: `${base}/questions?limit=200`,
    links: `${base}/links?limit=200`,
    interest: `${base}/interest?limit=200`,
    attachments: `${base}/attachments?limit=200`,
    time: `${base}/time-entries?limit=200`,
  };
  const empty = { items: [], next_cursor: null };

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

  function ticketEvent(name: TicketEventName, key = 'acme/COW-12'): StreamEvent {
    return { name, id: 'e-1', key, version: 3, kind: 'changed' };
  }

  /** The resources' `reload`, replaced so that no request goes out. */
  function spyOnReloads() {
    return {
      comments: vi.spyOn(relations.comments, 'reload').mockReturnValue(true),
      activity: vi.spyOn(relations.activity, 'reload').mockReturnValue(true),
      questions: vi.spyOn(relations.questions, 'reload').mockReturnValue(true),
      links: vi.spyOn(relations.links, 'reload').mockReturnValue(true),
      interest: vi.spyOn(relations.interest, 'reload').mockReturnValue(true),
      attachments: vi.spyOn(relations.attachments, 'reload').mockReturnValue(true),
      time: vi.spyOn(relations.time, 'reload').mockReturnValue(true),
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

    it('loads the comments, the activity, the questions, the links, the interest, the files and the time of the ticket that is set', async () => {
      relations.at.set(cow12);
      TestBed.tick();

      const bodies = {
        comments: { items: [], next_cursor: null },
        activity: { items: [], next_cursor: null },
        questions: { items: [], next_cursor: null },
        links: { items: [], next_cursor: null },
        interest: { items: [], next_cursor: null },
        attachments: { items: [], next_cursor: null },
        time: { items: [], next_cursor: null, total_minutes: 0 },
      };
      for (const [part, body] of Object.entries(bodies)) {
        http.expectOne(urls[part as keyof typeof urls]).flush(body);
      }
      await TestBed.inject(ApplicationRef).whenStable();

      for (const part of Object.keys(bodies) as (keyof typeof bodies)[]) {
        expect(relations[part].value(), part).toEqual(bodies[part]);
      }
      http.verify();
    });

    it('loads again for the next ticket when another one is set', () => {
      relations.at.set(cow12);
      TestBed.tick();
      http.match(() => true);

      relations.at.set({ tenant: 'acme', project: 'OPS', number: 3 });
      TestBed.tick();

      expect(http.match(() => true).map((request) => request.request.url)).toEqual([
        '/api/v1/tenants/acme/projects/OPS/tickets/3/comments',
        '/api/v1/tenants/acme/projects/OPS/tickets/3/activity',
        '/api/v1/tenants/acme/projects/OPS/tickets/3/questions',
        '/api/v1/tenants/acme/projects/OPS/tickets/3/links',
        '/api/v1/tenants/acme/projects/OPS/tickets/3/interest',
        '/api/v1/tenants/acme/projects/OPS/tickets/3/attachments',
        '/api/v1/tenants/acme/projects/OPS/tickets/3/time-entries',
      ]);
    });
  });

  describe('events of the ticket', () => {
    beforeEach(() => {
      relations.at.set(cow12);
    });

    it.each([
      ['comment.changed', ['comments', 'activity']],
      ['question.changed', ['questions', 'activity']],
      ['link.changed', ['links', 'activity']],
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

    it.each(['resync', 'poll'] as const)('a %s reloads everything', (name) => {
      const spies = spyOnReloads();

      events.next({ name });

      expect(reloaded(spies).sort()).toEqual([
        'activity',
        'attachments',
        'comments',
        'interest',
        'links',
        'questions',
        'time',
      ]);
    });

    it('follow the ticket that is shown when it changes', () => {
      const spies = spyOnReloads();
      relations.at.set({ tenant: 'acme', project: 'OPS', number: 3 });

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
