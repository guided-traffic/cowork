import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { signal, WritableSignal } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { By } from '@angular/platform-browser';
import { provideRouter, Router } from '@angular/router';
import { MessageService } from 'primeng/api';
import type { MockInstance } from 'vitest';
import { provideApiConfiguration } from '../../api/api-configuration';
import { ImportFile, ImportJob, Member, Problem, ProblemCode } from '../../api/models';
import { MembersService } from '../../core/members.service';
import { SessionService } from '../../core/session.service';
import { TenantService } from '../../core/tenant.service';
import { Clock } from '../../shared/time';
import { notesOf, ProjectImport } from './project-import';

const id = '0199a3c2-1d2e-7f00-8000-00000000000a';
const jobUrl = `/api/v1/tenants/acme/projects/VKO/imports/${id}`;
const ada = { id: 'p-ada', display_name: 'Ada Lovelace' };

const member = (person: { id: string; display_name: string }): Member => ({
  person,
  role: 'member',
  origins: [{ source: 'grant', role: 'member' }],
  local: true,
  email: null,
});

function reported(path: string, overrides: Partial<ImportFile> = {}): ImportFile {
  return {
    path,
    outcome: 'create',
    reason: null,
    format: 'repository',
    number: null,
    key: null,
    conflict: null,
    title: null,
    type: 'task',
    type_reason: 'named by the file',
    state: 'filed',
    block_candidate: null,
    block: null,
    columns: null,
    assignee: null,
    parent: null,
    note: null,
    questions: [],
    links: [],
    attachments: [],
    confidential: false,
    confidential_reason: null,
    warnings: [],
    errors: [],
    correction: null,
    ...overrides,
  };
}

/** A file of every outcome a dry run knows, and two more to create. */
const files: ImportFile[] = [
  reported('docs/tickets/001-the-export-fails.md', {
    number: 1,
    key: 'acme/VKO-1',
    title: 'the export fails',
    type: 'bug',
    type_reason: 'detected: the title says "fails"',
    links: [{ type: 'blocks', direction: 'outgoing', key: 'acme/VKO-6', source: 'blocked-by' }],
    questions: [
      { number: 1, question: 'Which format?', status: 'open' },
      { number: 2, question: 'Who?', status: 'answered' },
    ],
    warnings: [
      {
        field: 'opened',
        line: null,
        message: 'the file names no opened date; the ticket is opened at the execution',
      },
    ],
  }),
  reported('docs/tickets/002-taken.md', {
    outcome: 'conflict',
    number: 2,
    key: 'acme/VKO-2',
    title: 'taken',
    conflict: 'acme/VKO-2',
  }),
  reported('docs/tickets/003-broken.md', {
    outcome: 'error',
    number: 3,
    key: 'acme/VKO-3',
    title: 'broken',
    state: null,
    errors: [{ field: 'severity', line: 4, message: 'severity "urgent" is no severity' }],
  }),
  reported('docs/tickets/README.md', {
    outcome: 'skip',
    reason: 'no ticket file: its name is not NNN-<slug>.md',
    format: null,
    type: null,
    type_reason: null,
    state: null,
  }),
  reported('docs/tickets/local_005-a-finding.md', {
    number: 5,
    key: 'acme/VKO-5',
    title: 'a finding',
    state: 'in-progress',
    block_candidate: 'human',
    assignee: { source: 'Ada Lovelace <local:ada>', person: ada },
    confidential: true,
    confidential_reason: 'the file name carries the local_ prefix',
  }),
  reported('docs/tickets/archive/006-shipped.md', {
    number: 6,
    key: 'acme/VKO-6',
    title: 'shipped',
    state: 'done',
    note: 'imported from archive; the source carried no verification note',
  }),
];

function dryRun(overrides: Partial<ImportJob> = {}): ImportJob {
  return {
    id,
    project: 'VKO',
    status: 'dry_run',
    created_by: ada,
    created_at: '2026-10-07T08:00:00Z',
    expires_at: '2026-10-08T08:00:00Z',
    executed_by: null,
    executed_at: null,
    summary: {
      files: 6,
      create: 3,
      conflict: 1,
      error: 1,
      skip: 1,
      exclude: 0,
      created: 0,
      open: 2,
      confidential: 1,
      highest_number: 6,
    },
    files,
    ...overrides,
  };
}

