import { ChangeDetectionStrategy, Component } from '@angular/core';
import { ConfirmDialog as PrimeConfirmDialog } from 'primeng/confirmdialog';

/**
 * The confirmation dialog of a page, for the `ConfirmationService` the page provides: PrimeNG's
 * `p-confirmdialog` with a message template that shows the message as text, never as markup, so
 * that whatever a message quotes — a name, a group, a value somebody typed — shows as it was
 * written. Pages ask through this dialog, never through `p-confirmdialog` itself. With a message
 * template PrimeNG shows no icon; no confirmation of cowork has one.
 */
@Component({
  selector: 'app-confirm-dialog',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [PrimeConfirmDialog],
  template: `
    <p-confirmdialog>
      <ng-template #message let-confirmation>
        <span class="p-confirmdialog-message">{{ confirmation?.message }}</span>
      </ng-template>
    </p-confirmdialog>
  `,
})
export class ConfirmDialog {}
