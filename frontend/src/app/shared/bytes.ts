/** A count of bytes as people read it, in the binary units the configuration takes: `1.5 MiB`. */
export function byteSize(bytes: number): string {
  const units = ['bytes', 'KiB', 'MiB', 'GiB', 'TiB'];
  let value = bytes;
  let unit = 0;
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024;
    unit++;
  }
  if (unit === 0) {
    return `${value} ${value === 1 ? 'byte' : 'bytes'}`;
  }
  return `${value.toFixed(value < 10 ? 1 : 0)} ${units[unit]}`;
}
