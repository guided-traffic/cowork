import { HttpErrorResponse, provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { TestBed } from '@angular/core/testing';
import { MessageService } from 'primeng/api';
import { provideApiConfiguration } from '../api/api-configuration';
import { ImportJob, Problem } from '../api/models';
import { ExportArchive, exportNote, ImportsService } from './imports.service';
import { ProblemService } from './problem.service';

const id = '0199a3c2-1d2e-7f00-8000-00000000000a';

function job(overrides: Partial<ImportJob> = {}): ImportJob {
  return {
    id,
    project: 'VKO',
    status: 'dry_run',
    created_by: { id: 'p-ada', display_name: 'Ada Lovelace' },
    created_at: '2026-10-07T08:00:00Z',
    expires_at: '2026-10-08T08:00:00Z',
    executed_by: null,
    executed_at: null,
    summary: {
      files: 0,
      create: 0,
      conflict: 0,
      error: 0,
      skip: 0,
      exclude: 0,
      created: 0,
      open: 0,
      confidential: 0,
      highest_number: null,
    },
    files: [],
    ...overrides,
  };
}

describe('ImportsService', () => {
  let service: ImportsService;
  let http: HttpTestingController;

  beforeEach(() => {
    TestBed.configureTestingModule({
      providers: [
        provideHttpClient(),
        provideHttpClientTesting(),
        provideApiConfiguration(''),
        MessageService,
      ],
    });
    service = TestBed.inject(ImportsService);
    http = TestBed.inject(HttpTestingController);
  });

  afterEach(() => http.verify());

  /** Lets the request a call makes reach the testing backend. */
  const settle = () => new Promise((resolve) => setTimeout(resolve));

  describe('the dry run (docs/adr/0051 D1)', () => {
    it('sends every file as a part named file, under its own name', async () => {
      const files = [
        new File(['---\nid: T1\n---\n'], '001-the-first.md', { type: 'text/markdown' }),
        new File(['---\nid: T2\n---\n'], '002-the-second.md', { type: 'text/markdown' }),
      ];

      const answer = service.dryRun('acme', 'VKO', files);
      await settle();
      const request = http.expectOne('/api/v1/tenants/acme/projects/VKO/imports');
      expect(request.request.method).toBe('POST');
      const body = request.request.body as FormData;
      expect(body).toBeInstanceOf(FormData);
      expect([...body.keys()]).toEqual(['file', 'file']);
      expect(body.getAll('file').map((part) => (part as File).name)).toEqual([
        '001-the-first.md',
        '002-the-second.md',
      ]);
      request.flush(job(), { status: 201, statusText: 'Created' });

      expect(await answer).toEqual(job());
    });

    it('hands the job it answered to the page of its address once, then reads it again', async () => {
      const answered = job({ summary: { ...job().summary, files: 2, create: 2 } });
      const made = service.dryRun('acme', 'VKO', [new File(['x'], '001-a.md')]);
      await settle();
      http.expectOne('/api/v1/tenants/acme/projects/VKO/imports').flush(answered);
      await made;

      expect(await service.job('acme', 'VKO', id)).toEqual(answered);
      http.expectNone(`/api/v1/tenants/acme/projects/VKO/imports/${id}`);

      const again = service.job('acme', 'VKO', id);
      await settle();
      http.expectOne(`/api/v1/tenants/acme/projects/VKO/imports/${id}`).flush(answered);
      expect(await again).toEqual(answered);
    });

    it('hands nothing over to the address of another tenant or project', async () => {
      const made = service.dryRun('acme', 'VKO', [new File(['x'], '001-a.md')]);
      await settle();
      http.expectOne('/api/v1/tenants/acme/projects/VKO/imports').flush(job());
      await made;

      const elsewhere = service.job('globex', 'VKO', id);
      await settle();
      http.expectOne(`/api/v1/tenants/globex/projects/VKO/imports/${id}`).flush(job());
      await elsewhere;
    });
  });

  describe('a job', () => {
    it('is read by its id (GET …/imports/{import})', async () => {
      const read = service.job('acme', 'VKO', id);
      await settle();
      const request = http.expectOne(`/api/v1/tenants/acme/projects/VKO/imports/${id}`);
      expect(request.request.method).toBe('GET');
      request.flush(job({ status: 'executed', expires_at: null }));

      expect((await read).status).toBe('executed');
    });

    it('is executed with the corrections as they are given (docs/adr/0051 D2)', async () => {
      const corrections = [
        { path: 'docs/tickets/001-a.md', exclude: true },
        { path: 'docs/tickets/002-b.md', type: 'bug' as const, assignee: null },
      ];

      const executed = service.execute('acme', 'VKO', id, corrections);
      await settle();
      const request = http.expectOne(`/api/v1/tenants/acme/projects/VKO/imports/${id}/execution`);
      expect(request.request.method).toBe('POST');
      expect(request.request.body).toEqual({ corrections });
      request.flush(job({ status: 'executed' }));

      expect((await executed).status).toBe('executed');
    });
  });

  describe('the export (docs/adr/0051 D4)', () => {
    it("asks for the project's archive as a blob and names it as the server does", async () => {
      const archive = new Blob(['not really gzip'], { type: 'application/gzip' });

      const exported = service.exportProject('acme', 'VKO');
      await settle();
      const request = http.expectOne('/api/v1/tenants/acme/projects/VKO/export');
      expect(request.request.method).toBe('GET');
      expect(request.request.responseType).toBe('blob');
      expect(request.request.headers.get('Accept')).toBe('application/gzip');
      request.flush(archive, {
        headers: { 'Content-Disposition': 'attachment; filename="acme-VKO-20261007.tar.gz"' },
      });

      const answer = await exported;
      expect(answer.blob).toBe(archive);
      expect(answer.filename).toBe('acme-VKO-20261007.tar.gz');
      // The bytes are no archive: the page only says less.
      expect(answer.manifest).toBeNull();
    });

    it('names an archive itself where the server gives no name', async () => {
      const project = service.exportProject('acme', 'VKO');
      await settle();
      http.expectOne('/api/v1/tenants/acme/projects/VKO/export').flush(new Blob(['x']));
      expect((await project).filename).toBe('acme-VKO-export.tar.gz');

      const tenant = service.exportTenant('acme');
      await settle();
      http.expectOne('/api/v1/tenants/acme/export').flush(new Blob(['x']));
      expect((await tenant).filename).toBe('acme-export.tar.gz');
    });

    it('reads a refusal, which comes as a blob, as the problem it is', async () => {
      const body: Problem = {
        type: 'about:blank',
        title: 'Not found',
        status: 404,
        detail: 'no project VKO',
        code: 'not_found',
      };

      const exported = service.exportTenant('acme');
      await settle();
      http
        .expectOne('/api/v1/tenants/acme/export')
        .flush(new Blob([JSON.stringify(body)], { type: 'application/problem+json' }), {
          status: 404,
          statusText: 'Not Found',
        });

      const error = await exported.catch((refusal: unknown) => refusal);
      expect(error).toBeInstanceOf(HttpErrorResponse);
      const problem = TestBed.inject(ProblemService).read(error);
      expect(problem.code).toBe('not_found');
      expect(problem.detail).toBe('no project VKO');
    });

    it('passes on a refusal whose body is no problem as it came', async () => {
      const exported = service.exportTenant('acme');
      await settle();
      http
        .expectOne('/api/v1/tenants/acme/export')
        .flush(new Blob(['<html>bad gateway</html>']), { status: 502, statusText: 'Bad Gateway' });

      const error = await exported.catch((refusal: unknown) => refusal);
      expect(TestBed.inject(ProblemService).read(error).code).toBe('backend_unreachable');
    });
  });
});

describe('exportNote', () => {
  const archive = (manifest: ExportArchive['manifest']): ExportArchive => ({
    blob: new Blob([]),
    filename: 'acme-VKO-20261007.tar.gz',
    manifest,
  });
  const manifest = (tickets: number, left: number): ExportArchive['manifest'] => ({
    format: 'cowork export v1',
    tenant: 'acme',
    projects: [],
    exported_at: '2026-10-07T08:00:00Z',
    exported_by: 'Ada Lovelace <local:ada>',
    tickets,
    confidential_not_included: left,
  });

  it('counts the tickets the archive holds', () => {
    expect(exportNote(archive(manifest(1, 0)))).toBe('1 ticket in acme-VKO-20261007.tar.gz.');
    expect(exportNote(archive(manifest(12, 0)))).toBe('12 tickets in acme-VKO-20261007.tar.gz.');
  });

  it('says how many confidential tickets it leaves out (docs/adr/0065 D5)', () => {
    expect(exportNote(archive(manifest(12, 1)))).toBe(
      '12 tickets in acme-VKO-20261007.tar.gz. 1 confidential ticket you cannot read is not included.',
    );
    expect(exportNote(archive(manifest(12, 3)))).toBe(
      '12 tickets in acme-VKO-20261007.tar.gz. 3 confidential tickets you cannot read are not included.',
    );
  });

  it('names the file alone where the manifest could not be read', () => {
    expect(exportNote(archive(null))).toBe('Saved as acme-VKO-20261007.tar.gz.');
  });
});
