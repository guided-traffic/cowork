import {
  ChangeDetectionStrategy,
  Component,
  computed,
  inject,
  input,
  linkedSignal,
  output,
  signal,
} from '@angular/core';
import { FormsModule } from '@angular/forms';
import { MessageService } from 'primeng/api';
import { ButtonDirective } from 'primeng/button';
import { Dialog } from 'primeng/dialog';
import { InputText } from 'primeng/inputtext';
import { Select } from 'primeng/select';
import { ToggleSwitch } from 'primeng/toggleswitch';
import { Tooltip } from 'primeng/tooltip';
import { SavedFilter, SavedFilterParameters } from '../../api/models';
import { ProblemService } from '../../core/problem.service';
import { SavedFiltersService } from '../../core/saved-filters.service';
import { SessionService } from '../../core/session.service';
import { keepOpenWhile } from '../../shared/keep-open';
import { describe, LeftOut, notesOf } from './saved-filter-model';

/** An entry of the select: a filter by its name, another member's with its owner. */
interface Choice {
  id: string;
  label: string;
  disabled: boolean;
}

/**
 * The saved filters in a list's filter bar (docs/adr/0018 D5) — the backlog's and the tenant's
 * ticket list's: the person's own and those shared with the tenant to apply, the one applied with
 * its owner — or, the person's own, to share, unshare and delete —, and saving the conditions the
 * list applies now under a name, shared or not. Another member's filter that names something the
 * person cannot see is listed and cannot be applied: the server withholds its conditions
 * (docs/adr/0065 D5). The list's own controls stay the list's: applying a filter hands its
 * conditions to the list, which may change them further; a condition the list does not apply is
 * named under the bar with why (`leftOut`).
 */
@Component({
  selector: 'app-saved-filters',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [ButtonDirective, Dialog, FormsModule, InputText, Select, ToggleSwitch, Tooltip],
  template: `
    <span class="saved">
      <p-select
        [options]="choices()"
        optionLabel="label"
        optionValue="id"
        optionDisabled="disabled"
        [ngModel]="applied()?.id ?? null"
        (ngModelChange)="choose($event)"
        (onShow)="filters.reload()"
        placeholder="Saved filters"
        [showClear]="true"
        ariaLabel="Saved filters"
        data-testid="saved-filters"
      />
      @if (applied(); as f) {
        @if (own(f)) {
          <button
            pButton
            type="button"
            size="small"
            [text]="true"
            severity="secondary"
            [iconOnly]="true"
            [disabled]="busy()"
            [pTooltip]="
              f.shared ? 'Shared with the tenant: stop sharing it' : 'Share it with the tenant'
            "
            [showDelay]="400"
            [attr.aria-label]="
              f.shared ? 'Stop sharing ' + f.name : 'Share ' + f.name + ' with the tenant'
            "
            [attr.aria-pressed]="f.shared"
            data-testid="share-filter"
            (click)="share(f)"
          >
            <i class="pi" [class.pi-users]="f.shared" [class.pi-user]="!f.shared"></i>
          </button>
          <button
            pButton
            type="button"
            size="small"
            [text]="true"
            severity="secondary"
            [iconOnly]="true"
            [disabled]="busy()"
            pTooltip="Delete this saved filter"
            [showDelay]="400"
            [attr.aria-label]="'Delete the saved filter ' + f.name"
            data-testid="delete-filter"
            (click)="remove(f)"
          >
            <i class="pi pi-trash"></i>
          </button>
        } @else {
          <span class="muted small" data-testid="filter-owner">by {{ f.owner.display_name }}</span>
        }
      }
      <button
        pButton
        type="button"
        size="small"
        [text]="true"
        severity="secondary"
        data-testid="save-filter"
        [pTooltip]="'Save ' + conditions()"
        [showDelay]="400"
        (click)="saving.set(true)"
      >
        <i class="pi pi-bookmark"></i>Save filter
      </button>
    </span>
    @if (notes().length > 0) {
      <small class="notes muted" data-testid="filter-notes">{{ notes().join(' · ') }}</small>
    }

    <p-dialog
      [visible]="saving()"
      (visibleChange)="saving.set($event)"
      [modal]="true"
      [draggable]="false"
      [closable]="!busy()"
      [style]="{ width: '28rem' }"
      header="Save filter"
      data-testid="save-filter-dialog"
    >
      <form class="form" (ngSubmit)="save()">
        <p class="muted small">{{ conditions() }}</p>
        <label class="field">
          <span>Name</span>
          <input
            pInputText
            name="name"
            maxlength="100"
            autocomplete="off"
            [ngModel]="name()"
            (ngModelChange)="name.set($event)"
            data-testid="filter-name"
          />
        </label>
        <label class="share">
          <p-toggleswitch
            name="shared"
            [ngModel]="shared()"
            (ngModelChange)="shared.set($event)"
            data-testid="filter-shared"
          />
          <span>Share with the tenant — every member sees it, with you as its owner</span>
        </label>
        <div class="actions">
          <button
            pButton
            type="button"
            [text]="true"
            severity="secondary"
            [disabled]="busy()"
            (click)="saving.set(false)"
          >
            Cancel
          </button>
          <button
            pButton
            type="submit"
            data-testid="filter-save"
            [disabled]="name().trim() === '' || busy()"
          >
            Save
          </button>
        </div>
      </form>
    </p-dialog>
  `,
  styles: `
    :host {
      display: contents;
    }
    .saved {
      display: inline-flex;
      align-items: center;
      gap: 0.25rem;
    }
    .small {
      font-size: 0.8125rem;
    }
    .notes {
      flex-basis: 100%;
      font-size: 0.75rem;
    }
    .form {
      display: flex;
      flex-direction: column;
      gap: 0.875rem;
    }
    .field {
      display: flex;
      flex-direction: column;
      gap: 0.375rem;
      > span:first-child {
        font-size: 0.8125rem;
        font-weight: 550;
      }
    }
    .share {
      display: flex;
      align-items: center;
      gap: 0.5rem;
      font-size: 0.875rem;
    }
    .actions {
      display: flex;
      justify-content: flex-end;
      gap: 0.5rem;
    }
  `,
})
export class SavedFilters {
  /** What the list applies now: what saving keeps. */
  readonly current = input.required<SavedFilterParameters>();
  /** The filter the list applies, or none. */
  readonly applied = input<SavedFilter | null>(null);
  /** The conditions of a filter the list does not apply, each with why; none, it applies every one. */
  readonly leftOut = input<LeftOut>({});
  /** The person picked a filter — or none — to apply. */
  readonly chosen = output<SavedFilter | null>();

