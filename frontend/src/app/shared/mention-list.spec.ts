import { Component, signal } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { MentionList } from './mention-list';
import { Mentionable } from './mentions';

@Component({
  imports: [MentionList],
  template: `
    <textarea #box data-testid="box"></textarea>
    <app-mention-list [for]="box" [candidates]="people()" (picked)="picked.push($event)" />
  `,
})
class Host {
  readonly people = signal<Mentionable[]>([
    { id: 'p1', name: 'Ada Lovelace', username: 'ada' },
    { id: 'p2', name: 'Sam Rivera' },
  ]);
  readonly picked: Mentionable[] = [];
}

describe('MentionList', () => {
  let fixture: ComponentFixture<Host>;
  let box: HTMLTextAreaElement;

  beforeEach(async () => {
    fixture = TestBed.createComponent(Host);
    fixture.detectChanges();
    await fixture.whenStable();
    box = (fixture.nativeElement as HTMLElement).querySelector('textarea') as HTMLTextAreaElement;
  });

  function type(value: string) {
    box.value = value;
    box.setSelectionRange(value.length, value.length);
    box.dispatchEvent(new Event('input'));
    fixture.detectChanges();
  }
  const key = (name: string) => {
    const event = new KeyboardEvent('keydown', { key: name, cancelable: true });
    box.dispatchEvent(event);
    fixture.detectChanges();
    return event;
  };
  const list = () => (fixture.nativeElement as HTMLElement).querySelector('[role="listbox"]');

  it('says it completes a list, and names the list and its active option on the textarea while it is open', () => {
    expect(box.getAttribute('aria-autocomplete')).toBe('list');
    expect(box.hasAttribute('aria-controls')).toBe(false);

    type('@');
    expect(box.getAttribute('aria-controls')).toBe(list()?.id);
    expect(box.getAttribute('aria-activedescendant')).toBe(`${list()?.id}-0`);
    expect(list()?.querySelector('[aria-selected="true"]')?.textContent).toContain('Ada Lovelace');
  });

  it('picks with Tab, writes the name, hands the person on and closes', () => {
    type('Hi @s');
    const tab = key('Tab');

    expect(tab.defaultPrevented).toBe(true);
    expect(box.value).toBe('Hi @Sam Rivera ');
    expect(fixture.componentInstance.picked).toEqual([{ id: 'p2', name: 'Sam Rivera' }]);
    expect(list()).toBeNull();
    expect(box.hasAttribute('aria-activedescendant')).toBe(false);
  });

  it('leaves Enter and Tab to the textarea while nothing is offered, and closes when the textarea loses the keyboard', () => {
    type('@zz');
    expect(key('Enter').defaultPrevented).toBe(false);
    type('@');
    box.dispatchEvent(new Event('blur'));
    fixture.detectChanges();
    expect(list()).toBeNull();
    expect(key('ArrowDown').defaultPrevented).toBe(false);
  });

  it('wraps around with the arrows', () => {
    type('@');
    key('ArrowUp');
    expect(box.getAttribute('aria-activedescendant')).toBe(`${list()?.id}-1`);
    key('ArrowDown');
    expect(box.getAttribute('aria-activedescendant')).toBe(`${list()?.id}-0`);
  });
});
