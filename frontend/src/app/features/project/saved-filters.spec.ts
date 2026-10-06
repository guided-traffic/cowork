import { Component, computed, signal, WritableSignal } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { By } from '@angular/platform-browser';
import { MessageService } from 'primeng/api';
import { Select } from 'primeng/select';
import type { MockInstance } from 'vitest';
import { SavedFilter, SavedFilterParameters } from '../../api/models';
import { SavedFiltersService } from '../../core/saved-filters.service';
import { SessionService } from '../../core/session.service';
import { backlogLeftOut, LeftOut } from './saved-filter-model';
import { SavedFilters } from './saved-filters';

const filter = (id: string, overrides: Partial<SavedFilter> = {}): SavedFilter => ({
  id,
  name: `Filter ${id}`,
  owner: { id: 'p-ada', display_name: 'Ada', username: 'ada' },
  shared: false,
  parameters: { severity: ['high'] },
  redacted: false,
  warnings: [],
  version: 1,
  created_at: '2026-10-01T10:00:00Z',
  updated_at: '2026-10-01T10:00:00Z',
  ...overrides,
});

@Component({
  imports: [SavedFilters],
  template: `<app-saved-filters
    [current]="current()"
    [applied]="applied()"
    [leftOut]="leftOut()"
    (chosen)="chosen.push($event)"
  />`,
})
class Host {
  readonly current = signal<SavedFilterParameters>({ state: ['blocked'], q: 'crash' });
  readonly applied = signal<SavedFilter | null>(null);
  readonly leftOut = signal<LeftOut>({});
  readonly chosen: (SavedFilter | null)[] = [];
}

