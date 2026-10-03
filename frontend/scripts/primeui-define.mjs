// Prints the `ng build` / `ng serve` flag that puts the PrimeUI license key into the bundle
// (docs/adr/0052 D9), or nothing when there is no key. The key comes from PRIMEUI_LICENSE or from
// the file named by the first argument — a BuildKit secret in the image build, the untracked
// .dev/primeui-license locally — and is never kept in the repository. Without it the UI works
// and shows PrimeNG's license notice.
import { existsSync, readFileSync } from 'node:fs';

const file = process.argv[2];
const key = (process.env.PRIMEUI_LICENSE ?? (file && existsSync(file) ? readFileSync(file, 'utf8') : '')).trim();
if (key) {
  process.stdout.write(`--define=PRIMEUI_LICENSE=${JSON.stringify(key)}`);
}
