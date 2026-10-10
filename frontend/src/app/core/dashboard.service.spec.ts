import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import {
  createEnvironmentInjector,
  EnvironmentInjector,
  Injector,
  ResourceRef,
  signal,
  WritableSignal,
} from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { Subject } from 'rxjs';
import { provideApiConfiguration } from '../api/api-configuration';
import { Dashboard } from '../api/models';
import {
  changesDashboard,
  DashboardQuery,
  DashboardService,
  dashboardReloadDelay,
} from './dashboard.service';
import { EventStreamService, StreamEvent } from './event-stream.service';
import { SessionService } from './session.service';

const ticketEvent = (
  name:
    'ticket.changed' | 'comment.changed' | 'question.changed' | 'link.changed' | 'interest.changed',
  key: string,
): StreamEvent => ({ name, id: 'e1', key, version: 2, kind: 'transitioned' });

/** An answer with nothing in it but the period; the page does not care here. */
const dashboard = (from: string, to: string): Dashboard => ({
  period: { from, to },
  open_by_state: [],
  open_by_severity: [],
  security: [],
  blocked: { count: 0, oldest: null },
  age: [],
  throughput: [],
  lead_time: { from, to, done: 0, median_seconds: null },
  decisions: { count: 0, oldest: null },
  time: { total_minutes: 0, projects: [] },
  recent: [],
});

describe('changesDashboard', () => {
  it('reloads on a gap in the stream and on the fallback’s tick', () => {
    expect(changesDashboard({ name: 'resync' }, 'acme', 'p1')).toBe(true);
    expect(changesDashboard({ name: 'poll' }, 'acme', 'p1')).toBe(true);
  });

  it('reloads on a ticket’s and a question’s act in the tenant shown, and on no other tenant’s', () => {
    expect(changesDashboard(ticketEvent('ticket.changed', 'acme/ALPHA-1'), 'acme', 'p1')).toBe(
      true,
    );
    expect(changesDashboard(ticketEvent('question.changed', 'acme/ALPHA-1'), 'acme', 'p1')).toBe(
      true,
    );
    expect(changesDashboard(ticketEvent('question.changed', 'other/ALPHA-1'), 'acme', 'p1')).toBe(
      false,
    );
  });

  it('leaves a comment, a stake and a link alone: they change no tile', () => {
    for (const name of ['comment.changed', 'interest.changed', 'link.changed'] as const) {
      expect(changesDashboard(ticketEvent(name, 'acme/ALPHA-1'), 'acme', 'p1')).toBe(false);
    }
  });

  it('reloads on the deletion and the restoration of a ticket of the tenant shown', () => {
    for (const kind of ['deleted', 'restored', 'purged']) {
      const event: StreamEvent = {
        name: 'ticket.changed',
        id: 'e',
        key: 'acme/ALPHA-1',
        version: 3,
        kind,
      };
      expect(changesDashboard(event, 'acme', 'p1')).toBe(true);
    }
  });

  const membership = (keys: object): StreamEvent => ({
    name: 'membership.changed',
    id: 'e',
    ...keys,
  });

  it('reloads when who sees a project of the tenant may have changed, and not on another membership act', () => {
    expect(changesDashboard(membership({ tenant: 'acme', projectId: 'x' }), 'acme', 'p1')).toBe(
      true,
    );
    expect(changesDashboard(membership({ tenant: 'acme', personId: 'p1' }), 'acme', 'p1')).toBe(
      true,
    );
    expect(changesDashboard(membership({ tenant: 'acme', personId: 'p2' }), 'acme', 'p1')).toBe(
      false,
    );
    expect(changesDashboard(membership({ tenant: 'acme', mappingId: 'm' }), 'acme', 'p1')).toBe(
      false,
    );
  });

  it('leaves every event of another tenant of the person alone (docs/adr/0054 D1)', () => {
    expect(changesDashboard(ticketEvent('ticket.changed', 'other/ALPHA-1'), 'acme', 'p1')).toBe(
      false,
    );
    expect(changesDashboard(membership({ tenant: 'other', projectId: 'x' }), 'acme', 'p1')).toBe(
      false,
    );
    expect(changesDashboard(membership({ tenant: 'other', personId: 'p1' }), 'acme', 'p1')).toBe(
      false,
    );
  });

  it('takes a membership event without its tenant, from a server before it, for the tenant shown', () => {
    expect(changesDashboard(membership({ projectId: 'x' }), 'acme', 'p1')).toBe(true);
  });

  it('reloads on an import executed into a project of the tenant shown, and not on a sort of its rank', () => {
    const project = (key: string, kind: string): StreamEvent => ({
      name: 'project.changed',
      id: 'e',
      key,
      kind,
    });
    expect(changesDashboard(project('acme/ALPHA', 'imported'), 'acme', 'p1')).toBe(true);
    expect(changesDashboard(project('other/ALPHA', 'imported'), 'acme', 'p1')).toBe(false);
    expect(changesDashboard(project('acme/ALPHA', 'ranked'), 'acme', 'p1')).toBe(false);
  });

  it('leaves the unread count alone', () => {
    expect(changesDashboard({ name: 'inbox.changed', unread: 3 }, 'acme', 'p1')).toBe(false);
  });
});

