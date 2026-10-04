import { Component, signal, WritableSignal } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { AuthOptions } from '../../api/models';
import { AuthService } from '../../core/auth.service';
import {
  generatedLength,
  generatePassword,
  passwordAlphabet,
  TemporaryPassword,
} from './temporary-password';

describe('passwordAlphabet', () => {
  it('has no character that is read for another', () => {
    for (const ambiguous of ['0', 'O', '1', 'l', 'I']) {
      expect(passwordAlphabet).not.toContain(ambiguous);
    }
  });

  it('has each of its characters once, and only letters and digits', () => {
    expect(new Set(passwordAlphabet).size).toBe(passwordAlphabet.length);
    expect(passwordAlphabet).toMatch(/^[A-Za-z0-9]+$/);
  });
});

describe('generatePassword', () => {
  /** Makes the source of randomness answer with these bytes, a call after the other. */
  function bytes(...batches: number[][]) {
    const answers = [...batches];
    return vi.spyOn(crypto, 'getRandomValues').mockImplementation(((array: Uint8Array) => {
      const batch = answers.shift() ?? [];
      array.set(batch.slice(0, array.length));
      return array;
    }) as typeof crypto.getRandomValues);
  }

  afterEach(() => vi.restoreAllMocks());

  it('makes twenty-four characters, all of them from the alphabet', () => {
    const password = generatePassword();

    expect(generatedLength).toBe(24);
    expect(password).toHaveLength(24);
    expect([...password].every((character) => passwordAlphabet.includes(character))).toBe(true);
  });

  it('makes as many characters as it is asked for', () => {
    expect(generatePassword(24)).toHaveLength(24);
    expect(generatePassword(1)).toHaveLength(1);
  });

  it('does not make the same password twice', () => {
    const passwords = new Set(Array.from({ length: 50 }, () => generatePassword()));

    expect(passwords.size).toBe(50);
  });

  it('takes its randomness from crypto.getRandomValues', () => {
    const source = bytes([0, 1, 2, 3]);

    expect(generatePassword(4)).toBe('abcd');
    expect(source).toHaveBeenCalledOnce();
  });

  it('maps every byte below the limit to a character of the alphabet in turn', () => {
    // 256 = 4 * 55 + 36: bytes from 220 up are left out, so every character has four bytes.
    bytes([0, 54, 55, 109, 110, 219]);

    expect(generatePassword(6)).toBe(
      [0, 54, 0, 54, 0, 54].map((index) => passwordAlphabet[index]).join(''),
    );
  });

  it('leaves out the bytes that would make some characters likelier than others', () => {
    // Each call is asked for as many bytes as the password is long: here three.
    bytes([255, 220, 221], [219, 0, 250], [1, 254, 0]);

    // 255, 220, 221, 250 and 254 are not used: 219, then 0 and 1 are.
    expect(generatePassword(3)).toBe(
      [219 % passwordAlphabet.length, 0, 1].map((index) => passwordAlphabet[index]).join(''),
    );
  });

  it('asks again when a batch of bytes did not give enough characters', () => {
    const source = bytes([255, 255, 255, 255], [0, 1, 2, 3]);

    expect(generatePassword(4)).toBe('abcd');
    expect(source).toHaveBeenCalledTimes(2);
  });

  it('stops at the length when a batch gives more than is still needed', () => {
    bytes([255, 255, 0], [1, 2, 3]);

    expect(generatePassword(3)).toBe('abc');
  });
});

@Component({
  imports: [TemporaryPassword],
  template: `<app-temporary-password [(value)]="value" [error]="error()" [testId]="testId()" />`,
})
class Host {
  readonly value = signal('');
  readonly error = signal<string | undefined>(undefined);
  readonly testId = signal('temporary-password');
}

