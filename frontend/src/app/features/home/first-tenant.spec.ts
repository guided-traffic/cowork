import { HttpErrorResponse } from '@angular/common/http';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideRouter, Router } from '@angular/router';
import { MessageService } from 'primeng/api';
import type { MockInstance } from 'vitest';
import { Problem, Tenant } from '../../api/models';
import { TenantsService } from '../../core/tenants.service';
import { FirstTenant, tenantSlug } from './first-tenant';

const made: Tenant = {
  slug: 'acme',
  name: 'Acme Corp',
  version: 1,
  time_visible_to_members: false,
  members_create_projects: true,
  chat_external_allowed: false,
  created_at: '2026-10-03T10:00:00Z',
  updated_at: '2026-10-03T10:00:00Z',
};

function refusal(
  status: number,
  code: Problem['code'],
  errors: { pointer: string; message: string }[] = [],
) {
  const body: Problem = {
    type: 'about:blank',
    title: 'The tenant is not valid',
    status,
    detail: 'Check the slug.',
    code,
    errors,
  };
  return new HttpErrorResponse({ status, statusText: body.title, error: body });
}

const uuid = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/;

describe('tenantSlug', () => {
  it.each(['ab', 'acme', 'a1', '0a', 'a-b', 'a--b', 'ab-', 'a'.repeat(63)])(
    'accepts the slug %s',
    (slug) => {
      expect(tenantSlug.test(slug)).toBe(true);
    },
  );

  it.each([
    ['', 'nothing'],
    ['a', 'a single character'],
    ['a'.repeat(64), '64 characters'],
    ['-ab', 'a start with a hyphen'],
    ['Acme', 'an upper-case letter'],
    ['a_b', 'an underscore'],
    ['a.b', 'a dot'],
    ['a b', 'a space'],
    ['äb', 'a letter outside a to z'],
    [' ab', 'a leading space'],
    ['ab ', 'a trailing space'],
    ['ab\n', 'a trailing newline'],
  ])('rejects the slug %j: %s', (slug) => {
    expect(tenantSlug.test(slug)).toBe(false);
  });
});

