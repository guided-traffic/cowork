import { Component, signal } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { By } from '@angular/platform-browser';
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

  it('makes sixteen characters, all of them from the alphabet', () => {
    const password = generatePassword();

    expect(generatedLength).toBe(16);
    expect(password).toHaveLength(16);
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

  function typeInto(fixture: ComponentFixture<Host>, value: string) {
    const field = input(fixture);
    field.value = value;
    field.dispatchEvent(new Event('input'));
    fixture.detectChanges();
  }

  it('asks for a temporary password, hidden, with a button that generates one', async () => {
    const fixture = await render();

    const host = fixture.nativeElement as HTMLElement;
    expect(host.querySelector('.label')?.textContent).toBe('Temporary password');
    expect(input(fixture).getAttribute('aria-label')).toBe('Temporary password');
    expect(input(fixture).type).toBe('password');
    expect(input(fixture).getAttribute('autocomplete')).toBe('new-password');
    expect(el(fixture, 'temporary-password-generate')?.textContent?.trim()).toBe('Generate');
    expect(el(fixture, 'temporary-password-error')).toBeNull();
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

  describe('showing the password', () => {
    it('is turned on and off with the eye, and says what it will do', async () => {
      const fixture = await render();
      const reveal = el(fixture, 'temporary-password-reveal') as HTMLButtonElement;
      expect(reveal.getAttribute('aria-label')).toBe('Show the password');
      expect(reveal.querySelector('i')?.classList).toContain('pi-eye');

      reveal.click();
      await settle(fixture);
      expect(input(fixture).type).toBe('text');
      expect(reveal.getAttribute('aria-label')).toBe('Hide the password');
      expect(reveal.querySelector('i')?.classList).toContain('pi-eye-slash');

      reveal.click();
      await settle(fixture);
      expect(input(fixture).type).toBe('password');
      expect(reveal.getAttribute('aria-label')).toBe('Show the password');
    });

    it('follows the field when the field changes its own mask', async () => {
      const fixture = await render();

      fixture.debugElement.query(By.css('input')).triggerEventHandler('maskChange', false);
      await settle(fixture);
      expect(input(fixture).type).toBe('text');
      expect(
        (el(fixture, 'temporary-password-reveal') as HTMLButtonElement).getAttribute('aria-label'),
      ).toBe('Hide the password');

      fixture.debugElement.query(By.css('input')).triggerEventHandler('maskChange', true);
      await settle(fixture);
      expect(input(fixture).type).toBe('password');
    });

    it('is not a button that submits a form', async () => {
      const fixture = await render();

      expect((el(fixture, 'temporary-password-reveal') as HTMLButtonElement).type).toBe('button');
      expect((el(fixture, 'temporary-password-generate') as HTMLButtonElement).type).toBe('button');
    });
  });

  describe('generating', () => {
    it('fills the field with a generated password and shows it, so that it can be read out', async () => {
      const fixture = await render();

      el(fixture, 'temporary-password-generate')?.click();
      await settle(fixture);

      const password = fixture.componentInstance.value();
      expect(password).toHaveLength(generatedLength);
      expect([...password].every((character) => passwordAlphabet.includes(character))).toBe(true);
      expect(input(fixture).value).toBe(password);
      expect(input(fixture).type).toBe('text');
    });

    it('replaces what was typed, and makes another each time', async () => {
      const fixture = await render();
      typeInto(fixture, 'typed by hand');

      el(fixture, 'temporary-password-generate')?.click();
      await settle(fixture);
      const first = fixture.componentInstance.value();
      el(fixture, 'temporary-password-generate')?.click();
      await settle(fixture);

      expect(first).not.toBe('typed by hand');
      expect(fixture.componentInstance.value()).not.toBe(first);
    });
  });

  describe('what the server said about it', () => {
    it('shows the message beside the field, and nothing when there is none', async () => {
      const fixture = await render();

      fixture.componentInstance.error.set('must be at least 12 characters');
      await settle(fixture);
      expect(el(fixture, 'temporary-password-error')?.textContent).toBe(
        'must be at least 12 characters',
      );

      fixture.componentInstance.error.set(undefined);
      await settle(fixture);
      expect(el(fixture, 'temporary-password-error')).toBeNull();
    });
  });

  describe('the test id', () => {
    it('names the input and, with a suffix, its two buttons', async () => {
      const fixture = await render();
      fixture.componentInstance.testId.set('account-password');
      await settle(fixture);

      expect(input(fixture, 'account-password')).not.toBeNull();
      expect(el(fixture, 'account-password-reveal')).not.toBeNull();
      expect(el(fixture, 'account-password-generate')).not.toBeNull();
      expect(el(fixture, 'temporary-password')).toBeNull();
    });
  });
});