describe('TemporaryPassword', () => {
  let options: WritableSignal<AuthOptions | undefined>;

  beforeEach(() => {
    options = signal<AuthOptions | undefined>({
      local: true,
      oidc: false,
      oidc_name: null,
      password_min_length: 12,
    });
    TestBed.configureTestingModule({
      providers: [
        {
          provide: AuthService,
          useValue: {
            options: { hasValue: () => options() !== undefined, value: () => options() },
          },
        },
      ],
    });
  });

  async function render() {
    const fixture = TestBed.createComponent(Host);
    await settle(fixture);
    return fixture;
  }

  /** Lets what a click started finish, and shows it; fields take their model a moment after they appear. */
  async function settle(fixture: ComponentFixture<Host>) {
    await new Promise((resolve) => setTimeout(resolve));
    fixture.detectChanges();
    await fixture.whenStable();
  }

  const el = (fixture: ComponentFixture<Host>, testId: string) =>
    (fixture.nativeElement as HTMLElement).querySelector<HTMLElement>(`[data-testid="${testId}"]`);
  const input = (fixture: ComponentFixture<Host>, testId = 'temporary-password') =>
    el(fixture, testId) as HTMLInputElement;
  const generate = async (fixture: ComponentFixture<Host>, testId = 'temporary-password') => {
    el(fixture, `${testId}-generate`)?.click();
    await settle(fixture);
    return fixture.componentInstance.value();
  };

  function typeInto(fixture: ComponentFixture<Host>, value: string) {
    const field = input(fixture);
    field.value = value;
    field.dispatchEvent(new Event('input'));
    fixture.detectChanges();
  }

  describe('the field', () => {
    it('is a plain text field, never a password field, so that no password manager takes it for a credential', async () => {
      const fixture = await render();

      const field = input(fixture);
      expect(field.type).toBe('text');
      expect(field.getAttribute('type')).toBe('text');
      expect(field.hasAttribute('pinputpassword')).toBe(false);
      expect(field.classList.contains('p-inputpassword')).toBe(false);
      expect(field.querySelector('[class*="inputpassword"]')).toBeNull();
    });

    it('tells the browser and the common password managers to leave it alone', async () => {
      const fixture = await render();

      const field = input(fixture);
      expect(field.getAttribute('autocomplete')).toBe('off');
      expect(field.getAttribute('spellcheck')).toBe('false');
      expect(field.hasAttribute('data-1p-ignore')).toBe(true);
      expect(field.getAttribute('data-lpignore')).toBe('true');
      expect(field.hasAttribute('data-bwignore')).toBe(true);
      expect(field.getAttribute('data-form-type')).toBe('other');
    });

    it('has no button that reveals or hides it, because it is never hidden', async () => {
      const fixture = await render();

      expect(el(fixture, 'temporary-password-reveal')).toBeNull();
      expect((fixture.nativeElement as HTMLElement).querySelector('.reveal')).toBeNull();
      expect((fixture.nativeElement as HTMLElement).querySelectorAll('button')).toHaveLength(1);
    });

    it('is named by a label of its own, which a click on the name goes to', async () => {
      const fixture = await render();

      const label = (fixture.nativeElement as HTMLElement).querySelector(
        'label',
      ) as HTMLLabelElement;
      expect(label.textContent).toBe('Temporary password');
      expect(label.htmlFor).toBe('temporary-password-input');
      expect(input(fixture).id).toBe('temporary-password-input');
    });

    it('has a button that generates a password, which does not submit a form', async () => {
      const fixture = await render();

      const button = el(fixture, 'temporary-password-generate') as HTMLButtonElement;
      expect(button.textContent?.trim()).toBe('Generate');
      expect(button.type).toBe('button');
    });

    it('hands on what is typed, as it is', async () => {
      const fixture = await render();

      typeInto(fixture, '  Correct Horse  ');

      expect(fixture.componentInstance.value()).toBe('  Correct Horse  ');
    });

    it('shows what the host sets', async () => {
      const fixture = await render();

      fixture.componentInstance.value.set('from-the-host');
      await settle(fixture);

      expect(input(fixture).value).toBe('from-the-host');
    });
  });

  describe('generating', () => {
    it('fills the field with a generated password of 24 characters from the alphabet, in plain sight', async () => {
      const fixture = await render();

      const password = await generate(fixture);

      expect(password).toHaveLength(generatedLength);
      expect([...password].every((character) => passwordAlphabet.includes(character))).toBe(true);
      expect(input(fixture).value).toBe(password);
      expect(input(fixture).type).toBe('text');
    });

    it('replaces what was typed, and makes another each time', async () => {
      const fixture = await render();
      typeInto(fixture, 'typed by hand');

      const first = await generate(fixture);
      const second = await generate(fixture);

      expect(first).not.toBe('typed by hand');
      expect(second).not.toBe(first);
    });

    it('makes a password as long as an installation asks, when its minimum is longer than 24', async () => {
      options.set({ local: true, oidc: false, oidc_name: null, password_min_length: 30 });
      const fixture = await render();

      const password = await generate(fixture);

      expect(password).toHaveLength(30);
      expect([...password].every((character) => passwordAlphabet.includes(character))).toBe(true);
    });

    it('follows the minimum when it changes after the field was shown', async () => {
      const fixture = await render();
      expect(await generate(fixture)).toHaveLength(24);

      options.set({ local: true, oidc: false, oidc_name: null, password_min_length: 40 });

      expect(await generate(fixture)).toHaveLength(40);
    });

    it.each([8, 12, 16, 24])(
      'keeps to 24 characters when the minimum is %i, which is no longer',
      async (minimum) => {
        options.set({ local: true, oidc: false, oidc_name: null, password_min_length: minimum });
        const fixture = await render();

        expect(await generate(fixture)).toHaveLength(24);
      },
    );

    it('makes 24 characters while the options of the installation are not known', async () => {
      options.set(undefined);
      const fixture = await render();

      expect(await generate(fixture)).toHaveLength(24);
    });
  });

  describe('what the server said about it', () => {
    const message = (fixture: ComponentFixture<Host>, testId = 'temporary-password-error') =>
      el(fixture, testId);

    it('shows the message beside the field as an alert, and nothing when there is none', async () => {
      const fixture = await render();
      expect(message(fixture)).toBeNull();

      fixture.componentInstance.error.set('must be at least 12 characters');
      await settle(fixture);
      expect(message(fixture)?.textContent).toBe('must be at least 12 characters');
      expect(message(fixture)?.getAttribute('role')).toBe('alert');

      fixture.componentInstance.error.set(undefined);
      await settle(fixture);
      expect(message(fixture)).toBeNull();
    });

    it('tells assistive technology that the field is invalid and which text says why, only while there is a message', async () => {
      const fixture = await render();
      expect(input(fixture).getAttribute('aria-invalid')).toBe('false');
      expect(input(fixture).hasAttribute('aria-describedby')).toBe(false);

      fixture.componentInstance.error.set('must be at least 12 characters');
      await settle(fixture);
      expect(input(fixture).getAttribute('aria-invalid')).toBe('true');
      expect(input(fixture).getAttribute('aria-describedby')).toBe('temporary-password-error');
      expect(message(fixture)?.id).toBe('temporary-password-error');

      fixture.componentInstance.error.set(undefined);
      await settle(fixture);
      expect(input(fixture).getAttribute('aria-invalid')).toBe('false');
      expect(input(fixture).hasAttribute('aria-describedby')).toBe(false);
    });
  });

  describe('the test id', () => {
    it('names the input and, with a suffix, its generate button, its error and its label target', async () => {
      const fixture = await render();
      fixture.componentInstance.testId.set('account-password');
      fixture.componentInstance.error.set('refused');
      await settle(fixture);

      expect(input(fixture, 'account-password')).not.toBeNull();
      expect(el(fixture, 'account-password-generate')).not.toBeNull();
      expect(el(fixture, 'account-password-error')?.id).toBe('account-password-error');
      expect(input(fixture, 'account-password').id).toBe('account-password-input');
      expect(input(fixture, 'account-password').getAttribute('aria-describedby')).toBe(
        'account-password-error',
      );
      expect(el(fixture, 'temporary-password')).toBeNull();
      expect(el(fixture, 'temporary-password-error')).toBeNull();
    });
  });
});
