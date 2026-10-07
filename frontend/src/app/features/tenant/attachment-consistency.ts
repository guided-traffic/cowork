import {
  ChangeDetectionStrategy,
  Component,
  computed,
  effect,
  inject,
  signal,
  untracked,
} from '@angular/core';
import { RouterLink } from '@angular/router';
import { ConfirmationService, MessageService } from 'primeng/api';
import { ButtonDirective } from 'primeng/button';
import { TableModule } from 'primeng/table';
import { Tooltip } from 'primeng/tooltip';
import { AttachmentConsistency, OrphanRemovalRecord } from '../../api/models';
import { AttachmentConsistencyService } from '../../core/attachment-consistency.service';
import { ProblemService } from '../../core/problem.service';
import { SessionService } from '../../core/session.service';
import { byteSize } from '../../shared/bytes';
import { ConfirmDialog } from '../../shared/confirm-dialog';
import { ago, Clock, dateTime } from '../../shared/time';

/** "1 file", "3 files": a count with its noun, singular or plural. */
export function counted(n: number, one: string, many: string): string {
  return `${n} ${n === 1 ? one : many}`;
}

/**
 * What the latest check found, in one sentence: in step, or what is out of step and how much of
 * it was accepted as lost.
 */
export function consistencySummary(c: AttachmentConsistency): string {
  const parts: string[] = [];
  if (c.dangling > 0) {
    parts.push(`${counted(c.dangling, 'file', 'files')} whose bytes are missing`);
  }
  if (c.orphans > 0) {
    parts.push(
      `${counted(c.orphans, 'object', 'objects')} no file names (${byteSize(c.orphan_bytes)})`,
    );
  }
  if (c.accepted > 0) {
    parts.push(`${counted(c.accepted, 'file', 'files')} accepted as lost`);
  }
  if (parts.length === 0) {
    return 'Every file has its bytes, and the bucket holds nothing else.';
  }
  return `${parts.join(', ')}.`;
}

/** Whether the lists hold fewer entries than the counts: they show the first thousand. */
export function listsCut(c: AttachmentConsistency): boolean {
  return (
    c.dangling_attachments.length < c.dangling + c.accepted || c.orphaned_objects.length < c.orphans
  );
}

/** `acme/VKO-12` → `VKO-12`. */
function shortKey(key: string): string {
  return key.slice(key.indexOf('/') + 1);
}

/**
 * The tenant's latest consistency check of its attachments against the bucket (docs/adr/0059 D4),
 * on its settings page, for its administrators: the files whose bytes are missing — after a restore
 * that brought the database back without them — and the objects no file names — after a restore
 * that brought the bucket back from a later point, or a removal that failed. Accepting the loss asks
 * once: nothing is removed, the files stay listed on their tickets. Removing the objects asks twice,
 * the second time saying that nothing brings them back, with the focus on the button that does
 * nothing; it goes in the page's browser session, which the API asks of it (docs/adr/0035 D5).
 */
@Component({
  selector: 'app-attachment-consistency',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [ButtonDirective, ConfirmDialog, RouterLink, TableModule, Tooltip],
  providers: [ConfirmationService],
  templateUrl: './attachment-consistency.html',
  styleUrl: './attachment-consistency.scss',
})
export class AttachmentConsistencySection {
  protected readonly consistency = inject(AttachmentConsistencyService);
  protected readonly session = inject(SessionService);
  private readonly problems = inject(ProblemService);
  private readonly messages = inject(MessageService);
  private readonly confirm = inject(ConfirmationService);
  private readonly clock = inject(Clock);

  protected readonly byteSize = byteSize;
  protected readonly dateTime = dateTime;
  protected readonly shortKey = shortKey;
  protected readonly summary = consistencySummary;
  protected readonly cut = listsCut;
  /** The act on its way: its button waits for it. */
  protected readonly busy = signal<'accept' | 'remove' | null>(null);

  protected readonly result = computed(() =>
    this.consistency.latest.hasValue() ? this.consistency.latest.value() : undefined,
  );

  protected readonly failure = computed(() => {
    const error = this.consistency.latest.error();
    if (!error) {
      return undefined;
    }
    const problem = this.problems.read(error);
    return `The consistency check could not be loaded: ${problem.detail || problem.title}`;
  });

