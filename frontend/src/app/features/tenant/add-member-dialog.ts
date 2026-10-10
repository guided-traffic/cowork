import {
  ChangeDetectionStrategy,
  Component,
  computed,
  effect,
  inject,
  linkedSignal,
  model,
  output,
  signal,
  untracked,
} from '@angular/core';
import { FormsModule } from '@angular/forms';
import { ButtonDirective } from 'primeng/button';
import { Dialog } from 'primeng/dialog';
import { InputText } from 'primeng/inputtext';
import { Select } from 'primeng/select';
import { Member, ProblemCode, Role } from '../../api/models';
import { MembersService } from '../../core/members.service';
import { ProblemService } from '../../core/problem.service';
import { describedBy, selectAria } from '../../shared/field-aria';
import { keepOpenWhile } from '../../shared/keep-open';
import { roleMeanings, roles } from './roles';

/** What the form says, under the person's field, for the refusals that are about the person. */
export const addRefusals: Partial<Record<ProblemCode, string>> = {
  person_not_found:
    'Nobody active has that e-mail address or username. Somebody of the identity provider can be added after their first sign-in.',
  person_ambiguous:
    'Several people have signed in with that e-mail address, and cowork cannot tell which one you mean.',
  grant_exists: 'That person holds a grant here already: change its role in the list.',
};

/**
 * Grants a role to somebody who is not a member of the tenant yet (docs/adr/0030 D3): a person of
 * the identity provider by the e-mail address it gave at their last sign-in, a local account by
 * its username. A person nobody has seen yet cannot be added — the identity provider's people come
 * into being at their first sign-in. While the request is out nothing closes the dialog, so that a
 * refusal always lands in the form that was sent.
 */
@Component({
  selector: 'app-add-member-dialog',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [ButtonDirective, Dialog, FormsModule, InputText, Select],
  template: `
    <p-dialog
      [visible]="visible()"
      (visibleChange)="visible.set($event)"
      [modal]="true"
      [draggable]="false"
      [closable]="!saving()"
      [dismissableMask]="!saving()"
      [style]="{ width: '32rem' }"
      header="Add a member"
      data-testid="add-member-dialog"
    >
      <form class="form" (ngSubmit)="save()">
        <p class="muted lead">
          A grant for somebody who is not a member of this team yet. Somebody of the identity
          provider is found by the e-mail address it gave at their last sign-in, so they can be
          added once they have signed in; a local account by its username.
        </p>
        <div class="field">
          <label for="member-person-input">E-mail address or username</label>
          <input
            pInputText
            id="member-person-input"
            name="person"
            maxlength="320"
            autocomplete="off"
            spellcheck="false"
            placeholder="ada@example.com"
            [ngModel]="person()"
            (ngModelChange)="person.set($event)"
            [attr.aria-invalid]="!!errors()['person']"
            [attr.aria-describedby]="describedBy(errors()['person'] && 'member-person-error')"
            data-testid="member-person"
          />
          @if (errors()['person']; as error) {
            <small
              class="error"
              id="member-person-error"
              role="alert"
              data-testid="member-person-error"
              >{{ error }}</small
            >
          }
        </div>
        <div class="field">
          <span id="member-role-label">Role</span>
          <p-select
            [options]="roles"
            [ngModel]="role()"
            (ngModelChange)="role.set($event)"
            name="role"
            size="small"
            [invalid]="!!errors()['role']"
            [pt]="rolePt()"
            ariaLabelledBy="member-role-label"
            data-testid="member-role"
          />
          <small class="muted" id="member-role-meaning" data-testid="member-role-meaning">{{
            roleMeanings[role()]
          }}</small>
          @if (errors()['role']; as error) {
            <small
              class="error"
              id="member-role-error"
              role="alert"
              data-testid="member-role-error"
              >{{ error }}</small
            >
          }
        </div>
        <div class="actions">
          <button
            pButton
            type="button"
            [text]="true"
            severity="secondary"
            [disabled]="saving()"
            (click)="visible.set(false)"
            data-testid="member-cancel"
          >
            Cancel
          </button>
          <button
            pButton
            type="submit"
            data-testid="member-save"
            [disabled]="!canSave() || saving()"
          >
            @if (saving()) {
              <i class="pi pi-spinner pi-spin"></i>
            }
            Add member
          </button>
        </div>
      </form>
    </p-dialog>
  `,
  styles: `
    .form {
      display: flex;
      flex-direction: column;
      gap: 0.875rem;
    }
    .lead {
      margin: 0;
      font-size: 0.8125rem;
    }
    .field {
      display: flex;
      flex-direction: column;
      gap: 0.375rem;
      > label,
      > span:first-child {
        font-size: 0.8125rem;
        font-weight: 550;
      }
    }
    small {
      font-size: 0.75rem;
    }
    .error {
      color: var(--p-severity-critical);
    }
    .actions {
      display: flex;
      justify-content: flex-end;
      gap: 0.5rem;
    }
  `,
})
export class AddMemberDialog {
  readonly visible = model(false);
  /** The member as the grant made them, for the page to say so. */
  readonly added = output<Member>();

  private readonly members = inject(MembersService);
  private readonly problems = inject(ProblemService);

  protected readonly roles = roles;
  protected readonly roleMeanings = roleMeanings;
  protected readonly describedBy = describedBy;
  protected readonly person = signal('');
  protected readonly role = signal<Role>('member');
  protected readonly saving = signal(false);
  protected readonly errors = signal<Record<string, string>>({});
  protected readonly rolePt = computed(() =>
    selectAria(
      !!this.errors()['role'],
      describedBy('member-role-meaning', this.errors()['role'] && 'member-role-error'),
    ),
  );
  protected readonly canSave = computed(() => this.person().trim() !== '' && !this.saving());

  /**
   * The Idempotency-Key of the act this form is making: one for each content it holds, so a retry
   * of the same content answers a lost answer again; any change, and closing the form, make a new
   * one (docs/adr/0045).
   */
  private readonly key = linkedSignal(() => {
    this.person();
    this.role();
    return crypto.randomUUID();
  });

  constructor() {
    keepOpenWhile(() => this.saving());
    // However the dialog closes, what was typed goes with it.
    effect(() => {
      if (!this.visible()) {
        untracked(() => {
          this.person.set('');
          this.role.set('member');
          this.errors.set({});
        });
      }
    });
  }

  protected async save(): Promise<void> {
    if (!this.canSave()) {
      return;
    }
    this.saving.set(true);
    this.errors.set({});
    try {
      const member = await this.members.add(this.person().trim(), this.role(), this.key());
      this.added.emit(member);
      this.visible.set(false);
    } catch (error) {
      if (this.visible()) {
        const said = addRefusals[this.problems.read(error).code as ProblemCode];
        this.errors.set(
          said ? { person: said } : this.problems.report(error, { fields: true }).fields,
        );
      } else {
        // The page closed the dialog while the request was out, and the form is empty: a field
        // error would sit under nothing. A toast says it.
        this.problems.report(error);
      }
    } finally {
      this.saving.set(false);
    }
  }
}
