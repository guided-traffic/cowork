import { Type } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { By } from '@angular/platform-browser';
import { Tooltip } from 'primeng/tooltip';
import { SecurityClass, Severity, TicketState, TicketType } from '../api/models';
import { SecurityBadge, SeverityBadge, StateBadge, TypeIcon, typeIcons } from './badges';
import { meanings } from './vocabulary';

const states = Object.keys(meanings.state) as TicketState[];
const severities = Object.keys(meanings.severity) as Severity[];
const securities = Object.keys(meanings.security) as SecurityClass[];
const types = Object.keys(meanings.type) as TicketType[];

async function render<T>(
  component: Type<T>,
  inputs: Record<string, unknown>,
): Promise<ComponentFixture<T>> {
  const fixture = TestBed.createComponent(component);
  for (const [name, value] of Object.entries(inputs)) {
    fixture.componentRef.setInput(name, value);
  }
  await fixture.whenStable();
  return fixture;
}

const host = (fixture: ComponentFixture<unknown>) => fixture.nativeElement as HTMLElement;
const pill = (fixture: ComponentFixture<unknown>) =>
  host(fixture).querySelector<HTMLElement>('.pill');
const tooltipOf = (fixture: ComponentFixture<unknown>) =>
  fixture.debugElement.query(By.directive(Tooltip)).injector.get(Tooltip);

describe('StateBadge', () => {
  it.each(states)('shows %s exactly as the API spells it (docs/adr/0055 D4)', async (state) => {
    const fixture = await render(StateBadge, { value: state });

    expect(pill(fixture)?.textContent?.trim()).toBe(state);
    expect(pill(fixture)?.getAttribute('data-state')).toBe(state);
  });

  it('keeps the hyphen of in-progress', async () => {
    const fixture = await render(StateBadge, { value: 'in-progress' });

    expect(pill(fixture)?.textContent?.trim()).toBe('in-progress');
    expect(pill(fixture)?.textContent).not.toContain('inProgress');
  });

  it.each(states)('takes the accent of %s from the token of the preset', async (state) => {
    const fixture = await render(StateBadge, { value: state });

    expect(pill(fixture)?.style.getPropertyValue('--accent')).toBe(`var(--p-state-${state})`);
  });

  it('asks for --p-state-in-progress, which the preset defines with its hyphen', async () => {
    const fixture = await render(StateBadge, { value: 'in-progress' });

    expect(pill(fixture)?.style.getPropertyValue('--accent')).toBe('var(--p-state-in-progress)');
  });

  it.each(states)('explains %s in its tooltip, after a short delay', async (state) => {
    const fixture = await render(StateBadge, { value: state });

    expect(tooltipOf(fixture).content()).toBe(meanings.state[state]);
    expect(tooltipOf(fixture).showDelay()).toBe(400);
  });

  it('has a dot in front of the value', async () => {
    const fixture = await render(StateBadge, { value: 'filed' });

    expect(pill(fixture)?.firstElementChild?.classList.contains('dot')).toBe(true);
  });

  it('follows the value when it changes', async () => {
    const fixture = await render(StateBadge, { value: 'filed' });

    fixture.componentRef.setInput('value', 'blocked');
    await fixture.whenStable();

    expect(pill(fixture)?.textContent?.trim()).toBe('blocked');
    expect(pill(fixture)?.getAttribute('data-state')).toBe('blocked');
    expect(pill(fixture)?.style.getPropertyValue('--accent')).toBe('var(--p-state-blocked)');
    expect(tooltipOf(fixture).content()).toBe(meanings.state.blocked);
  });
});

describe('SeverityBadge', () => {
  it.each(severities)(
    'shows %s as an outlined pill, spelt as the API spells it',
    async (severity) => {
      const fixture = await render(SeverityBadge, { value: severity });

      expect(pill(fixture)?.classList.contains('outline')).toBe(true);
      expect(pill(fixture)?.textContent?.trim()).toBe(severity);
      expect(pill(fixture)?.getAttribute('data-severity')).toBe(severity);
    },
  );

  it.each(severities)('takes the accent of %s from the token of the preset', async (severity) => {
    const fixture = await render(SeverityBadge, { value: severity });

    expect(pill(fixture)?.style.getPropertyValue('--accent')).toBe(`var(--p-severity-${severity})`);
  });

  it.each(severities)('explains %s in its tooltip, after a short delay', async (severity) => {
    const fixture = await render(SeverityBadge, { value: severity });

    expect(tooltipOf(fixture).content()).toBe(meanings.severity[severity]);
    expect(tooltipOf(fixture).showDelay()).toBe(400);
  });
});

