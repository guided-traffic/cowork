import { Component, signal } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { By } from '@angular/platform-browser';
import { provideRouter, RouterLink } from '@angular/router';
import { Tooltip } from 'primeng/tooltip';
import { SearchHit, Ticket, TicketHead } from '../api/models';
import {
  HeadKey,
  headLabel,
  headName,
  headOfHit,
  headOfKey,
  headOfTicket,
  headRoute,
  ofAnotherTeam,
  parentChip,
  placeholderText,
  settledPrerequisite,
  shortKeyOf,
  TicketChoice,
} from './ticket-head';

/** A ticket of team `acme`, readable. */
const own: TicketHead = {
  team: { slug: 'acme', name: 'Acme' },
  key: 'acme/COW-3',
  title: 'Rework the board',
  type: 'feature',
  state: 'in-progress',
  placeholder: false,
  readable: true,
};
/** A ticket of team `globex`, which the reader reads by its head only. */
const stranger: TicketHead = {
  team: { slug: 'globex', name: 'Globex' },
  key: 'globex/API-7',
  title: 'Send the SameSite attribute',
  type: 'task',
  state: 'review',
  placeholder: false,
  readable: false,
};
/** A ticket of team `globex` the reader is a member of. */
const neighbour: TicketHead = { ...stranger, readable: true };
/** A confidential ticket the reader may not see. */
const confidential: TicketHead = {
  team: { slug: 'globex', name: 'Globex' },
  key: null,
  title: null,
  type: null,
  state: null,
  placeholder: true,
  readable: false,
};

describe('the head of a ticket (docs/adr/0005 D3, docs/adr/0065 D5)', () => {
  it('names a ticket the reader may not see by its team alone, `<team> [Confidential]`', () => {
    expect(placeholderText(confidential)).toBe('Globex [Confidential]');
    expect(shortKeyOf(confidential)).toBeNull();
    expect(headName(confidential, 'acme')).toBe('Globex [Confidential]');
    expect(headName(confidential, 'globex')).toBe('Globex [Confidential]');
    expect(headLabel(confidential, 'acme')).toBe('Globex [Confidential]');
  });

  it('names a ticket of the team the page shows by its short key, one of another team with its team', () => {
    expect(shortKeyOf(own)).toBe('COW-3');
    expect(headName(own, 'acme')).toBe('COW-3');
    expect(headName(stranger, 'acme')).toBe('Globex · API-7');
    expect(headLabel(own, 'acme')).toBe('COW-3 Rework the board');
    expect(headLabel(stranger, 'acme')).toBe('Globex · API-7 Send the SameSite attribute');
    expect(ofAnotherTeam(own, 'acme')).toBe(false);
    expect(ofAnotherTeam(stranger, 'acme')).toBe(true);
    expect(ofAnotherTeam(own, null)).toBe(true);
  });

  it('opens a ticket the reader reads in its own team, and neither a head they may not open nor the placeholder', () => {
    expect(headRoute(own)).toEqual(['/t', 'acme', 'tickets', 'COW-3']);
    expect(headRoute(neighbour)).toEqual(['/t', 'globex', 'tickets', 'API-7']);
    expect(headRoute(stranger)).toBeNull();
    expect(headRoute(confidential)).toBeNull();
  });

  it('makes the head of a ticket, of a search hit and of a key the person reads, readable', () => {
    const ticket = {
      key: 'acme/COW-3',
      title: 'Rework the board',
      type: 'feature',
      state: 'in-progress',
    } as Ticket;
    expect(headOfTicket(ticket, { slug: 'acme', name: 'Acme' })).toEqual(own);
    const hit = {
      key: 'globex/API-7',
      team: { slug: 'globex', name: 'Globex' },
      title: 'Send the SameSite attribute',
      type: 'task',
      state: 'review',
    } as SearchHit;
    expect(headOfHit(hit)).toEqual(neighbour);
    expect(headOfKey('globex/API-7')).toEqual({
      team: { slug: 'globex', name: 'globex' },
      key: 'globex/API-7',
      title: null,
      type: null,
      state: null,
      placeholder: false,
      readable: true,
    });
  });
});

describe('parentChip', () => {
  it('names a parent of the team by its key, linked, its title in the tooltip', () => {
    expect(parentChip(own, 'acme')).toEqual({
      text: 'COW-3',
      route: ['/t', 'acme', 'tickets', 'COW-3'],
      label: 'Parent COW-3',
      tip: 'Its parent: Rework the board',
    });
  });

  it('names a parent of another team the reader may not open by its team and key, unlinked', () => {
    expect(parentChip(stranger, 'acme')).toEqual({
      text: 'Globex · API-7',
      route: null,
      label: 'Parent Globex · API-7',
      tip: 'Its parent, which you cannot open: Send the SameSite attribute',
    });
  });

  it('names a parent the reader may not see by its placeholder alone', () => {
    expect(parentChip(confidential, 'acme')).toEqual({
      text: 'Globex [Confidential]',
      route: null,
      label: 'Parent Globex [Confidential]',
      tip: 'Its parent is confidential: you may not see it',
    });
  });
});

describe('settledPrerequisite (docs/adr/0012 D5)', () => {
  it('names the prerequisite of another team by its team and key, with the state it reached', () => {
    expect(settledPrerequisite({ prerequisite: { ...stranger, state: 'done' } })).toEqual({
      name: 'Globex · API-7',
      state: 'done',
    });
  });

  it('names a confidential one by its placeholder, without a state', () => {
    expect(settledPrerequisite({ prerequisite: confidential })).toEqual({
      name: 'Globex [Confidential]',
      state: null,
    });
  });

  it('names none where the act carries none', () => {
    expect(settledPrerequisite(null)).toBeNull();
    expect(settledPrerequisite({ state: 'done' })).toBeNull();
    expect(settledPrerequisite({ prerequisite: { team: stranger.team, key: 7 } })).toBeNull();
  });
});

