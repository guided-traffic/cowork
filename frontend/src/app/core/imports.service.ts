import { HttpErrorResponse } from '@angular/common/http';
import { inject, Injectable } from '@angular/core';
import { Api } from '../api/api';
import { createImport } from '../api/fn/imports/create-import';
import { executeImport } from '../api/fn/imports/execute-import';
import { exportProject } from '../api/fn/imports/export-project';
import { exportTenant } from '../api/fn/imports/export-tenant';
import { getImport } from '../api/fn/imports/get-import';
import { ExportManifest, ImportCorrection, ImportJob } from '../api/models';
import { attachmentName, readManifest } from './export-archive';

/** An export as the browser receives it (docs/adr/0051 D4). */
export interface ExportArchive {
  /** The `tar.gz`. */
  blob: Blob;
  /** The name the server's `Content-Disposition` gives, or the page's own. */
  filename: string;
  /** Its `manifest.json`, read from the archive; null where the browser could not read it. */
  manifest: ExportManifest | null;
}

/**
 * What an export says beside its button: how many tickets it holds and, where the reader cannot
 * read every confidential ticket, how many it leaves out — "n confidential tickets not included"
 * (docs/adr/0065 D5).
 */
export function exportNote(archive: ExportArchive): string {
  const manifest = archive.manifest;
  if (!manifest) {
    return `Saved as ${archive.filename}.`;
  }
  const tickets = `${manifest.tickets} ${manifest.tickets === 1 ? 'ticket' : 'tickets'}`;
  const left = manifest.confidential_not_included;
  return (
    `${tickets} in ${archive.filename}.` +
    (left > 0
      ? ` ${left} confidential ${left === 1 ? 'ticket' : 'tickets'} you cannot read ${left === 1 ? 'is' : 'are'} not included.`
      : '')
  );
}

/**
 * The import of tickets into a project and the export of a project or a tenant (docs/adr/0051):
 * the dry run of an upload, a job read by its id, its execution with the person's corrections, and
 * the two archives as downloads. Nothing here holds state a page shows, but for one hand-over: the
 * job a dry run answered is kept until the page of its address asks for it once, so that the report
 * is not read twice in a row.
 */
@Injectable({ providedIn: 'root' })
export class ImportsService {
  private readonly api = inject(Api);
  /** The jobs dry runs answered that no page has shown yet, by `<tenant>/<project>/<id>`. */
  private readonly answered = new Map<string, ImportJob>();

  /**
   * The dry run (`POST …/projects/{project}/imports`): the files as parts named `file`, each sent
   * under its own name — an archive, or a Markdown file named by its part (docs/adr/0051 D1).
   */
  async dryRun(tenant: string, project: string, files: readonly File[]): Promise<ImportJob> {
    const job = await this.api.invoke(createImport, {
      tenant,
      project,
      body: { file: [...files] },
    });
    this.answered.set(`${tenant}/${project}/${job.id}`, job);
    return job;
  }

  /**
   * The job (`GET …/imports/{import}`): the dry run's own answer the first time its page asks for
   * it, read from the server otherwise — after a reload, or coming back to its address.
   */
  async job(tenant: string, project: string, id: string): Promise<ImportJob> {
    const at = `${tenant}/${project}/${id}`;
    const held = this.answered.get(at);
    this.answered.delete(at);
    return held ?? this.api.invoke(getImport, { tenant, project, import: id });
  }

  /** The execution of a dry run with the corrections (`POST …/imports/{import}/execution`). */
  execute(
    tenant: string,
    project: string,
    id: string,
    corrections: ImportCorrection[],
  ): Promise<ImportJob> {
    return this.api.invoke(executeImport, {
      tenant,
      project,
      import: id,
      body: { corrections },
    });
  }

  /** The project's tickets the person reads, as an archive (`GET …/projects/{project}/export`). */
  exportProject(tenant: string, project: string): Promise<ExportArchive> {
    return this.archive(
      () => this.api.invoke$Response(exportProject, { tenant, project }),
      `${tenant}-${project}-export.tar.gz`,
    );
  }

  /** Every project of the tenant the person sees, as one archive (`GET …/tenants/{tenant}/export`). */
  exportTenant(tenant: string): Promise<ExportArchive> {
    return this.archive(
      () => this.api.invoke$Response(exportTenant, { tenant }),
      `${tenant}-export.tar.gz`,
    );
  }

  private async archive(
    fetch: () => Promise<{ body: Blob; headers: { get(name: string): string | null } }>,
    fallback: string,
  ): Promise<ExportArchive> {
    let response: { body: Blob; headers: { get(name: string): string | null } };
    try {
      response = await fetch();
    } catch (error) {
      throw await readable(error);
    }
    return {
      blob: response.body,
      filename: attachmentName(response.headers.get('Content-Disposition')) ?? fallback,
      manifest: await readManifest(response.body),
    };
  }
}

/**
 * A refusal of a download as the problem it is: a request that asks for a blob gets its error's
 * body as a blob too, which `ProblemService` cannot read — the problem details JSON is read out of
 * it. Anything else is passed on as it is.
 */
async function readable(error: unknown): Promise<unknown> {
  if (!(error instanceof HttpErrorResponse) || !(error.error instanceof Blob)) {
    return error;
  }
  try {
    return new HttpErrorResponse({
      error: JSON.parse(await error.error.text()) as unknown,
      headers: error.headers,
      status: error.status,
      statusText: error.statusText,
      url: error.url ?? undefined,
    });
  } catch {
    return error;
  }
}
