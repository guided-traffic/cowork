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
import { GroupMapping, Role } from '../../api/models';
import { GroupMappingsService } from '../../core/group-mappings.service';
import { ProblemService } from '../../core/problem.service';
import { describedBy, selectAria } from '../../shared/field-aria';
import { keepOpenWhile } from '../../shared/keep-open';
import { roleMeanings, roles } from './roles';

/** What the form says under the group's field when the tenant maps the group already. */
export const mappingExists = 'This team maps that group already: change its role in the list.';

/**
 * Maps a group of the identity provider to a role in the tenant (docs/adr/0030 D2). The group is
 * matched exactly, case and all, against the groups the provider's claim carries, so it is sent as
 * typed, without the spaces around it. While the request is out nothing closes the dialog, so that
 * a refusal always lands in the form that was sent.
 */
@Component({
  selector: 'app-new-mapping-dialog',
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
      appendTo="body"
      [style]="{ width: '32rem' }"
      header="New group mapping"
      data-testid="new-mapping-dialog"
    >
      <form class="form" (ngSubmit)="save()">
        <p class="muted lead">
          Everyone whose groups at the identity provider include this group gets the role here: at
          once where cowork knows their groups from their last sign-in, else when they next sign in.
        </p>
        <div class="field">
          <label for="mapping-group-input">Group</label>
          <input
            pInputText
            id="mapping-group-input"
            name="group"
            maxlength="256"
            autocomplete="off"
            spellcheck="false"
            placeholder="team-red"
            [ngModel]="group()"
            (ngModelChange)="group.set($event)"
            [attr.aria-invalid]="!!errors()['group']"
            [attr.aria-describedby]="
              describedBy('mapping-group-hint', errors()['group'] && 'mapping-group-error')
            "
            data-testid="mapping-group"
          />
          <small class="muted" id="mapping-group-hint">
            As the identity provider's groups claim names it: exactly, upper and lower case as they
            are.
          </small>
          @if (errors()['group']; as error) {
            <small
              class="error"
              id="mapping-group-error"
              role="alert"
              data-testid="mapping-group-error"
              >{{ error }}</small
            >
          }
        </div>
        <div class="field">
          <span id="mapping-role-label">Role</span>
          <p-select
            [options]="roles"
            [ngModel]="role()"
            (ngModelChange)="role.set($event)"
            name="role"
            size="small"
            [invalid]="!!errors()['role']"
            [pt]="rolePt()"
            ariaLabelledBy="mapping-role-label"
            data-testid="mapping-new-role"
          />
          <small class="muted" id="mapping-role-meaning" data-testid="mapping-role-meaning">{{
            roleMeanings[role()]
          }}</small>
          @if (errors()['role']; as error) {
            <small
              class="error"
              id="mapping-role-error"
              role="alert"
              data-testid="mapping-role-error"
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
            data-testid="mapping-cancel"
          >
            Cancel
          </button>
          <button
            pButton
            type="submit"
            data-testid="mapping-save"
            [disabled]="!canSave() || saving()"
          >
            @if (saving()) {
              <i class="pi pi-spinner pi-spin"></i>
            }
            Map group
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
export class NewMappingDialog {
  readonly visible = model(false);
  /** The mapping that was made, for the page to say so. */
  readonly created = output<GroupMapping>();

  private readonly mappings = inject(GroupMappingsService);
  private readonly problems = inject(ProblemService);

  protected readonly roles = roles;
  protected readonly roleMeanings = roleMeanings;
  protected readonly describedBy = describedBy;
  protected readonly group = signal('');
  protected readonly role = signal<Role>('member');
  protected readonly saving = signal(false);
  protected readonly errors = signal<Record<string, string>>({});
  protected readonly rolePt = computed(() =>
    selectAria(
      !!this.errors()['role'],
      describedBy('mapping-role-meaning', this.errors()['role'] && 'mapping-role-error'),
    ),
  );
  protected readonly canSave = computed(() => this.group().trim() !== '' && !this.saving());

  /**
   * The Idempotency-Key of the act this form is making: one for each content it holds, so a retry
   * of the same content answers a lost answer again; any change, and closing the form, make a new
   * one (docs/adr/0045).
   */
  private readonly key = linkedSignal(() => {
    this.group();
    this.role();
    return crypto.randomUUID();
  });

  constructor() {
    keepOpenWhile(() => this.saving());
    // However the dialog closes, what was typed goes with it.
    effect(() => {
      if (!this.visible()) {
        untracked(() => {
          this.group.set('');
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
      const mapping = await this.mappings.create(this.group().trim(), this.role(), this.key());
      this.created.emit(mapping);
      this.visible.set(false);
    } catch (error) {
      if (!this.visible()) {
        // The page closed the dialog while the request was out, and the form is empty: a field
        // error would sit under nothing. A toast says it.
        this.problems.report(error);
      } else if (this.problems.read(error).code === 'mapping_exists') {
        this.errors.set({ group: mappingExists });
      } else {
        this.errors.set(this.problems.report(error, { fields: true }).fields);
      }
    } finally {
      this.saving.set(false);
    }
  }
}
