import { TestBed } from '@angular/core/testing';
import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { App } from './app';

describe('App', () => {
  let http: HttpTestingController;

  beforeEach(async () => {
    await TestBed.configureTestingModule({
      imports: [App],
      providers: [provideHttpClient(), provideHttpClientTesting()],
    }).compileComponents();
    http = TestBed.inject(HttpTestingController);
  });

  afterEach(() => http.verify());

  it('renders the product name', async () => {
    const fixture = TestBed.createComponent(App);
    http.expectOne('/api/v1/version').flush({ version: '1.0.0', commit: 'abc', buildTime: '1' });
    await fixture.whenStable();

    const compiled = fixture.nativeElement as HTMLElement;
    expect(compiled.querySelector('h1')?.textContent).toContain('cowork');
  });

  it('shows the backend version once it answered', async () => {
    const fixture = TestBed.createComponent(App);
    http.expectOne('/api/v1/version').flush({ version: '1.0.0', commit: 'abc', buildTime: '1' });
    await fixture.whenStable();

    const compiled = fixture.nativeElement as HTMLElement;
    expect(compiled.querySelector('[data-testid="version"]')?.textContent).toBe('1.0.0 (abc)');
  });

  it('says so when the backend cannot be reached', async () => {
    const fixture = TestBed.createComponent(App);
    http.expectOne('/api/v1/version').flush('down', { status: 503, statusText: 'Service Unavailable' });
    await fixture.whenStable();

    const compiled = fixture.nativeElement as HTMLElement;
    expect(compiled.querySelector('[data-testid="version"]')?.textContent).toBe('backend unreachable');
  });
});
