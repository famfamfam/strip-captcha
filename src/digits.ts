/**
 * Цифры других систем письма → ASCII, всё прочее выбрасывается. Арабская
 * раскладка вводит ٠-٩, персидская — ۰-۹, CJK-IME в полноширинном режиме —
 * ０-９: без замены такой ввод молча съедался бы фильтром, и человек не смог
 * бы набрать код вовсе.
 */
export function toAsciiDigits(value: string): string {
  return value
    .replace(/[٠-٩]/g, (ch) => String(ch.charCodeAt(0) - 0x0660))
    .replace(/[۰-۹]/g, (ch) => String(ch.charCodeAt(0) - 0x06f0))
    .replace(/[０-９]/g, (ch) => String(ch.charCodeAt(0) - 0xff10))
    .replace(/\D/g, '');
}
