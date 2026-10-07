import { TestBed } from '@angular/core/testing';
import { signInAttemptKey, signInMethodKey, SignInMemory } from './sign-in-memory';

describe('SignInMemory (docs/adr/0029 D6)', () => {
  let memory: SignInMemory;

  beforeEach(() => {
    memory = TestBed.inject(SignInMemory);
  });

  afterEach(() => {
    vi.restoreAllMocks();
    localStorage.clear();
    sessionStorage.clear();
  });

  /** The browser refuses its storage, as a private window may: every access throws. */
  function refuseStorage() {
    for (const method of ['getItem', 'setItem', 'removeItem'] as const) {
      vi.spyOn(Storage.prototype, method).mockImplementation(() => {
        throw new DOMException('denied', 'SecurityError');
      });
    }
  }

  it('names the keys of docs/developer/frontend.md', () => {
    expect(signInMethodKey).toBe('cowork.sign-in');
    expect(signInAttemptKey).toBe('cowork.sign-in.attempt');
  });

  describe('how the person signs in', () => {
    it('knows nothing until the person chooses the identity provider', () => {
      expect(memory.provider()).toBe(false);
    });

    it("remembers the identity provider in the browser's storage, for every tab", () => {
      memory.rememberProvider();

      expect(localStorage.getItem('cowork.sign-in')).toBe('oidc');
      expect(memory.provider()).toBe(true);
    });

    it('forgets it again', () => {
      memory.rememberProvider();

      memory.forgetProvider();

      expect(localStorage.getItem('cowork.sign-in')).toBeNull();
      expect(memory.provider()).toBe(false);
    });

    it.each(['local', '', 'OIDC', 'true'])('takes %j for no provider', (value) => {
      localStorage.setItem('cowork.sign-in', value);

      expect(memory.provider()).toBe(false);
    });

    it('remembers nothing, and says no provider, where the browser refuses its storage', () => {
      refuseStorage();

      expect(() => memory.rememberProvider()).not.toThrow();
      expect(() => memory.forgetProvider()).not.toThrow();
      expect(memory.provider()).toBe(false);
    });
  });

  describe("this tab's own attempt", () => {
    it('has not tried until it notes an attempt', () => {
      expect(memory.tried()).toBe(false);
    });

    it("notes the attempt in the tab's session storage", () => {
      expect(memory.markTried()).toBe(true);

      expect(sessionStorage.getItem('cowork.sign-in.attempt')).not.toBeNull();
      expect(localStorage.getItem('cowork.sign-in.attempt')).toBeNull();
      expect(memory.tried()).toBe(true);
    });

    it('may try again once the attempt is cleared', () => {
      memory.markTried();

      memory.clearTried();

      expect(memory.tried()).toBe(false);
    });

    it('says it tried, and cannot note an attempt, where the browser refuses its storage', () => {
      refuseStorage();

      expect(memory.tried()).toBe(true);
      expect(memory.markTried()).toBe(false);
      expect(() => memory.clearTried()).not.toThrow();
    });

    it('cannot note an attempt where the storage is full', () => {
      vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
        throw new DOMException('quota', 'QuotaExceededError');
      });

      expect(memory.markTried()).toBe(false);
      expect(memory.tried()).toBe(false);
    });
  });
});
