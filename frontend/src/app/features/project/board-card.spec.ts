import { provideLocationMocks } from '@angular/common/testing';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { By } from '@angular/platform-browser';
import { provideRouter, Router } from '@angular/router';
import { Tooltip } from 'primeng/tooltip';
import { Block, Ticket, TicketHead, TicketState } from '../../api/models';
import { Move } from '../../shared/transitions';
import { BoardCard } from './board-card';

const sam = { id: 'p2', display_name: 'Sam Rivera', username: 'local:sam' };

function ticket(fields: Partial<Ticket> = {}): Ticket {
  return {
    key: 'acme/COW-12',
    number: 12,
    type: 'bug',
    title: 'The board flickers when an event arrives during a drag',
    state: 'in-progress',
    severity: 'high',
    security: 'boundary',
    horizon: 'now',
    effort: 'M',
    assignee: sam,
    block: null,
    open_prerequisites: 0,
    score: null,
    score_version: null,
    progress_derived: false,
    progress_refinement: 100,
    progress: 40,
    progress_review: 10,
    ...fields,
  } as Ticket;
}

const analyse: Move = { to: 'analysed', kind: 'forward', label: 'Move to analysed', input: 'none' };

/** The head of the card's parent (docs/adr/0005 D3). */
function parent(overrides: Partial<TicketHead> = {}): TicketHead {
  return {
    team: { slug: 'acme', name: 'Acme' },
    key: 'acme/COW-3',
    title: 'Rework the board',
    type: 'feature',
    state: 'in-progress',
    placeholder: false,
    readable: true,
    ...overrides,
  };
}

