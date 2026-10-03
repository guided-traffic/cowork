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
import { Ticket } from '../../api/models';
import { ProjectsService } from '../../core/projects.service';
import { SessionService } from '../../core/session.service';
import { NewTicketDialog } from '../ticket/new-ticket-dialog';

/**
 * What both pages of a project show above their content (docs/adr/0018 D1): the key, the name and
 * the description, the settings, the button that files a ticket, and the two tabs, Backlog and
 * Board, which are links to `/t/:tenant/p/:project/backlog` and `…/board`. A ticket filed here is
 * thanked with a toast and handed on, so that the page that shows it can load its list at once.
 */
@Component({
  selector: 'app-project-header',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [ButtonDirective, NewTicketDialog, RouterLink, RouterLinkActive],
  templateUrl: './project-header.html',
  styleUrl: './project-header.scss',
})
export class ProjectHeader {
  /** The project's key. */
  readonly project = input.required<string>();
  readonly filed = output<Ticket>();

  protected readonly session = inject(SessionService);
  private readonly projects = inject(ProjectsService);
  private readonly messages = inject(MessageService);

  protected readonly details = computed(() => this.projects.byKey(this.project()));
  protected readonly creating = signal(false);

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
