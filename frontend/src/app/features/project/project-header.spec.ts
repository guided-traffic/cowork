import { provideLocationMocks } from '@angular/common/testing';
import { Component, signal, WritableSignal } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { By } from '@angular/platform-browser';
import { provideRouter, Router } from '@angular/router';
import { MessageService } from 'primeng/api';
import type { MockInstance } from 'vitest';
import { HttpErrorResponse } from '@angular/common/http';
import { ExportManifest, Problem, Project, Ticket } from '../../api/models';
import { ExportArchive, ImportsService } from '../../core/imports.service';
import { MembersService } from '../../core/members.service';
import { ProjectsService } from '../../core/projects.service';
import { SessionService } from '../../core/session.service';
import { TenantService } from '../../core/tenant.service';
import { TicketActions } from '../../core/ticket-actions.service';
import { NewTicketDialog } from '../ticket/new-ticket-dialog';
import { ProjectHeader } from './project-header';

const cowork: Project = {
  id: 'id-COW',
  key: 'COW',
  name: 'Cowork',
  description: 'The tool itself',
  restricted: false,
  wip_limits: {},
  version: 1,
  created_at: '2026-10-01T09:00:00Z',
  updated_at: '2026-10-01T09:00:00Z',
};

@Component({ template: '' })
class Page {}

const manifest = (tickets: number, left: number): ExportManifest => ({
  format: 'cowork export v1',
  tenant: 'acme',
  projects: [
    { key: 'COW', name: 'Cowork', archived: false, tickets, confidential_not_included: left },
  ],
  exported_at: '2026-10-07T08:00:00Z',
  exported_by: 'Ada Lovelace <local:ada>',
  tickets,
  confidential_not_included: left,
});

