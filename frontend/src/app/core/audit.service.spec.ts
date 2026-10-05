import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { TestBed } from '@angular/core/testing';
import { provideApiConfiguration } from '../api/api-configuration';
import { AuditList } from '../api/models';
import { AuditService, csvRowLimit } from './audit.service';

const url = '/api/v1/tenants/acme/audit';
const header = 'id,created_at,actor_user_id\n';

const listOf = (total: number): AuditList => ({
  items: [],
  next_cursor: null,
  total,
  page: 1,
  per_page: 25,
});

describe('AuditService', () => {
  let http: HttpTestingController;
  let audit: AuditService;

  beforeEach(() => {
    TestBed.configureTestingModule({
      providers: [provideHttpClient(), provideHttpClientTesting(), provideApiConfiguration('')],
    });
    http = TestBed.inject(HttpTestingController);
    audit = TestBed.inject(AuditService);
  });

  afterEach(() => http.verify());

  /** Lets the next request of a chain go out. */
  const tick = async () => {
    for (let round = 0; round < 3; round++) {
      await Promise.resolve();
    }
  };

  describe('page', () => {
    it('asks for a numbered page with the filters, an action repeated (docs/adr/0048 D2, docs/adr/0049)', async () => {
      const done = audit.page(
        {
          tenant: 'acme',
          actor: 'p1',
          token: 't1',
          action: ['created', 'transitioned'],
          entity_type: 'ticket',
          from: '2026-10-01T00:00:00.000Z',
          to: '2026-10-02T00:00:00.000Z',
        },
        3,
        50,
      );

      const sent = http.expectOne((request) => request.url === url);
      expect(sent.request.method).toBe('GET');
      expect(sent.request.params.get('page')).toBe('3');
      expect(sent.request.params.get('per_page')).toBe('50');
      expect(sent.request.params.get('actor')).toBe('p1');
      expect(sent.request.params.get('token')).toBe('t1');
      expect(sent.request.params.getAll('action')).toEqual(['created', 'transitioned']);
      expect(sent.request.params.get('entity_type')).toBe('ticket');
      expect(sent.request.params.get('from')).toBe('2026-10-01T00:00:00.000Z');
      expect(sent.request.params.get('to')).toBe('2026-10-02T00:00:00.000Z');
      expect(sent.request.params.has('cursor')).toBe(false);
      sent.flush(listOf(120));

      expect((await done).total).toBe(120);
    });
  });

  describe('csv (docs/adr/0026 D6)', () => {
    /** The CSV page the download asks for next. */
    const csvPage = async (page: number) => {
      await tick();
      return http.expectOne(
        (request) =>
          request.url === url &&
          request.params.get('page') === String(page) &&
          request.headers.get('Accept') === 'text/csv',
      );
    };

    it('ends the period at the server clock, reads every page of a hundred and keeps the header once', async () => {
      const done = audit.csv({ tenant: 'acme', entity_type: 'ticket' });

      const counted = http.expectOne((request) => request.url === url);
      expect(counted.request.params.get('page')).toBe('1');
      expect(counted.request.headers.get('Accept')).toContain('application/json');
      counted.flush(listOf(250), { headers: { Date: 'Sun, 04 Oct 2026 12:00:00 GMT' } });

      const rows = ['a,1,p\n', 'b,2,p\n', 'c,3,p\n'];
      for (const [index, row] of rows.entries()) {
        const sent = await csvPage(index + 1);
        expect(sent.request.params.get('per_page')).toBe('100');
        expect(sent.request.params.get('to')).toBe('2026-10-04T12:00:00.000Z');
        expect(sent.request.params.get('entity_type')).toBe('ticket');
        expect(sent.request.responseType).toBe('text');
        sent.flush(header + row);
      }

      expect(await done).toEqual({
        text: header + rows.join(''),
        until: '2026-10-04T12:00:00.000Z',
        total: 250,
      });
    });

    it('keeps the end the filters name when it is earlier than the server clock', async () => {
      const done = audit.csv({ tenant: 'acme', to: '2026-10-02T00:00:00.000Z' });

      http
        .expectOne((request) => request.url === url)
        .flush(listOf(3), { headers: { Date: 'Sun, 04 Oct 2026 12:00:00 GMT' } });
      const sent = await csvPage(1);
      expect(sent.request.params.get('to')).toBe('2026-10-02T00:00:00.000Z');
      sent.flush(header + 'a,1,p\n');

      expect((await done).until).toBe('2026-10-02T00:00:00.000Z');
    });

    it('is the header alone when nothing matches', async () => {
      const done = audit.csv({ tenant: 'acme' });

      http.expectOne((request) => request.url === url).flush(listOf(0));
      (await csvPage(1)).flush(header);

      expect((await done).text).toBe(header);
    });

    it(`stops at the newest ${csvRowLimit} rows, where the numbered pages end (docs/adr/0048 D2)`, async () => {
      const done = audit.csv({ tenant: 'acme' });

      http
        .expectOne((request) => request.url === url)
        .flush(listOf(25_000), { headers: { Date: 'Sun, 04 Oct 2026 12:00:00 GMT' } });
      for (let page = 1; page <= 100; page++) {
        (await csvPage(page)).flush(header + `r${page},1,p\n`);
      }
      await tick();
      http.expectNone((request) => request.url === url);

      const csv = await done;
      expect(csv.total).toBe(25_000);
      expect(csv.text.split('\n').filter((line) => line !== '')).toHaveLength(101);
    });
  });
});
