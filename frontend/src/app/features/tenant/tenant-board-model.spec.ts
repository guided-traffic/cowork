import { Project } from '../../api/models';
import { chosenKeys, laneOf, lanesOf, refusingLane } from './tenant-board-model';

function project(key: string, fields: Partial<Project> = {}): Project {
  return {
    id: `id-${key}`,
    key,
    name: `Project ${key}`,
    description: '',
    restricted: false,
    wip_limits: {},
    version: 1,
    created_at: '2026-10-01T09:00:00Z',
    updated_at: '2026-10-01T09:00:00Z',
    ...fields,
  };
}

describe('the tenant board model (docs/adr/0018 D4)', () => {
  describe('chosenKeys, the filter of the address', () => {
    it('reads one project, or several, as the router hands a repeated parameter', () => {
      expect(chosenKeys('COW')).toEqual(['COW']);
      expect(chosenKeys(['COW', 'OPS'])).toEqual(['COW', 'OPS']);
    });

    it('reads no filter as none, and leaves out blanks and repeats', () => {
      expect(chosenKeys(undefined)).toEqual([]);
      expect(chosenKeys(null)).toEqual([]);
      expect(chosenKeys('')).toEqual([]);
      expect(chosenKeys(['COW', ' ', 'COW', ' OPS '])).toEqual(['COW', 'OPS']);
    });
  });

  describe('lanesOf, the swimlanes', () => {
    const cow = project('COW');
    const ops = project('OPS');
    const old = project('OLD', { archived_at: '2026-10-02T09:00:00Z' });

    it('has one for every project that is not archived, in the order of the list', () => {
      expect(lanesOf([cow, old, ops], []).map((each) => each.key)).toEqual(['COW', 'OPS']);
    });

    it('has one for each project the filter names, in the order of the list, not of the filter', () => {
      expect(lanesOf([cow, ops], ['OPS']).map((each) => each.key)).toEqual(['OPS']);
      expect(lanesOf([cow, ops], ['OPS', 'COW']).map((each) => each.key)).toEqual(['COW', 'OPS']);
    });

    it('has none for a project the filter names that the person does not see, or that is archived', () => {
      expect(lanesOf([cow, ops, old], ['GONE', 'OLD'])).toEqual([]);
    });
  });

  describe('laneOf, the swimlane under the pointer', () => {
    it('is the project of the nearest element that names one', () => {
      const lane = document.createElement('section');
      lane.setAttribute('data-lane', 'OPS');
      const inner = document.createElement('span');
      lane.append(inner);

      expect(laneOf(inner)).toBe('OPS');
      expect(laneOf(lane)).toBe('OPS');
    });

    it('is none outside every swimlane, and where there is no element', () => {
      expect(laneOf(document.createElement('div'))).toBeNull();
      expect(laneOf(null)).toBeNull();
      expect(laneOf(undefined)).toBeNull();
    });
  });

  describe('refusingLane, the swimlane that says no', () => {
    it('is any swimlane but the one of the card', () => {
      expect(refusingLane('COW', 'OPS')).toBe('OPS');
    });

    it('is none over the swimlane of the card, or over none', () => {
      expect(refusingLane('COW', 'COW')).toBeNull();
      expect(refusingLane('COW', null)).toBeNull();
    });
  });
});
