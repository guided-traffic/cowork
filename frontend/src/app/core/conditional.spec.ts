import { HttpErrorResponse, provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { TestBed } from '@angular/core/testing';
import { Api } from '../api/api';
import { provideApiConfiguration } from '../api/api-configuration';
import { listProjects } from '../api/fn/projects/list-projects';
import { Project, ProjectList } from '../api/models';
import { ConditionalPages } from './conditional';

const project = (key: string): Project => ({
  id: `id-${key}`,
  key,
  name: `Project ${key}`,
  description: '',
  restricted: false,
  created_at: '2026-10-01T10:00:00Z',
  updated_at: '2026-10-01T10:00:00Z',
  version: 1,
  wip_limits: {},
});

const pageOf = (keys: string[], next: string | null): ProjectList => ({
  items: keys.map(project),
  next_cursor: next,
});

const notModified = { status: 304, statusText: 'Not Modified' };

describe('ConditionalPages (docs/adr/0054 D7)', () => {
  let http: HttpTestingController;
  let pages: ConditionalPages;

  beforeEach(() => {
    TestBed.configureTestingModule({
      providers: [provideHttpClient(), provideHttpClientTesting(), provideApiConfiguration('')],
    });
    http = TestBed.inject(HttpTestingController);
    pages = new ConditionalPages(TestBed.inject(Api));
  });

  afterEach(() => http.verify());

  /** The request of the page with the cursor, which the loader asks for a promise later. */
  const request = async (cursor: string | null = null) => {
    await Promise.resolve();
    await Promise.resolve();
    return http.expectOne(
      (each) =>
        each.url === '/api/v1/tenants/acme/projects' && each.params.get('cursor') === cursor,
    );
  };

  /** A load of every page of the projects, as the services follow a cursor. */
  const everyPage = () =>
    pages.load(async (page) => {
      const keys: string[] = [];
      let cursor: string | undefined;
      do {
        const next = await page(listProjects, { tenant: 'acme', cursor, limit: 200 });
        keys.push(...next.items.map((each) => each.key));
        cursor = next.next_cursor ?? undefined;
      } while (cursor);
      return keys;
    });

  it('asks without If-None-Match the first time', async () => {
    const done = everyPage();
    const first = await request();

    expect(first.request.headers.has('If-None-Match')).toBe(false);
    first.flush(pageOf(['A'], null), { headers: { ETag: 'W/"a"' } });
    expect(await done).toEqual(['A']);
  });

  it("sends each page's tag again and keeps the page on a 304, cursor by cursor", async () => {
    let done = everyPage();
    (await request()).flush(pageOf(['A', 'B'], 'c1'), { headers: { ETag: 'W/"one"' } });
    (await request('c1')).flush(pageOf(['C'], null), { headers: { ETag: 'W/"two"' } });
    expect(await done).toEqual(['A', 'B', 'C']);

    done = everyPage();
    const first = await request();
    expect(first.request.headers.get('If-None-Match')).toBe('W/"one"');
    first.flush(null, notModified);
    const second = await request('c1');
    expect(second.request.headers.get('If-None-Match')).toBe('W/"two"');
    second.flush(null, notModified);

    expect(await done).toEqual(['A', 'B', 'C']);
  });

  it('takes a changed page and its new tag, which the next load sends', async () => {
    let done = everyPage();
    (await request()).flush(pageOf(['A'], null), { headers: { ETag: 'W/"one"' } });
    await done;

    done = everyPage();
    (await request()).flush(pageOf(['A', 'B'], null), { headers: { ETag: 'W/"two"' } });
    expect(await done).toEqual(['A', 'B']);

    done = everyPage();
    const again = await request();
    expect(again.request.headers.get('If-None-Match')).toBe('W/"two"');
    again.flush(null, notModified);
    expect(await done).toEqual(['A', 'B']);
  });

  it('sends no tag for a page it never received, and forgets the pages a load no longer asks for', async () => {
    let done = everyPage();
    (await request()).flush(pageOf(['A'], 'c1'), { headers: { ETag: 'W/"one"' } });
    (await request('c1')).flush(pageOf(['B'], null), { headers: { ETag: 'W/"two"' } });
    await done;

    // The list shrank to one page: the second is not asked for, and is forgotten.
    done = everyPage();
    (await request()).flush(pageOf(['A'], null), { headers: { ETag: 'W/"three"' } });
    await done;

    // It grew again: the second page is new to it, whatever cursor it has.
    done = everyPage();
    const first = await request();
    first.flush(pageOf(['A'], 'c1'), { headers: { ETag: 'W/"four"' } });
    const second = await request('c1');
    expect(second.request.headers.has('If-None-Match')).toBe(false);
    second.flush(pageOf(['B'], null), { headers: { ETag: 'W/"two"' } });
    expect(await done).toEqual(['A', 'B']);
  });

  it('keeps the tags it held when a load fails, and rejects with the failure', async () => {
    let done = everyPage();
    (await request()).flush(pageOf(['A'], null), { headers: { ETag: 'W/"one"' } });
    await done;

    done = everyPage();
    (await request()).flush(
      { type: 'about:blank', title: 'Unavailable', status: 503, code: 'not_ready' },
      { status: 503, statusText: 'Service Unavailable' },
    );
    await expect(done).rejects.toBeInstanceOf(HttpErrorResponse);

    done = everyPage();
    const again = await request();
    expect(again.request.headers.get('If-None-Match')).toBe('W/"one"');
    again.flush(null, notModified);
    expect(await done).toEqual(['A']);
  });

  it('holds nothing for an answer without a tag', async () => {
    let done = everyPage();
    (await request()).flush(pageOf(['A'], null));
    await done;

    done = everyPage();
    const again = await request();
    expect(again.request.headers.has('If-None-Match')).toBe(false);
    again.flush(pageOf(['A'], null));
    await done;
  });

  it('passes on a 304 it holds no page for: nothing asked for one', async () => {
    const done = everyPage();
    (await request()).flush(null, notModified);

    const failure = await done.then(
      () => null,
      (error: unknown) => error,
    );
    expect((failure as HttpErrorResponse).status).toBe(304);
  });
});
