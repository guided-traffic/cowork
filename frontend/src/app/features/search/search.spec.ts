import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { computed, signal, WritableSignal } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideRouter } from '@angular/router';
import { MessageService } from 'primeng/api';
import { provideApiConfiguration } from '../../api/api-configuration';
import { Membership, SearchHit, SearchHitList } from '../../api/models';
import { SessionService } from '../../core/session.service';
import { foundIn, hitFragment, SearchResults, SearchScope } from './search';

const acme: Membership = {
  role: 'member',
  tenant: { name: 'Acme Corp', slug: 'acme' },
  origins: [{ source: 'grant', role: 'member' }],
};
const globex: Membership = {
  role: 'member',
  tenant: { name: 'Globex', slug: 'globex' },
  origins: [{ source: 'grant', role: 'member' }],
};

function hit(overrides: Partial<SearchHit> = {}): SearchHit {
  return {
    tenant: { slug: 'acme', name: 'Acme Corp' },
    key: 'acme/COW-12',
    title: 'The gate fails',
    type: 'bug',
    state: 'in-progress',
    found_in: 'ticket',
    comment: null,
    question: null,
    snippet: [
      { text: 'the ', match: false },
      { text: 'gate', match: true },
      { text: ' fails on <b>a fresh</b> cluster', match: false },
    ],
    ...overrides,
  };
}

const page = (items: SearchHit[], next: string | null = null): SearchHitList => ({
  items,
  next_cursor: next,
});

describe('foundIn and hitFragment', () => {
  it.each([
    [hit({ found_in: 'key' }), 'by its key', undefined],
    [hit(), 'in the ticket', undefined],
    [hit({ found_in: 'comment', comment: 'c-7' }), 'in a comment', 'comment-c-7'],
    [hit({ found_in: 'question', question: 2 }), 'in question Q2', 'question-2'],
    [hit({ found_in: 'attachment' }), 'in a file name', undefined],
  ] as const)('says where %j was found: %s', (h, said, fragment) => {
    expect(foundIn(h)).toBe(said);
    expect(hitFragment(h)).toBe(fragment);
  });
});

describe('SearchResults', () => {
  let tenant: WritableSignal<string | null>;
  let memberships: WritableSignal<Membership[]>;
  let http: HttpTestingController;

  beforeEach(() => {
    tenant = signal<string | null>('acme');
    memberships = signal([acme]);
    TestBed.configureTestingModule({
      providers: [
        provideRouter([]),
        provideHttpClient(),
        provideHttpClientTesting(),
        provideApiConfiguration(''),
        MessageService,
        {
          provide: SessionService,
          useValue: {
            tenant,
            memberships,
            shown: computed(() =>
              memberships()
                .map((m) => m.tenant)
                .find((t) => t.slug === tenant()),
            ),
          },
        },
      ],
    });
    http = TestBed.inject(HttpTestingController);
  });

  async function render(q: string | undefined, scope: SearchScope) {
    const fixture = TestBed.createComponent(SearchResults);
    fixture.componentRef.setInput('q', q);
    fixture.componentRef.setInput('scope', scope);
    fixture.detectChanges();
    return fixture;
  }

  async function settle(fixture: ComponentFixture<SearchResults>) {
    await new Promise((resolve) => setTimeout(resolve));
    fixture.detectChanges();
  }

  const el = (fixture: ComponentFixture<SearchResults>) => fixture.nativeElement as HTMLElement;

  it("searches the tenant the pages show and links each hit, the snippet's found words marked as text", async () => {
    const fixture = await render(' gate ', 'tenant');

    http
      .expectOne('/api/v1/tenants/acme/search?q=gate&limit=50')
      .flush(
        page([
          hit(),
          hit({ key: 'acme/COW-3', title: 'Other', found_in: 'comment', comment: 'c-7' }),
        ]),
      );
    await settle(fixture);

    const first = el(fixture).querySelector('[data-testid="hit-acme/COW-12"]') as HTMLAnchorElement;
    expect(first.getAttribute('href')).toBe('/t/acme/tickets/COW-12');
    expect(first.querySelector('.ticket-key')?.textContent).toBe('COW-12');
    expect(first.querySelector('mark')?.textContent).toBe('gate');
    expect(first.querySelector('[data-testid="snippet"]')?.textContent).toBe(
      'the gate fails on <b>a fresh</b> cluster',
    );
    expect(first.querySelector('b')).toBeNull();
    expect(first.querySelector('[data-testid="tenant"]')).toBeNull();
    const second = el(fixture).querySelector('[data-testid="hit-acme/COW-3"]') as HTMLAnchorElement;
    expect(second.getAttribute('href')).toBe('/t/acme/tickets/COW-3#comment-c-7');
    expect(second.querySelector('[data-testid="found-in"]')?.textContent?.trim()).toBe(
      'in a comment',
    );
    expect(el(fixture).querySelector('[data-testid="search-headline"]')?.textContent?.trim()).toBe(
      '2 tickets for “gate” in Acme Corp',
    );
    expect(el(fixture).querySelector('[data-testid="search-everywhere"]')).toBeNull();
  });

  it("offers every tenant's search where the person has more than one", async () => {
    memberships.set([acme, globex]);
    const fixture = await render('gate', 'tenant');
    http.expectOne('/api/v1/tenants/acme/search?q=gate&limit=50').flush(page([]));
    await settle(fixture);

    const everywhere = el(fixture).querySelector('[data-testid="search-everywhere"]');
    expect(everywhere?.getAttribute('href')).toBe('/me/search?q=gate');
    expect(el(fixture).querySelector('[data-testid="search-nothing"]')?.textContent).toBe(
      'Nothing found.',
    );
  });

  it('searches every tenant of the person, each hit beside its tenant, a page more on request', async () => {
    const fixture = await render('gate', 'me');

    http
      .expectOne('/api/v1/me/search?q=gate&limit=50')
      .flush(
        page([hit({ tenant: { slug: 'globex', name: 'Globex' }, key: 'globex/OPS-1' })], 'next-1'),
      );
    await settle(fixture);
    expect(el(fixture).querySelector('[data-testid="tenant"]')?.textContent).toBe('Globex');

    (el(fixture).querySelector('[data-testid="load-more"]') as HTMLButtonElement).click();
    fixture.detectChanges();
    http
      .expectOne('/api/v1/me/search?q=gate&limit=50')
      .flush(page([hit({ key: 'globex/OPS-1' })], 'next-1'));
    await settle(fixture);
    http.expectOne('/api/v1/me/search?q=gate&cursor=next-1&limit=50').flush(page([hit()]));
    await settle(fixture);

    expect(el(fixture).querySelectorAll('a.hit')).toHaveLength(2);
    expect(el(fixture).querySelector('[data-testid="load-more"]')).toBeNull();
  });

  it('asks nothing without words, and says what to do', async () => {
    const fixture = await render('   ', 'me');

    http.expectNone(() => true);
    expect(el(fixture).querySelector('[data-testid="search-empty-query"]')).not.toBeNull();
  });

  it('says why a search failed', async () => {
    const fixture = await render('gate', 'me');

    http.expectOne('/api/v1/me/search?q=gate&limit=50').flush(
      {
        type: 'about:blank',
        title: 'The request is invalid',
        status: 400,
        detail: 'the search is too long',
        code: 'validation_failed',
      },
      {
        status: 400,
        statusText: 'Bad Request',
        headers: { 'Content-Type': 'application/problem+json' },
      },
    );
    await settle(fixture);

    expect(el(fixture).querySelector('[data-testid="search-failed"]')?.textContent?.trim()).toBe(
      'The search failed: the search is too long',
    );
  });
});
