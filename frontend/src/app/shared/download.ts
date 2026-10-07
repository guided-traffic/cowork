/** How long an object URL of a download stays usable after its click: WebKit may still read it then. */
const releaseAfter = 40_000;

/**
 * Saves a file the page holds as a download under its name: an object URL on a link the page
 * clicks, a request of nobody's origin but the page's own. The URL is let go some time after the
 * click rather than at once, so that a browser still reading it is not cut off.
 */
export function saveFile(document: Document, blob: Blob, name: string): void {
  const url = URL.createObjectURL(blob);
  const link = document.createElement('a');
  link.href = url;
  link.download = name;
  link.click();
  setTimeout(() => URL.revokeObjectURL(url), releaseAfter);
}
