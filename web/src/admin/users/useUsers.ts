import { useQuery } from "@connectrpc/connect-query";
import { fail } from "../../errors";
import { UI, type Invite, type User } from "../../gen/silo/v1/ui_pb";
import { patch, reload } from "../../query";

const noUsers: User[] = [];
const noInvites: Invite[] = [];

// useUsers is every account and the invite links still out, shared by the
// Users list, a user's page and the invite form through the one cached answer.
export function useUsers() {
  const q = useQuery(UI.method.listUsers, {});
  return {
    loaded: q.data !== undefined,
    users: q.data?.users ?? noUsers,
    invites: q.data?.invites ?? noInvites,
    err: q.error ? fail(q.error) : "",
  };
}

/** A user row the server just returned. It does not count Bots; the list's count stays. */
export function setUser(u: User) {
  patch(UI.method.listUsers, {}, (r) => ({ ...r, users: r.users.map((x) => (x.id === u.id ? { ...u, bots: x.bots } : x)) }));
}

export function dropUser(id: string) {
  patch(UI.method.listUsers, {}, (r) => ({ ...r, users: r.users.filter((x) => x.id !== id), invites: r.invites.filter((i) => i.userId !== id) }));
}

export function dropInvite(id: string) {
  patch(UI.method.listUsers, {}, (r) => ({ ...r, invites: r.invites.filter((i) => i.id !== id) }));
}

/** Invites are made with a link the list never sees again, so the list is asked anew. */
export function reloadUsers() {
  return reload(UI.method.listUsers);
}

/** How a user gets in, in a few words. */
export function signInWays(u: User): string {
  const ways = [];
  if (u.hasPassword) ways.push(u.totp ? "Password with two-factor" : "Password");
  if (u.oidc) ways.push("Single sign-on");
  return ways.join(", ") || "No way to sign in yet";
}