describe('BoardCard', () => {
  let clicks: number;

  beforeEach(() => {
    clicks = 0;
    // The key is a link that navigates when it is clicked; the navigation needs a route to end at.
    TestBed.configureTestingModule({
      providers: [provideRouter([{ path: '**', children: [] }]), provideLocationMocks()],
    });
  });

  async function render(current: Ticket, inputs: Record<string, unknown> = {}) {
    const fixture = TestBed.createComponent(BoardCard);
    fixture.componentRef.setInput('ticket', current);
    fixture.componentRef.setInput('tenant', 'acme');
    for (const [name, value] of Object.entries(inputs)) {
      fixture.componentRef.setInput(name, value);
    }
    (fixture.nativeElement as HTMLElement).addEventListener('click', () => clicks++);
    await fixture.whenStable();
    return fixture;
  }

  const host = (fixture: ComponentFixture<BoardCard>) => fixture.nativeElement as HTMLElement;
  const el = (fixture: ComponentFixture<BoardCard>, selector: string) =>
    host(fixture).querySelector<HTMLElement>(selector);
  const text = (fixture: ComponentFixture<BoardCard>, selector: string) =>
    el(fixture, selector)?.textContent?.replace(/\s+/g, ' ').trim();
  const bar = (fixture: ComponentFixture<BoardCard>) =>
    el(fixture, 'app-stage-bar')?.getAttribute('aria-label');

  describe('in a state column', () => {
    it('shows the type, the key as a link to the ticket, the title, severity, security, size and assignee', async () => {
      const fixture = await render(ticket());

      expect(el(fixture, 'app-type')?.getAttribute('title')).toContain('bug');
      expect(text(fixture, '[data-testid="card-key"]')).toBe('COW-12');
      expect(el(fixture, '[data-testid="card-key"]')?.getAttribute('href')).toBe(
        '/t/acme/tickets/COW-12',
      );
      expect(text(fixture, '[data-testid="card-title"]')).toBe(
        'The board flickers when an event arrives during a drag',
      );
      expect(el(fixture, '[data-severity]')?.getAttribute('data-severity')).toBe('high');
      expect(el(fixture, '[data-security]')?.getAttribute('data-security')).toBe('boundary');
      expect(el(fixture, 'app-size')?.getAttribute('aria-label')).toBe('Effort M');
      expect(text(fixture, '[data-testid="card-assignee"]')).toBe('Sam Rivera');
      expect(host(fixture).getAttribute('data-key')).toBe('acme/COW-12');
    });

    it('shows no assignee when nobody is assigned, and no security badge for none', async () => {
      const fixture = await render(ticket({ assignee: null, security: 'none' }));

      expect(el(fixture, '[data-testid="card-assignee"]')).toBeNull();
      expect(el(fixture, '[data-security]')).toBeNull();
    });

    it.each([
      ['filed', 'filed'],
      ['analysed', 'analysed'],
    ] as [TicketState, string][])(
      'says which state of Refinement a %s card is in',
      async (state, shown) => {
        const fixture = await render(ticket({ state }));

        expect(el(fixture, '[data-state]')?.getAttribute('data-state')).toBe(shown);
      },
    );

    it.each(['decided', 'in-progress', 'review'] as TicketState[])(
      'shows no state on a %s card: its column says it',
      async (state) => {
        const fixture = await render(ticket({ state }));

        expect(el(fixture, '[data-state]')).toBeNull();
      },
    );

    it.each([
      ['filed', null, 'Refinement 100%'],
      ['analysed', null, 'Refinement 100%'],
      ['in-progress', null, 'Implementation 40%'],
      ['review', null, 'Review 10%'],
      ['blocked', 'review', 'Review 10%'],
      ['blocked', 'in-progress', 'Implementation 40%'],
      ['blocked', 'filed', 'Refinement 100%'],
    ] as [TicketState, TicketState | null, string][])(
      'shows on a %s card (blocked from %s) the bar %s (docs/adr/0018 D1)',
      async (state, from, label) => {
        const block = from ? ({ kind: 'human', from, reason: 'Waiting' } as Block) : null;
        const fixture = await render(ticket({ state, block }));

        expect(bar(fixture)).toBe(label);
      },
    );

    it('shows no bar on a decided card, which waits, nor on one blocked from decided', async () => {
      const decided = await render(ticket({ state: 'decided' }));
      expect(bar(decided)).toBeUndefined();

      const blocked = await render(
        ticket({
          state: 'blocked',
          block: { kind: 'decision', from: 'decided', reason: 'Which preset' } as Block,
        }),
      );
      expect(bar(blocked)).toBeUndefined();
    });

    it('says a parent stage comes from its children', async () => {
      const fixture = await render(ticket({ progress_derived: true }));

      expect(bar(fixture)).toBe('Implementation 40%, from its children');
    });

    it('shows the kind and the reason of a block, the reason in full in the tooltip', async () => {
      const fixture = await render(
        ticket({
          state: 'blocked',
          block: {
            kind: 'external',
            from: 'in-progress',
            reason: 'The provider sets the rule',
          } as Block,
        }),
      );

      expect(text(fixture, '[data-testid="card-block"]')).toBe(
        'external: The provider sets the rule',
      );
      expect(
        fixture.debugElement
          .query(By.css('[data-testid="card-block"]'))
          .injector.get(Tooltip)
          .content(),
      ).toBe('The provider sets the rule');
    });

    it('counts the open prerequisites when there are any', async () => {
      const none = await render(ticket());
      expect(el(none, '[data-testid="card-prerequisites"]')).toBeNull();

      const one = await render(ticket({ open_prerequisites: 1 }));
      expect(text(one, '[data-testid="card-prerequisites"]')).toBe('1 open prerequisite');

      const three = await render(ticket({ open_prerequisites: 3 }));
      expect(text(three, '[data-testid="card-prerequisites"]')).toBe('3 open prerequisites');
    });

    it('marks a ticket in the horizon release, and explains it', async () => {
      const release = await render(ticket({ horizon: 'release' }));
      expect(text(release, '[data-testid="card-release"]')).toBe('release');
      expect(
        release.debugElement
          .query(By.css('[data-testid="card-release"]'))
          .injector.get(Tooltip)
          .content(),
      ).toBe('Release: has to be in the next release');

      const now = await render(ticket());
      expect(el(now, '[data-testid="card-release"]')).toBeNull();
    });

    it('offers the card action as a button that says where it goes, and hands it on', async () => {
      const fixture = await render(ticket({ state: 'filed' }), { action: analyse });
      const acted: Move[] = [];
      fixture.componentInstance.act.subscribe((move) => acted.push(move));

      const button = el(fixture, '[data-testid="card-action-acme/COW-12"]');
      expect(button?.textContent?.trim()).toBe('analysed');
      expect(button?.getAttribute('aria-label')).toBe('Move COW-12 to analysed');
      button?.click();

      expect(acted).toEqual([analyse]);
      expect(clicks).toBe(0);
    });

    it('offers no card action where it has none', async () => {
      const fixture = await render(ticket());

      expect(el(fixture, '[data-testid^="card-action-"]')).toBeNull();
    });

    it('opens its menu from a button where it has moves, and hands on the click', async () => {
      const fixture = await render(ticket(), { movable: true });
      const opened: Event[] = [];
      fixture.componentInstance.menu.subscribe((event) => opened.push(event));

      const button = el(fixture, '[data-testid="card-menu-acme/COW-12"]');
      expect(button?.getAttribute('aria-label')).toBe('Move COW-12');
      expect(button?.getAttribute('aria-haspopup')).toBe('menu');
      button?.click();

      expect(opened).toHaveLength(1);
      expect(clicks).toBe(0);
    });

    it('shows no menu button where it has no moves', async () => {
      const fixture = await render(ticket());

      expect(el(fixture, '[data-testid^="card-menu-"]')).toBeNull();
    });

    describe('its parent (docs/adr/0008 D2, docs/adr/0005 D3)', () => {
      const chip = (fixture: ComponentFixture<BoardCard>) =>
        el(fixture, '[data-testid="card-parent"]');
      const tip = (fixture: ComponentFixture<BoardCard>) =>
        fixture.debugElement
          .query(By.css('[data-testid="card-parent"]'))
          .injector.get(Tooltip)
          .content();

      it('names no parent where the ticket has none', async () => {
        const fixture = await render(ticket({ parent: null, parent_head: null }));

        expect(chip(fixture)).toBeNull();
      });

      it('names a parent of its team by its key, a link to it, its title in the tooltip', async () => {
        const fixture = await render(ticket({ parent: 'acme/COW-3', parent_head: parent() }));

        expect(chip(fixture)?.textContent?.trim()).toBe('COW-3');
        expect(chip(fixture)?.getAttribute('href')).toBe('/t/acme/tickets/COW-3');
        expect(chip(fixture)?.getAttribute('aria-label')).toBe('Parent COW-3');
        expect(tip(fixture)).toBe('Its parent: Rework the board');
      });

      it('names a parent of another team by its team and key, linked in its team where the reader opens it', async () => {
        const head = parent({ team: { slug: 'globex', name: 'Globex' }, key: 'globex/API-7' });
        const fixture = await render(ticket({ parent: 'globex/API-7', parent_head: head }));

        expect(chip(fixture)?.textContent?.trim()).toBe('Globex · API-7');
        expect(chip(fixture)?.getAttribute('href')).toBe('/t/globex/tickets/API-7');
      });

      it('names a parent the reader may not open unlinked, and says so in the tooltip', async () => {
        const head = parent({
          team: { slug: 'globex', name: 'Globex' },
          key: 'globex/API-7',
          readable: false,
        });
        const fixture = await render(ticket({ parent: 'globex/API-7', parent_head: head }));

        expect(chip(fixture)?.tagName).toBe('SPAN');
        expect(text(fixture, '[data-testid="card-parent"]')).toBe('Parent Globex · API-7');
        expect(tip(fixture)).toBe('Its parent, which you cannot open: Rework the board');
      });

      // docs/adr/0065 D5 as amended 2026-10-10.
      it('names a parent the reader may not see as `<team> [Confidential]`, unlinked', async () => {
        const head = parent({
          key: null,
          title: null,
          type: null,
          state: null,
          placeholder: true,
          readable: false,
        });
        const fixture = await render(ticket({ parent: null, parent_head: head }));

        expect(chip(fixture)?.tagName).toBe('SPAN');
        expect(text(fixture, '[data-testid="card-parent"]')).toBe('Parent Acme [Confidential]');
        expect(tip(fixture)).toBe('Its parent is confidential: you may not see it');
      });

      it('keeps a click on the parent to its link, not the card', async () => {
        const fixture = await render(ticket({ parent: 'acme/COW-3', parent_head: parent() }));

        chip(fixture)?.dispatchEvent(new MouseEvent('click', { bubbles: true, cancelable: true }));
        await fixture.whenStable();

        expect(clicks).toBe(0);
        expect(TestBed.inject(Router).url).toBe('/t/acme/tickets/COW-3');
      });
    });

    it('keeps a click on its key to the link, not the card', async () => {
      const fixture = await render(ticket());

      el(fixture, '[data-testid="card-key"]')?.dispatchEvent(
        new MouseEvent('click', { bubbles: true, cancelable: true }),
      );
      await fixture.whenStable();

      expect(clicks).toBe(0);
      expect(TestBed.inject(Router).url).toBe('/t/acme/tickets/COW-12');
    });
  });

  describe('in the column next', () => {
    it('is compact: the key, the size, the title, the state and the button that makes it now', async () => {
      const fixture = await render(ticket({ state: 'review', horizon: 'next' }), {
        compact: true,
      });

      expect(host(fixture).classList).toContain('compact');
      expect(text(fixture, '[data-testid="card-key"]')).toBe('COW-12');
      expect(el(fixture, 'app-size')?.getAttribute('aria-label')).toBe('Effort M');
      expect(text(fixture, '[data-testid="card-title"]')).toContain('The board flickers');
      expect(el(fixture, '[data-state]')?.getAttribute('data-state')).toBe('review');
      expect(el(fixture, 'app-type')).toBeNull();
      expect(el(fixture, '[data-severity]')).toBeNull();
      expect(el(fixture, 'app-stage-bar')).toBeNull();
      expect(el(fixture, '[data-testid^="card-menu-"]')).toBeNull();
    });

    it('names no parent: the compact card keeps to the key, the title, the size and the state', async () => {
      const fixture = await render(
        ticket({ horizon: 'next', parent: 'acme/COW-3', parent_head: parent() }),
        { compact: true },
      );

      expect(el(fixture, '[data-testid="card-parent"]')).toBeNull();
    });

    it('makes the ticket now from its button, and keeps the click from the card', async () => {
      const fixture = await render(ticket({ horizon: 'next' }), { compact: true });
      let made = 0;
      fixture.componentInstance.now.subscribe(() => made++);

      const button = el(fixture, '[data-testid="card-now-acme/COW-12"]');
      expect(button?.textContent?.trim()).toBe('Now');
      expect(button?.getAttribute('aria-label')).toBe('Make COW-12 now');
      expect(
        fixture.debugElement
          .query(By.css('[data-testid="card-now-acme/COW-12"]'))
          .injector.get(Tooltip)
          .content(),
      ).toBe('Move it to the horizon now; it moves to the column of its state');
      button?.click();

      expect(made).toBe(1);
      expect(clicks).toBe(0);
    });
  });
});
