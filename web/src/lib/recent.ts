// Orgs this browser has opened, newest first. There is no silent default org: the home page
// lists these and otherwise asks for an ENS name.
const KEY = "meshy.recentOrgs";

export function recentOrgs(): string[] {
  try {
    const a = JSON.parse(localStorage.getItem(KEY) ?? "[]");
    return Array.isArray(a) ? a.filter((x): x is string => typeof x === "string").slice(0, 8) : [];
  } catch {
    return [];
  }
}

export function rememberOrg(org: string) {
  try {
    localStorage.setItem(KEY, JSON.stringify([org, ...recentOrgs().filter((o) => o !== org)].slice(0, 8)));
  } catch {}
}

export function forgetOrg(org: string) {
  try {
    localStorage.setItem(KEY, JSON.stringify(recentOrgs().filter((o) => o !== org)));
  } catch {}
}
