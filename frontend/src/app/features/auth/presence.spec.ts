import type { Mock } from 'vitest';
import { presenceInputs, whenPresent } from './presence';

function setVisibility(state: DocumentVisibilityState): void {
  Object.defineProperty(document, 'visibilityState', { configurable: true, get: () => state });
  document.dispatchEvent(new Event('visibilitychange'));
}

describe('whenPresent (docs/adr/0029 D6)', () => {
  let then: Mock<() => void>;
  let stop: (() => void) | undefined;

  beforeEach(() => {
    then = vi.fn<() => void>();
  });

  afterEach(() => {
    stop?.();
    stop = undefined;
    Reflect.deleteProperty(document, 'visibilityState');
    vi.restoreAllMocks();
  });

  it('takes a pointer moved or pressed, a key, the wheel and a touch for a person', () => {
    expect(presenceInputs).toEqual([
      'pointerdown',
      'pointermove',
      'keydown',
      'wheel',
      'touchstart',
    ]);
  });

  it('waits while nobody gives a sign', () => {
    stop = whenPresent(document, then);

    expect(then).not.toHaveBeenCalled();
  });

  it.each(presenceInputs)('takes %s on the page for a sign', (name) => {
    stop = whenPresent(document, then);

    document.body.dispatchEvent(new Event(name, { bubbles: true }));

    expect(then).toHaveBeenCalledOnce();
  });

  describe('a pointer that rests', () => {
    /** A pointer move at a place of the window, as a browser sends it. */
    const moveAt = (x: number, y: number) =>
      document.dispatchEvent(Object.assign(new Event('pointermove'), { clientX: x, clientY: y }));

    it('gives no sign by the moves a browser sends where it rests, after a layout or a scroll', () => {
      stop = whenPresent(document, then);

      moveAt(120, 80);
      moveAt(120, 80);
      moveAt(120, 80);

      expect(then).not.toHaveBeenCalled();
    });

    it('gives a sign once it stands somewhere else than at the move before', () => {
      stop = whenPresent(document, then);

      moveAt(120, 80);
      moveAt(121, 80);

      expect(then).toHaveBeenCalledOnce();
    });

    it('gives a sign when it is pressed where it rests', () => {
      stop = whenPresent(document, then);

      moveAt(120, 80);
      document.dispatchEvent(
        Object.assign(new Event('pointerdown'), { clientX: 120, clientY: 80 }),
      );

      expect(then).toHaveBeenCalledOnce();
    });
  });

  it('takes an input that the page stops on its way for a sign as well, since it listens first', () => {
    const swallow = (event: Event) => event.stopPropagation();
    document.body.addEventListener('keydown', swallow);
    stop = whenPresent(document, then);

    document.body.dispatchEvent(new Event('keydown', { bubbles: true }));

    expect(then).toHaveBeenCalledOnce();
    document.body.removeEventListener('keydown', swallow);
  });

  it('takes the window taking the focus for a sign', () => {
    stop = whenPresent(document, then);

    window.dispatchEvent(new Event('focus'));

    expect(then).toHaveBeenCalledOnce();
  });

  it('takes the document becoming visible for a sign, and its becoming hidden for none', () => {
    stop = whenPresent(document, then);

    setVisibility('hidden');
    expect(then).not.toHaveBeenCalled();
    setVisibility('visible');

    expect(then).toHaveBeenCalledOnce();
  });

  it('answers once, however many signs come', () => {
    stop = whenPresent(document, then);

    document.dispatchEvent(new Event('pointermove'));
    document.dispatchEvent(new Event('pointermove'));
    document.dispatchEvent(new Event('keydown'));
    window.dispatchEvent(new Event('focus'));

    expect(then).toHaveBeenCalledOnce();
  });

  it('stops listening once the sign came', () => {
    const removed = vi.spyOn(document, 'removeEventListener');
    stop = whenPresent(document, then);

    document.dispatchEvent(new Event('wheel'));

    for (const name of presenceInputs) {
      expect(removed).toHaveBeenCalledWith(name, expect.any(Function), true);
    }
    expect(removed).toHaveBeenCalledWith('visibilitychange', expect.any(Function));
  });

  it('answers nothing once it was stopped', () => {
    stop = whenPresent(document, then);

    stop();
    document.dispatchEvent(new Event('pointerdown'));
    window.dispatchEvent(new Event('focus'));
    setVisibility('visible');

    expect(then).not.toHaveBeenCalled();
  });

  it('listens passively, so that no scroll or touch waits for it', () => {
    const added = vi.spyOn(document, 'addEventListener');

    stop = whenPresent(document, then);

    for (const name of presenceInputs) {
      expect(added).toHaveBeenCalledWith(name, expect.any(Function), {
        capture: true,
        passive: true,
      });
    }
  });
});
