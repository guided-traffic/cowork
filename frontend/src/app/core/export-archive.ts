import { ExportManifest } from '../api/models';

/** The manifest an export writes first at the root of its archive (docs/adr/0051 D4). */
const manifestName = 'manifest.json';

/**
 * At most this much of an archive is unpacked to find its manifest: the export writes it first, and
 * a few kilobytes hold it, so the rest of a large archive is never unpacked in the page.
 */
const unpackLimit = 4 * 1024 * 1024;

/** The slices an archive is fed to the decompression in, so that it unpacks as far as it is read. */
const sliceSize = 64 * 1024;

const block = 512;

/**
 * The file name of `Content-Disposition: attachment; filename="…"` — quoted or not, without a
 * directory, which a browser would not take anyway; null without one.
 */
export function attachmentName(disposition: string | null): string | null {
  const match = /filename\s*=\s*(?:"([^"]*)"|([^;\s]+))/i.exec(disposition ?? '');
  const name = (match?.[1] ?? match?.[2] ?? '').split(/[\\/]/).pop()?.trim() ?? '';
  return name === '' || name === '.' || name === '..' ? null : name;
}

/**
 * The `manifest.json` of an export's `tar.gz`, read in the browser: unpacked with the browser's own
 * `DecompressionStream` only as far as the manifest, which the export writes first. Null where the
 * browser cannot unpack it or the archive holds none — the archive is saved all the same, and the
 * page only says less.
 */
export async function readManifest(archive: Blob): Promise<ExportManifest | null> {
  if (typeof DecompressionStream === 'undefined' || typeof ReadableStream === 'undefined') {
    return null;
  }
  try {
    const bytes = new Uint8Array(await archive.arrayBuffer());
    let offset = 0;
    const source = new ReadableStream<BufferSource>({
      pull(controller) {
        if (offset >= bytes.length) {
          controller.close();
          return;
        }
        controller.enqueue(bytes.slice(offset, offset + sliceSize));
        offset += sliceSize;
      },
    });
    const reader = source.pipeThrough(new DecompressionStream('gzip')).getReader();
    try {
      let tar = new Uint8Array(0);
      for (;;) {
        const found = entryOf(tar, manifestName);
        if (found instanceof Uint8Array) {
          return JSON.parse(new TextDecoder().decode(found)) as ExportManifest;
        }
        if (found === 'absent' || tar.length >= unpackLimit) {
          return null;
        }
        const { done, value } = await reader.read();
        if (done) {
          return null;
        }
        tar = joined(tar, value);
      }
    } finally {
      reader.cancel().catch(() => undefined);
    }
  } catch {
    return null;
  }
}

function joined(a: Uint8Array, b: Uint8Array): Uint8Array<ArrayBuffer> {
  const both = new Uint8Array(a.length + b.length);
  both.set(a);
  both.set(b, a.length);
  return both;
}

/**
 * The bytes of the regular file `name` in the start of a tar: `'more'` while the bytes end before
 * them, `'absent'` once the archive ends without it. A PAX header's `path` names the entry after
 * it, as Go's writer puts one before every entry of an export.
 */
export function entryOf(tar: Uint8Array, name: string): Uint8Array | 'more' | 'absent' {
  let offset = 0;
  let paxPath: string | undefined;
  while (offset + block <= tar.length) {
    const header = tar.subarray(offset, offset + block);
    if (header.every((byte) => byte === 0)) {
      return 'absent';
    }
    const size = octal(header.subarray(124, 136));
    if (size === null) {
      return 'absent';
    }
    const type = String.fromCharCode(header[156]);
    const start = offset + block;
    const end = start + size;
    if (type === 'x') {
      if (end > tar.length) {
        return 'more';
      }
      paxPath = paxRecord(tar.subarray(start, end), 'path');
    } else if (type !== 'g') {
      const path = paxPath ?? headerPath(header);
      paxPath = undefined;
      if (path === name && (type === '0' || type === '\0')) {
        return end > tar.length ? 'more' : tar.slice(start, end);
      }
    }
    offset = start + Math.ceil(size / block) * block;
  }
  return 'more';
}

const ascii = new TextDecoder();

/** A NUL-terminated field of a header. */
function text(field: Uint8Array): string {
  const end = field.indexOf(0);
  return ascii.decode(end < 0 ? field : field.subarray(0, end));
}

/** A number of a header, octal digits; null for anything else, a size in base 256 among it. */
function octal(field: Uint8Array): number | null {
  const digits = text(field).trim();
  if (digits === '') {
    return 0;
  }
  return /^[0-7]+$/.test(digits) ? parseInt(digits, 8) : null;
}

/** The name of a ustar header, with its prefix where it has one. */
function headerPath(header: Uint8Array): string {
  const name = text(header.subarray(0, 100));
  const ustar = text(header.subarray(257, 262)) === 'ustar';
  const prefix = ustar ? text(header.subarray(345, 500)) : '';
  return prefix === '' ? name : `${prefix}/${name}`;
}

/**
 * The value of a PAX record, `<length> <key>=<value>\n`, the length counting the record's bytes;
 * undefined where the header has none.
 */
function paxRecord(data: Uint8Array, key: string): string | undefined {
  let at = 0;
  while (at < data.length) {
    const space = data.indexOf(0x20, at);
    const length = space < 0 ? NaN : Number(ascii.decode(data.subarray(at, space)));
    if (!Number.isInteger(length) || length <= space - at + 1 || at + length > data.length) {
      return undefined;
    }
    const record = ascii.decode(data.subarray(space + 1, at + length - 1));
    const equals = record.indexOf('=');
    if (equals > 0 && record.slice(0, equals) === key) {
      return record.slice(equals + 1);
    }
    at += length;
  }
  return undefined;
}
