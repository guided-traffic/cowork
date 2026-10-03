import { TestBed } from '@angular/core/testing';
import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { VersionService, VersionInfo } from './version.service';

describe('VersionService', () => {
  let service: VersionService;
  let http: HttpTestingController;

  beforeEach(() => {
    TestBed.configureTestingModule({
      providers: [provideHttpClient(), provideHttpClientTesting()],
    });
    service = TestBed.inject(VersionService);
    http = TestBed.inject(HttpTestingController);
  });

  afterEach(() => http.verify());

  it('reads the backend version', async () => {
    const expected: VersionInfo = { version: '1.2.3', commit: 'abc', build_time: '42' };
    const result = new Promise<VersionInfo>((resolve) => service.get().subscribe(resolve));

    const req = http.expectOne('/api/v1/version');
    expect(req.request.method).toBe('GET');
    req.flush(expected);

    expect(await result).toEqual(expected);
  });
});
