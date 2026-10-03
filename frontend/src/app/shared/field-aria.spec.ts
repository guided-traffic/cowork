import { Component, signal } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { FormsModule } from '@angular/forms';
import { InputNumber } from 'primeng/inputnumber';
import { Select } from 'primeng/select';
import { describedBy, numberAria, selectAria } from './field-aria';

describe('describedBy', () => {
  it('names the ids that are there, in the order given, separated by a space', () => {
    expect(describedBy('hint', 'error')).toBe('hint error');
    expect(describedBy('hint')).toBe('hint');
  });

  it('leaves out what is absent, so that it never points at an element that is not there', () => {
    expect(describedBy('hint', false)).toBe('hint');
    expect(describedBy('hint', null, undefined, '')).toBe('hint');
    expect(describedBy(false, 'error')).toBe('error');
  });

  it('is null when nothing is there, which leaves the attribute off', () => {
    expect(describedBy()).toBeNull();
    expect(describedBy(false, null, undefined, '')).toBeNull();
  });
});

describe('selectAria', () => {
  it('says that the combobox is invalid, and what describes it, once the server refused the field', () => {
    expect(selectAria(true, 'hint error')).toEqual({
      label: { 'aria-invalid': 'true', 'aria-describedby': 'hint error' },
    });
  });

  it('claims nothing about validity while the field is not refused, and keeps the hint', () => {
    expect(selectAria(false, 'hint')).toEqual({
      label: { 'aria-invalid': null, 'aria-describedby': 'hint' },
    });
    expect(selectAria(false, null)).toEqual({
      label: { 'aria-invalid': null, 'aria-describedby': null },
    });
  });
});

describe('numberAria', () => {
  it('says that the number field is invalid, and nothing while it is not', () => {
    expect(numberAria(true)).toEqual({ pcInputText: { root: { 'aria-invalid': 'true' } } });
    expect(numberAria(false)).toEqual({ pcInputText: { root: { 'aria-invalid': null } } });
  });
});

describe('what PrimeNG does with them', () => {
  @Component({
    imports: [FormsModule, InputNumber, Select],
    template: `
      <p-select
        [options]="['a', 'b']"
        [ngModel]="'a'"
        ariaLabelledBy="caption"
        [pt]="select()"
        data-testid="select"
      />
      <p-inputnumber
        [ngModel]="1"
        ariaLabelledBy="caption"
        [ariaDescribedBy]="describing() ?? undefined"
        [pt]="number()"
        data-testid="number"
      />
    `,
  })
  class Host {
    readonly refused = signal(false);
    readonly describing = signal<string | null>('hint');
    readonly select = () => selectAria(this.refused(), this.describing());
    readonly number = () => numberAria(this.refused());
  }

  async function render() {
    const fixture = TestBed.createComponent(Host);
    await fixture.whenStable();
    const page = fixture.nativeElement as HTMLElement;
    return {
      fixture,
      combobox: () => page.querySelector('[role="combobox"]') as HTMLElement,
      spinbutton: () => page.querySelector('[role="spinbutton"]') as HTMLElement,
    };
  }

  it('puts aria-invalid and aria-describedby on the combobox of a select, and takes them off again', async () => {
    const { fixture, combobox } = await render();
    expect(combobox().getAttribute('aria-invalid')).toBeNull();
    expect(combobox().getAttribute('aria-describedby')).toBe('hint');

    fixture.componentInstance.describing.set('hint error');
    fixture.componentInstance.refused.set(true);
    fixture.detectChanges();
    await fixture.whenStable();
    expect(combobox().getAttribute('aria-invalid')).toBe('true');
    expect(combobox().getAttribute('aria-describedby')).toBe('hint error');

    fixture.componentInstance.describing.set(null);
    fixture.componentInstance.refused.set(false);
    fixture.detectChanges();
    await fixture.whenStable();
    expect(combobox().getAttribute('aria-invalid')).toBeNull();
    expect(combobox().getAttribute('aria-describedby')).toBeNull();
  });

  it('puts aria-invalid on the input of a number field, which describes itself through its own input', async () => {
    const { fixture, spinbutton } = await render();
    expect(spinbutton().getAttribute('aria-invalid')).toBeNull();
    expect(spinbutton().getAttribute('aria-describedby')).toBe('hint');

    fixture.componentInstance.refused.set(true);
    fixture.detectChanges();
    await fixture.whenStable();

    expect(spinbutton().getAttribute('aria-invalid')).toBe('true');
  });
});
