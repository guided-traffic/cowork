import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting, TestRequest } from '@angular/common/http/testing';
import { signal, WritableSignal } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { Subject } from 'rxjs';
import { provideApiConfiguration } from '../api/api-configuration';
import { MyProject, MyProjectList, Project } from '../api/models';
import { EventStreamService, StreamEvent } from './event-stream.service';
import { MyProjectsService, shownAgainWithin } from './my-projects.service';
import { SessionService } from './session.service';

function project(key: string): Project {
  return {
    id: `id-${key}`,
    key,
    name: `Project ${key}`,
    description: '',
    restricted: false,
    wip_limits: {},
    version: 1,
    created_at: '2026-10-01T09:00:00Z',
    updated_at: '2026-10-01T09:00:00Z',
  };
}

function mine(team: string, key: string): MyProject {
  return { team: { slug: team, name: `Team ${team}` }, project: project(key) };
}

function listOf(items: MyProject[], next: string | null = null): MyProjectList {
  return { items, next_cursor: next };
}

describe('MyProjectsService', () => {
  let service: MyProjectsService;
  let http: HttpTestingController;
  let stream: Subject<StreamEvent>;
  let person: WritableSignal<{ id: string } | undefined>;
  let tenant: WritableSignal<string | null>;

  const settle = async () => {
    TestBed.tick();
    await vi.advanceTimersByTimeAsync(0);
    TestBed.tick();
  };
  const listRequest = (): TestRequest =>
    http.expectOne(
      (request) => request.method === 'GET' && request.url === '/api/v1/me/projects',
    );
  const keys = () =>
    (service.projects.hasValue() ? service.projects.value() : []).map(
      ({ team, project }) => `${team.slug}/${project.key}`,
    );
  /** The first load, answered with a weak ETag, which the next load sends back. */
  const loaded = async (items: MyProject[]) => {
    listRequest().flush(listOf(items), { headers: { ETag: 'W/"first"' } });
    await settle();
  };

  beforeEach(async () => {
    vi.useFakeTimers();
    stream = new Subject<StreamEvent>();
    person = signal<{ id: string } | undefined>({ id: 'p1' });
    tenant = signal<string | null>(null);
    TestBed.configureTestingModule({
      providers: [
        provideHttpClient(),
        provideHttpClientTesting(),
        provideApiConfiguration(''),
        { provide: EventStreamService, useValue: { events: stream.asObservable() } },
        { provide: SessionService, useValue: { person, tenant } },
      ],
    });
    service = TestBed.inject(MyProjectsService);
    http = TestBed.inject(HttpTestingController);
    await settle();
  });

  afterEach(() => {
    try {
      http.verify();
    } finally {
      vi.useRealTimers();
      TestBed.resetTestingModule();
    }
  });

  it('reads every page of the projects of every team of the person, two hundred a page', async () => {
    const first = listRequest();
    expect(first.request.params.get('limit')).toBe('200');
    expect(first.request.params.has('team')).toBe(false);
    first.flush(listOf([mine('acme', 'COW'), mine('acme', 'OPS')], 'c1'));
    await settle();
    const second = listRequest();
    expect(second.request.params.get('cursor')).toBe('c1');
    second.flush(listOf([mine('globex', 'WEB')]));
    await settle();

    expect(keys()).toEqual(['acme/COW', 'acme/OPS', 'globex/WEB']);
    expect(service.of('acme').map((each) => each.key)).toEqual(['COW', 'OPS']);
    expect(service.of('globex').map((each) => each.key)).toEqual(['WEB']);
    expect(service.of('initech')).toEqual([]);
  });

  it('reads nothing while nobody is known', async () => {
    listRequest().flush(listOf([]));
    await settle();

    person.set(undefined);
    await settle();
    stream.next({ name: 'poll' });
    document.dispatchEvent(new Event('visibilitychange'));
    await settle();

    http.expectNone('/api/v1/me/projects');
    expect(keys()).toEqual([]);
  });

  describe('once it holds the list', () => {
    beforeEach(() => loaded([mine('acme', 'COW')]));

    /** The load again: it asks whether anything changed, and is told something did. */
    const again = async (items: MyProject[]) => {
      const request = listRequest();
      expect(request.request.headers.get('If-None-Match')).toBe('W/"first"');
      request.flush(listOf(items), { headers: { ETag: 'W/"second"' } });
      await settle();
    };

    it('reads it again when the tab is shown again, asking whether anything changed', async () => {
      document.dispatchEvent(new Event('visibilitychange'));
      await settle();

      await again([mine('acme', 'COW'), mine('globex', 'NEW')]);
      expect(keys()).toEqual(['acme/COW', 'globex/NEW']);
    });

    it('reads it again when the window gets the focus back', async () => {
      window.dispatchEvent(new Event('focus'));
      await settle();

      await again([mine('acme', 'COW')]);
    });

    it('reads it once for the tab shown again and the focus that comes with it', async () => {
      document.dispatchEvent(new Event('visibilitychange'));
      window.dispatchEvent(new Event('focus'));
      await settle();
      await again([mine('acme', 'COW')]);

      await vi.advanceTimersByTimeAsync(shownAgainWithin);
      window.dispatchEvent(new Event('focus'));
      await settle();
      const later = listRequest();
      expect(later.request.headers.get('If-None-Match')).toBe('W/"second"');
      later.flush(listOf([mine('acme', 'COW')]));
      await settle();
    });

    it('reads nothing while the tab is hidden', async () => {
      const visibility = vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('hidden');
      try {
        document.dispatchEvent(new Event('visibilitychange'));
        window.dispatchEvent(new Event('focus'));
        await settle();

        http.expectNone('/api/v1/me/projects');
      } finally {
        visibility.mockRestore();
      }
    });

    it('reads it again when the pages enter a team, and when they leave it', async () => {
      tenant.set('acme');
      await settle();
      await again([mine('acme', 'COW')]);

      tenant.set(null);
      await settle();
      listRequest().flush(listOf([mine('acme', 'COW')]));
      await settle();
    });

    it.each<[string, StreamEvent]>([
      [
        'a team the person joined or left, in any of their teams',
        { name: 'membership.changed', id: 'e1', tenant: 'globex', personId: 'p1' },
      ],
      [
        'a project restricted or opened in any of their teams',
        { name: 'membership.changed', id: 'e2', tenant: 'globex', projectId: 'j1' },
      ],
      ['a gap in the stream', { name: 'resync' }],
      ["the fallback's poll", { name: 'poll' }],
    ])('reads it again on %s', async (_what, event) => {
      stream.next(event);
      await settle();

      await again([mine('acme', 'COW')]);
    });

    it.each<[string, StreamEvent]>([
      [
        "somebody else's membership",
        { name: 'membership.changed', id: 'e1', tenant: 'globex', personId: 'p2' },
      ],
      [
        'a ticket act',
        { name: 'ticket.changed', id: 'e1', key: 'acme/COW-1', version: 2, kind: 'transitioned' },
      ],
      ['a sort of a rank', { name: 'project.changed', id: 'e1', key: 'acme/COW', kind: 'ranked' }],
    ])('leaves it alone on %s', async (_what, event) => {
      stream.next(event);
      await settle();

      http.expectNone('/api/v1/me/projects');
    });

    it('keeps the list shown when a load again fails', async () => {
      stream.next({ name: 'poll' });
      await settle();
      listRequest().flush(null, { status: 503, statusText: 'Service Unavailable' });
      await settle();

      expect(keys()).toEqual(['acme/COW']);
    });
  });
});
