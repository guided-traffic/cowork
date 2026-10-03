import {
  ChangeDetectionStrategy,
  Component,
  computed,
  inject,
  input,
  model,
  output,
  signal,
} from '@angular/core';
import { FormsModule } from '@angular/forms';
import { ButtonDirective } from 'primeng/button';
import { Dialog } from 'primeng/dialog';
import { InputText } from 'primeng/inputtext';
import { Select } from 'primeng/select';
import { SelectButton } from 'primeng/selectbutton';
import { Textarea } from 'primeng/textarea';
import { Effort, SecurityClass, Severity, Ticket, TicketType } from '../../api/models';
import { MembersService } from '../../core/members.service';
import { ProblemService } from '../../core/problem.service';
import { TicketActions } from '../../core/ticket-actions.service';
import { typeIcons } from '../../shared/badges';
import { meanings } from '../../shared/vocabulary';

const types: TicketType[] = ['task', 'bug', 'feature', 'decision', 'question'];
const severities: Severity[] = ['critical', 'high', 'medium', 'low', 'cosmetic'];
const securities: SecurityClass[] = ['none', 'hardening', 'boundary', 'live'];
const efforts: Effort[] = ['XS', 'S', 'M', 'L'];

/**
 * Files a ticket in a project (docs/adr/0018 D1, the API's `createTicket`): the frontmatter
 * fields, the threat when the security class is not `none`, an assignee and the Markdown body.
 * The server's field errors appear beside their fields (docs/adr/0047 D2).
 */
@Component({
  selector: 'app-new-ticket-dialog',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [ButtonDirective, Dialog, FormsModule, InputText, Select, SelectButton, Textarea],
  templateUrl: './new-ticket-dialog.html',
  styleUrl: './new-ticket-dialog.scss',
})
export class NewTicketDialog {
  readonly tenant = input.required<string>();
  readonly project = input.required<string>();
  readonly visible = model(false);
  readonly filed = output<Ticket>();

  private readonly actions = inject(TicketActions);
  private readonly problems = inject(ProblemService);
  protected readonly members = inject(MembersService);

  protected readonly typeOptions = types.map((type) => ({
    value: type,
    icon: typeIcons[type],
    meaning: meanings.type[type],
  }));
  protected readonly severities = severities.map((value) => ({
    value,
    meaning: meanings.severity[value],
  }));
  protected readonly securities = securities.map((value) => ({
    value,
    meaning: meanings.security[value],
  }));
  protected readonly efforts = efforts;

  protected readonly title = signal('');
  protected readonly type = signal<TicketType>('task');
  protected readonly severity = signal<Severity>('medium');
  protected readonly security = signal<SecurityClass>('none');
  protected readonly threat = signal('');
  protected readonly effort = signal<Effort>('M');
  protected readonly assignee = signal<string | null>(null);
  protected readonly body = signal('');

  protected readonly saving = signal(false);
  protected readonly errors = signal<Record<string, string>>({});
  protected readonly needsThreat = computed(() => this.security() !== 'none');
  protected readonly canSave = computed(
    () =>
      this.title().trim() !== '' &&
      (!this.needsThreat() || this.threat().trim() !== '') &&
      !this.saving(),
  );
  protected readonly people = computed(() =>
    this.members
      .list()
      .map((member) => ({ id: member.person.id, name: member.person.display_name })),
  );

  protected async save(): Promise<void> {
    if (!this.canSave()) {
      return;
    }
    this.saving.set(true);
    this.errors.set({});
    try {
      const ticket = await this.actions.create(this.tenant(), this.project(), {
        title: this.title().trim(),
        type: this.type(),
        severity: this.severity(),
        security: this.security(),
        effort: this.effort(),
        ...(this.needsThreat() ? { threat: this.threat().trim() } : {}),
        ...(this.assignee() ? { assignee: this.assignee() as string } : {}),
        ...(this.body().trim() ? { body: this.body() } : {}),
      });
      this.filed.emit(ticket);
      this.reset();
      this.visible.set(false);
    } catch (error) {
      this.errors.set(this.problems.report(error, { fields: true }).fields);
    } finally {
      this.saving.set(false);
    }
  }

  protected cancel(): void {
    this.reset();
    this.visible.set(false);
  }

  private reset(): void {
    this.title.set('');
    this.type.set('task');
    this.severity.set('medium');
    this.security.set('none');
    this.threat.set('');
    this.effort.set('M');
    this.assignee.set(null);
    this.body.set('');
    this.errors.set({});
  }
}
