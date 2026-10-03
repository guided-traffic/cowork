import { provideLocationMocks } from '@angular/common/testing';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { By } from '@angular/platform-browser';
import { provideRouter, Router } from '@angular/router';
import { Tooltip } from 'primeng/tooltip';
import { Block, Ticket, TicketState } from '../../api/models';
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
    urgency: 'now',
    effort: 'M',
    assignee: sam,
    block: null,
    open_prerequisites: 0,
    progress_derived: false,
    progress_refinement: 100,
    progress: 40,
    progress_review: 10,
    ...fields,
  } as Ticket;
}

const analyse: Move = { to: 'analysed', kind: 'forward', label: 'Move to analysed', input: 'none' };

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

    it('marks a ticket of urgency release, and explains it', async () => {
      const release = await render(ticket({ urgency: 'release' }));
      expect(text(release, '[data-testid="card-release"]')).toBe('release');
      expect(
        release.debugElement
          .query(By.css('[data-testid="card-release"]'))
          .injector.get(Tooltip)
          .content(),
      ).toBe('Gates the release, or is gated on it');

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
      const fixture = await render(ticket({ state: 'review', urgency: 'next' }), {
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

    it('makes the ticket now from its button, and keeps the click from the card', async () => {
      const fixture = await render(ticket({ urgency: 'next' }), { compact: true });
      let made = 0;
      fixture.componentInstance.now.subscribe(() => made++);

      const button = el(fixture, '[data-testid="card-now-acme/COW-12"]');
      expect(button?.textContent?.trim()).toBe('Now');
      expect(button?.getAttribute('aria-label')).toBe('Make COW-12 now');
      button?.click();

      expect(made).toBe(1);
      expect(clicks).toBe(0);
    });
  });
});
