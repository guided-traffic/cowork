import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { TestBed } from '@angular/core/testing';
import { provideApiConfiguration } from '../api/api-configuration';
import { TenantTokensService } from './tenant-tokens.service';

describe('TenantTokensService', () => {
  let service: TenantTokensService;
  let http: HttpTestingController;

  beforeEach(() => {
    TestBed.configureTestingModule({
      providers: [provideHttpClient(), provideHttpClientTesting(), provideApiConfiguration('')],
    });
    service = TestBed.inject(TenantTokensService);
    http = TestBed.inject(HttpTestingController);
  });

  afterEach(() => http.verify());

  it("asks for a numbered page of the tenant's tokens", async () => {
    const done = service.page('acme', 2, 50);

    const sent = http.expectOne((request) => request.url === '/api/v1/tenants/acme/tokens');
    expect(sent.request.method).toBe('GET');
    expect(sent.request.params.get('page')).toBe('2');
    expect(sent.request.params.get('per_page')).toBe('50');
    expect(sent.request.params.has('cursor')).toBe(false);
    sent.flush({ items: [], next_cursor: null, total: 0, page: 2, per_page: 50 });

    expect((await done).total).toBe(0);
  });

  it('asks for the same page again with the weak ETag it holds, and keeps the page on a 304 (docs/adr/0054 D7)', async () => {
    const first = service.page('acme', 1, 25);
    http
      .expectOne((request) => request.url === '/api/v1/tenants/acme/tokens')
      .flush(
        { items: [], next_cursor: null, total: 3, page: 1, per_page: 25 },
        { headers: { ETag: 'W/"tokens"' } },
      );
    expect((await first).total).toBe(3);

    const again = service.page('acme', 1, 25);
    const sent = http.expectOne((request) => request.url === '/api/v1/tenants/acme/tokens');
    expect(sent.request.headers.get('If-None-Match')).toBe('W/"tokens"');
    sent.flush(null, { status: 304, statusText: 'Not Modified' });

    expect((await again).total).toBe(3);
  });

  it('revokes a token by its id in the tenant, with no body and no key', async () => {
    const done = service.revoke('acme', 't1');

    const sent = http.expectOne((request) => request.url === '/api/v1/tenants/acme/tokens/t1');
    expect(sent.request.method).toBe('DELETE');
    expect(sent.request.body).toBeNull();
    expect(sent.request.headers.has('Idempotency-Key')).toBe(false);
    sent.flush(null, { status: 204, statusText: 'No Content' });

    await done;
  });
});
