export const builtinBase = '/__opsd/builtin';
export const inBuiltinConsole = window.location.pathname === builtinBase || window.location.pathname.startsWith(`${builtinBase}/`);

export function loginAddress(address: string) {
  const url = new URL(address, window.location.origin);
  if (inBuiltinConsole || new URLSearchParams(window.location.search).get('builtin') === '1') url.searchParams.set('builtin', '1');
  return url.pathname + url.search + url.hash;
}

export function loginDestination() {
  return new URLSearchParams(window.location.search).get('builtin') === '1' ? `${builtinBase}/` : '/';
}

export function isConsolePage(path: string) {
  return path === '/' || /^\/(nodes|containers|databases|storage|firewall|tools|settings|terminal|__opsd)(\/|$)/.test(path);
}
