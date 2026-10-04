import { Component, signal } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { By } from '@angular/platform-browser';
import { Tooltip } from 'primeng/tooltip';
import { TokenMark } from '../api/models';
import { AgentMark, agentName } from './agent-mark';

describe('agentName', () => {
  it("is a mark's first part, the agent", () => {
    expect(agentName('claude-code/claude-opus/7f3a')).toBe('claude-code');
    expect(agentName('chat/qwen:qwen3-30b-a3b-2507/1c2d')).toBe('chat');
    expect(agentName('claude')).toBe('claude');
  });

  it('says unknown agent for the mark of an agent that named none', () => {
    expect(agentName('unknown-agent')).toBe('unknown agent');
  });
});

const script: TokenMark = { id: 'tok-1', name: 'ci-script' };
const laptop: TokenMark = { id: 'tok-2', name: 'claude-laptop' };

@Component({
  imports: [AgentMark],
  template: `<app-agent-mark [mark]="mark()" [token]="token()" [agent]="agent()" />`,
})
class Host {
  readonly mark = signal<string | null>('claude-code/claude-opus/7f3a');
  readonly token = signal<TokenMark | null>(null);
  readonly agent = signal<boolean | null>(null);
}

describe('AgentMark', () => {
  async function render(
    mark: string | null,
    token: TokenMark | null = null,
    agent: boolean | null = null,
  ) {
    TestBed.configureTestingModule({});
    const fixture = TestBed.createComponent(Host);
    fixture.componentInstance.mark.set(mark);
    fixture.componentInstance.token.set(token);
    fixture.componentInstance.agent.set(agent);
    fixture.detectChanges();
    await fixture.whenStable();
    const badge = (fixture.nativeElement as HTMLElement).querySelector(
      '[data-testid="agent-mark"]',
    ) as HTMLElement;
    const tooltip = () =>
      fixture.debugElement
        .query(By.css('[data-testid="agent-mark"]'))
        .injector.get(Tooltip)
        .content();
    return { fixture, badge, tooltip };
  }

  const read = (badge: HTMLElement) => badge.textContent?.replace(/\s+/g, ' ').trim();
  /** What a sighted person reads: the text outside the screen reader's spans. */
  const shown = (badge: HTMLElement) => {
    const copy = badge.cloneNode(true) as HTMLElement;
    copy.querySelectorAll('.sr-only').forEach((only) => only.remove());
    return copy.textContent?.trim();
  };

  afterEach(() => TestBed.resetTestingModule());

  it('shows the agent icon and the agent, reads the whole mark, and can be focused', async () => {
    const { badge, tooltip } = await render('claude-code/claude-opus/7f3a');

    expect(badge.querySelector('.pi-microchip-ai')?.getAttribute('aria-hidden')).toBe('true');
    expect(read(badge)).toBe('by the agent claude-code (claude-code/claude-opus/7f3a)');
    expect(shown(badge)).toBe('claude-code');
    expect(badge.getAttribute('data-agent')).toBe('claude-code/claude-opus/7f3a');
    expect(badge.hasAttribute('data-token')).toBe(false);
    expect(badge.getAttribute('tabindex')).toBe('0');
    expect(tooltip()).toBe("Done by an agent in this person's name: claude-code/claude-opus/7f3a");
  });

  it('reads a mark that is only a name once', async () => {
    const { badge } = await render('claude');

    expect(read(badge)).toBe('by the agent claude');
  });

  it('says only that an agent did it where no mark is known', async () => {
    const { badge } = await render(null);

    expect(read(badge)).toBe('by an agent');
    expect(badge.getAttribute('data-agent')).toBe('agent');
  });

  describe('an act through a token', () => {
    it('marks an act of a token without an agent as made through the token, by its name', async () => {
      const { badge, tooltip } = await render(null, script);

      expect(badge.querySelector('.pi-microchip-ai')).not.toBeNull();
      expect(shown(badge)).toBe('token ci-script');
      expect(read(badge)).toBe('through the token ci-script');
      expect(badge.hasAttribute('data-agent')).toBe(false);
      expect(badge.getAttribute('data-token')).toBe('tok-1');
      expect(tooltip()).toBe("Done through the token ci-script in this person's name");
    });

    it('marks it as made through a token where the act did not record the name', async () => {
      const { badge, tooltip } = await render(null, { id: 'tok-0', name: null });

      expect(shown(badge)).toBe('token');
      expect(read(badge)).toBe('through a token');
      expect(tooltip()).toBe("Done through a token in this person's name");
    });

    it("shows an agent's act by the agent, and names the token it came through", async () => {
      const { badge, tooltip } = await render('claude-code/claude-opus/7f3a', laptop);

      expect(shown(badge)).toBe('claude-code');
      expect(read(badge)).toBe(
        'by the agent claude-code (claude-code/claude-opus/7f3a), through the token claude-laptop',
      );
      expect(badge.getAttribute('data-agent')).toBe('claude-code/claude-opus/7f3a');
      expect(badge.getAttribute('data-token')).toBe('tok-2');
      expect(tooltip()).toBe(
        "Done by an agent in this person's name: claude-code/claude-opus/7f3a, through the token claude-laptop",
      );
    });

    it("takes the act as an agent's where the page says so, without a mark", async () => {
      const { badge, tooltip } = await render(null, laptop, true);

      expect(shown(badge)).toBe('agent');
      expect(read(badge)).toBe('by an agent, through the token claude-laptop');
      expect(badge.getAttribute('data-agent')).toBe('agent');
      expect(tooltip()).toBe(
        "Done by an agent in this person's name, through the token claude-laptop",
      );
    });

    it("takes the act as the person's through the token where the page says no agent did it", async () => {
      const { badge } = await render(null, script, false);

      expect(read(badge)).toBe('through the token ci-script');
      expect(badge.hasAttribute('data-agent')).toBe(false);
    });

    it('says only that a token was used where the page says no agent did it and names no token', async () => {
      const { badge, tooltip } = await render(null, null, false);

      expect(read(badge)).toBe('through a token');
      expect(tooltip()).toBe("Done through a token in this person's name");
    });
  });
});
