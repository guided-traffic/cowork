/**
 * The Idempotency-Key of an upload whose answer did not come (docs/adr/0045 D3): the same file
 * picked again for the same place — a ticket, or a comment of it — is sent with the same key, so a
 * lost answer is answered again instead of storing the file twice; another file, another place, or
 * an upload that went through make a new one. A file is its name, size and modification time; the
 * server's fingerprint is its bytes, so a file that changed under the same name meets
 * `422 idempotency_mismatch` instead of being stored as the earlier one.
 */
export class UploadKey {
  private unanswered: { upload: string; key: string } | null = null;

  /** The key to send this file to this place with. */
  for(place: string, file: File): string {
    const upload = [place, file.name, file.size, file.lastModified].join('\n');
    if (this.unanswered?.upload !== upload) {
      this.unanswered = { upload, key: crypto.randomUUID() };
    }
    return this.unanswered.key;
  }

  /** The upload went through: the next one, of the same file too, is another act. */
  answered(): void {
    this.unanswered = null;
  }
}