describe('SecurityBadge', () => {
  it('renders nothing for none: only a security class worth noticing gets a badge', async () => {
    const fixture = await render(SecurityBadge, { value: 'none' });

    expect(pill(fixture)).toBeNull();
    expect(host(fixture).textContent?.trim()).toBe('');
    expect(host(fixture).querySelector('i')).toBeNull();
  });

  it.each(securities.filter((security) => security !== 'none'))(
    'shows %s with a shield, spelt as the API spells it',
    async (security) => {
      const fixture = await render(SecurityBadge, { value: security });

      expect(pill(fixture)?.textContent?.trim()).toBe(security);
      expect(pill(fixture)?.getAttribute('data-security')).toBe(security);
      expect(pill(fixture)?.querySelector('i.pi.pi-shield')).not.toBeNull();
    },
  );

  it.each(securities.filter((security) => security !== 'none'))(
    'takes the accent of %s from the token of the preset',
    async (security) => {
      const fixture = await render(SecurityBadge, { value: security });

      expect(pill(fixture)?.style.getPropertyValue('--accent')).toBe(
        `var(--p-security-${security})`,
      );
    },
  );

  it.each(securities.filter((security) => security !== 'none'))(
    'explains %s in its tooltip, after a short delay',
    async (security) => {
      const fixture = await render(SecurityBadge, { value: security });

      expect(tooltipOf(fixture).content()).toBe(meanings.security[security]);
      expect(tooltipOf(fixture).showDelay()).toBe(400);
    },
  );

  it('appears when the class changes from none and goes again', async () => {
    const fixture = await render(SecurityBadge, { value: 'none' });

    fixture.componentRef.setInput('value', 'boundary');
    await fixture.whenStable();
    expect(pill(fixture)?.textContent?.trim()).toBe('boundary');

    fixture.componentRef.setInput('value', 'none');
    await fixture.whenStable();
    expect(pill(fixture)).toBeNull();
  });
});

describe('TypeIcon', () => {
  it('has an icon for every ticket type and for nothing else (docs/adr/0008)', () => {
    expect(Object.keys(typeIcons).sort()).toEqual([...types].sort());
  });

  it.each([
    ['task', 'pi pi-check-square'],
    ['bug', 'pi pi-exclamation-circle'],
    ['feature', 'pi pi-star'],
    ['decision', 'pi pi-directions'],
    ['question', 'pi pi-question-circle'],
  ] as const)('draws a %s as %s', async (type, classes) => {
    expect(typeIcons[type]).toBe(classes);
    const fixture = await render(TypeIcon, { value: type });
    const icon = host(fixture).querySelector('i');

    expect(icon?.getAttribute('class')).toBe(classes);
    expect(icon?.getAttribute('aria-hidden')).toBe('true');
  });

  it.each(types)('names a %s and what it means in the title', async (type) => {
    const fixture = await render(TypeIcon, { value: type });

    expect(host(fixture).getAttribute('title')).toBe(`${type}: ${meanings.type[type]}`);
  });

  it('shows the icon alone unless the label is asked for', async () => {
    const fixture = await render(TypeIcon, { value: 'bug' });

    expect(host(fixture).querySelector('span')).toBeNull();
    expect(host(fixture).textContent?.trim()).toBe('');
  });

  it('shows the type beside the icon when the label is asked for', async () => {
    const fixture = await render(TypeIcon, { value: 'decision', showLabel: true });

    expect(host(fixture).querySelector('span')?.textContent).toBe('decision');
    expect(host(fixture).querySelector('i')).not.toBeNull();
  });

  it('follows the type when it changes', async () => {
    const fixture = await render(TypeIcon, { value: 'task', showLabel: true });

    fixture.componentRef.setInput('value', 'feature');
    await fixture.whenStable();

    expect(host(fixture).querySelector('i')?.getAttribute('class')).toBe('pi pi-star');
    expect(host(fixture).querySelector('span')?.textContent).toBe('feature');
    expect(host(fixture).getAttribute('title')).toBe(`feature: ${meanings.type.feature}`);
  });
});
