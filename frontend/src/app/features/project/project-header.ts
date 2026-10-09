import { DOCUMENT } from '@angular/common';
import {
  ChangeDetectionStrategy,
  Component,
  computed,
  inject,
  input,
  output,
  signal,
} from '@angular/core';
import { RouterLink, RouterLinkActive } from '@angular/router';
import { MessageService } from 'primeng/api';
import { ButtonDirective } from 'primeng/button';
import { Tooltip } from 'primeng/tooltip';
import { Ticket } from '../../api/models';
import { exportNote, ImportsService } from '../../core/imports.service';
import { ProblemService } from '../../core/problem.service';
import { ProjectsService } from '../../core/projects.service';
import { SessionService } from '../../core/session.service';
import { TenantService } from '../../core/tenant.service';
import { saveFile } from '../../shared/download';
import { NewTicketDialog } from '../ticket/new-ticket-dialog';

/**
 * What both pages of a project show above their content (docs/adr/0018 D1): the key, the name and
 * the description, the export of its tickets for everybody who reads it and, for the tenant's
 * administrators, the way to its import (docs/adr/0051), the settings, the button that files a
 * ticket, and the two tabs, Board and Backlog, which are links to `/t/:tenant/p/:project/board` and
 * `…/backlog`. A ticket filed here is thanked with a toast and handed on, so that the page that
 * shows it can load its list at once.
 */
@Component({
  selector: 'app-project-header',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [ButtonDirective, NewTicketDialog, RouterLink, RouterLinkActive, Tooltip],
  templateUrl: './project-header.html',
  styleUrl: './project-header.scss',
})
export class ProjectHeader {
  /** The project's key. */
  readonly project = input.required<string>();
  readonly filed = output<Ticket>();

  protected readonly session = inject(SessionService);
  protected readonly tenantInfo = inject(TenantService);
  private readonly projects = inject(ProjectsService);
  private readonly imports = inject(ImportsService);
  private readonly problems = inject(ProblemService);
  private readonly messages = inject(MessageService);
  private readonly document = inject(DOCUMENT);

  protected readonly details = computed(() => this.projects.byKey(this.project()));
  /**
   * An import is a writer's act, as filing a ticket is (docs/adr/0051 D6), into a project the list
   * holds — not an archived one. A restricted project's list may lower the role; the API is the check.
   */
  protected readonly mayImport = computed(() => {
    const project = this.details();
    return this.tenantInfo.canWrite() && !!project && !project.archived_at;
  });
  protected readonly creating = signal(false);
  protected readonly exporting = signal(false);

  /**
   * Downloads the project's tickets the person reads as an archive (docs/adr/0051 D4), saved under
   * the name the server gives it, and says what it holds — and how many confidential tickets it
   * leaves out (docs/adr/0065 D5).
   */
  protected async exportTickets(tenant: string): Promise<void> {
    const project = this.project();
    if (this.exporting()) {
      return;
    }
    this.exporting.set(true);
    try {
      const archive = await this.imports.exportProject(tenant, project);
      saveFile(this.document, archive.blob, archive.filename);
      this.messages.add({
        severity: (archive.manifest?.confidential_not_included ?? 0) > 0 ? 'info' : 'success',
        summary: `${project} exported`,
        detail: exportNote(archive),
        life: 8000,
      });
    } catch (error) {
      this.problems.report(error);
    } finally {
      this.exporting.set(false);
    }
  }

  protected onFiled(ticket: Ticket): void {
    this.messages.add({
      severity: 'success',
      summary: `${ticket.project}-${ticket.number} filed`,
      detail: ticket.title,
      life: 4000,
    });
    this.filed.emit(ticket);
  }
}
