import { HttpErrorResponse } from '@angular/common/http';
import { signal } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { By } from '@angular/platform-browser';
import { provideRouter } from '@angular/router';
import { MessageService } from 'primeng/api';
import { SelectButton } from 'primeng/selectbutton';
import {
  PrerequisiteHeadNode,
  PrerequisiteHeadTree as Tree,
  TicketHead,
  TicketState,
} from '../../api/models';
import { SessionService } from '../../core/session.service';
import { StageBar } from '../../shared/stage-bar';
import { nodeStage, nodeValue, PrerequisiteTree } from './prerequisite-tree';
import { TicketRelations, TreeDirection } from './ticket-relations';

/** The head of a ticket of the reader's team, which they read. */
function head(key = 'acme/COW-7', overrides: Partial<TicketHead> = {}): TicketHead {
  return {
    team: {
      slug: key.slice(0, key.indexOf('/')),
      name: key.startsWith('acme/') ? 'Acme' : 'Globex',
    },
    key,
    title: 'Pick the format',
    type: 'task',
    state: 'in-progress',
    placeholder: false,
    readable: true,
    ...overrides,
  };
}

/** A node of the reader's own team, which they read: with its assignee and its stages. */
function node(
  overrides: Partial<PrerequisiteHeadNode> = {},
  state: TicketState = 'in-progress',
): PrerequisiteHeadNode {
  return {
    depth: 1,
    settled: false,
    repeated: false,
    blocked_from: null,
    head: head('acme/COW-7', { state }),
    assignee: { id: 'p1', display_name: 'Ada Lovelace' },
    progress: { refinement: 100, implementation: 40, review: 0, derived: false },
    ...overrides,
  };
}

/** A node the reader reads by its head only: another team's, no assignee and no stages. */
function stranger(overrides: Partial<TicketHead> = {}): PrerequisiteHeadNode {
  return node({
    head: head('globex/API-7', {
      title: 'Send the SameSite attribute',
      state: 'review',
      readable: false,
      ...overrides,
    }),
    assignee: null,
    progress: null,
  });
}

/** A node the reader may not see: the placeholder, nothing behind it. */
const confidential: PrerequisiteHeadNode = node({
  settled: null,
  head: {
    team: { slug: 'globex', name: 'Globex' },
    key: null,
    title: null,
    type: null,
    state: null,
    placeholder: true,
    readable: false,
  },
  assignee: null,
  progress: null,
});

describe('nodeStage', () => {
  it.each([
    ['filed', null, 'refinement'],
    ['analysed', null, 'refinement'],
    ['decided', null, null],
    ['in-progress', null, 'implementation'],
    ['review', null, 'review'],
    ['blocked', 'review', 'review'],
    ['blocked', null, null],
    ['done', null, null],
    ['dropped', null, null],
  ] as const)('is for %s (from %s) the stage %s', (state, from, stage) => {
    expect(nodeStage(node({ blocked_from: from }, state))).toBe(stage);
  });

  it('is none for a node read by its head only, and for the placeholder: their stages are not shown', () => {
    expect(nodeStage(stranger())).toBeNull();
    expect(nodeStage(confidential)).toBeNull();
  });

  it('reads the value of each stage, and 0 where the stages are not shown', () => {
    const n = node({
      progress: { refinement: 100, implementation: 40, review: 5, derived: false },
    });

    expect([
      nodeValue(n, 'refinement'),
      nodeValue(n, 'implementation'),
      nodeValue(n, 'review'),
    ]).toEqual([100, 40, 5]);
    expect(nodeValue(stranger(), 'implementation')).toBe(0);
  });
});

