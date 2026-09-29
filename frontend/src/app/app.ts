import { Component, inject } from '@angular/core';
import { toSignal } from '@angular/core/rxjs-interop';
import { RouterOutlet } from '@angular/router';
import { catchError, of } from 'rxjs';
import { VersionService } from './core/version.service';

@Component({
  imports: [RouterOutlet],
  selector: 'app-root',
  styleUrl: './app.scss',
  templateUrl: './app.html',
})
export class App {
  protected readonly title = 'cowork';

  /** null until the backend answered, and null when it cannot be reached. */
  protected readonly version = toSignal(
    inject(VersionService)
      .get()
      .pipe(catchError(() => of(null))),
    { initialValue: null },
  );
}
