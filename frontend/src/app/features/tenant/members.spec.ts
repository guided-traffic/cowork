import { HttpErrorResponse } from '@angular/common/http';
import { signal, WritableSignal } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { MessageService } from 'primeng/api';
import { Member, Problem } from '../../api/models';
import { MembersService } from '../../core/members.service';
import { Members } from './members';

const ada: Member = {
  role: 'admin',
  person: { id: 'p1', display_name: 'Ada Lovelace', username: 'local:ada' },
};
const sam: Member = {
  role: 'member',
  person: { id: 'p2', display_name: 'Sam Rivera', username: 'local:sam' },
};
const robot: Member = {
  role: 'viewer',
  person: { id: 'p9', display_name: 'Build Robot', username: null },
};

describe('Members', () => {
  let list: WritableSignal<Member[]>;
  let loading: WritableSignal<boolean>;
  let error: WritableSignal<unknown>;

  beforeEach(() => {
    list = signal<Member[]>([]);
    loading = signal(false);
    error = signal<unknown>(undefined);
    TestBed.configureTestingModule({
      providers: [
        MessageService,
        { provide: MembersService, useValue: { list, members: { isLoading: loading, error } } },
      ],
    });
  });

  async function render() {
    const fixture = TestBed.createComponent(Members);
    await fixture.whenStable();
    return { fixture, page: fixture.nativeElement as HTMLElement };
  }

  const cells = (row: Element | null) =>
    [...(row?.querySelectorAll('td') ?? [])].map((cell) => cell.textContent);

  const text = (page: HTMLElement, testId: string) =>
    page.querySelector(`[data-testid="${testId}"]`)?.textContent?.trim();

  it('shows a row per member with the name, the username and the role', async () => {
    list.set([ada, sam]);

    const { page } = await render();

    expect(page.querySelector('h1')?.textContent).toBe('Members');
    expect(cells(page.querySelector('[data-testid="member-local:ada"]'))).toEqual([
      'Ada Lovelace',
      'local:ada',
      'admin',
    ]);
    expect(cells(page.querySelector('[data-testid="member-local:sam"]'))).toEqual([
      'Sam Rivera',
      'local:sam',
      'member',
    ]);
    expect(page.querySelector('[data-testid="members-empty"]')).toBeNull();
  });

  it('identifies a member without a username by the id of the person and shows a dash', async () => {
    list.set([robot]);

    const { page } = await render();

    expect(cells(page.querySelector('[data-testid="member-p9"]'))).toEqual([
      'Build Robot',
      '—',
      'viewer',
    ]);
  });

  it('shows the column headers', async () => {
    const { page } = await render();

    expect([...page.querySelectorAll('th')].map((header) => header.textContent)).toEqual([
      'Name',
      'Username',
      'Role',
    ]);
    expect(page.querySelector('[data-testid="members"]')).not.toBeNull();
  });

  it('follows the list when members arrive', async () => {
    const { fixture, page } = await render();
    expect(page.querySelector('tbody tr[data-testid^="member-"]')).toBeNull();

    list.set([ada]);
    await fixture.whenStable();

    expect(page.querySelector('[data-testid="member-local:ada"]')).not.toBeNull();
  });

  describe('without members', () => {
    it('says there are none', async () => {
      const { page } = await render();

      expect(text(page, 'members-empty')).toBe('No members.');
    });

    it('does not say so while the members load', async () => {
      loading.set(true);

      const { fixture, page } = await render();
      expect(page.querySelector('[data-testid="members-empty"]')).toBeNull();

      loading.set(false);
      await fixture.whenStable();

      expect(text(page, 'members-empty')).toBe('No members.');
    });
  });

  describe('members that could not be loaded', () => {
    it('says why, with the detail of the problem', async () => {
      const body: Problem = {
        type: 'about:blank',
        title: 'The service is not ready',
        status: 503,
        detail: 'The database is starting.',
        code: 'not_ready',
      };
      error.set(new HttpErrorResponse({ status: 503, statusText: body.title, error: body }));

      const { page } = await render();

      expect(text(page, 'members-empty')).toBe(
        'The members could not be loaded: The database is starting.',
      );
    });

    it('says why with the title when the problem has no detail', async () => {
      const body: Problem = {
        type: 'about:blank',
        title: 'Forbidden',
        status: 403,
        code: 'forbidden',
      };
      error.set(new HttpErrorResponse({ status: 403, statusText: 'Forbidden', error: body }));

      const { page } = await render();

      expect(text(page, 'members-empty')).toBe('The members could not be loaded: Forbidden');
    });

    it('says why when the backend cannot be reached', async () => {
      error.set(new HttpErrorResponse({ status: 0, statusText: 'Unknown Error' }));

      const { page } = await render();

      expect(text(page, 'members-empty')).toBe(
        'The members could not be loaded: The connection failed; cowork tries again on its own.',
      );
    });
  });
});