describe('PrerequisiteTree', () => {
  let value: ReturnType<typeof signal<Tree | undefined>>;
  let failure: ReturnType<typeof signal<unknown>>;
  let direction: ReturnType<typeof signal<TreeDirection>>;

  beforeEach(() => {
    value = signal<Tree | undefined>(undefined);
    failure = signal<unknown>(undefined);
    direction = signal<TreeDirection>('down');
    TestBed.configureTestingModule({
      providers: [
        provideRouter([]),
        MessageService,
        {
          provide: TicketRelations,
          useValue: {
            direction,
            tree: { hasValue: () => value() !== undefined, value, error: failure },
          },
        },
        { provide: SessionService, useValue: { tenant: signal('acme') } },
      ],
    });
  });

  function render(tree?: Tree) {
    value.set(tree);
    const fixture = TestBed.createComponent(PrerequisiteTree);
    fixture.detectChanges();
    return fixture;
  }

  const page = (fixture: ComponentFixture<PrerequisiteTree>) =>
    fixture.nativeElement as HTMLElement;
  const el = (fixture: ComponentFixture<PrerequisiteTree>, testId: string) =>
    page(fixture).querySelector<HTMLElement>(`[data-testid="${testId}"]`);
  const nodes = (fixture: ComponentFixture<PrerequisiteTree>) => [
    ...page(fixture).querySelectorAll<HTMLElement>('li.node'),
  ];

  it('shows a skeleton until the tree is loaded', () => {
    const fixture = render();

    expect(page(fixture).querySelector('p-skeleton')).not.toBeNull();
    expect(el(fixture, 'tree-open')).toBeNull();
  });

  it('shows each node with its key as a link, its title, its state, its assignee and the bar of its stage', () => {
    const fixture = render({ items: [node()], next_cursor: null, open: 1 });

    const [first] = nodes(fixture);
    const link = first.querySelector('a[data-testid="head-key"]') as HTMLAnchorElement;
    expect(link.textContent?.trim()).toBe('COW-7');
    expect(link.getAttribute('href')).toBe('/t/acme/tickets/COW-7');
    expect(first.querySelector('.title')?.textContent).toBe('Pick the format');
    expect(first.querySelector('app-state')?.textContent).toContain('in-progress');
    expect(first.textContent).toContain('Ada Lovelace');
    const bar = fixture.debugElement.query(By.directive(StageBar)).componentInstance as StageBar;
    expect([bar.stage(), bar.value(), bar.derived()]).toEqual(['implementation', 40, false]);
  });

  it('indents each node by its depth, depth first as the API orders them', () => {
    const fixture = render({
      items: [
        node({ head: head('acme/COW-7'), depth: 1 }),
        node({ head: head('acme/COW-3'), depth: 2 }),
        node({ head: head('acme/OPS-1'), depth: 3 }),
      ],
      next_cursor: null,
      open: 3,
    });

    expect(nodes(fixture).map((li) => [li.dataset['key'], li.style.paddingLeft])).toEqual([
      ['acme/COW-7', '0rem'],
      ['acme/COW-3', '1.25rem'],
      ['acme/OPS-1', '2.5rem'],
    ]);
  });

  it('names a ticket of another project by its own key', () => {
    const fixture = render({
      items: [node({ head: head('acme/OPS-1') })],
      next_cursor: null,
      open: 1,
    });

    expect(nodes(fixture)[0].querySelector('a[data-testid="head-key"]')?.textContent?.trim()).toBe(
      'OPS-1',
    );
  });

  // docs/adr/0012 D6 as amended 2026-10-10, docs/adr/0005 D3.
  it('shows a node of another team the reader may not open by its team and head, unlinked, without assignee or bar', () => {
    const fixture = render({ items: [stranger()], next_cursor: null, open: 1 });

    const [shown] = nodes(fixture);
    expect(shown.dataset['key']).toBe('globex/API-7');
    expect(shown.querySelector('[data-testid="head-team"]')?.textContent?.trim()).toBe('Globex');
    expect(shown.querySelector('[data-testid="head-key"]')?.tagName).toBe('SPAN');
    expect(shown.querySelector('a')).toBeNull();
    expect(shown.querySelector('.title')?.textContent).toBe('Send the SameSite attribute');
    expect(shown.querySelector('app-state [data-state]')?.getAttribute('data-state')).toBe(
      'review',
    );
    expect(shown.textContent).not.toContain('unassigned');
    expect(shown.querySelector('app-stage-bar')).toBeNull();
  });

  it('links a node of another team the reader opens in its team, still without assignee or bar', () => {
    const fixture = render({
      items: [stranger({ readable: true })],
      next_cursor: null,
      open: 1,
    });

    const [shown] = nodes(fixture);
    expect(shown.querySelector('a[data-testid="head-key"]')?.getAttribute('href')).toBe(
      '/t/globex/tickets/API-7',
    );
    expect(shown.querySelector('app-stage-bar')).toBeNull();
  });

  // docs/adr/0065 D5 as amended 2026-10-10: the node shows as the placeholder, nothing behind it.
  it('shows a node the reader may not see as `<team> [Confidential]` and nothing else', () => {
    const fixture = render({ items: [node(), confidential], next_cursor: null, open: 1 });

    const shown = nodes(fixture)[1];
    expect(shown.querySelector('[data-testid="head-placeholder"]')?.textContent?.trim()).toBe(
      'Globex [Confidential]',
    );
    expect(shown.querySelector('[data-testid="head-key"]')).toBeNull();
    expect(shown.querySelector('.title')).toBeNull();
    expect(shown.querySelector('app-state')).toBeNull();
    expect(shown.querySelector('.settled-mark')).toBeNull();
    expect(shown.textContent?.replace(/\s+/g, ' ').trim()).toBe('Globex [Confidential]');
  });

  it('marks the settled nodes and the ones the tree holds a second time, and draws no bar for a closed one', () => {
    const fixture = render({
      items: [
        node({ head: head('acme/COW-7', { state: 'done' }), settled: true }),
        node({ head: head('acme/COW-3'), repeated: true, depth: 2 }),
      ],
      next_cursor: null,
      open: 1,
    });

    const [settled, repeated] = nodes(fixture);
    expect(settled.classList).toContain('settled');
    expect(settled.querySelector('.settled-mark')?.getAttribute('aria-label')).toBe('settled');
    expect(settled.querySelector('app-stage-bar')).toBeNull();
    expect(repeated.classList).toContain('repeated');
    expect(repeated.textContent).toContain('also above');
    expect(fixture.debugElement.queryAll(By.directive(StageBar))).toHaveLength(1);
  });

  it('counts the open tickets of the whole tree, as the API counts them', () => {
    const fixture = render({ items: [node()], next_cursor: 'more', open: 12 });

    expect(el(fixture, 'tree-open')?.textContent).toBe('12 open');
    expect(el(fixture, 'tree-more')?.textContent?.trim()).toBe(
      'The tree goes on beyond what is shown here.',
    );
  });

  it('says that nothing blocks the ticket, and read upward that it blocks nothing', () => {
    const fixture = render({ items: [], next_cursor: null, open: 0 });
    expect(el(fixture, 'tree-empty')?.textContent?.trim()).toBe('Nothing blocks this ticket.');
    expect(page(fixture).querySelector('h2')?.textContent).toContain('Prerequisites');

    direction.set('up');
    fixture.detectChanges();

    expect(el(fixture, 'tree-empty')?.textContent?.trim()).toBe('This ticket blocks nothing.');
    expect(page(fixture).querySelector('h2')?.textContent).toContain('Dependents');
  });

  it('turns the tree the other way on request', () => {
    const fixture = render({ items: [], next_cursor: null, open: 0 });

    fixture.debugElement
      .query(By.directive(SelectButton))
      .triggerEventHandler('ngModelChange', 'up');

    expect(direction()).toBe('up');
  });

  it('says why when the tree cannot be loaded', () => {
    failure.set(
      new HttpErrorResponse({
        status: 500,
        error: {
          type: 'about:blank',
          title: 'Internal',
          status: 500,
          code: 'internal',
          detail: 'Boom',
        },
      }),
    );
    const fixture = render();

    expect(el(fixture, 'tree-failed')?.textContent).toBe('Could not load this: Boom');
    expect(page(fixture).querySelector('p-skeleton')).toBeNull();
  });
});
