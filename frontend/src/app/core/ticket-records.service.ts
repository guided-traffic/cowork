import { inject, Injectable } from '@angular/core';
import { Api } from '../api/api';
import { bookTime, editTimeEntry, uploadAttachment, voidTimeEntry } from '../api/functions';
import { Attachment, TimeEntry, TimeEntryPatch } from '../api/models';
import { etagOf } from './entity-cache';
import { routeOf } from './ticket-actions.service';

/**
 * What a ticket keeps besides its conversation: files (docs/adr/0016) and the time people book
 * on it (docs/adr/0017). An upload reaches other pages through the event stream; time entries
 * are not published (docs/adr/0054 D4), so the page that books reloads its own list.
 */
@Injectable({ providedIn: 'root' })
export class TicketRecords {
  private readonly api = inject(Api);

  attach(key: string, file: File): Promise<Attachment> {
    return this.api.invoke(uploadAttachment, {
      ...routeOf(key),
      'Idempotency-Key': crypto.randomUUID(),
      body: { file },
    });
  }

  book(key: string, day: string, minutes: number, note: string): Promise<TimeEntry> {
    return this.api.invoke(bookTime, {
      ...routeOf(key),
      'Idempotency-Key': crypto.randomUUID(),
      body: { day, minutes, ...(note.trim() ? { note: note.trim() } : {}) },
    });
  }

  /** Corrects an entry over the version read (docs/adr/0050 D3). */
  edit(key: string, entry: TimeEntry, patch: TimeEntryPatch): Promise<TimeEntry> {
    return this.api.invoke(editTimeEntry, {
      ...routeOf(key),
      entry: entry.id,
      'If-Match': etagOf(entry.version),
      body: patch,
    });
  }

  void(key: string, entry: TimeEntry): Promise<TimeEntry> {
    return this.api.invoke(voidTimeEntry, { ...routeOf(key), entry: entry.id });
  }
}