describe('FirstTenant', () => {
  let create: MockInstance<TenantsService['create']>;
  let navigate: MockInstance<Router['navigate']>;

  beforeEach(() => {
    create = vi.fn<TenantsService['create']>().mockResolvedValue(made);
    TestBed.configureTestingModule({
      providers: [
        provideRouter([]),
        MessageService,
        { provide: TenantsService, useValue: { create } },
      ],
    });
    navigate = vi.spyOn(TestBed.inject(Router), 'navigate').mockResolvedValue(true);
  });

  async function render() {
    const fixture = TestBed.createComponent(FirstTenant);
    await settle(fixture);
    return fixture;
  }

  /** Lets what a submit started finish, and shows it; fields register with the form a moment later. */
  async function settle(fixture: ComponentFixture<FirstTenant>) {
    await new Promise((resolve) => setTimeout(resolve));
    fixture.detectChanges();
    await fixture.whenStable();
  }

  const host = (fixture: ComponentFixture<FirstTenant>) => fixture.nativeElement as HTMLElement;
  const el = (fixture: ComponentFixture<FirstTenant>, testId: string) =>
    host(fixture).querySelector<HTMLElement>(`[data-testid="${testId}"]`);

  function typeInto(fixture: ComponentFixture<FirstTenant>, testId: string, value: string) {
    const field = el(fixture, testId) as HTMLInputElement;
    field.value = value;
    field.dispatchEvent(new Event('input'));
    fixture.detectChanges();
  }

  const submit = (fixture: ComponentFixture<FirstTenant>) =>
    host(fixture)
      .querySelector('form')
      ?.dispatchEvent(new Event('submit', { cancelable: true }));

  const createButton = (fixture: ComponentFixture<FirstTenant>) =>
    el(fixture, 'first-tenant-create') as HTMLButtonElement;

  function fill(fixture: ComponentFixture<FirstTenant>, slug = 'acme', name = 'Acme Corp') {
    typeInto(fixture, 'first-tenant-slug', slug);
    typeInto(fixture, 'first-tenant-name', name);
  }

  describe('what it says', () => {
    it('is headed Create the first tenant, and says what a tenant is and what the person becomes', async () => {
      const fixture = await render();

      expect(host(fixture).querySelector('h1')?.textContent).toBe('Create the first tenant');
      const lead = host(fixture).querySelector('.lead')?.textContent ?? '';
      expect(lead).toContain('A tenant is one client or one organisation');
      expect(lead).toContain(
        'You are a global administrator and are not a member of any tenant yet',
      );
      expect(lead).toContain('The tenant you create makes you its administrator');
    });

    it('is a labelled region of the page, and asks for a slug and a name', async () => {
      const fixture = await render();

      const region = el(fixture, 'first-tenant');
      expect(region?.getAttribute('aria-labelledby')).toBe('first-tenant-title');
      expect(host(fixture).querySelector('#first-tenant-title')?.textContent).toBe(
        'Create the first tenant',
      );
      expect(el(fixture, 'first-tenant-slug')).not.toBeNull();
      expect(el(fixture, 'first-tenant-name')).not.toBeNull();
      expect(createButton(fixture).textContent?.trim()).toBe('Create tenant');
    });

    it('limits the slug to 63 characters and the name to two hundred, as the schema does', async () => {
      const fixture = await render();

      expect(el(fixture, 'first-tenant-slug')?.getAttribute('maxlength')).toBe('63');
      expect(el(fixture, 'first-tenant-name')?.getAttribute('maxlength')).toBe('200');
    });
  });

  describe('the slug while it is typed', () => {
    const hint = (fixture: ComponentFixture<FirstTenant>) => el(fixture, 'first-tenant-slug-hint');
    const field = (fixture: ComponentFixture<FirstTenant>) => el(fixture, 'first-tenant-slug');

    it('says what a slug is, quietly, before anything is typed and while it is valid', async () => {
      const fixture = await render();

      expect(hint(fixture)?.textContent).toContain('2 to 63 lower-case letters, digits or hyphens');
      expect(hint(fixture)?.textContent).toContain('never changes');
      expect(hint(fixture)?.classList).toContain('muted');
      expect(hint(fixture)?.classList).not.toContain('error');
      expect(field(fixture)?.getAttribute('aria-invalid')).toBe('false');

      typeInto(fixture, 'first-tenant-slug', 'acme');

      expect(hint(fixture)?.classList).toContain('muted');
      expect(field(fixture)?.getAttribute('aria-invalid')).toBe('false');
    });

    it('turns the same sentence into an error as soon as what is typed cannot be a slug', async () => {
      const fixture = await render();

      typeInto(fixture, 'first-tenant-slug', 'a');
      expect(hint(fixture)?.classList).toContain('error');
      expect(hint(fixture)?.classList).not.toContain('muted');
      expect(field(fixture)?.getAttribute('aria-invalid')).toBe('true');
      expect(field(fixture)?.getAttribute('aria-describedby')).toBe('first-tenant-slug-hint');

      typeInto(fixture, 'first-tenant-slug', 'acme corp');
      expect(hint(fixture)?.classList).toContain('error');

      typeInto(fixture, 'first-tenant-slug', '-acme');
      expect(hint(fixture)?.classList).toContain('error');

      typeInto(fixture, 'first-tenant-slug', 'acme');
      expect(hint(fixture)?.classList).toContain('muted');
      expect(field(fixture)?.getAttribute('aria-invalid')).toBe('false');
    });

    it('takes a slug typed in upper case as the same slug in lower case', async () => {
      const fixture = await render();

      typeInto(fixture, 'first-tenant-slug', 'Acme-Corp');
      await settle(fixture);

      expect((field(fixture) as HTMLInputElement).value).toBe('acme-corp');
      expect(hint(fixture)?.classList).toContain('muted');
    });
  });

  describe('what a tenant needs', () => {
    it('cannot be created without a valid slug and a name', async () => {
      const fixture = await render();
      expect(createButton(fixture).disabled).toBe(true);

      typeInto(fixture, 'first-tenant-slug', 'acme');
      expect(createButton(fixture).disabled).toBe(true);
      typeInto(fixture, 'first-tenant-name', 'Acme Corp');
      expect(createButton(fixture).disabled).toBe(false);
      typeInto(fixture, 'first-tenant-name', '   ');
      expect(createButton(fixture).disabled).toBe(true);
    });

    it.each(['a', '-acme', 'acme corp', 'a'.repeat(64)])(
      'cannot be created with the slug %j, which is no slug',
      async (slug) => {
        const fixture = await render();

        fill(fixture, slug);
        submit(fixture);
        await settle(fixture);

        expect(createButton(fixture).disabled).toBe(true);
        expect(create).not.toHaveBeenCalled();
      },
    );

    it('creates nothing while another one is on its way, and shows the button as busy', async () => {
      let finish: (tenant: Tenant) => void = () => undefined;
      create.mockReturnValue(new Promise<Tenant>((resolve) => (finish = resolve)));
      const fixture = await render();
      fill(fixture);

      submit(fixture);
      submit(fixture);
      await settle(fixture);
      expect(create).toHaveBeenCalledOnce();
      expect(createButton(fixture).disabled).toBe(true);
      expect(createButton(fixture).querySelector('i.pi-spinner')).not.toBeNull();
      finish(made);
      await settle(fixture);

      expect(createButton(fixture).querySelector('i.pi-spinner')).toBeNull();
    });
  });

  describe('creating', () => {
    it('creates the tenant with the slug and the name that were typed, the name trimmed', async () => {
      const fixture = await render();
      fill(fixture, 'acme', '  Acme Corp  ');

      submit(fixture);
      await settle(fixture);

      expect(create).toHaveBeenCalledExactlyOnceWith(
        { slug: 'acme', name: 'Acme Corp' },
        expect.stringMatching(uuid),
      );
    });

    it('goes into the tenant that was made, by the slug the server answered with', async () => {
      create.mockResolvedValue({ ...made, slug: 'acme-corp' });
      const fixture = await render();
      fill(fixture, 'acme', 'Acme Corp');

      submit(fixture);
      await settle(fixture);

      expect(navigate).toHaveBeenCalledExactlyOnceWith(['/t', 'acme-corp']);
    });
  });

  describe('a tenant that the server refuses', () => {
    it('shows the problem of the slug beside the slug, and keeps what was typed and goes nowhere', async () => {
      create.mockRejectedValue(
        refusal(409, 'tenant_slug_taken', [{ pointer: '/slug', message: 'is taken' }]),
      );
      const add = vi.spyOn(TestBed.inject(MessageService), 'add');
      const fixture = await render();
      fill(fixture);

      submit(fixture);
      await settle(fixture);

      expect(el(fixture, 'first-tenant-slug-error')?.textContent).toBe('is taken');
      expect((el(fixture, 'first-tenant-slug') as HTMLInputElement).value).toBe('acme');
      expect((el(fixture, 'first-tenant-name') as HTMLInputElement).value).toBe('Acme Corp');
      expect(navigate).not.toHaveBeenCalled();
      expect(add).not.toHaveBeenCalled();
      expect(createButton(fixture).disabled).toBe(false);
    });

    it('shows the problem of the name beside the name', async () => {
      create.mockRejectedValue(
        refusal(422, 'validation_failed', [{ pointer: '/name', message: 'must not be blank' }]),
      );
      const fixture = await render();
      fill(fixture);

      submit(fixture);
      await settle(fixture);

      expect(el(fixture, 'first-tenant-name-error')?.textContent).toBe('must not be blank');
    });

    it('toasts a refusal that names no field, such as a person who is no global administrator', async () => {
      const body: Problem = {
        type: 'about:blank',
        title: 'Forbidden',
        status: 403,
        detail: 'Only a global administrator creates a tenant.',
        code: 'forbidden',
      };
      create.mockRejectedValue(
        new HttpErrorResponse({ status: 403, statusText: 'Forbidden', error: body }),
      );
      const add = vi.spyOn(TestBed.inject(MessageService), 'add');
      const fixture = await render();
      fill(fixture);

      submit(fixture);
      await settle(fixture);

      expect(add).toHaveBeenCalledExactlyOnceWith(
        expect.objectContaining({
          severity: 'warn',
          summary: 'Forbidden',
          detail: 'Only a global administrator creates a tenant.',
        }),
      );
      expect(host(fixture).querySelector('small.error')).toBeNull();
      expect(navigate).not.toHaveBeenCalled();
    });

    it('shows no old problem when the next attempt is made, and goes into the tenant when it works', async () => {
      create.mockRejectedValueOnce(
        refusal(409, 'tenant_slug_taken', [{ pointer: '/slug', message: 'is taken' }]),
      );
      const fixture = await render();
      fill(fixture);
      submit(fixture);
      await settle(fixture);
      expect(el(fixture, 'first-tenant-slug-error')).not.toBeNull();

      typeInto(fixture, 'first-tenant-slug', 'acme-2');
      submit(fixture);
      await settle(fixture);

      expect(create).toHaveBeenCalledTimes(2);
      expect(create.mock.calls[1][0].slug).toBe('acme-2');
      expect(el(fixture, 'first-tenant-slug-error')).toBeNull();
      expect(navigate).toHaveBeenCalledOnce();
    });
  });

  describe('the fields', () => {
    it('names each field by a label of its own, whose hint and error are not part of the name', async () => {
      const fixture = await render();

      const label = (text: string) =>
        [...host(fixture).querySelectorAll('label')].find((each) => each.textContent === text);
      expect((label('Slug') as HTMLLabelElement).htmlFor).toBe('first-tenant-slug-input');
      expect((label('Name') as HTMLLabelElement).htmlFor).toBe('first-tenant-name-input');
      expect((el(fixture, 'first-tenant-slug') as HTMLInputElement).id).toBe(
        'first-tenant-slug-input',
      );
      expect((el(fixture, 'first-tenant-name') as HTMLInputElement).id).toBe(
        'first-tenant-name-input',
      );
    });
  });

  describe('for assistive technology', () => {
    const refused = async () => {
      create.mockRejectedValue(
        refusal(409, 'tenant_slug_taken', [
          { pointer: '/slug', message: 'is taken' },
          { pointer: '/name', message: 'must not be blank' },
        ]),
      );
      const fixture = await render();
      fill(fixture);
      submit(fixture);
      await settle(fixture);
      return fixture;
    };

    it('marks each refused field as invalid, with the text that says why, which is an alert', async () => {
      const fixture = await refused();

      for (const [field, error] of [
        ['first-tenant-slug', 'first-tenant-slug-error'],
        ['first-tenant-name', 'first-tenant-name-error'],
      ]) {
        const input = el(fixture, field) as HTMLInputElement;
        expect(input.getAttribute('aria-invalid'), field).toBe('true');
        expect(input.getAttribute('aria-describedby')?.split(' '), field).toContain(error);
        expect(el(fixture, error)?.id, error).toBe(error);
        expect(el(fixture, error)?.getAttribute('role'), error).toBe('alert');
      }
    });

    it('keeps the hint of the slug in the description beside the error, and claims no validity while an error shows', async () => {
      const fixture = await refused();

      const input = el(fixture, 'first-tenant-slug') as HTMLInputElement;
      expect(input.getAttribute('aria-describedby')).toBe(
        'first-tenant-slug-hint first-tenant-slug-error',
      );
      expect(input.getAttribute('aria-invalid')).not.toBe('false');
      for (const id of input.getAttribute('aria-describedby')?.split(' ') ?? []) {
        expect(host(fixture).querySelector(`#${id}`), id).not.toBeNull();
      }
    });

    it('takes it away again with the next attempt, and leaves no field claiming to be invalid', async () => {
      const fixture = await refused();
      let finish: (tenant: Tenant) => void = () => undefined;
      create.mockReturnValue(new Promise<Tenant>((resolve) => (finish = resolve)));

      submit(fixture);
      await settle(fixture);

      for (const field of ['first-tenant-slug', 'first-tenant-name']) {
        const input = el(fixture, field) as HTMLInputElement;
        expect(input.getAttribute('aria-invalid'), field).toBe('false');
        expect(input.getAttribute('aria-describedby') ?? '', field).not.toContain('-error');
      }
      finish(made);
      await settle(fixture);
    });
  });

  describe('the key of the act (docs/adr/0045)', () => {
    const keyOf = (call: number) => create.mock.calls[call][1];
    const lost = () => new HttpErrorResponse({ status: 0, statusText: 'Unknown Error' });

    it('is a UUID, and is the same for a retry of the same slug and name after a network failure', async () => {
      create.mockRejectedValueOnce(lost());
      const fixture = await render();
      fill(fixture);

      submit(fixture);
      await settle(fixture);
      submit(fixture);
      await settle(fixture);

      expect(create).toHaveBeenCalledTimes(2);
      expect(keyOf(0)).toMatch(uuid);
      expect(keyOf(1)).toBe(keyOf(0));
      expect(create.mock.calls[1][0]).toEqual(create.mock.calls[0][0]);
    });

    it('lets the retry of a lost answer go into the tenant, which the server answers again', async () => {
      create.mockRejectedValueOnce(lost());
      const fixture = await render();
      fill(fixture);
      submit(fixture);
      await settle(fixture);
      expect(navigate).not.toHaveBeenCalled();

      submit(fixture);
      await settle(fixture);

      expect(navigate).toHaveBeenCalledExactlyOnceWith(['/t', 'acme']);
    });

    it.each([
      ['the slug', 'first-tenant-slug', 'acme-2'],
      ['the name', 'first-tenant-name', 'Acme Corporation'],
    ])(
      'is another one as soon as %s is changed, because the body is another',
      async (_what, field, value) => {
        create.mockRejectedValueOnce(lost());
        const fixture = await render();
        fill(fixture);
        submit(fixture);
        await settle(fixture);

        typeInto(fixture, field, value);
        submit(fixture);
        await settle(fixture);

        expect(create).toHaveBeenCalledTimes(2);
        expect(keyOf(1)).not.toBe(keyOf(0));
      },
    );

    it('does not change while the person only looks at the form', async () => {
      create.mockRejectedValueOnce(lost());
      const fixture = await render();
      fill(fixture);
      submit(fixture);
      await settle(fixture);
      await settle(fixture);

      submit(fixture);
      await settle(fixture);

      expect(keyOf(1)).toBe(keyOf(0));
    });
  });
});
