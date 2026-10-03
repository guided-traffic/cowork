import { Type } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { By } from '@angular/platform-browser';
import { MessageService } from 'primeng/api';
import { Select } from 'primeng/select';
import { SelectButton } from 'primeng/selectbutton';
import { Slider } from 'primeng/slider';
import { ToggleSwitch } from 'primeng/toggleswitch';
import type { MockInstance } from 'vitest';
import { logoColours } from '../theme/cowork-preset';
import { DesignPreview } from './design-preview';

const schemes = ['light', 'dark'];

describe('DesignPreview', () => {
  let add: MockInstance<MessageService['add']>;

  // The page is heavy, so each test renders it once and looks at as much of it as it needs.
  async function renderFixture(): Promise<ComponentFixture<DesignPreview>> {
    TestBed.configureTestingModule({ providers: [MessageService] });
    add = vi.spyOn(TestBed.inject(MessageService), 'add');
    const fixture = TestBed.createComponent(DesignPreview);
    await fixture.whenStable();
    return fixture;
  }

  async function render() {
    return (await renderFixture()).nativeElement as HTMLElement;
  }

  const text = (parent: Element | null, selector: string) =>
    parent?.querySelector(selector)?.textContent?.replace(/\s+/g, ' ').trim();

  const all = (parent: Element | null, selector: string) => [
    ...(parent?.querySelectorAll(selector) ?? []),
  ];

  const distinct = (panel: Element | null, attribute: string) => [
    ...new Set(all(panel, `[${attribute}]`).map((badge) => badge.getAttribute(attribute))),
  ];

  describe('the logo', () => {
    /** The name in the heading of a card, without the pill that may follow it. */
    const nameOf = (variant: Element | null) =>
      [...(variant?.querySelector('h3')?.childNodes ?? [])]
        .filter((node) => node.nodeType === Node.TEXT_NODE)
        .map((node) => node.textContent)
        .join('')
        .trim();

    it('shows the three variants, the board first, each with its name and its idea', async () => {
      const page = await render();

      const variants = all(page, '.logos .logo-card');
      expect(variants.map((variant) => variant.getAttribute('data-testid'))).toEqual([
        'logo-board',
        'logo-twin',
        'logo-spark-c',
      ]);
      expect(variants.map(nameOf)).toEqual(['Board spark', 'Twin sparkles', 'Spark C']);
      expect(variants.map((variant) => text(variant, 'p.muted'))).toEqual([
        'Three board columns and the spark that works them',
        'Two co-workers, a person and an agent: the motif of the reference',
        'The c of cowork with the spark in its opening',
      ]);
    });

    it('marks the variant that was chosen, and only that one', async () => {
      const page = await render();

      expect(all(page, '.logo-card .chosen').map((pill) => pill.textContent)).toEqual(['chosen']);
      expect(text(page.querySelector('[data-testid="logo-board"]'), 'h3 .chosen')).toBe('chosen');
      expect(page.querySelector('[data-testid="logo-twin"] .chosen')).toBeNull();
      expect(page.querySelector('[data-testid="logo-spark-c"] .chosen')).toBeNull();
    });

    it('shows each variant in five sizes and lets the marks from 32 pixels on glow', async () => {
      const page = await render();

      for (const testId of ['logo-twin', 'logo-spark-c', 'logo-board']) {
        const marks = all(
          page.querySelector(`[data-testid="${testId}"]`),
          'app-logo-mark',
        ) as HTMLElement[];
        expect(
          marks.map((mark) => mark.style.getPropertyValue('--logo-size')),
          testId,
        ).toEqual(['16px', '24px', '32px', '48px', '72px']);
        expect(
          marks.map((mark) => mark.classList.contains('glow')),
          testId,
        ).toEqual([false, false, true, true, true]);
      }
    });

    it('shows the header in three forms: the pill at 42 px, the mark with the name, the pill at 56 px', async () => {
      const page = await render();

      const [today, lockup, login] = all(page, '.wordmarks figure');
      expect(all(page, '.wordmarks figure')).toHaveLength(3);
      expect(
        (today.querySelector('app-wordmark') as HTMLElement).style.getPropertyValue(
          '--wordmark-height',
        ),
      ).toBe('42px');
      expect(text(today, 'figcaption')).toBe('The top bar today: the pill at 42 px');
      expect(lockup.querySelector('app-wordmark')).toBeNull();
      expect(
        (lockup.querySelector('app-logo-mark') as HTMLElement).style.getPropertyValue(
          '--logo-size',
        ),
      ).toBe('40px');
      expect(text(lockup, '.lockup-name')).toBe('cowork');
      expect(text(lockup, 'figcaption')).toBe('Alternative: the mark and the name, no pill');
      expect(
        (login.querySelector('app-wordmark') as HTMLElement).style.getPropertyValue(
          '--wordmark-height',
        ),
      ).toBe('56px');
      expect(text(login, 'figcaption')).toBe('The pill at 56 px (the login page)');
    });

    it('lists the colours of the logo with their values', async () => {
      const page = await render();

      const swatches = all(page, '.swatches .swatch').map((swatch) => [
        text(swatch, 'span:not(.chip)'),
        text(swatch, 'code'),
      ]);
      expect(swatches).toEqual(Object.entries(logoColours));
    });
  });

  describe('the panels', () => {
    it('shows a light and a dark panel, each in its own colour scheme', async () => {
      const page = await render();

      const panels = all(page, '.schemes [data-scheme]') as HTMLElement[];
      expect(panels.map((panel) => panel.dataset['scheme'])).toEqual(schemes);
      expect(panels.map((panel) => panel.style.colorScheme)).toEqual(schemes);
      expect(panels.map((panel) => text(panel, 'h2'))).toEqual(schemes);
    });

    it('shows the primary and the surface scale on both panels', async () => {
      const page = await render();

      const steps = [50, 100, 200, 300, 400, 500, 600, 700, 800, 900, 950];
      for (const scheme of schemes) {
        const swatches = all(page, `[data-scheme="${scheme}"] .scale .step`) as HTMLElement[];
        expect(
          swatches.map((step) => step.title),
          scheme,
        ).toEqual([
          ...steps.map((step) => `primary ${step}`),
          ...steps.map((step) => `surface ${step}`),
        ]);
        expect(swatches[5].style.background, scheme).toBe('var(--p-primary-500)');
        expect(swatches[steps.length + 5].style.background, scheme).toBe('var(--p-surface-500)');
      }
    });

    it('shows every state, severity, security class and type as a badge on both panels', async () => {
      const page = await render();

      for (const scheme of schemes) {
        const panel = page.querySelector(`[data-scheme="${scheme}"]`);
        expect(distinct(panel, 'data-state'), scheme).toEqual([
          'filed',
          'analysed',
          'decided',
          'in-progress',
          'review',
          'blocked',
          'done',
          'dropped',
        ]);
        expect(distinct(panel, 'data-severity'), scheme).toEqual([
          'critical',
          'high',
          'medium',
          'low',
          'cosmetic',
        ]);
        expect(distinct(panel, 'data-security'), scheme).toEqual(['live', 'boundary', 'hardening']);
        // The five types of the row, then the bug of the sample card.
        expect(
          all(panel, 'app-type[title]').map((type) => type.getAttribute('title')?.split(':')[0]),
          scheme,
        ).toEqual(['task', 'bug', 'feature', 'decision', 'question', 'bug']);
      }
    });

    it('shows the buttons and the widgets with their sample values on both panels', async () => {
      const page = await render();

      for (const scheme of schemes) {
        const panel = page.querySelector(`[data-scheme="${scheme}"]`);
        const buttons = all(panel, 'button[pButton]');
        expect(
          buttons.map((button) => button.textContent?.trim()),
          scheme,
        ).toEqual(['File a ticket', 'Move to decided', 'Comment', 'Cancel', '']);
        expect(buttons.at(-1)?.getAttribute('aria-label'), scheme).toBe('Delete');
        expect(panel?.querySelector<HTMLInputElement>('input[pInputText]')?.value, scheme).toBe(
          'The board flickers when an event arrives',
        );
        const [assignee, states] = all(panel, 'p-select .p-select-label');
        expect(assignee.textContent?.trim(), scheme).toBe('Ada Lovelace');
        expect(states.textContent?.trim(), scheme).toBe('in-progress, blocked');
        expect(text(panel, 'p-selectbutton .p-togglebutton-checked'), scheme).toBe('M');
        expect(text(panel, '.slider .muted'), scheme).toBe('Progress 50%');
        expect(
          panel?.querySelector('p-toggleswitch input')?.getAttribute('aria-checked'),
          scheme,
        ).toBe('true');
      }
    });

    it('keeps the widgets of both panels in step with what is chosen in one of them', async () => {
      const fixture = await renderFixture();
      const page = fixture.nativeElement as HTMLElement;
      const change = (widget: Type<unknown>, value: unknown, nth = 0) => {
        const widgets = fixture.debugElement.queryAll(By.directive(widget));
        widgets[nth].triggerEventHandler('ngModelChange', value);
      };

      change(Slider, 75);
      change(Select, 'sam');
      change(Select, ['done'], 1);
      change(SelectButton, 'L');
      change(ToggleSwitch, false);
      await fixture.whenStable();
      fixture.detectChanges();
      await fixture.whenStable();

      for (const scheme of schemes) {
        const panel = page.querySelector(`[data-scheme="${scheme}"]`);
        expect(text(panel, '.slider .muted'), scheme).toBe('Progress 75%');
        const [assignee, states] = all(panel, 'p-select .p-select-label');
        expect(assignee.textContent?.trim(), scheme).toBe('Sam Rivera');
        expect(states.textContent?.trim(), scheme).toBe('done');
        expect(text(panel, 'p-selectbutton .p-togglebutton-checked'), scheme).toBe('L');
        expect(
          panel?.querySelector('p-toggleswitch input')?.getAttribute('aria-checked'),
          scheme,
        ).toBe('false');
      }
    });

    it('shows a sample card on both panels', async () => {
      const page = await render();

      for (const scheme of schemes) {
        const card = page.querySelector(`[data-scheme="${scheme}"] .sample-card`);
        expect(text(card, '.sample-title'), scheme).toBe(
          'The board flickers when an event arrives during a drag',
        );
        expect(text(card, '.tabular'), scheme).toBe('COW-9');
        expect(card?.querySelector('[data-state]')?.getAttribute('data-state'), scheme).toBe(
          'in-progress',
        );
        expect(card?.querySelector('[data-severity]')?.getAttribute('data-severity'), scheme).toBe(
          'medium',
        );
      }
    });
  });

  describe('the toasts', () => {
    it('adds a toast of its severity through the message service when a button is clicked', async () => {
      const page = await render();
      expect(add).not.toHaveBeenCalled();
      const moved = { summary: 'Ticket moved', detail: 'COW-12 is in-progress now.', life: 4000 };
      const unreachable = {
        summary: 'The backend cannot be reached',
        detail: 'cowork tries again on its own.',
        life: 4000,
      };

      for (const label of ['Success', 'Info', 'Warning', 'Error']) {
        all(page, 'button[pButton]')
          .find((button) => button.textContent?.trim() === label)
          ?.dispatchEvent(new MouseEvent('click', { bubbles: true }));
      }

      expect(add.mock.calls.map(([message]) => message)).toEqual([
        { severity: 'success', ...moved },
        { severity: 'info', ...moved },
        { severity: 'warn', ...moved },
        { severity: 'error', ...unreachable },
      ]);
    });
  });
});
