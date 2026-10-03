import { computed } from '@angular/core';
import { EntityCache, etagOf } from './entity-cache';

interface Item {
  version: number;
  label: string;
}

const item = (version: number, label = `v${version}`): Item => ({ version, label });

describe('etagOf', () => {
  it('quotes the version, as the strong ETag of docs/adr/0050 D2', () => {
    expect(etagOf(7)).toBe('"7"');
  });
});

describe('EntityCache', () => {
  let clock: number;
  let cache: EntityCache<Item>;

  beforeEach(() => {
    clock = 1000;
    cache = new EntityCache<Item>(() => clock);
  });

  afterEach(() => vi.useRealTimers());

  describe('an id that was never stored', () => {
    it('has no value, no ETag and an empty entry', () => {
      expect(cache.value('a')).toBeUndefined();
      expect(cache.etag('a')).toBeUndefined();
      expect(cache.entry('a')()).toBeUndefined();
    });

    it('is not listed by ids(), even once a view has asked for its entry', () => {
      cache.entry('a');

      expect(cache.ids()).toEqual([]);
    });
  });

  describe('put', () => {
    it('stores the value with the quoted version as its ETag and the time it was loaded', () => {
      const stored = item(3);

      expect(cache.put('a', stored)).toBe(true);

      expect(cache.value('a')).toBe(stored);
      expect(cache.etag('a')).toBe('"3"');
      expect(cache.entry('a')()).toEqual({ value: stored, etag: '"3"', loadedAt: 1000 });
    });

    it('keeps an ETag it is given instead of deriving one', () => {
      cache.put('a', item(3), '"custom"');

      expect(cache.etag('a')).toBe('"custom"');
    });

    it('replaces the value with a newer version', () => {
      cache.put('a', item(3));
      clock = 2000;

      expect(cache.put('a', item(4))).toBe(true);

      expect(cache.value('a')?.version).toBe(4);
      expect(cache.etag('a')).toBe('"4"');
      expect(cache.entry('a')()?.loadedAt).toBe(2000);
    });

    it('refuses a value older than the one held, so a slow list answer cannot undo a refetch', () => {
      const newer = item(5);
      cache.put('a', newer);
      clock = 2000;

      expect(cache.put('a', item(4))).toBe(false);

      expect(cache.value('a')).toBe(newer);
      expect(cache.etag('a')).toBe('"5"');
      expect(cache.entry('a')()?.loadedAt).toBe(1000);
    });

    it('replaces the value with an equal version, because derived fields change without a new version', () => {
      cache.put('a', item(5, 'before'));
      clock = 2000;
      const after = item(5, 'after');

      expect(cache.put('a', after)).toBe(true);

      expect(cache.value('a')).toBe(after);
      expect(cache.entry('a')()?.loadedAt).toBe(2000);
    });

    it('keeps the entities of different ids apart', () => {
      cache.put('a', item(1, 'first'));
      cache.put('b', item(9, 'second'));

      expect(cache.value('a')?.label).toBe('first');
      expect(cache.value('b')?.label).toBe('second');
    });

    it('stamps the entry with the current time by default', () => {
      vi.useFakeTimers();
      vi.setSystemTime(new Date('2026-10-03T10:00:00Z'));
      const real = new EntityCache<Item>();

      real.put('a', item(1));

      expect(real.entry('a')()?.loadedAt).toBe(Date.parse('2026-10-03T10:00:00Z'));
    });
  });

  describe('entry', () => {
    it('is a read-only signal', () => {
      expect('set' in cache.entry('a')).toBe(false);
    });

    it('is the same signal for an id every time it is asked for', () => {
      expect(cache.entry('a')).toBe(cache.entry('a'));
    });

    it('shows later puts and deletes to a view that read it earlier', () => {
      const label = computed(() => cache.entry('a')()?.value.label ?? 'none');
      expect(label()).toBe('none');

      cache.put('a', item(1, 'one'));
      expect(label()).toBe('one');

      cache.put('a', item(2, 'two'));
      expect(label()).toBe('two');

      cache.delete('a');
      expect(label()).toBe('none');
    });
  });

  describe('delete', () => {
    it('empties the entry so that views see it go', () => {
      cache.put('a', item(5));

      cache.delete('a');

      expect(cache.value('a')).toBeUndefined();
      expect(cache.etag('a')).toBeUndefined();
      expect(cache.entry('a')()).toBeUndefined();
      expect(cache.ids()).toEqual([]);
    });

    it('does nothing for an id that is not there', () => {
      expect(() => cache.delete('missing')).not.toThrow();
      expect(cache.ids()).toEqual([]);
    });

    it('lets an older version in again, because nothing is held to be older than', () => {
      cache.put('a', item(5));
      cache.delete('a');

      expect(cache.put('a', item(2))).toBe(true);
      expect(cache.value('a')?.version).toBe(2);
    });
  });

  describe('clear', () => {
    it('empties every entry', () => {
      cache.put('a', item(1));
      cache.put('b', item(2));

      cache.clear();

      expect(cache.value('a')).toBeUndefined();
      expect(cache.value('b')).toBeUndefined();
      expect(cache.ids()).toEqual([]);
    });

    it('keeps the slots, so an entry read before shows what is put afterwards', () => {
      const before = cache.entry('a');
      cache.put('a', item(1));
      cache.clear();
      expect(before()).toBeUndefined();

      cache.put('a', item(1, 'again'));

      expect(before()?.value.label).toBe('again');
    });

    it('does not remember the versions it dropped', () => {
      cache.put('a', item(9));
      cache.clear();

      expect(cache.put('a', item(1))).toBe(true);
    });

    it('does nothing to an empty cache', () => {
      expect(() => cache.clear()).not.toThrow();
    });
  });

  describe('ids', () => {
    it('lists the ids that hold a value, in the order they were first stored', () => {
      cache.put('b', item(1));
      cache.put('a', item(1));
      cache.put('c', item(1));
      cache.delete('a');

      expect(cache.ids()).toEqual(['b', 'c']);
    });
  });
});
