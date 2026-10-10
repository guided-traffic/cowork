/**
 * The tabs of a team's configuration (docs/adr/0023 D4 as amended 2026-10-10), apart from the
 * dialog that shows them: the sidebar's gear, the head of a team's pages and the dialog read them,
 * and only the dialog is a lazy chunk.
 */

/** A page of a team's configuration: its last path segment under `/t/{slug}` and its tab's label. */
export interface ConfigTab {
  path: string;
  label: string;
}

/** Who sees a configuration page: everybody, the team's administrators, or those and a global administrator's oversight. */
type ShownTo = 'everybody' | 'admin' | 'admin-or-oversight';

/** The configuration's pages in the order of their tabs, each with who sees it (docs/adr/0034). */
const configPages: readonly (ConfigTab & { shownTo: ShownTo })[] = [
  { path: 'members', label: 'Members', shownTo: 'everybody' },
  { path: 'accounts', label: 'Accounts', shownTo: 'admin' },
  { path: 'group-mappings', label: 'Group mappings', shownTo: 'admin-or-oversight' },
  { path: 'tokens', label: 'Tokens', shownTo: 'admin' },
  { path: 'settings', label: 'Settings', shownTo: 'everybody' },
  { path: 'audit', label: 'Audit record', shownTo: 'admin' },
  { path: 'deleted-tickets', label: 'Deleted tickets', shownTo: 'admin' },
];

/**
 * The tabs of a team's configuration for a person: every page the role shows, as the navigation
 * showed them before the dialog — the members and the settings to everybody, the accounts, the
 * tokens, the audit record and the deleted tickets to the team's administrators, the group mappings
 * to them and to a global administrator who only oversees the team (docs/adr/0034 D2).
 */
export function configTabs(admin: boolean, oversight: boolean): ConfigTab[] {
  return configPages
    .filter(
      ({ shownTo }) =>
        shownTo === 'everybody' ||
        (shownTo === 'admin' && admin) ||
        (shownTo === 'admin-or-oversight' && (admin || oversight)),
    )
    .map(({ path, label }) => ({ path, label }));
}

/** The tab a team's gear opens: the settings, or the first tab the role shows without them. */
export function gearTab(tabs: ConfigTab[]): string {
  return tabs.some((tab) => tab.path === 'settings') ? 'settings' : (tabs[0]?.path ?? 'settings');
}

/**
 * The history state of an address of the configuration that a link of this application opened over
 * a page — a gear, a tab of the dialog itself —, which closing the dialog goes back to. An address
 * opened directly carries none: a bookmark, a typed or pasted address. The browser keeps the state
 * with its history entry, across the back button and a reload.
 */
export const openedFromPage: Readonly<Record<string, unknown>> = { openedFromPage: true };

/** Whether a history state is {@link openedFromPage}'s. */
export function isOpenedFromPage(state: unknown): boolean {
  return (state as Record<string, unknown> | null | undefined)?.['openedFromPage'] === true;
}
