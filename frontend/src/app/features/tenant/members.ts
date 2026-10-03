import { ChangeDetectionStrategy, Component, computed, inject } from '@angular/core';
import { TableModule } from 'primeng/table';
import { MembersService } from '../../core/members.service';
import { ProblemService } from '../../core/problem.service';

/** The tenant's members and their roles (docs/adr/0034 D7, docs/adr/0018), every page of them. */
@Component({
  selector: 'app-members',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [TableModule],
  template: `
    <section class="page">
      <h1>Members</h1>
      <div class="panel">
        <p-table
          [value]="members.list()"
          [loading]="members.members.isLoading()"
          [showLoader]="false"
          size="small"
          data-testid="members"
        >
          <ng-template #header>
            <tr>
              <th>Name</th>
              <th>Username</th>
              <th class="role">Role</th>
            </tr>
          </ng-template>
          <ng-template #body let-member>
            <tr [attr.data-testid]="'member-' + (member.person.username ?? member.person.id)">
              <td>{{ member.person.display_name }}</td>
              <td class="muted">{{ member.person.username ?? '—' }}</td>
              <td class="role">{{ member.role }}</td>
            </tr>
          </ng-template>
          <ng-template #emptymessage>
            <tr>
              <td colspan="3" class="muted empty" data-testid="members-empty">
                {{ failure() ?? 'No members.' }}
              </td>
            </tr>
          </ng-template>
        </p-table>
      </div>
    </section>
  `,
  styles: `
    .page {
      display: flex;
      flex-direction: column;
      gap: 1rem;
      max-width: 56rem;
      padding: 2rem;
    }
    h1 {
      font-size: 1.5rem;
    }
    .panel {
      border: 1px solid var(--p-app-border);
      border-radius: var(--p-border-radius-xl);
      background: var(--p-app-panel);
      overflow: hidden;
    }
    .role {
      width: 8rem;
    }
    .empty {
      padding: 1.5rem;
      text-align: center;
    }
  `,
})
export class Members {
  protected readonly members = inject(MembersService);
  private readonly problems = inject(ProblemService);

  protected readonly failure = computed(() => {
    const error = this.members.members.error();
    if (!error) {
      return undefined;
    }
    const problem = this.problems.read(error);
    return `The members could not be loaded: ${problem.detail || problem.title}`;
  });
}