function executed(): ImportJob {
  return dryRun({
    status: 'executed',
    expires_at: null,
    executed_by: ada,
    executed_at: '2026-10-07T08:30:00Z',
    summary: { ...dryRun().summary, create: 0, conflict: 0, error: 0, exclude: 2, created: 3 },
    files: files.map((file) =>
      file.outcome === 'create'
        ? { ...file, outcome: 'created' }
        : file.outcome === 'skip'
          ? file
          : { ...file, outcome: 'exclude', reason: 'left out by a correction' },
    ),
  });
}

function problem(status: number, code: ProblemCode, errors: Problem['errors'] = []): Problem {
  return {
    type: 'about:blank',
    title: 'Refused',
    status,
    detail: 'The server says no',
    code,
    errors,
  };
}

describe('notesOf', () => {
  it('puts the refusal, the errors and the conflict first, then the warnings, then what was read', () => {
    const file = reported('a.md', {
      outcome: 'error',
      errors: [{ field: 'state', line: 3, message: 'no state' }],
      conflict: 'acme/VKO-1',
      warnings: [{ field: 'body', line: null, message: 'a warning' }],
      block: { kind: 'ticket', reason: 'waits', from: 'decided', ticket: 'acme/VKO-9' },
      confidential: true,
      confidential_reason: 'the local_ prefix',
      note: 'shipped in 0.3.0',
      parent: 'acme/VKO-4',
      links: [
        { type: 'relates-to', direction: 'incoming', key: 'acme/VKO-8', source: 'links.json' },
      ],
      reason: 'why',
    });

    expect(notesOf(file, ['refused: conflict'])).toEqual([
      { tone: 'error', place: 'execution', text: 'refused: conflict' },
      { tone: 'error', place: 'line 3 · state', text: 'no state' },
      {
        tone: 'error',
        place: 'number',
        text: 'VKO-1 holds this number in the project',
      },
      { tone: 'warn', place: 'body', text: 'a warning' },
      { tone: 'info', place: 'block', text: 'ticket: waits (from decided, waiting on VKO-9)' },
      { tone: 'info', place: 'confidential', text: 'the local_ prefix' },
      { tone: 'info', place: 'note', text: 'shipped in 0.3.0' },
      { tone: 'info', place: 'parent', text: 'VKO-4' },
      { tone: 'info', place: 'links', text: 'VKO-8 relates-to this one' },
      { tone: 'info', place: 'error', text: 'why' },
    ]);
  });

  it('names a dropped ticket’s note its reason, and a flag left unset why', () => {
    const file = reported('a.md', {
      state: 'dropped',
      note: 'no longer needed',
      confidential_reason: 'publication-accepted 2026-10-01',
    });

    expect(notesOf(file)).toEqual([
      { tone: 'info', place: 'not confidential', text: 'publication-accepted 2026-10-01' },
      { tone: 'info', place: 'reason', text: 'no longer needed' },
    ]);
  });
});

