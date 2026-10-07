import { byteSize } from './bytes';

describe('byteSize', () => {
  it('names a count of bytes in binary units', () => {
    expect(byteSize(0)).toBe('0 bytes');
    expect(byteSize(1)).toBe('1 byte');
    expect(byteSize(1023)).toBe('1023 bytes');
    expect(byteSize(1536)).toBe('1.5 KiB');
    expect(byteSize(70 * 1024 * 1024)).toBe('70 MiB');
    expect(byteSize(10 * 1024 ** 3)).toBe('10 GiB');
  });
});
