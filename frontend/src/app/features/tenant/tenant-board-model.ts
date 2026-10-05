import { Project } from '../../api/models';

/**
 * The projects the address names, `?project=COW&project=OPS` — a repeated parameter, as the API's
 * filters are (docs/adr/0049 D1): the swimlanes the filter shows, each once; none, every one.
 */
export function chosenKeys(param: string | readonly string[] | null | undefined): string[] {
  const values = param === undefined || param === null ? [] : [param].flat();
  return [...new Set(values.map((value) => value.trim()).filter((value) => value !== ''))];
}

/**
 * The swimlanes of the tenant's board (docs/adr/0018 D4): one for each project the person sees that
 * is not archived, in the order of the list, which is the projects' keys — or, where the filter
 * names projects, one for each of them.
 */
export function lanesOf(projects: readonly Project[], chosen: readonly string[]): Project[] {
  const open = projects.filter((project) => !project.archived_at);
  if (chosen.length === 0) {
    return open;
  }
  const named = new Set(chosen);
  return open.filter((project) => named.has(project.key));
}

/** The swimlane an element of the page is in, by the project key its `data-lane` names. */
export function laneOf(element: Element | null | undefined): string | null {
  return element?.closest('[data-lane]')?.getAttribute('data-lane') ?? null;
}

/**
 * The swimlane that refuses a card dragged from the swimlane `from` while the pointer is over the
 * swimlane `over`: any other one — a ticket never changes project on a board (docs/adr/0018 D4) —,
 * and none while the pointer is over the card's own or over no swimlane.
 */
export function refusingLane(from: string, over: string | null): string | null {
  return over !== null && over !== from ? over : null;
}
