import { Injector, resource, ResourceRef, signal } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { refresh } from './refresh';

describe('refresh', () => {
  let injector: Injector;

  beforeEach(() => {
    injector = TestBed.inject(Injector);
  });

  /** A resource whose loads end when the test says so; `loads` has one entry per load started. */
  function controlled(params: () => string | undefined = () => 'x') {
    const loads: { resolve: (value: string) => void; reject: (error: unknown) => void }[] = [];
    const ref: ResourceRef<string | undefined> = TestBed.runInInjectionContext(() =>
      resource({
        params,
        loader: () =>
          new Promise<string>((resolve, reject) => {
            loads.push({ resolve, reject });
          }),
      }),
    );
    return { ref, loads };
  }

  /** Lets the answer of a load reach the resource and the effects that watch it run. */
  async function settle() {
    await new Promise((resolve) => setTimeout(resolve));
    TestBed.tick();
  }

  /** A resource that has loaded once. */
  async function resolved() {
    const loaded = controlled();
    TestBed.tick();
    loaded.loads[0].resolve('first');
    await settle();
    expect(loaded.ref.status()).toBe('resolved');
    return loaded;
  }

  describe('a resource that is not loading', () => {
    it('reloads at once', async () => {
      const { ref, loads } = await resolved();

      refresh(ref, injector);
      TestBed.tick();

      expect(loads).toHaveLength(2);
      expect(ref.status()).toBe('reloading');
    });

    it('reloads at once after a failed load', async () => {
      const { ref, loads } = controlled();
      TestBed.tick();
      loads[0].reject(new Error('the backend is down'));
      await settle();
      expect(ref.status()).toBe('error');

      refresh(ref, injector);
      TestBed.tick();

      expect(loads).toHaveLength(2);
    });

    it('does not wait for anything afterwards', async () => {
      const { ref, loads } = await resolved();
      refresh(ref, injector);
      TestBed.tick();
      loads[1].resolve('second');
      await settle();

      expect(ref.value()).toBe('second');
      expect(loads).toHaveLength(2);
    });
  });

  describe('a resource that is loading', () => {
    it('is not restarted, and loads once more when the load ends', async () => {
      const { ref, loads } = controlled();
      TestBed.tick();

      refresh(ref, injector);
      TestBed.tick();
      expect(loads).toHaveLength(1);
      loads[0].resolve('first');
      await settle();

      expect(loads).toHaveLength(2);
      loads[1].resolve('second');
      await settle();
      expect(ref.value()).toBe('second');
    });

    it('shows the first answer while it loads once more', async () => {
      const { ref, loads } = controlled();
      TestBed.tick();
      refresh(ref, injector);

      loads[0].resolve('first');
      await settle();

      expect(ref.status()).toBe('reloading');
      expect(ref.value()).toBe('first');
    });

    it('loads once more after a reload that is in flight as well', async () => {
      const { ref, loads } = await resolved();
      ref.reload();
      TestBed.tick();
      expect(loads).toHaveLength(2);

      refresh(ref, injector);
      TestBed.tick();
      expect(loads).toHaveLength(2);
      loads[1].resolve('second');
      await settle();

      expect(loads).toHaveLength(3);
    });

    it('loads once more only once, however often it is refreshed meanwhile', async () => {
      const { ref, loads } = controlled();
      TestBed.tick();

      refresh(ref, injector);
      refresh(ref, injector);
      refresh(ref, injector);
      TestBed.tick();
      loads[0].resolve('first');
      await settle();
      loads[1].resolve('second');
      await settle();

      expect(loads).toHaveLength(2);
    });

    it('waits again when it is refreshed during the load it waited for', async () => {
      const { ref, loads } = controlled();
      TestBed.tick();
      refresh(ref, injector);
      loads[0].resolve('first');
      await settle();
      expect(loads).toHaveLength(2);

      refresh(ref, injector);
      loads[1].resolve('second');
      await settle();

      expect(loads).toHaveLength(3);
      loads[2].resolve('third');
      await settle();
      expect(ref.value()).toBe('third');
      expect(loads).toHaveLength(3);
    });

    it('loads once more also when the load fails', async () => {
      const { ref, loads } = controlled();
      TestBed.tick();
      refresh(ref, injector);

      loads[0].reject(new Error('the backend is down'));
      await settle();

      expect(loads).toHaveLength(2);
      loads[1].resolve('second');
      await settle();
      expect(ref.value()).toBe('second');
      expect(ref.error()).toBeUndefined();
    });

    it('is not reloaded when it is destroyed while it waits', async () => {
      const { ref, loads } = controlled();
      TestBed.tick();
      refresh(ref, injector);

      ref.destroy();
      await settle();

      expect(loads).toHaveLength(1);
      expect(ref.status()).toBe('idle');
    });

    it('waits independently of the other resources', async () => {
      const first = controlled();
      const second = controlled();
      TestBed.tick();

      refresh(first.ref, injector);
      first.loads[0].resolve('a');
      second.loads[0].resolve('b');
      await settle();

      expect(first.loads).toHaveLength(2);
      expect(second.loads).toHaveLength(1);
    });
  });

  describe('a resource without parameters', () => {
    it('has nothing to reload, now or later', async () => {
      const params = signal<string | undefined>(undefined);
      const { ref, loads } = controlled(() => params());
      TestBed.tick();
      expect(ref.status()).toBe('idle');

      refresh(ref, injector);
      TestBed.tick();
      expect(loads).toHaveLength(0);
      params.set('x');
      TestBed.tick();
      expect(loads).toHaveLength(1);
      loads[0].resolve('first');
      await settle();

      expect(loads).toHaveLength(1);
    });

    it('leaves no reload waiting for the load that starts right after', async () => {
      const params = signal<string | undefined>(undefined);
      const { ref, loads } = controlled(() => params());
      TestBed.tick();

      refresh(ref, injector);
      params.set('x');
      TestBed.tick();
      loads[0].resolve('first');
      await settle();

      expect(loads).toHaveLength(1);
      expect(ref.value()).toBe('first');
    });
  });
});
