import { ChangeDetectionStrategy, Component, computed, input } from '@angular/core';
import { Tooltip } from 'primeng/tooltip';
import { TokenMark } from '../api/models';

/**
 * The agent's name in a mark — its first part: `claude-code` of `claude-code/<model>/<session>`,
 * `chat` of the chat's `chat/<model>/<conversation>` (docs/adr/0036 D3).
 */
export function agentName(mark: string): string {
  return mark === 'unknown-agent' ? 'unknown agent' : (mark.split('/')[0] ?? mark);
}

/**
 * Marks an act made in a person's name by an agent, or through one of the person's tokens — a
 * change, a comment, a question, an answer recorded, a file, a time entry — with the agent icon,
 * so that neither ever reads as the person's own (docs/adr/0036 D6). An agent's act shows the
 * agent's name, with the whole mark and the token it came through in the tooltip; an act through a
 * token and no agent shows `token <name>`. Only the person's own browser session acts unmarked, and
 * the page leaves the mark out there. The tooltip opens on focus as well, and the mark is part of
 * the text a screen reader reads.
 */
@Component({
  selector: 'app-agent-mark',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [Tooltip],
  template: `<span
    class="pill outline agent-mark"
    tabindex="0"
    [pTooltip]="tooltip()"
    tooltipEvent="both"
    [showDelay]="400"
    [attr.data-agent]="isAgent() ? (mark() ?? 'agent') : null"
    [attr.data-token]="token()?.id ?? null"
    data-testid="agent-mark"
    ><span class="sr-only">{{ lead() }}</span><i class="pi pi-microchip-ai" aria-hidden="true"></i
    >{{ name() }}@if (suffix(); as more) {<span class="sr-only">{{ more }}</span>}</span
  >`,
  styles: `
    .agent-mark {
      --accent: var(--p-primary-color);
      margin-inline: 0.25rem;
      vertical-align: middle;
    }
    .sr-only {
      position: absolute;
      width: 1px;
      height: 1px;
      overflow: hidden;
      clip-path: inset(50%);
      white-space: nowrap;
    }
  `,
})
export class AgentMark {
  /** The agent mark the act was recorded with; none where the API says only that an agent did it. */
  readonly mark = input<string | null>(null);
  /** The token the act came through; none for an act of the person's browser session. */
  readonly token = input<TokenMark | null>(null);
  /**
   * Whether the act is an agent's. Left out, it is when a mark is given or no token is: an act
   * through a token without a mark is the person's, through that token. An answer an agent recorded
   * says so here, because the API marks it with a flag and no mark.
   */
  readonly agent = input<boolean | null>(null);

  protected readonly isAgent = computed(
    () => this.agent() ?? (this.mark() !== null || this.token() === null),
  );
  /** The token as a sentence names it: by its name, or as a token whose name was not recorded. */
  private readonly via = computed(() => {
    const token = this.token();
    if (!token) {
      return null;
    }
    return token.name ? `the token ${token.name}` : 'a token';
  });
  protected readonly name = computed(() => {
    if (!this.isAgent()) {
      const name = this.token()?.name;
      return name ? `token ${name}` : 'token';
    }
    const mark = this.mark();
    return mark ? agentName(mark) : 'agent';
  });
  /** What a screen reader reads before the name, which a sighted person learns from the icon. */
  protected readonly lead = computed(() => {
    if (!this.isAgent()) {
      return this.token()?.name ? ' through the ' : ' through a ';
    }
    return this.mark() ? ' by the agent ' : ' by an ';
  });
  /**
   * What a screen reader reads after the name, which a sighted person finds in the tooltip: the
   * whole mark, where it says more than the name, and the token an agent's act came through.
   */
  protected readonly suffix = computed(() => {
    if (!this.isAgent()) {
      return null;
    }
    const mark = this.mark();
    const via = this.via();
    const detail = mark && mark !== this.name() ? ` (${mark})` : '';
    return detail + (via ? `, through ${via}` : '') || null;
  });
  protected readonly tooltip = computed(() => {
    const via = this.via();
    if (!this.isAgent()) {
      return `Done through ${via ?? 'a token'} in this person's name`;
    }
    const mark = this.mark();
    const done = mark
      ? `Done by an agent in this person's name: ${mark}`
      : "Done by an agent in this person's name";
    return via ? `${done}, through ${via}` : done;
  });
}
