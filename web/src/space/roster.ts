// initials are a person's letters on their circle: the first two letters of
// the name part of their email ("alice.smith@…" is AS, "bob@…" is BO).
export function initials(email: string): string {
  const name = email.split("@")[0] ?? "";
  const parts = name.split(/[._\-+]+/).filter(Boolean);
  const pick = parts.length >= 2 ? parts[0][0] + parts[1][0] : name.slice(0, 2);
  return pick.toUpperCase() || "?";
}

// mayChange says whether a caller with a Workspace role may change or
// remove a person with another: the owner anyone but themselves, an admin
// members and guests. The server holds the same rule.
export function mayChange(caller: string, theirs: string): boolean {
  if (theirs === "owner") return false;
  if (caller === "owner") return true;
  return caller === "admin" && (theirs === "member" || theirs === "guest");
}
