import { HttpErrorResponse } from '@angular/common/http';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { By } from '@angular/platform-browser';
import { provideRouter } from '@angular/router';
import { MessageService } from 'primeng/api';
import { Select } from 'primeng/select';
import type { MockInstance } from 'vitest';
import { SearchHit, Ticket, TicketHead, TicketState } from '../../api/models';
import { TicketsService } from '../../core/tickets.service';
import { confidentialParent, ParentOption, ParentPicker, searchDelay } from './parent-picker';

const open = (key: string, title: string) =>
  ({ key, title, type: 'task', state: 'filed' }) as Ticket;

function hit(key: string, title: string, state: TicketState = 'filed'): SearchHit {
  const team = key.slice(0, key.indexOf('/'));
  const name = team === 'acme' ? 'Acme' : 'Globex';
  return {
    key,
    title,
    state,
    type: 'task',
    team: { slug: team, name },
    tenant: { slug: team, name },
    found_in: 'ticket',
    comment: null,
    question: null,
    snippet: [],
  };
}

/** The parent of another team as the reader sees it: its head, which they may not open. */
const stranger: TicketHead = {
  team: { slug: 'globex', name: 'Globex' },
  key: 'globex/API-7',
  title: 'Send the SameSite attribute',
  type: 'task',
  state: 'review',
  placeholder: false,
  readable: false,
};

/** A parent the reader may not see (docs/adr/0065 D5). */
const confidential: TicketHead = {
  team: { slug: 'globex', name: 'Globex' },
  key: null,
  title: null,
  type: null,
  state: null,
  placeholder: true,
  readable: false,
};

