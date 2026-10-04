import { test } from './support/fixtures';

/**
 * The phase's own path, two identities in one test (docs/adr/0056 D2, D3): pending, because the
 * pages it walks through — "assigned to me" and the inbox — are not built yet. Playwright lists it
 * as skipped until it is written; it becomes a test with those pages, not later.
 *
 * The path, each step through the UI and the second identity in a browser context of its own:
 * 1. the local administrator files a ticket in a project of the fixture tenant and assigns it to a
 *    local account it made (seeded through the API, the temporary password changed);
 * 2. the account sees it in "assigned to me" and in its inbox, within the event stream's latency,
 *    without reloading;
 * 3. the account moves it forward on the board, and closes it with a verification note;
 * 4. the administrator's open page shows it done.
 */
test.fixme(
  'the owner files a ticket, assigns it to a second identity, which sees it in assigned to me and its inbox, moves it and closes it',
  { tag: '@smoke' },
  async () => {
    throw new Error('pending: the pages "assigned to me" and the inbox do not exist yet');
  },
);
