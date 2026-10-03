import { ChangeDetectionStrategy, Component } from '@angular/core';
import { RouterLink } from '@angular/router';

@Component({
  selector: 'app-not-found',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [RouterLink],
  template: `
    <section class="page" data-testid="not-found">
      <h1>Nothing here</h1>
      <p class="muted">This page does not exist, or you cannot see it.</p>
      <a routerLink="/">Back to the start</a>
    </section>
  `,
  styles: `
    .page {
      padding: 2rem;
      display: flex;
      flex-direction: column;
      gap: 0.75rem;
    }
  `,
})
export class NotFound {}