describe('ParentPicker', () => {
  let openTickets: MockInstance<TicketsService['openTickets']>;
  let search: MockInstance<TicketsService['search']>;

  beforeEach(() => {
    openTickets = vi
      .fn<TicketsService['openTickets']>()
      .mockResolvedValue([
        open('acme/COW-3', 'Rework the board'),
        open('acme/COW-12', 'The board flickers'),
        open('acme/COW-7', 'Pick the format'),
      ]);
    search = vi.fn<TicketsService['search']>().mockResolvedValue([]);
    TestBed.configureTestingModule({
      providers: [
        provideRouter([]),
        MessageService,
        { provide: TicketsService, useValue: { openTickets, search } },
      ],
    });
  });

  afterEach(() => vi.useRealTimers());

  async function render(inputs: Record<string, unknown> = {}) {
    const fixture = TestBed.createComponent(ParentPicker);
    fixture.componentRef.setInput('tenant', 'acme');
    fixture.componentRef.setInput('project', 'COW');
    for (const [name, value] of Object.entries(inputs)) {
      fixture.componentRef.setInput(name, value);
    }
    await settle(fixture);
    return fixture;
  }

  async function settle(fixture: ComponentFixture<ParentPicker>) {
    await new Promise((resolve) => setTimeout(resolve));
    fixture.detectChanges();
    await fixture.whenStable();
  }

  const select = (fixture: ComponentFixture<ParentPicker>) =>
    fixture.debugElement.query(By.directive(Select)).componentInstance as Select;
  const options = (fixture: ComponentFixture<ParentPicker>) =>
    (select(fixture).options() as ParentOption[]).map((option) => [option.key, option.label]);
  const page = (fixture: ComponentFixture<ParentPicker>) => fixture.nativeElement as HTMLElement;
  const text = (element: Element | null | undefined) =>
    element?.textContent?.replace(/\s+/g, ' ').trim();

  async function opened(fixture: ComponentFixture<ParentPicker>) {
    await fixture.componentInstance.load();
    await settle(fixture);
  }

  /** Types into the select's search, as the person does: the select says it on `onFilter`. */
  function typeIn(fixture: ComponentFixture<ParentPicker>, filter: string) {
    fixture.debugElement.query(By.directive(Select)).triggerEventHandler('onFilter', { filter });
    fixture.detectChanges();
  }

  it('asks for nothing until it is opened', async () => {
    await render();

    expect(openTickets).not.toHaveBeenCalled();
    expect(search).not.toHaveBeenCalled();
  });

  it('offers the open tickets of the project by short key and title once opened, the ticket itself not among them', async () => {
    const fixture = await render({ exclude: 'acme/COW-12' });

    await opened(fixture);

    expect(openTickets).toHaveBeenCalledExactlyOnceWith('acme', 'COW');
    expect(options(fixture)).toEqual([
      ['acme/COW-3', 'COW-3 Rework the board'],
      ['acme/COW-7', 'COW-7 Pick the format'],
    ]);
  });

  it('loads them once, however often it is opened', async () => {
    const fixture = await render();

    await opened(fixture);
    await opened(fixture);

    expect(openTickets).toHaveBeenCalledOnce();
  });

  it('shows a parent that is not open by its key, before and after the list is loaded', async () => {
    const fixture = await render({ value: 'acme/COW-1' });
    expect(options(fixture)).toEqual([['acme/COW-1', 'COW-1']]);
    expect(text(page(fixture).querySelector('[data-testid="parent-chosen"]'))).toBe('COW-1');

    await opened(fixture);

    expect(options(fixture)[0]).toEqual(['acme/COW-1', 'COW-1']);
    expect(options(fixture)).toHaveLength(4);
  });

  it('says what the person picked, by its canonical key, and none when the choice is cleared', async () => {
    const fixture = await render();
    const picked: (string | null)[] = [];
    fixture.componentInstance.picked.subscribe((key) => picked.push(key));

    fixture.debugElement
      .query(By.directive(Select))
      .triggerEventHandler('ngModelChange', 'acme/COW-7');
    fixture.debugElement.query(By.directive(Select)).triggerEventHandler('ngModelChange', null);

    expect(picked).toEqual(['acme/COW-7', null]);
  });

  it('starts again for another project, whose tickets it has not loaded', async () => {
    const fixture = await render();
    await opened(fixture);

    fixture.componentRef.setInput('project', 'OPS');
    await settle(fixture);
    expect(select(fixture).options()).toEqual([]);
    await opened(fixture);

    expect(openTickets).toHaveBeenLastCalledWith('acme', 'OPS');
  });

  it('toasts a failed load and may be opened again', async () => {
    openTickets.mockRejectedValueOnce(new HttpErrorResponse({ status: 0 }));
    const add = vi.spyOn(TestBed.inject(MessageService), 'add');
    const fixture = await render();

    await opened(fixture);
    expect(add).toHaveBeenCalledOnce();
    await opened(fixture);

    expect(openTickets).toHaveBeenCalledTimes(2);
    expect(select(fixture).options()).toHaveLength(3);
  });

  describe('the parent the ticket has, by its head (docs/adr/0005 D3)', () => {
    it('shows a parent of another team by its team, key, title, type and state, which the reader may not open', async () => {
      const fixture = await render({ value: 'globex/API-7', head: stranger });

      expect(options(fixture)).toEqual([
        ['globex/API-7', 'Globex · API-7 Send the SameSite attribute'],
      ]);
      const chosen = page(fixture).querySelector('[data-testid="parent-chosen"]');
      expect(text(chosen?.querySelector('[data-testid="head-team"]'))).toBe('Globex');
      expect(text(chosen?.querySelector('[data-testid="head-key"]'))).toContain('API-7');
      expect(text(chosen)).toContain('Send the SameSite attribute');
      expect(chosen?.querySelector('app-type')?.getAttribute('title')).toContain('task');
      expect(chosen?.querySelector('[data-state]')?.getAttribute('data-state')).toBe('review');
      expect(page(fixture).querySelector('a')).toBeNull();
    });

    it('offers a link to a parent the reader may open, beside the choice', async () => {
      const readable = { ...stranger, readable: true };
      const fixture = await render({ value: 'globex/API-7', head: readable });

      const link = page(fixture).querySelector('[data-testid="parent-open"]');
      expect(link?.getAttribute('href')).toBe('/t/globex/tickets/API-7');
      expect(link?.getAttribute('aria-label')).toBe('Open the parent');
    });

    it('offers no link while the ticket is being filed, without a head', async () => {
      const fixture = await render({ value: 'acme/COW-3' });

      expect(page(fixture).querySelector('[data-testid="parent-open"]')).toBeNull();
    });

    // docs/adr/0065 D5 as amended 2026-10-10.
    it('shows a parent the reader may not see as `<team> [Confidential]`, which the person may remove', async () => {
      const fixture = await render({ value: null, head: confidential });
      const picked: (string | null)[] = [];
      fixture.componentInstance.picked.subscribe((key) => picked.push(key));

      expect(options(fixture)).toEqual([[confidentialParent, 'Globex [Confidential]']]);
      expect(text(page(fixture).querySelector('[data-testid="parent-chosen"]'))).toBe(
        'Globex [Confidential]',
      );
      expect(page(fixture).querySelector('[data-testid="parent-open"]')).toBeNull();

      const choice = fixture.debugElement.query(By.directive(Select));
      choice.triggerEventHandler('ngModelChange', confidentialParent);
      expect(picked).toEqual([]);
      choice.triggerEventHandler('ngModelChange', null);
      expect(picked).toEqual([null]);
    });
  });

  describe('the search across the teams of the person (docs/adr/0023 D2, docs/adr/0025)', () => {
    beforeEach(() => vi.useFakeTimers());

    /** Lets the debounce end and the answer reach the page. */
    async function rest(fixture: ComponentFixture<ParentPicker>, ms = searchDelay) {
      await vi.advanceTimersByTimeAsync(ms);
      fixture.detectChanges();
    }

    async function renderFake(inputs: Record<string, unknown> = {}) {
      const fixture = TestBed.createComponent(ParentPicker);
      fixture.componentRef.setInput('tenant', 'acme');
      fixture.componentRef.setInput('project', 'COW');
      for (const [name, value] of Object.entries(inputs)) {
        fixture.componentRef.setInput(name, value);
      }
      fixture.detectChanges();
      await vi.advanceTimersByTimeAsync(0);
      return fixture;
    }

    it('searches once the person rests, and offers the open hits of every team, each under the text that found it', async () => {
      search.mockResolvedValue([
        hit('globex/API-7', 'Send the quoll attribute'),
        hit('acme/OPS-2', 'Count the quolls'),
        hit('acme/COW-9', 'A closed quoll', 'done'),
        hit('acme/COW-12', 'The ticket itself'),
      ]);
      const fixture = await renderFake({ exclude: 'acme/COW-12' });

      typeIn(fixture, 'quo');
      typeIn(fixture, ' quoll ');
      await rest(fixture, searchDelay - 1);
      expect(search).not.toHaveBeenCalled();
      await rest(fixture, 1);

      expect(search).toHaveBeenCalledExactlyOnceWith('quoll');
      expect(options(fixture)).toEqual([
        ['globex/API-7', 'Globex · API-7 Send the quoll attribute'],
        ['acme/OPS-2', 'OPS-2 Count the quolls'],
      ]);
      expect((select(fixture).options() as ParentOption[]).map((each) => each.query)).toEqual([
        'quoll',
        'quoll',
      ]);
    });

    it('drops an answer that comes after the person typed something else', async () => {
      let answer: (hits: SearchHit[]) => void = () => undefined;
      search.mockImplementationOnce(() => new Promise((resolve) => (answer = resolve)));
      search.mockResolvedValueOnce([hit('acme/OPS-3', 'Second words')]);
      const fixture = await renderFake();

      typeIn(fixture, 'first');
      await rest(fixture);
      typeIn(fixture, 'second');
      await rest(fixture);
      answer([hit('acme/OPS-1', 'First words')]);
      await rest(fixture, 0);

      expect(options(fixture)).toEqual([['acme/OPS-3', 'OPS-3 Second words']]);
    });

    it('offers the open tickets of the project again once the text is empty, without a search', async () => {
      const fixture = await renderFake();
      await fixture.componentInstance.load();
      typeIn(fixture, 'quoll');
      await rest(fixture);

      typeIn(fixture, '   ');
      await rest(fixture);

      expect(search).toHaveBeenCalledOnce();
      expect(options(fixture).map(([key]) => key)).toEqual([
        'acme/COW-3',
        'acme/COW-12',
        'acme/COW-7',
      ]);
    });

    it('keeps the label of a hit the person picked after the list offers others', async () => {
      search.mockResolvedValue([hit('globex/API-7', 'Send the quoll attribute')]);
      const fixture = await renderFake();
      const picked: (string | null)[] = [];
      fixture.componentInstance.picked.subscribe((key) => picked.push(key));
      typeIn(fixture, 'quoll');
      await rest(fixture);

      fixture.debugElement
        .query(By.directive(Select))
        .triggerEventHandler('ngModelChange', 'globex/API-7');
      fixture.componentRef.setInput('value', 'globex/API-7');
      typeIn(fixture, '');
      await rest(fixture);

      expect(picked).toEqual(['globex/API-7']);
      expect(options(fixture)).toEqual([
        ['globex/API-7', 'Globex · API-7 Send the quoll attribute'],
      ]);
    });

    it('toasts a failed search', async () => {
      search.mockRejectedValue(new HttpErrorResponse({ status: 0 }));
      const add = vi.spyOn(TestBed.inject(MessageService), 'add');
      const fixture = await renderFake();

      typeIn(fixture, 'quoll');
      await rest(fixture);

      expect(add).toHaveBeenCalledOnce();
      expect(options(fixture)).toEqual([]);
    });
  });
});