  constructor() {
    // A question belongs to the tenant it was asked in: another tenant's page starts clean, and an
    // act confirmed after the switch would name a check of the tenant shown then.
    effect(() => {
      this.session.tenant();
      untracked(() => this.confirm.close());
    });
  }

  protected ago(iso: string): string {
    return ago(iso, this.clock.now());
  }

  /** Who removed the check's objects, when, and how many stayed because a file named them. */
  protected removalNote(removal: OrphanRemovalRecord): string {
    const kept =
      removal.kept > 0
        ? `; ${counted(removal.kept, 'object', 'objects')} had gained a file and stayed`
        : '';
    return (
      `${counted(removal.removed, 'object', 'objects')} of this check removed by ` +
      `${removal.removed_by.display_name} ${this.ago(removal.removed_at)}${kept}.`
    );
  }

  /** Accepting the loss removes nothing: one question. */
  protected accept(c: AttachmentConsistency): void {
    const check = c.check_id;
    if (check === null) {
      return;
    }
    this.confirm.confirm({
      header: `Accept the loss of ${counted(c.dangling, 'file', 'files')}?`,
      message:
        'Their bytes are gone from the bucket. From now on they count as lost, no longer as missing, ' +
        'and hold no alert. Nothing is removed: they stay listed on their tickets, and their download ' +
        'says that the bytes are missing. A file whose bytes come back counts as whole again.',
      acceptLabel: 'Accept the loss',
      rejectLabel: 'Not yet',
      defaultFocus: 'reject',
      accept: () =>
        this.run('accept', async () => {
          const done = await this.consistency.acceptLoss(check);
          this.messages.add({
            severity: 'success',
            summary: 'Loss accepted',
            detail: `${counted(done.accepted, 'file counts', 'files count')} as lost.`,
            life: 4000,
          });
        }),
    });
  }

  /** Removing the objects is irreversible: a first question, then a second that says so. */
  protected remove(c: AttachmentConsistency): void {
    const check = c.check_id;
    if (check === null) {
      return;
    }
    this.confirm.confirm({
      header: `Remove ${counted(c.orphans, 'object', 'objects')} from the bucket?`,
      message:
        `${counted(c.orphans, 'object', 'objects')} under this tenant's prefix, ${byteSize(c.orphan_bytes)}, ` +
        'are named by no file of any ticket — files uploaded between the snapshots a restore brought ' +
        'back, or bytes a removal left. Each is asked again whether a file names it now, and one that ' +
        'does is kept. Whoever runs the bucket can copy them out first.',
      acceptLabel: 'Continue',
      rejectLabel: 'Keep them',
      defaultFocus: 'reject',
      accept: () => setTimeout(() => this.removeForGood(check, c.orphans)),
    });
  }

  private removeForGood(check: string, orphans: number): void {
    this.confirm.confirm({
      header: `Remove ${counted(orphans, 'object', 'objects')} for good?`,
      message: 'Nothing brings a removed object back.',
      acceptLabel: 'Remove for good',
      rejectLabel: 'Keep them',
      acceptButtonProps: { severity: 'danger' },
      // Nothing undoes it: an Enter that comes a moment late must not confirm it.
      defaultFocus: 'reject',
      accept: () =>
        this.run('remove', async () => {
          const done = await this.consistency.removeOrphans(check);
          const kept =
            done.kept > 0
              ? ` ${counted(done.kept, 'object had', 'objects had')} gained a file and stayed.`
              : '';
          const failed =
            done.failed > 0
              ? ` ${counted(done.failed, 'removal', 'removals')} failed; the next check lists them again.`
              : '';
          this.messages.add({
            severity: done.failed > 0 ? 'warn' : 'success',
            summary: 'Objects removed',
            detail: `${counted(done.removed, 'object', 'objects')} removed.${kept}${failed}`,
            life: 6000,
          });
        }),
    });
  }

  private async run(act: 'accept' | 'remove', work: () => Promise<void>): Promise<void> {
    this.busy.set(act);
    try {
      await work();
    } catch (error) {
      this.problems.report(error);
      this.consistency.latest.reload();
    } finally {
      this.busy.set(null);
    }
  }
}
