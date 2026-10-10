import { inject, Injectable } from '@angular/core';
import { Api } from '../api/api';
import { listAudit } from '../api/fn/teams/list-audit';
import { listAudit$Csv } from '../api/fn/teams/list-audit-csv';
import { AuditAction, AuditList } from '../api/models';
import { PerPage } from './table-pages';

/** The filters of the tenant's audit record (docs/adr/0026 D6): actor, token, action, entity, period. */
export interface AuditQuery {
  team: string;
  /** A person's id. */
  actor?: string;
  /** A token's id. */
  token?: string;
  /** Repeated values combine with OR. */
  action?: AuditAction[];
  entity_type?: string;
  /** The period: `from` inclusive, `to` exclusive, both instants. */
  from?: string;
  to?: string;
}

export type { PerPage } from './table-pages';

/** The rows a CSV holds at most: numbered pages end at row 10 000 (docs/adr/0048 D2). */
export const csvRowLimit = 10_000;
const csvPerPage = 100;

/** A CSV of the record as `AuditService.csv` puts it together. */
export interface AuditCsv {
  /** The file: the header once, then the rows, newest first. */
  text: string;
  /** The moment the file ends at: the acts after it are not in it. */
  until: string;
  /** The rows the filters selected when the download began; the file holds at most `csvRowLimit`. */
  total: number;
}

/**
 * The tenant's audit record for its administrators (docs/adr/0026 D6): numbered pages with a total
 * (docs/adr/0048 D2), and the CSV of what the filters select.
 */
@Injectable({ providedIn: 'root' })
export class AuditService {
  private readonly api = inject(Api);

  /** One numbered page, newest first. */
  page(query: AuditQuery, page: number, perPage: PerPage): Promise<AuditList> {
    return this.api.invoke(listAudit, { ...query, page, per_page: perPage });
  }

  /**
   * The CSV of what the filters select, read as numbered pages of a hundred rows and put together
   * with the header once. A CSV answer carries no cursor, and a newer act would move every row of
   * the later pages down by one, so the period ends where the download began — at the server's
   * clock, the `Date` of its first answer, or the end the filters name if that is earlier. At most
   * the newest `csvRowLimit` rows: the depth of the numbered pages ends there.
   */
  async csv(query: AuditQuery): Promise<AuditCsv> {
    const first = await this.api.invoke$Response(listAudit, { ...query, page: 1, per_page: 25 });
    const date = first.headers.get('Date');
    const now = date && !Number.isNaN(Date.parse(date)) ? new Date(date) : new Date();
    const until = earlier(query.to, now.toISOString());
    const total = first.body.total ?? 0;
    const pages = Math.max(1, Math.ceil(Math.min(total, csvRowLimit) / csvPerPage));
    let text = '';
    for (let page = 1; page <= pages; page++) {
      const csv = await this.api.invoke(listAudit$Csv, {
        ...query,
        to: until,
        page,
        per_page: csvPerPage,
      });
      text += page === 1 ? csv : withoutHeader(csv);
    }
    return { text, until, total };
  }
}

/** The earlier of two instants, the first one optional. */
function earlier(a: string | undefined, b: string): string {
  return a !== undefined && Date.parse(a) < Date.parse(b) ? a : b;
}

/** A page of CSV without its first line, the header, which holds no line break. */
function withoutHeader(csv: string): string {
  const end = csv.indexOf('\n');
  return end < 0 ? '' : csv.slice(end + 1);
}
