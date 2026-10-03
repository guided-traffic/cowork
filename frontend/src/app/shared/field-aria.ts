import type { InputNumberPassThrough } from 'primeng/types/inputnumber';
import type { SelectPassThrough } from 'primeng/types/select';

/**
 * The value of `aria-describedby` for the ids that are there — a hint that is always shown, an
 * error that is shown only while the server has refused the field — or `null` for none, which
 * leaves the attribute off. An id that names no element would point at nothing.
 */
export function describedBy(...ids: (string | false | null | undefined)[]): string | null {
  const present = ids.filter((id): id is string => !!id);
  return present.length > 0 ? present.join(' ') : null;
}

/**
 * What a PrimeNG 22 select leaves out of the element a person tabs to: it renders neither
 * `aria-invalid` nor `aria-describedby` on its combobox. The pass-through puts them there, so a
 * screen reader that reaches the field is told that it was refused and why.
 */
export function selectAria(invalid: boolean, describing: string | null): SelectPassThrough {
  return { label: { 'aria-invalid': invalid ? 'true' : null, 'aria-describedby': describing } };
}

/**
 * A PrimeNG 22 number field takes `ariaDescribedBy` but renders no `aria-invalid`; the
 * pass-through puts it on the input.
 */
export function numberAria(invalid: boolean): InputNumberPassThrough {
  return { pcInputText: { root: { 'aria-invalid': invalid ? 'true' : null } } };
}
