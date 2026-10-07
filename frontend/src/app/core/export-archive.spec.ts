import { ExportManifest } from '../api/models';
import { attachmentName, entryOf, readManifest } from './export-archive';

const encoder = new TextEncoder();

function joined(parts: Uint8Array[]): Uint8Array<ArrayBuffer> {
  const all = new Uint8Array(parts.reduce((sum, part) => sum + part.length, 0));
  let at = 0;
  for (const part of parts) {
    all.set(part, at);
    at += part.length;
  }
  return all;
}

/** A ustar header block, as Go's archive/tar writes one. */
function header(name: string, size: number, type: string): Uint8Array {
  const block = new Uint8Array(512);
  block.set(encoder.encode(name), 0);
  block.set(encoder.encode('0000644\0'), 100);
  block.set(encoder.encode(`${size.toString(8).padStart(11, '0')}\0`), 124);
  block[156] = type.charCodeAt(0);
  block.set(encoder.encode('ustar\0'), 257);
  block.set(encoder.encode('00'), 263);
  return block;
}

/** An entry: its header and its bytes, padded to whole blocks. */
function entry(name: string, body: Uint8Array | string, type = '0'): Uint8Array {
  const bytes = typeof body === 'string' ? encoder.encode(body) : body;
  const padded = new Uint8Array(Math.ceil(bytes.length / 512) * 512);
  padded.set(bytes);
  return joined([header(name, bytes.length, type), padded]);
}

/** A PAX header's records, `<length> <key>=<value>\n` with the length counting itself. */
function pax(records: Record<string, string>): Uint8Array {
  const lines = Object.entries(records).map(([key, value]) => {
    const rest = ` ${key}=${value}\n`;
    let length = encoder.encode(rest).length + 1;
    while (encoder.encode(`${length}${rest}`).length !== length) {
      length = encoder.encode(`${length}${rest}`).length;
    }
    return `${length}${rest}`;
  });
  return encoder.encode(lines.join(''));
}

/** A tar of the entries, closed by its two empty blocks. */
function tar(...entries: Uint8Array[]): Uint8Array<ArrayBuffer> {
  return joined([...entries, new Uint8Array(1024)]);
}

async function gzip(bytes: Uint8Array<ArrayBuffer>): Promise<Blob> {
  const reader = new ReadableStream<BufferSource>({
    start(controller) {
      controller.enqueue(bytes);
      controller.close();
    },
  })
    .pipeThrough(new CompressionStream('gzip'))
    .getReader();
  const chunks: Uint8Array[] = [];
  for (;;) {
    const { done, value } = await reader.read();
    if (done) {
      return new Blob([joined(chunks)], { type: 'application/gzip' });
    }
    chunks.push(value);
  }
}

const manifest: ExportManifest = {
  format: 'cowork export v1',
  tenant: 'acme',
  projects: [
    { key: 'VKO', name: 'Vertrag', archived: false, tickets: 2, confidential_not_included: 1 },
  ],
  exported_at: '2026-10-07T08:00:00Z',
  exported_by: 'Ada Lovelace <local:ada>',
  tickets: 2,
  confidential_not_included: 1,
};

describe('attachmentName', () => {
  it('reads the file name of Content-Disposition, quoted or not', () => {
    expect(attachmentName('attachment; filename="acme-VKO-20261007.tar.gz"')).toBe(
      'acme-VKO-20261007.tar.gz',
    );
    expect(attachmentName('attachment; filename=acme-20261007.tar.gz')).toBe(
      'acme-20261007.tar.gz',
    );
    expect(attachmentName('attachment; FileName="a.tar.gz"; size=12')).toBe('a.tar.gz');
  });

  it('keeps the name only, never a directory', () => {
    expect(attachmentName('attachment; filename="../../etc/acme.tar.gz"')).toBe('acme.tar.gz');
    expect(attachmentName('attachment; filename="C:\\\\temp\\\\acme.tar.gz"')).toBe('acme.tar.gz');
  });

  it('is null without a header, a name, or with a name that is none', () => {
    expect(attachmentName(null)).toBeNull();
    expect(attachmentName('attachment')).toBeNull();
    expect(attachmentName('attachment; filename=""')).toBeNull();
    expect(attachmentName('attachment; filename=".."')).toBeNull();
  });
});

describe('entryOf', () => {
  it('finds a regular file among the entries before it', () => {
    const archive = tar(entry('links.json', '[]'), entry('manifest.json', '{"a":1}'));

    expect(new TextDecoder().decode(entryOf(archive, 'manifest.json') as Uint8Array)).toBe(
      '{"a":1}',
    );
  });

  it('takes the path of a PAX header for the entry after it, as Go writes an export', () => {
    const archive = tar(
      entry('PaxHeaders.0/x', pax({ path: 'manifest.json', mtime: '1759824000.5' }), 'x'),
      entry('manifest.jso', '{}'),
    );

    expect(new TextDecoder().decode(entryOf(archive, 'manifest.json') as Uint8Array)).toBe('{}');
  });

  it('asks for more while the bytes end before the entry does', () => {
    const archive = tar(entry('manifest.json', 'x'.repeat(600)));

    expect(entryOf(archive.subarray(0, 700), 'manifest.json')).toBe('more');
    expect(entryOf(archive.subarray(0, 200), 'manifest.json')).toBe('more');
  });

  it('says absent once the archive ends without it', () => {
    expect(entryOf(tar(entry('acme/VKO-1.md', '# One')), 'manifest.json')).toBe('absent');
  });

  it('says absent for a size it cannot read', () => {
    const archive = tar(entry('manifest.json', '{}'));
    archive.set(encoder.encode('zz'), 124);

    expect(entryOf(archive, 'manifest.json')).toBe('absent');
  });
});

describe('readManifest', () => {
  it('reads the manifest of an export, a PAX header before every entry', async () => {
    const archive = await gzip(
      tar(
        entry('PaxHeaders.0/manifest.json', pax({ mtime: '1759824000.123456789' }), 'x'),
        entry('manifest.json', JSON.stringify(manifest)),
        entry('PaxHeaders.0/links.json', pax({ mtime: '1759824000.123456789' }), 'x'),
        entry('links.json', '[]'),
        entry('acme/VKO-1.md', '---\nkey: acme/VKO-1\n---\n'),
      ),
    );

    expect(await readManifest(archive)).toEqual(manifest);
  });

  it('reads it from an archive larger than what one slice of it unpacks to', async () => {
    const archive = await gzip(
      tar(entry('manifest.json', JSON.stringify(manifest)), entry('big.md', 'x'.repeat(300_000))),
    );

    expect(await readManifest(archive)).toEqual(manifest);
  });

  it('is null for an archive without one, and for bytes that are no gzip', async () => {
    expect(await readManifest(await gzip(tar(entry('acme/VKO-1.md', '# One'))))).toBeNull();
    expect(await readManifest(new Blob(['not an archive']))).toBeNull();
  });

  it('is null where the browser has no DecompressionStream', async () => {
    const archive = await gzip(tar(entry('manifest.json', JSON.stringify(manifest))));
    vi.stubGlobal('DecompressionStream', undefined);
    try {
      expect(await readManifest(archive)).toBeNull();
    } finally {
      vi.unstubAllGlobals();
    }
  });
});
