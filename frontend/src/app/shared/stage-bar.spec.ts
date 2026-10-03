import { ComponentFixture, TestBed } from '@angular/core/testing';
import { By } from '@angular/platform-browser';
import { Tooltip } from 'primeng/tooltip';
import { StageBar } from './stage-bar';
import { Stage } from './stages';

async function render(
  stage: Stage,
  value: number,
  derived?: boolean,
): Promise<ComponentFixture<StageBar>> {
  const fixture = TestBed.createComponent(StageBar);
  fixture.componentRef.setInput('stage', stage);
  fixture.componentRef.setInput('value', value);
  if (derived !== undefined) {
    fixture.componentRef.setInput('derived', derived);
  }
  await fixture.whenStable();
  return fixture;
}

const host = (fixture: ComponentFixture<StageBar>) => fixture.nativeElement as HTMLElement;
const tooltip = (fixture: ComponentFixture<StageBar>) =>
  fixture.debugElement.query(By.directive(Tooltip)).injector.get(Tooltip);

describe('StageBar', () => {
  it.each([
    ['refinement', 40, 'Refinement 40%'],
    ['implementation', 0, 'Implementation 0%'],
    ['review', 100, 'Review 100%'],
  ] as [Stage, number, string][])(
    'fills the track of %s to %i percent and names it %s',
    async (stage, value, label) => {
      const fixture = await render(stage, value);

      expect(host(fixture).querySelector<HTMLElement>('.fill')?.style.width).toBe(`${value}%`);
      expect(host(fixture).getAttribute('aria-label')).toBe(label);
      expect(tooltip(fixture).content()).toBe(label);
      expect(tooltip(fixture).showDelay()).toBe(400);
      expect(host(fixture).getAttribute('data-stage')).toBe(stage);
    },
  );

  it('is a progress bar from 0 to 100 for assistive technology', async () => {
    const fixture = await render('implementation', 55);

    expect(host(fixture).getAttribute('role')).toBe('progressbar');
    expect(host(fixture).getAttribute('aria-valuemin')).toBe('0');
    expect(host(fixture).getAttribute('aria-valuemax')).toBe('100');
    expect(host(fixture).getAttribute('aria-valuenow')).toBe('55');
  });

  it('says where a parent stage comes from', async () => {
    const fixture = await render('review', 35, true);

    expect(host(fixture).getAttribute('aria-label')).toBe('Review 35%, from its children');
    expect(tooltip(fixture).content()).toBe('Review 35%, from its children');
  });

  it('follows the value and the stage when they change', async () => {
    const fixture = await render('refinement', 10);

    fixture.componentRef.setInput('stage', 'review');
    fixture.componentRef.setInput('value', 75);
    await fixture.whenStable();

    expect(host(fixture).querySelector<HTMLElement>('.fill')?.style.width).toBe('75%');
    expect(host(fixture).getAttribute('aria-valuenow')).toBe('75');
    expect(host(fixture).getAttribute('aria-label')).toBe('Review 75%');
  });
});