  protected readonly filters = inject(SavedFiltersService);
  private readonly session = inject(SessionService);
  private readonly problems = inject(ProblemService);
  private readonly messages = inject(MessageService);

  protected readonly saving = signal(false);
  protected readonly busy = signal(false);
  /** The form starts empty each time it opens. */
  protected readonly name = linkedSignal({ source: this.saving, computation: () => '' });
  protected readonly shared = linkedSignal({ source: this.saving, computation: () => false });
  /**
   * The Idempotency-Key of the filter the form saves: one for each content — the name, the sharing,
   * the conditions and the tenant —, so a retry of a lost answer is answered again rather than
   * saving a second filter; any change, and the dialog opened again, make a new one
   * (docs/adr/0045 D3).
   */
  private readonly idempotencyKey = linkedSignal(() => {
    this.session.tenant();
    this.saving();
    this.name();
    this.shared();
    JSON.stringify(this.current());
    return crypto.randomUUID();
  });

  protected readonly choices = computed<Choice[]>(() =>
    this.filters.list().map((f) => ({
      id: f.id,
      disabled: f.redacted,
      label: this.own(f)
        ? `${f.name}${f.shared ? ' · shared' : ''}`
        : `${f.name} · ${f.owner.display_name}${f.redacted ? ' (names something you cannot see)' : ''}`,
    })),
  );
  protected readonly conditions = computed(() => describe(this.current()));
  protected readonly notes = computed(() => {
    const f = this.applied();
    return f ? notesOf(f, this.leftOut()) : [];
  });

  constructor() {
    keepOpenWhile(this.busy);
    this.filters.reload();
  }

  protected own(filter: SavedFilter): boolean {
    return filter.owner.id === this.session.person()?.id;
  }

  protected choose(id: string | null): void {
    this.chosen.emit(this.filters.list().find((f) => f.id === id && !f.redacted) ?? null);
  }

  protected async save(): Promise<void> {
    await this.act(async () => {
      const filter = await this.filters.create(
        this.name().trim(),
        this.current(),
        this.shared(),
        this.idempotencyKey(),
      );
      this.saving.set(false);
      this.chosen.emit(filter);
      this.messages.add({
        severity: 'success',
        summary: 'Filter saved',
        detail: filter.shared
          ? `${filter.name} is shared with the tenant.`
          : `${filter.name} is yours.`,
        life: 3000,
      });
    });
  }

  protected async share(filter: SavedFilter): Promise<void> {
    await this.act(async () => {
      this.chosen.emit(await this.filters.update(filter, { shared: !filter.shared }));
    });
  }

  protected async remove(filter: SavedFilter): Promise<void> {
    await this.act(async () => {
      await this.filters.remove(filter);
      this.chosen.emit(null);
    });
  }

  private async act(run: () => Promise<void>): Promise<void> {
    this.busy.set(true);
    try {
      await run();
    } catch (error) {
      this.problems.report(error);
    } finally {
      this.busy.set(false);
    }
  }
}
