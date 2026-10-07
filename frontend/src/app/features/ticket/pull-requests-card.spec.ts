import { HttpErrorResponse } from '@angular/common/http';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { ConfirmationService, MessageService } from 'primeng/api';
import { PullRequest } from '../../api/models';
import { Conversation } from '../../core/conversation.service';
import { ProblemService } from '../../core/problem.service';
import { PullRequestsCard } from './pull-requests-card';

const sha = '0d1a26e67d8f5eaf1f6ba5c57fc3c7d91ac0fd1c';

function pr(overrides: Partial<PullRequest> = {}): PullRequest {
  return {
    id: 'l1',
    kind: 'pull_request',
    repository: 'github.com/acme/app',
    number: 34,
    sha: null,
    title: 'fix: the gate (VKO-12)',
    state: 'open',
    url: 'https://github.com/acme/app/pull/34',
    author: 'octocat',
    merged_at: null,
    found_in: 'subject',
    first_seen_at: '2026-10-05T08:00:00Z',
    last_seen_at: '2026-10-05T08:00:00Z',
    ...overrides,
  };
}

describe('PullRequestsCard (docs/adr/0071 D6)', () => {
  let removePullRequest: ReturnType<typeof vi.fn>;
  let report: ReturnType<typeof vi.fn>;

  beforeEach(() => {
    removePullRequest = vi.fn().mockResolvedValue(undefined);
    report = vi.fn();
    TestBed.configureTestingModule({
      providers: [
        MessageService,
        ConfirmationService,
        { provide: Conversation, useValue: { removePullRequest } },
        { provide: ProblemService, useValue: { report } },
      ],
    });
  });

  it('lists a pull request and a commit with their page, state, title, author and where the key was read', async () => {
    const fixture = await render([
      pr(),
      pr({
        id: 'l2',
        kind: 'commit',
        number: null,
        sha,
        state: 'merged',
        title: 'docs: name it (VKO-12)',
        url: `https://github.com/acme/app/commit/${sha}`,
        author: null,
        merged_at: '2026-10-06T09:00:00Z',
        found_in: 'trailer',
      }),
    ]);

    const first = item(fixture, 'l1');
    const link = first.querySelector('a') as HTMLAnchorElement;
    expect(link.textContent?.trim()).toBe('#34');
    expect(link.href).toBe('https://github.com/acme/app/pull/34');
    expect(link.target).toBe('_blank');
    expect(link.rel).toBe('noopener noreferrer');
    expect(first.querySelector('[data-testid="pull-request-state"]')?.textContent?.trim()).toBe(
      'open',
    );
    expect(first.textContent).toContain('fix: the gate (VKO-12)');
    expect(first.textContent).toContain('github.com/acme/app · octocat');
    expect(first.querySelector('[data-testid="pull-request-found-in"]')?.textContent?.trim()).toBe(
      'key in its title',
    );

    const commit = item(fixture, 'l2');
    expect(commit.querySelector('a')?.textContent?.trim()).toBe('0d1a26e');
    expect(commit.querySelector('[data-testid="pull-request-state"]')?.textContent?.trim()).toBe(
      'pushed',
    );
    expect(commit.textContent).toContain('pushed ');
    expect(commit.textContent).toContain('key in a Cowork-Ticket trailer');
  });

  it('shows the title as text, never as markup', async () => {
    const fixture = await render([pr({ title: '<img src=x onerror=alert(1)> (VKO-12)' })]);

    expect(item(fixture, 'l1').querySelector('img')).toBeNull();
    expect(item(fixture, 'l1').textContent).toContain('<img src=x onerror=alert(1)>');
  });

  it('offers no removal to a reader who does not work on the ticket', async () => {
    const fixture = await render([pr()], false);

    expect(
      fixture.nativeElement.querySelector('[data-testid="remove-pull-request-l1"]'),
    ).toBeNull();
  });

  it('removes a link after the question, for good, and reports a refusal', async () => {
    const fixture = await render([pr()]);
    const confirm = vi
      .spyOn(TestBed.inject(ConfirmationService), 'confirm')
      .mockImplementation(function (this: ConfirmationService) {
        return this;
      });

    (
      fixture.nativeElement.querySelector(
        '[data-testid="remove-pull-request-l1"]',
      ) as HTMLButtonElement
    ).click();
    expect(removePullRequest).not.toHaveBeenCalled();
    expect(confirm.mock.calls[0][0].message).toContain('do not link it again');

    confirm.mock.calls[0][0].accept?.();
    await settle(fixture);
    expect(removePullRequest).toHaveBeenCalledWith('acme/VKO-12', 'l1');

    removePullRequest.mockRejectedValueOnce(new HttpErrorResponse({ status: 403 }));
    confirm.mock.calls[0][0].accept?.();
    await settle(fixture);
    expect(report).toHaveBeenCalledOnce();
  });

  async function render(items: PullRequest[], mayRemove = true) {
    const fixture = TestBed.createComponent(PullRequestsCard);
    fixture.componentRef.setInput('ticketKey', 'acme/VKO-12');
    fixture.componentRef.setInput('items', items);
    fixture.componentRef.setInput('mayRemove', mayRemove);
    await settle(fixture);
    return fixture;
  }

  async function settle(fixture: ComponentFixture<PullRequestsCard>) {
    await new Promise((resolve) => setTimeout(resolve));
    fixture.detectChanges();
    await fixture.whenStable();
  }

  const item = (fixture: ComponentFixture<PullRequestsCard>, id: string) =>
    fixture.nativeElement.querySelector(`[data-testid="pull-request-${id}"]`) as HTMLElement;
});
