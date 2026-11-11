export const builtinBase = '/__opsd/builtin';
export const inBuiltinConsole = window.location.pathname === builtinBase || window.location.pathname.startsWith(`${builtinBase}/`);

export function loginAddress(address: string) {
  return inBuiltinConsole || new URLSearchParams(window.location.search).get('builtin') === '1' ? `${address}?builtin=1` : address;
}