@Component({
  imports: [TicketChoice],
  template: `<app-ticket-choice [head]="head()" here="acme" />`,
})
class ChoiceHost {
  readonly head = signal<TicketHead>(own);
}

describe('TicketChoice', () => {
  async function render(head: TicketHead) {
    TestBed.configureTestingModule({ providers: [provideRouter([])] });
    const fixture = TestBed.createComponent(ChoiceHost);
    fixture.componentInstance.head.set(head);
    fixture.detectChanges();
    await fixture.whenStable();
    return fixture.nativeElement as HTMLElement;
  }

  it('shows the type, the key of another team with its team, the title and the state, and no link', async () => {
    const page = await render(stranger);

    expect(page.querySelector('app-type')?.getAttribute('title')).toContain('task');
    expect(page.querySelector('[data-testid="head-team"]')?.textContent?.trim()).toBe('Globex');
    expect(page.querySelector('.title')?.textContent).toBe('Send the SameSite attribute');
    expect(page.querySelector('[data-state]')?.getAttribute('data-state')).toBe('review');
    expect(page.querySelector('a')).toBeNull();
  });

  it('shows a readable ticket of the team by its key alone, never as a link', async () => {
    const page = await render(own);

    expect(page.querySelector('[data-testid="head-key"]')?.tagName).toBe('SPAN');
    expect(page.querySelector('[data-testid="head-team"]')).toBeNull();
    expect(page.querySelector('a')).toBeNull();
  });

  it('shows the placeholder and nothing else', async () => {
    const page = await render(confidential);

    expect(page.textContent?.replace(/\s+/g, ' ').trim()).toBe('Globex [Confidential]');
  });
});

@Component({
  imports: [HeadKey],
  template: `<app-head-key [head]="head()" [here]="here()" [linked]="linked()" />`,
})
class Host {
  readonly head = signal<TicketHead>(own);
  readonly here = signal<string | null>('acme');
  readonly linked = signal(true);
}

describe('HeadKey', () => {
  async function render(head: TicketHead, linked = true) {
    TestBed.configureTestingModule({ providers: [provideRouter([])] });
    const fixture = TestBed.createComponent(Host);
    fixture.componentInstance.head.set(head);
    fixture.componentInstance.linked.set(linked);
    fixture.detectChanges();
    await fixture.whenStable();
    return { fixture, page: fixture.nativeElement as HTMLElement };
  }

  const byTestId = (page: HTMLElement, id: string) =>
    page.querySelector<HTMLElement>(`[data-testid="${id}"]`);
  const text = (element: HTMLElement | null) => element?.textContent?.replace(/\s+/g, ' ').trim();

  it('links a ticket of the team the page shows by its short key, without its team', async () => {
    const { page } = await render(own);

    const key = byTestId(page, 'head-key');
    expect(key?.tagName).toBe('A');
    expect(key?.getAttribute('href')).toBe('/t/acme/tickets/COW-3');
    expect(text(key)).toBe('COW-3');
    expect(byTestId(page, 'head-team')).toBeNull();
    expect(byTestId(page, 'head-placeholder')).toBeNull();
  });

  it('links a ticket of another team the reader reads in that team, its team before the key', async () => {
    const { page } = await render(neighbour);

    expect(text(byTestId(page, 'head-team'))).toBe('Globex');
    expect(byTestId(page, 'head-key')?.getAttribute('href')).toBe('/t/globex/tickets/API-7');
  });

  it('shows a head the reader may not open unlinked, and says so to a screen reader and in its tooltip', async () => {
    const { fixture, page } = await render(stranger);

    const key = byTestId(page, 'head-key');
    expect(key?.tagName).toBe('SPAN');
    expect(page.querySelector('a')).toBeNull();
    expect(text(key)).toBe('API-7, which you cannot open');
    expect(text(byTestId(page, 'head-team'))).toBe('Globex');
    expect(
      fixture.debugElement
        .query(By.css('[data-testid="head-key"]'))
        .injector.get(Tooltip)
        .content(),
    ).toBe('You cannot open it: you see its key, title, type and state only');
  });

  it('shows a ticket the reader may not see as `<team> [Confidential]` and nothing else — in another team or their own', async () => {
    const { page } = await render(confidential);

    expect(text(byTestId(page, 'head-placeholder'))).toBe('Globex [Confidential]');
    expect(byTestId(page, 'head-key')).toBeNull();
    expect(byTestId(page, 'head-team')).toBeNull();
    expect(page.querySelector('a')).toBeNull();

    const inOwnTeam = await renderAgain({ ...confidential, team: { slug: 'acme', name: 'Acme' } });
    expect(text(byTestId(inOwnTeam, 'head-placeholder'))).toBe('Acme [Confidential]');
  });

  it('shows the key without its link where the place asks for none, as an option of a choice', async () => {
    const { fixture, page } = await render(own, false);

    expect(page.querySelector('a')).toBeNull();
    expect(text(byTestId(page, 'head-key'))).toBe('COW-3');
    expect(fixture.debugElement.query(By.directive(RouterLink))).toBeNull();
  });

  /** A second rendering in the same test: the test bed is configured once. */
  async function renderAgain(head: TicketHead) {
    const fixture = TestBed.createComponent(Host);
    fixture.componentInstance.head.set(head);
    fixture.detectChanges();
    await fixture.whenStable();
    return fixture.nativeElement as HTMLElement;
  }
});
