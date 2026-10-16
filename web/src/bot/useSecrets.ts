import { skipToken, useQuery } from "@connectrpc/connect-query";
import { useEffect, useState } from "react";
import { fail } from "../errors";
import { useSave } from "../Feedback";
import { UI } from "../gen/silo/v1/ui_pb";
import { patch, reload } from "../query";

// "surrogate" is the default: the Bot holds a stand-in and the network filter
// swaps in the real value for the hosts listed. "plain" hands the Bot the real
// value (an SSH key, a database password) — nothing can stand in for it.
export type SecretMode = "surrogate" | "plain";

// A comma or whitespace separated list of hosts, as typed in the form.
export function parseHosts(text: string): string[] {
  return text
    .split(/[\s,]+/)
    .map((h) => h.trim())
    .filter((h) => h.length > 0);
}

export type SecretEdit = { id: string; name: string; mode: string; hosts: string; value: string; swapBody: boolean };

// The Secrets tab's list and add form. The list reloads each time the tab
// opens; a half-typed secret survives leaving the tab.
export function useSecrets(botId: string | undefined, open: boolean, onError: (message: string) => void) {
  const q = useQuery(UI.method.listSecrets, botId && open ? { botId } : skipToken);
  const saver = useSave();
  const [name, setName] = useState("");
  const [value, setValue] = useState("");
  const [mode, setMode] = useState<SecretMode>("surrogate");
  const [hosts, setHosts] = useState("");
  const [swapBody, setSwapBody] = useState(false);
  const [editing, setEditing] = useState<SecretEdit | null>(null);

  useEffect(() => {
    if (q.error) onError(fail(q.error));
  }, [q.error]);

  const reset = () => {
    setName("");
    setValue("");
    setMode("surrogate");
    setHosts("");
    setSwapBody(false);
  };

  return {
    // null while the first load is out; a failed load shows the empty list.
    secrets: q.data?.secrets ?? (q.error ? [] : null),
    removed: (id: string) => {
      if (botId) patch(UI.method.listSecrets, { botId }, (r) => ({ ...r, secrets: r.secrets.filter((x) => x.id !== id) }));
    },
    reload: () => (botId ? reload(UI.method.listSecrets, { botId }) : Promise.resolve()),
    saver,
    name,
    setName,
    value,
    setValue,
    mode,
    setMode,
    hosts,
    setHosts,
    swapBody,
    setSwapBody,
    reset,
    editing,
    startEdit: (s: { id: string; name: string; mode: string; hosts: string[]; swapBody: boolean }) =>
      setEditing({ id: s.id, name: s.name, mode: s.mode, hosts: s.hosts.join(", "), value: "", swapBody: s.swapBody }),
    stopEdit: () => setEditing(null),
    editField: (patch: Partial<SecretEdit>) => setEditing((e) => (e ? { ...e, ...patch } : e)),
  };
}

export type SecretsState = ReturnType<typeof useSecrets>;
