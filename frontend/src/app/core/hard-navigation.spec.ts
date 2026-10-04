import { TestBed } from '@angular/core/testing';
import { HARD_NAVIGATION, RELOAD } from './hard-navigation';

describe('HARD_NAVIGATION', () => {
  afterEach(() => vi.unstubAllGlobals());

  it('loads the path it is given as a new document, by the location of the window', () => {
    const assign = vi.fn<(url: string) => void>();
    vi.stubGlobal('location', { assign });

    TestBed.inject(HARD_NAVIGATION)('/login');

    expect(assign).toHaveBeenCalledExactlyOnceWith('/login');
  });

  it('keeps the query of the path, which is where the way back travels', () => {
    const assign = vi.fn<(url: string) => void>();
    vi.stubGlobal('location', { assign });

    TestBed.inject(HARD_NAVIGATION)('/password?return=%2Ft%2Facme');

    expect(assign).toHaveBeenCalledExactlyOnceWith('/password?return=%2Ft%2Facme');
  });

  it('is there without a provider, because it is provided in root', () => {
    expect(typeof TestBed.inject(HARD_NAVIGATION)).toBe('function');
  });

  it('can be replaced by a test, which keeps the test from leaving its own document', () => {
    const navigate = vi.fn<(url: string) => void>();
    TestBed.configureTestingModule({
      providers: [{ provide: HARD_NAVIGATION, useValue: navigate }],
    });

    TestBed.inject(HARD_NAVIGATION)('/login');

    expect(navigate).toHaveBeenCalledExactlyOnceWith('/login');
  });
});

describe('RELOAD', () => {
  afterEach(() => vi.unstubAllGlobals());

  it('loads the document again, by the location of the window', () => {
    const reload = vi.fn<() => void>();
    vi.stubGlobal('location', { reload });

    TestBed.inject(RELOAD)();

    expect(reload).toHaveBeenCalledOnce();
  });
});
