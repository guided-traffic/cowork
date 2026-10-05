import { HttpErrorResponse } from '@angular/common/http';
import { signal } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { By } from '@angular/platform-browser';
import { provideRouter } from '@angular/router';
import { MessageService } from 'primeng/api';
import { SelectButton } from 'primeng/selectbutton';
import { PrerequisiteNode, PrerequisiteTree as Tree } from '../../api/models';
import { StageBar } from '../../shared/stage-bar';
import { nodeStage, nodeValue, PrerequisiteTree } from './prerequisite-tree';
import { TicketRelations, TreeDirection } from './ticket-relations';

function node(overrides: Partial<PrerequisiteNode> = {}): PrerequisiteNode {
  return {
    key: 'acme/COW-7',
    title: 'Pick the format',
    state: 'in-progress',
    blocked_from: null,
    assignee: { id: 'p1', display_name: 'Ada Lovelace' },
    progress: 40,
    progress_refinement: 100,
    progress_review: 0,
    progress_derived: false,
    depth: 1,
    settled: false,
    repeated: false,
    ...overrides,
  };
}

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
    expect(nodeStage(node({ state, blocked_from: from }))).toBe(stage);
  });

  it('reads the value of each stage', () => {
    const n = node({ progress_refinement: 100, progress: 40, progress_review: 5 });

    expect([
      nodeValue(n, 'refinement'),
      nodeValue(n, 'implementation'),
      nodeValue(n, 'review'),
    ]).toEqual([100, 40, 5]);
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
    const link = first.querySelector('a.key') as HTMLAnchorElement;
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
        node({ key: 'acme/COW-7', depth: 1 }),
        node({ key: 'acme/COW-3', depth: 2 }),
        node({ key: 'acme/OPS-1', depth: 3 }),
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
    const fixture = render({ items: [node({ key: 'acme/OPS-1' })], next_cursor: null, open: 1 });

    expect(nodes(fixture)[0].querySelector('a.key')?.textContent?.trim()).toBe('OPS-1');
  });

  it('marks the settled nodes and the ones the tree holds a second time, and draws no bar for a closed one', () => {
    const fixture = render({
      items: [
        node({ key: 'acme/COW-7', state: 'done', settled: true }),
        node({ key: 'acme/COW-3', repeated: true, depth: 2 }),
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
