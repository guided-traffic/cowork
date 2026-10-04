import { HttpErrorResponse } from '@angular/common/http';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { By } from '@angular/platform-browser';
import { MessageService } from 'primeng/api';
import { Select } from 'primeng/select';
import type { MockInstance } from 'vitest';
import { Ticket } from '../../api/models';
import { TicketsService } from '../../core/tickets.service';
import { ParentPicker } from './parent-picker';

const open = (key: string, title: string) => ({ key, title }) as Ticket;

describe('ParentPicker', () => {
  let openTickets: MockInstance<TicketsService['openTickets']>;

  beforeEach(() => {
    openTickets = vi
      .fn<TicketsService['openTickets']>()
      .mockResolvedValue([
        open('acme/COW-3', 'Rework the board'),
        open('acme/COW-12', 'The board flickers'),
        open('acme/COW-7', 'Pick the format'),
      ]);
    TestBed.configureTestingModule({
      providers: [MessageService, { provide: TicketsService, useValue: { openTickets } }],
    });
  });

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

  async function opened(fixture: ComponentFixture<ParentPicker>) {
    await fixture.componentInstance.load();
    await settle(fixture);
  }

  it('asks for nothing until it is opened', async () => {
    await render();

    expect(openTickets).not.toHaveBeenCalled();
  });

  it('offers the open tickets of the project by short key and title once opened, the ticket itself not among them', async () => {
    const fixture = await render({ exclude: 'acme/COW-12' });

    await opened(fixture);

    expect(openTickets).toHaveBeenCalledExactlyOnceWith('acme', 'COW');
    expect(select(fixture).options()).toEqual([
      { key: 'acme/COW-3', label: 'COW-3 Rework the board' },
      { key: 'acme/COW-7', label: 'COW-7 Pick the format' },
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
    expect(select(fixture).options()).toEqual([{ key: 'acme/COW-1', label: 'COW-1' }]);
    expect(
      (fixture.nativeElement as HTMLElement).querySelector('.p-select-label')?.textContent?.trim(),
    ).toBe('COW-1');

    await opened(fixture);

    expect(select(fixture).options()?.[0]).toEqual({ key: 'acme/COW-1', label: 'COW-1' });
    expect(select(fixture).options()).toHaveLength(4);
  });

  it('says what the person picked, and none when the choice is cleared', async () => {
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
});