describe('DashboardService', () => {
  let service: DashboardService;
  let http: HttpTestingController;
  let stream: Subject<StreamEvent>;
  let query: WritableSignal<DashboardQuery | undefined>;

  const settle = async () => {
    TestBed.tick();
    await vi.advanceTimersByTimeAsync(0);
    TestBed.tick();
  };
  const url = '/api/v1/teams/acme/dashboard';
  const pending = () => http.match((request) => request.url === url);

  beforeEach(() => {
    vi.useFakeTimers();
    stream = new Subject<StreamEvent>();
    query = signal<DashboardQuery | undefined>({
      team: 'acme',
      project: ['ALPHA', '!BETA'],
      from: '2026-09-08',
      to: '2026-10-07',
    });
    TestBed.configureTestingModule({
      providers: [
        provideHttpClient(),
        provideHttpClientTesting(),
        provideApiConfiguration(''),
        { provide: EventStreamService, useValue: { events: stream.asObservable() } },
        {
          provide: SessionService,
          useValue: { tenant: signal('acme'), person: signal({ id: 'p1' }) },
        },
      ],
    });
    service = TestBed.inject(DashboardService);
    http = TestBed.inject(HttpTestingController);
  });

  afterEach(() => {
    try {
      http.verify();
    } finally {
      vi.useRealTimers();
      TestBed.resetTestingModule();
    }
  });

  function open(
    injector: Injector = TestBed.inject(EnvironmentInjector),
  ): ResourceRef<Dashboard | undefined> {
    return service.dashboard(() => query(), injector);
  }

  async function load(ref: ResourceRef<Dashboard | undefined>) {
    await settle();
    const [request] = pending();
    request.flush(dashboard('2026-09-08', '2026-10-07'), { headers: { ETag: 'W/"one"' } });
    await settle();
    return { ref, request };
  }

  it('asks for the tenant’s dashboard with the filter and the period', async () => {
    const { ref, request } = await load(open());

    expect(request.request.method).toBe('GET');
    expect(request.request.params.getAll('project')).toEqual(['ALPHA', '!BETA']);
    expect(request.request.params.get('from')).toBe('2026-09-08');
    expect(request.request.params.get('to')).toBe('2026-10-07');
    expect(request.request.headers.has('If-None-Match')).toBe(false);
    expect(ref.value()?.period).toEqual({ from: '2026-09-08', to: '2026-10-07' });
  });

  it('asks for nothing without a query', async () => {
    query.set(undefined);
    const ref = open();
    await settle();

    expect(pending()).toEqual([]);
    expect(ref.status()).toBe('idle');
  });

  it('loads again once a second after the first event of a burst, with the tag it holds', async () => {
    const { ref } = await load(open());

    stream.next(ticketEvent('ticket.changed', 'acme/ALPHA-1'));
    stream.next(ticketEvent('question.changed', 'acme/ALPHA-2'));
    await vi.advanceTimersByTimeAsync(dashboardReloadDelay - 1);
    expect(pending()).toEqual([]);
    stream.next(ticketEvent('ticket.changed', 'acme/ALPHA-3'));
    await vi.advanceTimersByTimeAsync(1);
    await settle();

    const again = pending();
    expect(again).toHaveLength(1);
    expect(again[0].request.headers.get('If-None-Match')).toBe('W/"one"');
    again[0].flush(null, { status: 304, statusText: 'Not Modified', headers: { ETag: 'W/"one"' } });
    await settle();
    expect(ref.value()?.period.to).toBe('2026-10-07');
    expect(ref.error()).toBeUndefined();
  });

  it('does not load again for an event that changes no tile', async () => {
    await load(open());

    stream.next(ticketEvent('comment.changed', 'acme/ALPHA-1'));
    stream.next(ticketEvent('ticket.changed', 'other/ALPHA-1'));
    await vi.advanceTimersByTimeAsync(dashboardReloadDelay * 2);
    await settle();

    expect(pending()).toEqual([]);
  });

  it('does not load again for an event of another tenant of the person', async () => {
    await load(open());

    stream.next(ticketEvent('ticket.changed', 'other/ALPHA-1'));
    stream.next(ticketEvent('question.changed', 'other/ALPHA-2'));
    stream.next({ name: 'membership.changed', id: 'e', tenant: 'other', projectId: 'x' });
    stream.next({ name: 'membership.changed', id: 'e', tenant: 'other', personId: 'p1' });
    await vi.advanceTimersByTimeAsync(dashboardReloadDelay * 2);
    await settle();

    expect(pending()).toEqual([]);
  });

  it('keeps what it shows when a reload fails', async () => {
    const { ref } = await load(open());

    stream.next({ name: 'poll' });
    await vi.advanceTimersByTimeAsync(dashboardReloadDelay);
    await settle();
    pending()[0].flush(
      { type: 'about:blank', title: 'Not ready', status: 503, code: 'not_ready' },
      { status: 503, statusText: 'Service Unavailable' },
    );
    await settle();

    expect(ref.value()?.period.from).toBe('2026-09-08');
  });

  it('forgets a dashboard whose view is gone', async () => {
    const view = createEnvironmentInjector([], TestBed.inject(EnvironmentInjector));
    await load(open(view));
    view.destroy();

    stream.next({ name: 'resync' });
    await vi.advanceTimersByTimeAsync(dashboardReloadDelay);
    await settle();

    expect(pending()).toEqual([]);
  });
});
