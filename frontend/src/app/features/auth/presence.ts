/** The input that says a person is at the page: a pointer moved or pressed, a key, wheel, touch. */
export const presenceInputs = [
  'pointerdown',
  'pointermove',
  'keydown',
  'wheel',
  'touchstart',
] as const;

/**
 * Calls `then` once, at the first sign that a person is at the page (docs/adr/0029 D6): an input of
 * {@link presenceInputs}, the window taking the focus, or the document becoming visible after it
 * was hidden. An open tab nobody looks at gives none, so the login page waiting on this does not sign
 * itself in, and show what the person works on, to an empty room.
 *
 * A pointer move counts once the pointer stands somewhere else than at the move before: a browser
 * may send a move of its own after a layout or a scroll under a pointer that rests, to update what
 * the pointer is over, and no person did that. A move that names no place counts at once. The
 * listeners are passive and do nothing but this. Returns what stops the waiting; the listeners go
 * when it is called or the sign came, whichever is first.
 */
export function whenPresent(document: Document, then: () => void): () => void {
  const view = document.defaultView;
  let waiting = true;
  let pointer: { x: number; y: number } | undefined;
  /** Whether the input is no pointer move, or one to another place than the move before. */
  const moved = (event: Event) => {
    const { clientX: x, clientY: y } = event as Partial<Pick<PointerEvent, 'clientX' | 'clientY'>>;
    if (event.type !== 'pointermove' || x === undefined || y === undefined) {
      return true;
    }
    const before = pointer;
    pointer = { x, y };
    return before !== undefined && (before.x !== x || before.y !== y);
  };
  const stop = () => {
    waiting = false;
    for (const name of presenceInputs) {
      document.removeEventListener(name, sign, true);
    }
    view?.removeEventListener('focus', sign);
    document.removeEventListener('visibilitychange', shown);
  };
  const sign = (event: Event) => {
    if (waiting && moved(event)) {
      stop();
      then();
    }
  };
  const shown = (event: Event) => {
    if (document.visibilityState === 'visible') {
      sign(event);
    }
  };
  for (const name of presenceInputs) {
    document.addEventListener(name, sign, { capture: true, passive: true });
  }
  view?.addEventListener('focus', sign);
  document.addEventListener('visibilitychange', shown);
  return stop;
}