describe('SavedFilters', () => {
  let list: WritableSignal<SavedFilter[]>;
  let create: MockInstance<SavedFiltersService['create']>;
  let update: MockInstance<SavedFiltersService['update']>;
  let remove: MockInstance<SavedFiltersService['remove']>;
  let reload: MockInstance<SavedFiltersService['reload']>;
  let role: WritableSignal<string>;

  beforeEach(() => {
    role = signal('member');
    list = signal([
      filter('mine'),
      filter('shared-mine', { shared: true }),
      filter('team', {
        shared: true,
        owner: { id: 'p-sam', display_name: 'Sam', username: 'sam' },
      }),
      filter('hidden', {
        shared: true,
        redacted: true,
        parameters: {},
        owner: { id: 'p-sam', display_name: 'Sam', username: 'sam' },
      }),
    ]);
    create = vi
      .fn<SavedFiltersService['create']>()
      .mockResolvedValue(filter('new', { name: 'Crashes' }));
    update = vi
      .fn<SavedFiltersService['update']>()
      .mockResolvedValue(filter('mine', { shared: true, version: 2 }));
    remove = vi.fn<SavedFiltersService['remove']>().mockResolvedValue(undefined);
    reload = vi.fn<SavedFiltersService['reload']>();
    TestBed.configureTestingModule({
      providers: [
        MessageService,
        { provide: SavedFiltersService, useValue: { list, create, update, remove, reload } },
        {
          provide: SessionService,
          useValue: {
            person: signal({ id: 'p-ada' }),
            tenant: signal('acme'),
            membership: computed(() => ({ tenant: { slug: 'acme' }, role: role() })),
          },
        },
      ],
    });
  });

  async function render() {
    const fixture = TestBed.createComponent(Host);
    await settle(fixture);
    return fixture;
  }

  async function settle(fixture: ComponentFixture<Host>) {
    for (let round = 0; round < 3; round++) {
      await new Promise((resolve) => setTimeout(resolve));
      fixture.detectChanges();
    }
    await fixture.whenStable();
  }

  const select = (fixture: ComponentFixture<Host>) =>
    fixture.debugElement.query(By.css('[data-testid="saved-filters"]')).componentInstance as Select;
  const choices = (fixture: ComponentFixture<Host>) =>
    (select(fixture).options() ?? []) as { id: string; label: string; disabled: boolean }[];
  const el = (fixture: ComponentFixture<Host>, testId: string) =>
    (fixture.nativeElement as HTMLElement).querySelector<HTMLElement>(`[data-testid="${testId}"]`);

  it('offers the person’s filters by name, another member’s with its owner, and one it cannot apply as such', async () => {
    const fixture = await render();

    expect(reload).toHaveBeenCalled();
    expect(select(fixture).options()).toEqual([
      { id: 'mine', label: 'Filter mine', disabled: false },
      { id: 'shared-mine', label: 'Filter shared-mine · shared', disabled: false },
      { id: 'team', label: 'Filter team · Sam', disabled: false },
      {
        id: 'hidden',
        label: 'Filter hidden · Sam (names something you cannot see)',
        disabled: true,
      },
    ]);
  });

  it('hands the chosen filter to the list, and none when cleared', async () => {
    const fixture = await render();
    const host = fixture.componentInstance;

    fixture.debugElement
      .query(By.css('[data-testid="saved-filters"]'))
      .triggerEventHandler('ngModelChange', 'team');
    fixture.debugElement
      .query(By.css('[data-testid="saved-filters"]'))
      .triggerEventHandler('ngModelChange', null);

    expect(host.chosen.map((f) => f?.id ?? null)).toEqual(['team', null]);
  });

  it('names the owner of another member’s filter and offers no change of it', async () => {
    const fixture = await render();
    fixture.componentInstance.applied.set(list()[2]);
    await settle(fixture);

    expect(el(fixture, 'filter-owner')?.textContent).toBe('by Sam');
    expect(el(fixture, 'share-filter')).toBeNull();
    expect(el(fixture, 'unshare-filter')).toBeNull();
    expect(el(fixture, 'delete-filter')).toBeNull();
  });

  // docs/adr/0018 D5 as amended 2026-10-06.
  it('offers an administrator to unshare and delete another member’s shared filter, and nothing else of it', async () => {
    role.set('admin');
    const fixture = await render();
    fixture.componentInstance.applied.set(list()[2]);
    await settle(fixture);

    expect(el(fixture, 'filter-owner')?.textContent).toBe('by Sam');
    expect(el(fixture, 'share-filter')).toBeNull();
    expect(el(fixture, 'unshare-filter')?.getAttribute('aria-label')).toBe(
      'Stop sharing Filter team of Sam',
    );
    expect(el(fixture, 'delete-filter')?.getAttribute('aria-label')).toBe(
      'Delete Filter team of Sam',
    );
  });

  it('unshares another member’s filter as an administrator, which leaves the list without it', async () => {
    role.set('admin');
    const fixture = await render();
    fixture.componentInstance.applied.set(list()[2]);
    await settle(fixture);

    el(fixture, 'unshare-filter')?.click();
    await settle(fixture);

    expect(update).toHaveBeenCalledExactlyOnceWith(list()[2], { shared: false });
    expect(fixture.componentInstance.chosen.at(-1)).toBeNull();
  });

  it('deletes another member’s shared filter as an administrator', async () => {
    role.set('admin');
    const fixture = await render();
    fixture.componentInstance.applied.set(list()[2]);
    await settle(fixture);

    el(fixture, 'delete-filter')?.click();
    await settle(fixture);

    expect(remove).toHaveBeenCalledExactlyOnceWith(list()[2]);
    expect(fixture.componentInstance.chosen.at(-1)).toBeNull();
  });

  // docs/adr/0018 D5 as amended 2026-10-06: any shared filter, one the server withholds too.
  it('lets an administrator choose a filter the server withholds, to withdraw it, and applies none', async () => {
    role.set('admin');
    const fixture = await render();
    const host = fixture.componentInstance;
    expect(choices(fixture).find((c) => c.id === 'hidden')?.disabled).toBe(false);

    fixture.debugElement
      .query(By.css('[data-testid="saved-filters"]'))
      .triggerEventHandler('ngModelChange', 'hidden');
    await settle(fixture);

    expect(host.chosen).toEqual([null]);
    expect(el(fixture, 'filter-owner')?.textContent).toBe('by Sam');
    expect(el(fixture, 'filter-notes')?.textContent).toBe(
      'not applied: it names something you cannot see',
    );
    el(fixture, 'unshare-filter')?.click();
    await settle(fixture);

    expect(update).toHaveBeenCalledExactlyOnceWith(list()[3], { shared: false });
    expect(host.chosen).toEqual([null]);
    expect(el(fixture, 'unshare-filter')).toBeNull();
  });

  it('deletes a filter the server withholds as an administrator, the list left as it is', async () => {
    role.set('admin');
    const fixture = await render();
    fixture.debugElement
      .query(By.css('[data-testid="saved-filters"]'))
      .triggerEventHandler('ngModelChange', 'hidden');
    await settle(fixture);

    el(fixture, 'delete-filter')?.click();
    await settle(fixture);

    expect(remove).toHaveBeenCalledExactlyOnceWith(list()[3]);
    expect(fixture.componentInstance.chosen).toEqual([null]);
    expect(el(fixture, 'filter-owner')).toBeNull();
  });

  it('holds a filter the server withholds no longer once the list applies another', async () => {
    role.set('admin');
    const fixture = await render();
    fixture.debugElement
      .query(By.css('[data-testid="saved-filters"]'))
      .triggerEventHandler('ngModelChange', 'hidden');
    await settle(fixture);

    fixture.componentInstance.applied.set(list()[0]);
    await settle(fixture);
    fixture.componentInstance.applied.set(null);
    await settle(fixture);

    expect(el(fixture, 'filter-owner')).toBeNull();
    expect(el(fixture, 'filter-notes')).toBeNull();
  });

  // The tenant no longer reads a person who left it: the API names them by their id alone.
  it('names the owner who left the tenant a former member', async () => {
    role.set('admin');
    const gone = filter('gone', {
      shared: true,
      name: 'Left behind',
      owner: { id: 'p-gone', display_name: '', username: null },
    });
    list.update((filters) => [...filters, gone]);
    const fixture = await render();
    fixture.componentInstance.applied.set(gone);
    await settle(fixture);

    expect(choices(fixture).at(-1)?.label).toBe('Left behind · a former member');
    expect(el(fixture, 'filter-owner')?.textContent).toBe('by a former member');
    expect(el(fixture, 'unshare-filter')?.getAttribute('aria-label')).toBe(
      'Stop sharing Left behind of a former member',
    );
    expect(el(fixture, 'delete-filter')?.getAttribute('aria-label')).toBe(
      'Delete Left behind of a former member',
    );
  });

  it('offers an administrator the owner’s acts on their own filter', async () => {
    role.set('admin');
    const fixture = await render();
    fixture.componentInstance.applied.set(list()[1]);
    await settle(fixture);

    expect(el(fixture, 'share-filter')?.getAttribute('aria-label')).toBe(
      'Stop sharing Filter shared-mine',
    );
    expect(el(fixture, 'unshare-filter')).toBeNull();
    expect(el(fixture, 'filter-owner')).toBeNull();
  });

  it('shares the person’s own filter and hands the changed one to the list', async () => {
    const fixture = await render();
    fixture.componentInstance.applied.set(list()[0]);
    await settle(fixture);

    el(fixture, 'share-filter')?.click();
    await settle(fixture);

    expect(update).toHaveBeenCalledExactlyOnceWith(list()[0], { shared: true });
    expect(fixture.componentInstance.chosen.at(-1)?.shared).toBe(true);
  });

  it('deletes the person’s own filter and clears it from the list', async () => {
    const fixture = await render();
    fixture.componentInstance.applied.set(list()[0]);
    await settle(fixture);

    el(fixture, 'delete-filter')?.click();
    await settle(fixture);

    expect(remove).toHaveBeenCalledExactlyOnceWith(list()[0]);
    expect(fixture.componentInstance.chosen.at(-1)).toBeNull();
  });

  it('saves what the list applies now under a name, shared when asked, and applies it', async () => {
    const fixture = await render();
    el(fixture, 'save-filter')?.click();
    await settle(fixture);

    const dialog = document.body.querySelector('[data-testid="save-filter-dialog"]');
    expect(dialog?.textContent).toContain('state=blocked · q="crash"');
    const name = document.body.querySelector<HTMLInputElement>('[data-testid="filter-name"]');
    name!.value = ' Crashes ';
    name!.dispatchEvent(new Event('input'));
    fixture.debugElement
      .query(By.css('[data-testid="filter-shared"]'))
      ?.triggerEventHandler('ngModelChange', true);
    await settle(fixture);
    document.body.querySelector<HTMLButtonElement>('[data-testid="filter-save"]')?.click();
    await settle(fixture);

    expect(create).toHaveBeenCalledExactlyOnceWith(
      'Crashes',
      { state: ['blocked'], q: 'crash' },
      true,
      expect.stringMatching(/^[0-9a-f-]{36}$/),
    );
    expect(fixture.componentInstance.chosen.at(-1)?.name).toBe('Crashes');
  });

  it('sends a lost save again under the same key (docs/adr/0045 D3)', async () => {
    create.mockRejectedValueOnce(new Error('the answer was lost'));
    const fixture = await render();
    el(fixture, 'save-filter')?.click();
    await settle(fixture);
    const name = document.body.querySelector<HTMLInputElement>('[data-testid="filter-name"]');
    name!.value = 'Crashes';
    name!.dispatchEvent(new Event('input'));
    await settle(fixture);
    const save = () =>
      document.body.querySelector<HTMLButtonElement>('[data-testid="filter-save"]')?.click();

    save();
    await settle(fixture);
    save();
    await settle(fixture);

    const [first, second] = create.mock.calls.map((call) => call[3]);
    expect(second).toBe(first);
  });

  // The backlog leaves a filter's project out; the tenant's ticket list applies it (docs/adr/0018 D5).
  it('names a condition its list leaves out, with why, and none for a list that applies every one', async () => {
    const fixture = await render();
    fixture.componentInstance.applied.set(filter('across', { parameters: { project: ['OPS'] } }));
    await settle(fixture);
    expect(el(fixture, 'filter-notes')).toBeNull();

    fixture.componentInstance.leftOut.set(backlogLeftOut);
    await settle(fixture);

    expect(el(fixture, 'filter-notes')?.textContent).toBe(
      'project: a backlog is one project; the tenant’s ticket list applies this condition',
    );
  });

  it('shows the warnings of the applied filter', async () => {
    const fixture = await render();
    fixture.componentInstance.applied.set(
      filter('old', {
        warnings: [{ parameter: 'state', message: 'not a value of state: triaged' }],
      }),
    );
    await settle(fixture);

    expect(el(fixture, 'filter-notes')?.textContent).toContain(
      'state: not a value of state: triaged',
    );
  });
});
