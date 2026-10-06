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
 * its owner — or, the person's own, to share, unshare and delete; another person's shared one, to
 * a tenant administrator, to unshare and delete (D5 as amended 2026-10-06), both at once as the
 * owner's own acts are —, and saving the conditions the list applies now under a name, shared or
 * not. Another member's filter that names something the person cannot see is listed and cannot be
 * applied: the server withholds its conditions (docs/adr/0065 D5). A tenant administrator may
 * still choose it, to unshare or delete it: the bar holds it (`withheld`) and the list applies
 * none. An owner who left the tenant is one the tenant no longer reads, named "a former member".
 * The list's own controls stay the list's: applying a filter hands its conditions to the list,
 * which may change them further; a condition the list does not apply is named under the bar with
 * why (`leftOut`).
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
        [ngModel]="shown()?.id ?? null"
        (ngModelChange)="choose($event)"
        (onShow)="filters.reload()"
        placeholder="Saved filters"
        [showClear]="true"
        ariaLabel="Saved filters"
        data-testid="saved-filters"
      />
      @if (shown(); as f) {
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
          <span class="muted small" data-testid="filter-owner">by {{ ownerName(f) }}</span>
          @if (administers()) {
            <button
              pButton
              type="button"
              size="small"
              [text]="true"
              severity="secondary"
              [iconOnly]="true"
              [disabled]="busy()"
              [pTooltip]="'Stop sharing it with the tenant — it stays ' + ownerName(f) + '’s'"
              [showDelay]="400"
              [attr.aria-label]="'Stop sharing ' + f.name + ' of ' + ownerName(f)"
              data-testid="unshare-filter"
              (click)="unshare(f)"
            >
              <i class="pi pi-eye-slash"></i>
            </button>
            <button
              pButton
              type="button"
              size="small"
              [text]="true"
              severity="secondary"
              [iconOnly]="true"
              [disabled]="busy()"
              [pTooltip]="'Delete this saved filter of ' + ownerName(f)"
              [showDelay]="400"
              [attr.aria-label]="'Delete ' + f.name + ' of ' + ownerName(f)"
              data-testid="delete-filter"
              (click)="removeAnothers(f)"
            >
              <i class="pi pi-trash"></i>
            </button>
          }
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

  /**
   * The person administers the tenant: they unshare and delete another person's shared filter —
   * one whose owner left the tenant among them — and change nothing else of it.
   */
  protected readonly administers = computed(() => this.session.membership()?.role === 'admin');
  protected readonly choices = computed<Choice[]>(() =>
    this.filters.list().map((f) => ({
      id: f.id,
      disabled: f.redacted && !this.administers(),
      label: this.own(f)
        ? `${f.name}${f.shared ? ' · shared' : ''}`
        : `${f.name} · ${this.ownerName(f)}${f.redacted ? ' (names something you cannot see)' : ''}`,
    })),
  );
  protected readonly conditions = computed(() => describe(this.current()));
  /**
   * The id of a filter the server withholds that an administrator chose, to unshare or delete it:
   * the list applies none meanwhile, and a filter the list applies afterwards replaces it.
   */
  private readonly withheld = linkedSignal<SavedFilter | null, string | null>({
    source: this.applied,
    computation: (applied, previous) => (applied ? null : (previous?.value ?? null)),
  });
  /**
   * The filter the bar shows: the one the list applies, or the withheld one held. The hold is read
   * first, so that it sees every filter the list applies and lets go of it.
   */
  protected readonly shown = computed(() => {
    const id = this.withheld();
    const applied = this.applied();
    if (applied) {
      return applied;
    }
    return this.filters.list().find((f) => f.id === id && f.redacted) ?? null;
  });
  protected readonly notes = computed(() => {
    const f = this.shown();
    if (!f) {
      return [];
    }
    return f.redacted
      ? ['not applied: it names something you cannot see']
      : notesOf(f, this.leftOut());
  });

  constructor() {
    keepOpenWhile(this.busy);
    this.filters.reload();
  }

  protected own(filter: SavedFilter): boolean {
    return filter.owner.id === this.session.person()?.id;
  }

  /**
   * The owner's name; none is a person the tenant no longer reads — one who left it, whose shared
   * filter an administrator withdraws.
   */
  protected ownerName(filter: SavedFilter): string {
    return filter.owner.display_name || 'a former member';
  }

  /** A filter the server withholds is held for an administrator, never applied: the list applies none. */
  protected choose(id: string | null): void {
    const f = this.filters.list().find((filter) => filter.id === id) ?? null;
    this.withheld.set(f?.redacted && this.administers() ? f.id : null);
    this.chosen.emit(f && !f.redacted ? f : null);
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

  /**
   * An administrator's unshare of another person's filter: it stays its owner's and leaves every
   * other list, the administrator's too, so the list applies none from then on.
   */
  protected async unshare(filter: SavedFilter): Promise<void> {
    await this.act(async () => {
      await this.filters.update(filter, { shared: false });
      this.release(filter);
      this.messages.add({
        severity: 'success',
        summary: 'Filter no longer shared',
        detail: `${filter.name} stays ${this.ownerName(filter)}’s.`,
        life: 3000,
      });
    });
  }

  /** An administrator's deletion of another person's shared filter. */
  protected async removeAnothers(filter: SavedFilter): Promise<void> {
    await this.act(async () => {
      await this.filters.remove(filter);
      this.release(filter);
      this.messages.add({
        severity: 'success',
        summary: 'Filter deleted',
        detail: `${filter.name} of ${this.ownerName(filter)}`,
        life: 3000,
      });
    });
  }

  /** Another person's filter an administrator withdrew leaves the bar, and the list if it applied it. */
  private release(filter: SavedFilter): void {
    this.withheld.set(null);
    if (this.applied()?.id === filter.id) {
      this.chosen.emit(null);
    }
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