describe('ProjectHeader', () => {
  let tenant: WritableSignal<string | null>;
  let projects: WritableSignal<Project[]>;
  let canWrite: WritableSignal<boolean>;
  let exportProject: MockInstance<ImportsService['exportProject']>;
  let warn: MockInstance<typeof console.warn>;

  beforeEach(() => {
    warn = vi.spyOn(console, 'warn');
    tenant = signal<string | null>('acme');
    projects = signal<Project[]>([cowork]);
    canWrite = signal(false);
    exportProject = vi.fn<ImportsService['exportProject']>();
    TestBed.configureTestingModule({
      providers: [
        provideRouter([{ path: '**', component: Page }]),
        provideLocationMocks(),
        MessageService,
        { provide: TicketActions, useValue: { create: vi.fn() } },
        { provide: MembersService, useValue: { list: signal([]) } },
        { provide: SessionService, useValue: { tenant } },
        { provide: TenantService, useValue: { canWrite } },
        { provide: ImportsService, useValue: { exportProject } },
        {
          provide: ProjectsService,
          useValue: { byKey: (key: string) => projects().find((p) => p.key === key) },
        },
      ],
    });
  });

  afterEach(() => {
    // The dialog for a new ticket is part of the header; it must not warn (NG01354).
    const warnings = warn.mock.calls.filter((call) => String(call[0]).includes('NG01354'));
    warn.mockRestore();
    expect(warnings).toEqual([]);
  });

  async function render(project = 'COW') {
    const fixture = TestBed.createComponent(ProjectHeader);
    fixture.componentRef.setInput('project', project);
    await fixture.whenStable();
    return { fixture, page: fixture.nativeElement as HTMLElement };
  }

  const text = (page: HTMLElement, selector: string) =>
    page.querySelector(selector)?.textContent?.replace(/\s+/g, ' ').trim();

  describe('the project', () => {
    it('shows the key, the name and the description', async () => {
      const { page } = await render();

      expect(text(page, '[data-testid="project-key"]')).toBe('COW');
      expect(text(page, 'h1')).toBe('Cowork');
      expect(text(page, '[data-testid="project-description"]')).toBe('The tool itself');
    });

    it('shows the key in place of the name and no description while the project is not known', async () => {
      projects.set([]);

      const { page } = await render();

      expect(text(page, 'h1')).toBe('COW');
      expect(page.querySelector('[data-testid="project-description"]')).toBeNull();
    });

    it('shows no description for a project that has none', async () => {
      projects.set([{ ...cowork, description: '' }]);

      const { page } = await render();

      expect(page.querySelector('[data-testid="project-description"]')).toBeNull();
    });

    it('follows the project of the path', async () => {
      const { fixture, page } = await render();

      fixture.componentRef.setInput('project', 'OPS');
      await fixture.whenStable();

      expect(text(page, '[data-testid="project-key"]')).toBe('OPS');
      expect(text(page, 'h1')).toBe('OPS');
    });
  });

  describe('the tabs', () => {
    const tab = (page: HTMLElement, name: 'backlog' | 'board') =>
      page.querySelector<HTMLAnchorElement>(`[data-testid="tab-${name}"]`);
    const go = async (fixture: ComponentFixture<ProjectHeader>, url: string) => {
      await TestBed.inject(Router).navigateByUrl(url);
      await fixture.whenStable();
    };

    it('are two links, Board and Backlog, in a navigation that has a name', async () => {
      const { page } = await render();

      const nav = page.querySelector('nav.tabs');
      expect(nav?.getAttribute('aria-label')).toBe('Views of the project');
      expect(
        [...(nav?.querySelectorAll('a') ?? [])].map((link) => link.textContent?.trim()),
      ).toEqual(['Board', 'Backlog']);
    });

    it('put the board first, left of the backlog', async () => {
      const { page } = await render();

      const links = [...page.querySelectorAll<HTMLAnchorElement>('nav.tabs a')];
      expect(links.map((link) => link.getAttribute('href'))).toEqual([
        '/t/acme/p/COW/board',
        '/t/acme/p/COW/backlog',
      ]);
    });

    it('lead to the board and the backlog of the project under the tenant', async () => {
      const { page } = await render();

      expect(tab(page, 'backlog')?.getAttribute('href')).toBe('/t/acme/p/COW/backlog');
      expect(tab(page, 'board')?.getAttribute('href')).toBe('/t/acme/p/COW/board');
    });

    it('follow the project of the path', async () => {
      const { fixture, page } = await render();

      fixture.componentRef.setInput('project', 'OPS');
      await fixture.whenStable();

      expect(tab(page, 'backlog')?.getAttribute('href')).toBe('/t/acme/p/OPS/backlog');
      expect(tab(page, 'board')?.getAttribute('href')).toBe('/t/acme/p/OPS/board');
    });

    it('mark the view that is open as the current page, and no other', async () => {
      const { fixture, page } = await render();

      await go(fixture, '/t/acme/p/COW/backlog');
      expect(tab(page, 'backlog')?.classList).toContain('active');
      expect(tab(page, 'backlog')?.getAttribute('aria-current')).toBe('page');
      expect(tab(page, 'board')?.classList).not.toContain('active');
      expect(tab(page, 'board')?.hasAttribute('aria-current')).toBe(false);

      await go(fixture, '/t/acme/p/COW/board');
      expect(tab(page, 'board')?.classList).toContain('active');
      expect(tab(page, 'board')?.getAttribute('aria-current')).toBe('page');
      expect(tab(page, 'backlog')?.classList).not.toContain('active');
      expect(tab(page, 'backlog')?.hasAttribute('aria-current')).toBe(false);
    });

    it('mark neither view on a page of the project that is none of them', async () => {
      const { fixture, page } = await render();

      await go(fixture, '/t/acme/p/COW/settings');

      expect(tab(page, 'backlog')?.classList).not.toContain('active');
      expect(tab(page, 'board')?.classList).not.toContain('active');
    });

    it('are not there outside a tenant', async () => {
      tenant.set(null);

      const { page } = await render();

      expect(page.querySelector('nav.tabs')).toBeNull();
    });
  });

  describe('the settings of the project', () => {
    const gear = (page: HTMLElement) =>
      page.querySelector<HTMLAnchorElement>('[data-testid="project-settings"]');

    it('are linked from the header', async () => {
      const { page } = await render();

      expect(gear(page)?.getAttribute('href')).toBe('/t/acme/p/COW/settings');
      expect(gear(page)?.getAttribute('aria-label')).toBe('Project settings');
      expect(gear(page)?.querySelector('i')?.classList).toContain('pi-cog');
    });

    it('follow the project of the path', async () => {
      const { fixture, page } = await render();

      fixture.componentRef.setInput('project', 'OPS');
      await fixture.whenStable();

      expect(gear(page)?.getAttribute('href')).toBe('/t/acme/p/OPS/settings');
    });

    it('are not offered outside a tenant', async () => {
      tenant.set(null);

      const { page } = await render();

      expect(gear(page)).toBeNull();
    });
  });

  describe('the export (docs/adr/0051 D4)', () => {
    let created: MockInstance<typeof URL.createObjectURL>;
    let revoked: MockInstance<typeof URL.revokeObjectURL>;
    let clicked: MockInstance<HTMLAnchorElement['click']>;
    const button = (page: HTMLElement) =>
      page.querySelector<HTMLButtonElement>('[data-testid="project-export"]');
    const archive = (left: number | null): ExportArchive => ({
      blob: new Blob(['archive'], { type: 'application/gzip' }),
      filename: 'acme-COW-20261007.tar.gz',
      manifest: left === null ? null : manifest(12, left),
    });

    /** Lets the export's promise and what follows it run, then renders. */
    const settle = async (fixture: ComponentFixture<ProjectHeader>) => {
      await new Promise((resolve) => setTimeout(resolve));
      await fixture.whenStable();
    };

    beforeEach(() => {
      created = vi.fn<typeof URL.createObjectURL>().mockReturnValue('blob:export');
      revoked = vi.fn<typeof URL.revokeObjectURL>();
      vi.stubGlobal(
        'URL',
        Object.assign(URL, { createObjectURL: created, revokeObjectURL: revoked }),
      );
      clicked = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => undefined);
    });

    afterEach(() => {
      vi.useRealTimers();
      vi.unstubAllGlobals();
    });

    it('is offered to everybody who reads the project, a member as well', async () => {
      const { page } = await render();

      expect(button(page)?.getAttribute('aria-label')).toBe('Export the tickets');
      expect(button(page)?.querySelector('i')?.classList).toContain('pi-download');
    });

    it('saves the archive under the name the server gives it, and says what it holds', async () => {
      exportProject.mockResolvedValue(archive(0));
      const add = vi.spyOn(TestBed.inject(MessageService), 'add');
      const { fixture, page } = await render();
      vi.useFakeTimers({ toFake: ['setTimeout'] });

      button(page)?.click();
      await fixture.whenStable();

      expect(exportProject).toHaveBeenCalledExactlyOnceWith('acme', 'COW');
      expect(created).toHaveBeenCalledOnce();
      const link = clicked.mock.contexts[0] as HTMLAnchorElement;
      expect(link.download).toBe('acme-COW-20261007.tar.gz');
      expect(link.href).toBe('blob:export');
      expect(add).toHaveBeenCalledExactlyOnceWith({
        severity: 'success',
        summary: 'COW exported',
        detail: '12 tickets in acme-COW-20261007.tar.gz.',
        life: 8000,
      });
      // The object URL is let go a while after the click, not at once.
      expect(revoked).not.toHaveBeenCalled();
      vi.advanceTimersByTime(40_000);
      expect(revoked).toHaveBeenCalledExactlyOnceWith('blob:export');
    });

    it('says how many confidential tickets the archive leaves out (docs/adr/0065 D5)', async () => {
      exportProject.mockResolvedValue(archive(3));
      const add = vi.spyOn(TestBed.inject(MessageService), 'add');
      const { fixture, page } = await render();

      button(page)?.click();
      await settle(fixture);

      expect(add).toHaveBeenCalledWith(
        expect.objectContaining({
          severity: 'info',
          detail:
            '12 tickets in acme-COW-20261007.tar.gz. 3 confidential tickets you cannot read are not included.',
        }),
      );
    });

    it('saves the archive also where the browser could not read its manifest', async () => {
      exportProject.mockResolvedValue(archive(null));
      const add = vi.spyOn(TestBed.inject(MessageService), 'add');
      const { fixture, page } = await render();

      button(page)?.click();
      await settle(fixture);

      expect(clicked).toHaveBeenCalledOnce();
      expect(add).toHaveBeenCalledWith(
        expect.objectContaining({ detail: 'Saved as acme-COW-20261007.tar.gz.' }),
      );
    });

    it('waits while the archive is made, and offers it again after a refusal', async () => {
      let refuse: (error: unknown) => void = () => undefined;
      exportProject.mockReturnValue(new Promise((_, reject) => (refuse = reject)));
      const add = vi.spyOn(TestBed.inject(MessageService), 'add');
      const { fixture, page } = await render();

      button(page)?.click();
      await fixture.whenStable();
      expect(button(page)?.disabled).toBe(true);
      button(page)?.click();
      expect(exportProject).toHaveBeenCalledOnce();

      const body: Problem = {
        type: 'about:blank',
        title: 'Not found',
        status: 404,
        detail: 'no project COW',
        code: 'not_found',
      };
      refuse(new HttpErrorResponse({ status: 404, error: body }));
      await settle(fixture);

      expect(clicked).not.toHaveBeenCalled();
      expect(add).toHaveBeenCalledWith(expect.objectContaining({ summary: 'Not found' }));
      expect(button(page)?.disabled).toBe(false);
    });
  });

  describe('the import (docs/adr/0051 D6)', () => {
    const link = (page: HTMLElement) =>
      page.querySelector<HTMLAnchorElement>('[data-testid="project-import"]');

    it('leads a writer of the tenant — a member or an administrator — to the import of the project', async () => {
      canWrite.set(true);

      const { page } = await render();

      expect(link(page)?.getAttribute('href')).toBe('/t/acme/p/COW/imports');
      expect(link(page)?.getAttribute('aria-label')).toBe('Import tickets');
    });

    it('is not offered to a viewer', async () => {
      const { page } = await render();

      expect(link(page)).toBeNull();
    });

    it('is not offered for a project the list does not hold, an archived one', async () => {
      canWrite.set(true);
      projects.set([{ ...cowork, archived_at: '2026-10-06T10:00:00Z' }]);

      const { page } = await render();

      expect(link(page)).toBeNull();
    });
  });

  describe('filing a ticket', () => {
    const dialog = (fixture: ComponentFixture<ProjectHeader>) =>
      fixture.debugElement.query(By.directive(NewTicketDialog));
    const filed = {
      key: 'acme/COW-7',
      project: 'COW',
      number: 7,
      title: 'The board flickers',
    } as Ticket;

    it('offers a button that opens the dialog for the project', async () => {
      const { fixture, page } = await render();
      const filing = dialog(fixture).componentInstance as NewTicketDialog;
      expect(filing.visible()).toBe(false);
      expect(filing.tenant()).toBe('acme');
      expect(filing.project()).toBe('COW');

      page.querySelector<HTMLButtonElement>('[data-testid="new-ticket"]')?.click();
      await fixture.whenStable();

      expect(filing.visible()).toBe(true);
    });

    it('closes the dialog when it asks to be closed', async () => {
      const { fixture, page } = await render();
      page.querySelector<HTMLButtonElement>('[data-testid="new-ticket"]')?.click();
      await fixture.whenStable();
      const filing = dialog(fixture).componentInstance as NewTicketDialog;
      expect(filing.visible()).toBe(true);

      dialog(fixture).triggerEventHandler('visibleChange', false);
      await fixture.whenStable();

      expect(filing.visible()).toBe(false);
    });

    it('follows the project of the path', async () => {
      const { fixture } = await render();

      fixture.componentRef.setInput('project', 'OPS');
      await fixture.whenStable();

      expect((dialog(fixture).componentInstance as NewTicketDialog).project()).toBe('OPS');
    });

    it('thanks the person with a toast and hands the ticket on when one was filed', async () => {
      const { fixture } = await render();
      const add = vi.spyOn(TestBed.inject(MessageService), 'add');
      const handed: Ticket[] = [];
      fixture.componentInstance.filed.subscribe((ticket) => handed.push(ticket));

      dialog(fixture).triggerEventHandler('filed', filed);

      expect(add).toHaveBeenCalledExactlyOnceWith({
        severity: 'success',
        summary: 'COW-7 filed',
        detail: 'The board flickers',
        life: 4000,
      });
      expect(handed).toEqual([filed]);
    });

    it('offers nothing outside a tenant', async () => {
      tenant.set(null);

      const { fixture, page } = await render();

      expect(page.querySelector('[data-testid="new-ticket"]')).toBeNull();
      expect(dialog(fixture)).toBeNull();
    });
  });
});