describe('ProjectImport', () => {
  let http: HttpTestingController;
  let tenant: WritableSignal<string | null>;
  let canWrite: WritableSignal<boolean>;
  let meLoading: WritableSignal<boolean>;
  let now: WritableSignal<number>;
  let navigate: MockInstance<Router['navigate']>;

  beforeEach(() => {
    tenant = signal<string | null>('acme');
    canWrite = signal(true);
    meLoading = signal(false);
    now = signal(Date.parse('2026-10-07T09:00:00Z'));
    TestBed.configureTestingModule({
      providers: [
        provideRouter([]),
        provideHttpClient(),
        provideHttpClientTesting(),
        provideApiConfiguration(''),
        MessageService,
        { provide: SessionService, useValue: { tenant, me: { isLoading: meLoading } } },
        { provide: TenantService, useValue: { canWrite } },
        {
          provide: MembersService,
          useValue: { list: signal([member(ada), member({ id: 'p-sam', display_name: 'Sam' })]) },
        },
        { provide: Clock, useValue: { now } },
      ],
    });
    http = TestBed.inject(HttpTestingController);
    navigate = vi.spyOn(TestBed.inject(Router), 'navigate').mockResolvedValue(true);
  });

  afterEach(() => {
    http.verify();
    vi.restoreAllMocks();
    // The confirmation renders into the document's body, beside the fixture.
    document.body.querySelectorAll('.p-confirmdialog').forEach((dialog) => dialog.remove());
  });

  /**
   * Runs what is due and shows it, without waiting for the requests that are open, which a test
   * answers itself: in a zoneless test bed whenStable would wait for them.
   */
  async function tick(fixture: ComponentFixture<ProjectImport>) {
    for (let round = 0; round < 3; round++) {
      await new Promise((resolve) => setTimeout(resolve));
      fixture.detectChanges();
    }
  }

  /** Lets what a click started finish, and shows it, where no request is open. */
  async function settle(fixture: ComponentFixture<ProjectImport>) {
    await tick(fixture);
    await fixture.whenStable();
  }

  async function render(job?: string) {
    const fixture = TestBed.createComponent(ProjectImport);
    fixture.componentRef.setInput('project', 'VKO');
    if (job !== undefined) {
      fixture.componentRef.setInput('job', job);
    }
    await tick(fixture);
    return fixture;
  }

  /** The page of the job, its report answered as given. */
  async function open(report: ImportJob = dryRun()) {
    const fixture = await render(id);
    const request = http.expectOne(jobUrl);
    expect(request.request.method).toBe('GET');
    request.flush(report);
    await settle(fixture);
    return fixture;
  }

  const host = (fixture: ComponentFixture<ProjectImport>) => fixture.nativeElement as HTMLElement;
  const el = (fixture: ComponentFixture<ProjectImport>, testId: string) =>
    host(fixture).querySelector<HTMLElement>(`[data-testid="${testId}"]`);
  const text = (element: Element | null | undefined) =>
    element?.textContent?.replace(/\s+/g, ' ').trim();
  const debug = (fixture: ComponentFixture<ProjectImport>, testId: string) =>
    fixture.debugElement.query(By.css(`[data-testid="${testId}"]`));
  async function change(fixture: ComponentFixture<ProjectImport>, testId: string, value: unknown) {
    debug(fixture, testId).triggerEventHandler('ngModelChange', value);
    await settle(fixture);
  }
  const dialog = () => document.body.querySelector('.p-confirmdialog');
  const press = (label: string) =>
    [...(dialog()?.querySelectorAll('button') ?? [])]
      .find((button) => button.textContent?.trim() === label)
      ?.dispatchEvent(new MouseEvent('click', { bubbles: true }));
  const executeButton = (fixture: ComponentFixture<ProjectImport>) =>
    el(fixture, 'import-execute') as HTMLButtonElement | null;

  /** Leaves out the conflict and the error by hand, as a person may: the execution would anyway. */
  async function leaveOutTheBlocking(fixture: ComponentFixture<ProjectImport>) {
    await change(fixture, 'exclude-docs/tickets/002-taken.md', true);
    await change(fixture, 'exclude-docs/tickets/003-broken.md', true);
  }

  describe('who', () => {
    it('tells a viewer that the import is a writer’s, and asks nothing', async () => {
      canWrite.set(false);

      const fixture = await render(id);

      expect(text(el(fixture, 'import-not-writer'))).toContain('a viewer imports nothing');
      expect(el(fixture, 'import-drop')).toBeNull();
      http.expectNone(jobUrl);
    });

    it('waits for the person before it says so', async () => {
      canWrite.set(false);
      meLoading.set(true);

      const fixture = await render();

      expect(el(fixture, 'import-not-writer')).toBeNull();
      expect(host(fixture).querySelector('p-skeleton')).not.toBeNull();
    });
  });

  describe('the files and the dry run', () => {
    const markdown = (name: string) => new File([`---\ntitle: ${name}\n---\n`], name);

    async function pick(fixture: ComponentFixture<ProjectImport>, picked: File[]) {
      const input = el(fixture, 'import-input') as HTMLInputElement;
      Object.defineProperty(input, 'files', { value: picked, configurable: true });
      input.dispatchEvent(new Event('change'));
      await settle(fixture);
    }

    const chosen = (fixture: ComponentFixture<ProjectImport>) =>
      [...(el(fixture, 'import-chosen')?.querySelectorAll('.name') ?? [])].map((name) =>
        name.textContent?.trim(),
      );

    it('offers a drop zone and a picker of several files, and no dry run before there is one', async () => {
      const fixture = await render();

      const input = el(fixture, 'import-input') as HTMLInputElement;
      expect(input.multiple).toBe(true);
      expect(input.getAttribute('aria-label')).toBe('Ticket files');
      expect(text(el(fixture, 'import-drop'))).toContain('Drop the files here, or');
      expect((el(fixture, 'import-dry-run') as HTMLButtonElement).disabled).toBe(true);
    });

    it('lists the files picked and dropped, one of a name already chosen taking its place', async () => {
      const fixture = await render();

      await pick(fixture, [markdown('001-a.md'), markdown('002-b.md')]);
      const dropped = new File(['other'], '001-a.md');
      debug(fixture, 'import-drop').triggerEventHandler('drop', {
        preventDefault: () => undefined,
        dataTransfer: { files: [dropped, markdown('tickets.zip')] },
      });
      await settle(fixture);

      expect(chosen(fixture)).toEqual(['002-b.md', '001-a.md', 'tickets.zip']);
      expect((el(fixture, 'import-input') as HTMLInputElement).value).toBe('');
    });

    it('leaves a dropped folder out and says how to pack it', async () => {
      const fixture = await render();
      const folder = new File([], 'tickets');
      const entry = (file: File, isDirectory: boolean) => ({
        kind: 'file',
        webkitGetAsEntry: () => ({ isDirectory, name: file.name }),
      });
      const readme = markdown('README.md');

      debug(fixture, 'import-drop').triggerEventHandler('drop', {
        preventDefault: () => undefined,
        dataTransfer: {
          files: [folder, readme],
          items: [entry(folder, true), entry(readme, false)],
        },
      });
      await settle(fixture);

      expect(chosen(fixture)).toEqual(['README.md']);
      expect(text(el(fixture, 'import-folders'))).toContain(
        'A folder is not uploaded (tickets): pack it — tar czf tickets.tar.gz docs/tickets',
      );

      await pick(fixture, [markdown('001-a.md')]);
      expect(el(fixture, 'import-folders')).toBeNull();
    });

    it('marks the zone while files are dragged over it', async () => {
      const fixture = await render();
      const zone = debug(fixture, 'import-drop');

      zone.triggerEventHandler('dragover', { preventDefault: () => undefined });
      await settle(fixture);
      expect(el(fixture, 'import-drop')?.classList).toContain('over');

      zone.triggerEventHandler('dragleave', {
        currentTarget: zone.nativeElement,
        relatedTarget: null,
      });
      await settle(fixture);
      expect(el(fixture, 'import-drop')?.classList).not.toContain('over');
    });

    it('takes a file out of the upload', async () => {
      const fixture = await render();
      await pick(fixture, [markdown('001-a.md'), markdown('002-b.md')]);

      host(fixture).querySelector<HTMLButtonElement>('[aria-label="Remove 001-a.md"]')?.click();
      await settle(fixture);

      expect(chosen(fixture)).toEqual(['002-b.md']);
    });

    it("sends the files as parts named file and opens the dry run's report at its own address", async () => {
      const fixture = await render();
      await pick(fixture, [markdown('001-a.md'), markdown('002-b.md')]);

      el(fixture, 'import-dry-run')?.click();
      await tick(fixture);
      const request = http.expectOne('/api/v1/tenants/acme/projects/VKO/imports');
      expect(request.request.method).toBe('POST');
      const body = request.request.body as FormData;
      expect(body.getAll('file').map((part) => (part as File).name)).toEqual([
        '001-a.md',
        '002-b.md',
      ]);
      expect((el(fixture, 'import-dry-run') as HTMLButtonElement).disabled).toBe(true);
      request.flush(dryRun(), { status: 201, statusText: 'Created' });
      await settle(fixture);

      expect(navigate).toHaveBeenCalledExactlyOnceWith(['/t', 'acme', 'p', 'VKO', 'imports', id]);
    });

    it('says why a dry run is refused, and offers it again', async () => {
      const fixture = await render();
      await pick(fixture, [markdown('tickets.tar.gz')]);

      el(fixture, 'import-dry-run')?.click();
      await tick(fixture);
      http
        .expectOne('/api/v1/tenants/acme/projects/VKO/imports')
        .flush(
          problem(400, 'validation_failed', [
            { pointer: '/file', message: 'the upload names docs/a.md twice' },
          ]),
          { status: 400, statusText: 'Bad Request' },
        );
      await settle(fixture);

      expect(text(el(fixture, 'import-upload-refusal'))).toBe(
        'Refused. the upload names docs/a.md twice',
      );
      expect((el(fixture, 'import-dry-run') as HTMLButtonElement).disabled).toBe(false);
      expect(navigate).not.toHaveBeenCalled();
    });

    it('names the bound an upload is above', async () => {
      const fixture = await render();
      await pick(fixture, [markdown('big.zip')]);

      el(fixture, 'import-dry-run')?.click();
      await tick(fixture);
      http
        .expectOne('/api/v1/tenants/acme/projects/VKO/imports')
        .flush(problem(413, 'payload_too_large'), { status: 413, statusText: 'Too Large' });
      await settle(fixture);

      expect(text(el(fixture, 'import-upload-refusal'))).toBe('Refused. The server says no');
    });
  });

  describe('the report', () => {
    const row = (fixture: ComponentFixture<ProjectImport>, path: string) =>
      el(fixture, `file-${path}`);

    it('reads the job at its address and shows the summary first', async () => {
      const fixture = await open();

      expect(text(el(fixture, 'import-status'))).toContain('A dry run by Ada Lovelace');
      expect(text(el(fixture, 'summary-files'))).toBe('6');
      expect(text(el(fixture, 'summary-create'))).toBe('3');
      expect(text(el(fixture, 'summary-conflict'))).toBe('1');
      expect(text(el(fixture, 'summary-error'))).toBe('1');
      expect(text(el(fixture, 'summary-skip'))).toBe('1');
      expect(text(el(fixture, 'import-totals'))).toBe(
        'Of the tickets to create, 2 open and 1 confidential. The highest number is 6; VKO numbers its next ticket after it.',
      );
    });

    it('shows every file in its order with its outcome as the API spells it', async () => {
      const fixture = await open();

      const rows = [...host(fixture).querySelectorAll('tbody[data-testid^="file-"]')];
      expect(rows.map((each) => each.getAttribute('data-outcome'))).toEqual([
        'create',
        'conflict',
        'error',
        'skip',
        'create',
        'create',
      ]);
      expect(text(el(fixture, 'outcome-docs/tickets/002-taken.md'))).toBe('conflict');
    });

    it('shows a file to create with its key, title, type and why, its links, questions and warnings', async () => {
      const fixture = await open();
      const first = row(fixture, 'docs/tickets/001-the-export-fails.md');

      expect(text(first?.querySelector('.line'))).toBe('VKO-1 the export fails');
      expect(text(first?.querySelector('.path'))).toBe(
        'docs/tickets/001-the-export-fails.md · repository',
      );
      expect(text(first?.querySelector('.col-type .why'))).toBe('detected: the title says "fails"');
      expect(text(first?.querySelector('.col-questions'))).toBe('2');
      const notes = [...(first?.querySelectorAll('.notes li') ?? [])].map((note) => text(note));
      expect(notes).toEqual([
        'opened the file names no opened date; the ticket is opened at the execution',
        'links blocks VKO-6',
      ]);
      expect(first?.querySelector('.notes li.warn')).not.toBeNull();
    });

    it('shows the confidential flag with its reason, and the assignee the identity resolved to', async () => {
      const fixture = await open();
      const finding = row(fixture, 'docs/tickets/local_005-a-finding.md');

      expect(text(finding?.querySelector('.lock'))).toBe('confidential');
      expect(text(finding?.querySelector('.notes'))).toContain(
        'confidential the file name carries the local_ prefix',
      );
      const assignee = debug(fixture, 'assignee-docs/tickets/local_005-a-finding.md');
      expect(assignee.componentInstance.value).toBe('p-ada');
    });

    it('shows an error with its line and its field, and a conflict with the key that holds the number', async () => {
      const fixture = await open();

      const broken = row(fixture, 'docs/tickets/003-broken.md');
      expect(text(broken?.querySelector('.notes li.error'))).toBe(
        'line 4 · severity severity "urgent" is no severity',
      );
      const taken = row(fixture, 'docs/tickets/002-taken.md');
      expect(text(taken?.querySelector('.notes li.error'))).toBe(
        'number VKO-2 holds this number in the project',
      );
      // A conflict is only ever left out: no type, state or assignee to correct.
      expect(taken?.querySelector('p-select')).toBeNull();
      expect(taken?.querySelector('p-checkbox')).not.toBeNull();
    });

    it('shows a skipped file with its reason and takes no correction of it', async () => {
      const fixture = await open();
      const readme = row(fixture, 'docs/tickets/README.md');

      expect(text(readme?.querySelector('.notes'))).toBe(
        'skip no ticket file: its name is not NNN-<slug>.md',
      );
      expect(readme?.querySelector('p-checkbox')).toBeNull();
      expect(readme?.querySelector('p-select')).toBeNull();
    });

    it('shows the note a ticket imported as done carries', async () => {
      const fixture = await open();

      expect(
        text(row(fixture, 'docs/tickets/archive/006-shipped.md')?.querySelector('.notes')),
      ).toBe('note imported from archive; the source carried no verification note');
    });

    it('shows the files of one outcome', async () => {
      const fixture = await open();

      await change(fixture, 'import-filter', 'conflict');

      const rows = [...host(fixture).querySelectorAll('tbody[data-testid^="file-"]')];
      expect(rows.map((each) => each.getAttribute('data-testid'))).toEqual([
        'file-docs/tickets/002-taken.md',
      ]);
    });
  });

  describe('what the execution leaves out', () => {
    it('names the conflict and the error, marks their rows, and executes the rest', async () => {
      const fixture = await open();

      const notice = el(fixture, 'import-left-out');
      expect(text(notice?.querySelector('h2'))).toBe('2 files will be left out');
      expect(text(notice?.querySelector('ul'))).toBe(
        'docs/tickets/002-taken.md — conflict: VKO-2 holds its number docs/tickets/003-broken.md — error: severity "urgent" is no severity',
      );
      for (const path of ['docs/tickets/002-taken.md', 'docs/tickets/003-broken.md']) {
        expect(text(el(fixture, `left-out-${path}`))).toBe('will be left out');
      }
      expect(el(fixture, 'left-out-docs/tickets/001-the-export-fails.md')).toBeNull();
      expect(executeButton(fixture)?.disabled).toBe(false);
      expect(text(executeButton(fixture))).toBe('Import 3 tickets');
      expect(text(el(fixture, 'import-execute-text'))).toBe('3 of 6 files to import, 2 left out.');
    });

    it('takes a file left out by hand as excluded', async () => {
      const fixture = await open();

      await leaveOutTheBlocking(fixture);

      expect(el(fixture, 'import-left-out')).toBeNull();
      expect(text(executeButton(fixture))).toBe('Import 3 tickets');
      expect(text(el(fixture, 'import-execute-text'))).toBe('3 of 6 files to import, 2 left out.');
      expect(el(fixture, 'file-docs/tickets/002-taken.md')?.classList).toContain('excluded');
      expect(text(el(fixture, 'outcome-docs/tickets/002-taken.md'))).toBe('exclude');
    });

    it('takes a correction of a file with an error as an answer the execution checks', async () => {
      const fixture = await open();
      await change(fixture, 'exclude-docs/tickets/002-taken.md', true);

      await change(fixture, 'state-docs/tickets/003-broken.md', 'analysed');

      expect(el(fixture, 'import-left-out')).toBeNull();
      expect(el(fixture, 'left-out-docs/tickets/003-broken.md')).toBeNull();
      expect(text(executeButton(fixture))).toBe('Import 4 tickets');
    });

    it('offers nothing to execute once every ticket file is left out', async () => {
      const fixture = await open();
      await leaveOutTheBlocking(fixture);
      for (const path of [
        'docs/tickets/001-the-export-fails.md',
        'docs/tickets/local_005-a-finding.md',
        'docs/tickets/archive/006-shipped.md',
      ]) {
        await change(fixture, `exclude-${path}`, true);
      }

      expect(executeButton(fixture)?.disabled).toBe(true);
      expect(text(el(fixture, 'import-execute-text'))).toBe(
        'Nothing to import: every ticket file is left out.',
      );
    });

    it('says when the dry run’s day is over by the clock, and offers no execution', async () => {
      const fixture = await open();
      await leaveOutTheBlocking(fixture);

      now.set(Date.parse('2026-10-08T08:00:01Z'));
      await settle(fixture);

      expect(el(fixture, 'import-lapsed')).not.toBeNull();
      expect(executeButton(fixture)?.disabled).toBe(true);
    });
  });

  describe('the corrections and the execution', () => {
    it('asks first, saying that an import creates every ticket at once and is undone only one by one', async () => {
      const fixture = await open();
      await leaveOutTheBlocking(fixture);

      executeButton(fixture)?.click();
      await settle(fixture);

      expect(text(dialog()?.querySelector('.p-dialog-title'))).toBe('Import 3 tickets into VKO?');
      expect(text(dialog()?.querySelector('.p-confirmdialog-message'))).toBe(
        '3 tickets — 2 open, 1 confidential — are created at once, with their questions and the links among them, and you are their reporter. An import cannot be undone as a whole: a ticket it created goes only by deleting it on its own page, one at a time.',
      );
      press('Not yet');
      await settle(fixture);

      http.expectNone(`${jobUrl}/execution`);
    });

    it('sends every correction in the order of the report', async () => {
      const fixture = await open();
      await leaveOutTheBlocking(fixture);
      const first = 'docs/tickets/001-the-export-fails.md';
      await change(fixture, `type-${first}`, 'feature');
      await change(fixture, `state-${first}`, 'blocked');
      await change(fixture, `block-kind-${first}`, 'external');
      const reason = el(fixture, `block-reason-${first}`) as HTMLInputElement;
      reason.value = '  the provider opens the firewall ';
      reason.dispatchEvent(new Event('input'));
      await settle(fixture);
      await change(fixture, 'assignee-docs/tickets/local_005-a-finding.md', null);
      await change(fixture, 'state-docs/tickets/archive/006-shipped.md', 'analysed');

      executeButton(fixture)?.click();
      await settle(fixture);
      press('Import 3 tickets');
      await tick(fixture);

      const request = http.expectOne(`${jobUrl}/execution`);
      expect(request.request.method).toBe('POST');
      expect(request.request.body).toEqual({
        corrections: [
          {
            path: first,
            type: 'feature',
            state: 'blocked',
            block: { kind: 'external', reason: 'the provider opens the firewall', from: 'filed' },
          },
          { path: 'docs/tickets/002-taken.md', exclude: true },
          { path: 'docs/tickets/003-broken.md', exclude: true },
          { path: 'docs/tickets/local_005-a-finding.md', assignee: null },
          { path: 'docs/tickets/archive/006-shipped.md', state: 'analysed' },
        ],
      });
      request.flush(executed());
      await settle(fixture);
    });

    it('offers the kind a blocked-by names, and waits for the reason of the block', async () => {
      const fixture = await open();
      await leaveOutTheBlocking(fixture);
      const finding = 'docs/tickets/local_005-a-finding.md';

      el(fixture, `block-it-${finding}`)?.click();
      await settle(fixture);

      expect(debug(fixture, `state-${finding}`).componentInstance.value).toBe('blocked');
      expect(debug(fixture, `block-kind-${finding}`).componentInstance.value).toBe('human');
      expect(debug(fixture, `block-from-${finding}`).componentInstance.value).toBe('in-progress');
      expect(executeButton(fixture)?.disabled).toBe(true);
      expect(text(el(fixture, 'import-execute-text'))).toBe(
        `${finding}: The block needs its reason.`,
      );
      expect(text(el(fixture, `file-${finding}`)?.querySelector('.notes li.error'))).toBe(
        'The block needs its reason.',
      );
    });

    it('shows what the execution created, with the keys as links, and leads to the backlog', async () => {
      const fixture = await open();
      await leaveOutTheBlocking(fixture);
      executeButton(fixture)?.click();
      await settle(fixture);
      press('Import 3 tickets');
      await tick(fixture);

      http.expectOne(`${jobUrl}/execution`).flush(executed());
      await settle(fixture);

      expect(text(el(fixture, 'import-done')?.querySelector('h2'))).toBe(
        '3 tickets imported into VKO',
      );
      expect(el(fixture, 'import-backlog')?.getAttribute('href')).toBe('/t/acme/p/VKO/backlog');
      expect(el(fixture, 'import-board')?.getAttribute('href')).toBe('/t/acme/p/VKO/board');
      expect(text(el(fixture, 'summary-created'))).toBe('3');
      expect(
        el(fixture, 'file-docs/tickets/001-the-export-fails.md')
          ?.querySelector('a.number')
          ?.getAttribute('href'),
      ).toBe('/t/acme/tickets/VKO-1');
      expect(host(fixture).querySelector('p-checkbox, p-select')).toBeNull();
      expect(el(fixture, 'import-execute-bar')).toBeNull();
    });

    it('executes with the conflict and the error in it, which the execution leaves out', async () => {
      const fixture = await open();
      executeButton(fixture)?.click();
      await settle(fixture);
      press('Import 3 tickets');
      await tick(fixture);

      const request = http.expectOne(`${jobUrl}/execution`);
      expect(request.request.body).toEqual({ corrections: [] });
      request.flush(executed());
      await settle(fixture);

      expect(el(fixture, 'import-done')).not.toBeNull();
    });

    it('lands a refused correction on the file it corrects, and leaves it out at one click', async () => {
      const fixture = await open();
      await leaveOutTheBlocking(fixture);
      await change(fixture, 'type-docs/tickets/001-the-export-fails.md', 'decision');
      executeButton(fixture)?.click();
      await settle(fixture);
      press('Import 3 tickets');
      await tick(fixture);

      http
        .expectOne(`${jobUrl}/execution`)
        .flush(
          problem(400, 'validation_failed', [
            { pointer: '/corrections/0/type', message: 'is no type' },
          ]),
          { status: 400, statusText: 'Bad Request' },
        );
      await settle(fixture);

      expect(text(el(fixture, 'import-refusal')?.querySelector('ul'))).toBe(
        'docs/tickets/001-the-export-fails.md — is no type',
      );
      expect(
        text(el(fixture, 'file-docs/tickets/001-the-export-fails.md')?.querySelector('.notes li')),
      ).toBe('execution is no type');

      el(fixture, 'exclude-refused')?.click();
      await settle(fixture);
      expect(text(executeButton(fixture))).toBe('Import 2 tickets');
    });

    it('reads the job again when it was executed elsewhere meanwhile', async () => {
      const fixture = await open();
      await leaveOutTheBlocking(fixture);
      executeButton(fixture)?.click();
      await settle(fixture);
      press('Import 3 tickets');
      await tick(fixture);

      http
        .expectOne(`${jobUrl}/execution`)
        .flush(problem(409, 'import_executed'), { status: 409, statusText: 'Conflict' });
      await tick(fixture);
      http.expectOne(jobUrl).flush(executed());
      await settle(fixture);

      expect(el(fixture, 'import-executed-elsewhere')).not.toBeNull();
      expect(el(fixture, 'import-done')).not.toBeNull();
    });

    it('says the dry run is gone when its execution finds it expired', async () => {
      const fixture = await open();
      await leaveOutTheBlocking(fixture);
      executeButton(fixture)?.click();
      await settle(fixture);
      press('Import 3 tickets');
      await tick(fixture);

      http
        .expectOne(`${jobUrl}/execution`)
        .flush(problem(404, 'not_found'), { status: 404, statusText: 'Not Found' });
      await settle(fixture);

      expect(el(fixture, 'import-gone')).not.toBeNull();
    });

    it('toasts any other refusal and keeps the report and the corrections', async () => {
      const add = vi.spyOn(TestBed.inject(MessageService), 'add');
      const fixture = await open();
      await leaveOutTheBlocking(fixture);
      executeButton(fixture)?.click();
      await settle(fixture);
      press('Import 3 tickets');
      await tick(fixture);

      http
        .expectOne(`${jobUrl}/execution`)
        .flush(problem(504, 'timeout'), { status: 504, statusText: 'Gateway Timeout' });
      await settle(fixture);

      expect(add).toHaveBeenCalledOnce();
      expect(executeButton(fixture)?.disabled).toBe(false);
      expect(el(fixture, 'file-docs/tickets/002-taken.md')?.classList).toContain('excluded');
    });
  });

  describe('a job that cannot be read', () => {
    it('says that an expired dry run is gone and offers a new one', async () => {
      const fixture = await render(id);
      http
        .expectOne(jobUrl)
        .flush(problem(404, 'not_found'), { status: 404, statusText: 'Not Found' });
      await settle(fixture);

      expect(text(el(fixture, 'import-gone')?.querySelector('h2'))).toBe('This dry run is gone');
      expect(el(fixture, 'import-gone-new')?.getAttribute('href')).toBe('/t/acme/p/VKO/imports');
    });

    it('says why it could not be loaded, and loads it again on request', async () => {
      const fixture = await render(id);
      http
        .expectOne(jobUrl)
        .flush(problem(503, 'not_ready'), { status: 503, statusText: 'Unavailable' });
      await settle(fixture);

      expect(text(el(fixture, 'import-failed'))).toContain('The server says no');
      host(fixture)
        .querySelector<HTMLButtonElement>('[data-testid="import-failed"] button')
        ?.click();
      await tick(fixture);
      http.expectOne(jobUrl).flush(dryRun());
      await settle(fixture);

      expect(el(fixture, 'import-summary')).not.toBeNull();
    });
  });
});
