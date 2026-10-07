import { HttpErrorResponse } from '@angular/common/http';
import { signal } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { ConfirmationService, MessageService } from 'primeng/api';
import { Api } from '../../api/api';
import { createGitHubSecret } from '../../api/fn/integrations/create-git-hub-secret';
import { getGitHubIntegration } from '../../api/fn/integrations/get-git-hub-integration';
import { revokeGitHubSecret } from '../../api/fn/integrations/revoke-git-hub-secret';
import { GitHubIntegration, GitHubSecretCreated, Problem } from '../../api/models';
import { SessionService } from '../../core/session.service';
import { GitHubWebhook, gitHubEventName } from './github-webhook';

const path = '/api/v1/tenants/acme/integrations/github/webhook';
const madeBy = { id: 'p1', username: 'hans', display_name: 'Hans' };

function integration(withSecret: boolean): GitHubIntegration {
  return {
    webhook_path: path,
    events: ['pull_request', 'push'],
    secret: withSecret ? { created_at: '2026-10-06T08:00:00Z', created_by: madeBy } : null,
  };
}

describe('gitHubEventName', () => {
  it("names the events as GitHub's webhook form does", () => {
    expect(gitHubEventName('pull_request')).toBe('Pull requests');
    expect(gitHubEventName('push')).toBe('Pushes');
    expect(gitHubEventName('release')).toBe('release');
  });
});

describe('GitHubWebhook (docs/adr/0071 D1, D7)', () => {
  let state: GitHubIntegration;
  let invoke: ReturnType<typeof vi.fn>;
  const secret = 'a'.repeat(64);

  beforeEach(() => {
    state = integration(false);
    invoke = vi.fn(async (fn: unknown) => {
      if (fn === getGitHubIntegration) {
        return state;
      }
      if (fn === createGitHubSecret) {
        const replaced = state.secret !== null;
        state = integration(true);
        const made: GitHubSecretCreated = {
          secret,
          replaced,
          created_at: '2026-10-06T08:00:00Z',
          created_by: madeBy,
        };
        return made;
      }
      if (fn === revokeGitHubSecret) {
        state = integration(false);
        return undefined;
      }
      throw new Error('unexpected call');
    });
    TestBed.configureTestingModule({
      providers: [
        MessageService,
        { provide: Api, useValue: { invoke } },
        { provide: SessionService, useValue: { tenant: signal('acme') } },
      ],
    });
  });

  it('says that the tenant takes no webhook, and where GitHub would post, until a secret exists', async () => {
    const fixture = await render();

    expect(text(fixture, 'github-state')).toContain('takes no webhook');
    expect((el(fixture, 'github-url') as HTMLInputElement).value).toBe(
      `${window.location.origin}${path}`,
    );
    expect(text(fixture, 'github-make')).toBe('Make the secret');
    expect(el(fixture, 'github-revoke')).toBeNull();
    expect(host(fixture).textContent).toContain('application/json');
    expect(host(fixture).textContent).toContain('Pull requests and Pushes');
  });

  it('makes the secret and shows it once, with the payload URL, until it is stored', async () => {
    const fixture = await render();

    click(fixture, 'github-make');
    await settle(fixture);

    expect(invoke).toHaveBeenCalledWith(createGitHubSecret, { tenant: 'acme' });
    expect((document.querySelector('[data-testid="secret-value"]') as HTMLInputElement).value).toBe(
      secret,
    );
    expect(document.querySelector('[data-testid="secret-url"]')?.textContent).toBe(
      `${window.location.origin}${path}`,
    );
    expect(text(fixture, 'github-state')).toContain('made');
    expect(text(fixture, 'github-state')).toContain('Hans');

    (document.querySelector('[data-testid="secret-done"]') as HTMLButtonElement).click();
    await settle(fixture);
    expect(document.querySelector('[data-testid="secret-value"]')).toBeNull();
    expect(host(fixture).textContent).not.toContain(secret);
  });

  it('asks before it rotates, and says the old secret is refused from now on', async () => {
    state = integration(true);
    const fixture = await render();
    const confirm = spyOnConfirm(fixture);

    expect(text(fixture, 'github-make')).toBe('Rotate the secret');
    click(fixture, 'github-make');
    expect(invoke).not.toHaveBeenCalledWith(createGitHubSecret, expect.anything());
    expect(confirm).toHaveBeenCalledOnce();
    expect(confirm.mock.calls[0][0].message).toContain('refused');

    confirm.mock.calls[0][0].accept?.();
    await settle(fixture);

    expect(invoke).toHaveBeenCalledWith(createGitHubSecret, { tenant: 'acme' });
    expect(document.querySelector('[data-testid="secret-dialog"]')?.textContent).toContain(
      'The secret before it is refused from now on',
    );
  });

  it('asks before it revokes, and then reads the tenant without a secret', async () => {
    state = integration(true);
    const fixture = await render();
    const confirm = spyOnConfirm(fixture);

    click(fixture, 'github-revoke');
    confirm.mock.calls[0][0].accept?.();
    await settle(fixture);

    expect(invoke).toHaveBeenCalledWith(revokeGitHubSecret, { tenant: 'acme' });
    expect(text(fixture, 'github-state')).toContain('takes no webhook');
  });

  it('says why when the webhook could not be loaded', async () => {
    const problem: Problem = {
      type: 'about:blank',
      title: 'Forbidden',
      status: 403,
      detail: 'the act needs the admin role',
      code: 'forbidden',
    };
    invoke.mockRejectedValue(new HttpErrorResponse({ status: 403, error: problem }));
    const fixture = await render();

    expect(text(fixture, 'github-failure')).toBe(
      "GitHub's webhook could not be loaded: the act needs the admin role",
    );
  });

  async function render() {
    const fixture = TestBed.createComponent(GitHubWebhook);
    await settle(fixture);
    return fixture;
  }

  async function settle(fixture: ComponentFixture<GitHubWebhook>) {
    for (let round = 0; round < 3; round++) {
      await new Promise((resolve) => setTimeout(resolve));
      fixture.detectChanges();
    }
    await fixture.whenStable();
  }

  function spyOnConfirm(fixture: ComponentFixture<GitHubWebhook>) {
    return vi
      .spyOn(fixture.debugElement.injector.get(ConfirmationService), 'confirm')
      .mockImplementation(function (this: ConfirmationService) {
        return this;
      });
  }

  const host = (fixture: ComponentFixture<GitHubWebhook>) => fixture.nativeElement as HTMLElement;
  const el = (fixture: ComponentFixture<GitHubWebhook>, testId: string) =>
    host(fixture).querySelector<HTMLElement>(`[data-testid="${testId}"]`);
  const text = (fixture: ComponentFixture<GitHubWebhook>, testId: string) =>
    el(fixture, testId)?.textContent?.replace(/\s+/g, ' ').trim();
  const click = (fixture: ComponentFixture<GitHubWebhook>, testId: string) =>
    (el(fixture, testId) as HTMLButtonElement).click();
});
