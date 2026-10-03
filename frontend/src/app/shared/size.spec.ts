import { ComponentFixture, TestBed } from '@angular/core/testing';
import { By } from '@angular/platform-browser';
import { Tooltip } from 'primeng/tooltip';
import { Effort } from '../api/models';
import { SizeIcon } from './size';

const efforts: Effort[] = ['XS', 'S', 'M', 'L'];

async function render(value: Effort): Promise<ComponentFixture<SizeIcon>> {
  const fixture = TestBed.createComponent(SizeIcon);
  fixture.componentRef.setInput('value', value);
  await fixture.whenStable();
  return fixture;
}

const host = (fixture: ComponentFixture<SizeIcon>) => fixture.nativeElement as HTMLElement;

describe('SizeIcon', () => {
  it.each(efforts)('shows %s as the letter on a T-shirt', async (value) => {
    const fixture = await render(value);

    expect(host(fixture).querySelector('.letter')?.textContent?.trim()).toBe(value);
    expect(host(fixture).querySelector('svg path')).not.toBeNull();
    expect(host(fixture).getAttribute('data-size')).toBe(value);
  });

  it.each(efforts)('names %s for assistive technology and in the tooltip', async (value) => {
    const fixture = await render(value);

    expect(host(fixture).getAttribute('role')).toBe('img');
    expect(host(fixture).getAttribute('aria-label')).toBe(`Effort ${value}`);
    const tooltip = fixture.debugElement.query(By.directive(Tooltip)).injector.get(Tooltip);
    expect(tooltip.content()).toBe(`Effort ${value}`);
    expect(tooltip.showDelay()).toBe(400);
  });

  it('leaves the glyph and the letter to the label, so that nothing is read twice', async () => {
    const fixture = await render('M');

    expect(host(fixture).querySelector('svg')?.getAttribute('aria-hidden')).toBe('true');
    expect(host(fixture).querySelector('.letter')?.getAttribute('aria-hidden')).toBe('true');
  });

  it('follows the value when it changes', async () => {
    const fixture = await render('XS');

    fixture.componentRef.setInput('value', 'L');
    await fixture.whenStable();

    expect(host(fixture).querySelector('.letter')?.textContent?.trim()).toBe('L');
    expect(host(fixture).getAttribute('aria-label')).toBe('Effort L');
    expect(host(fixture).getAttribute('data-size')).toBe('L');
  });
});
