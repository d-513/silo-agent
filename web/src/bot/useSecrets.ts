import { skipToken, useQuery } from "@connectrpc/connect-query";
import { useEffect, useState } from "react";
import { fail } from "../errors";
import { useSave } from "../Feedback";
import { UI } from "../gen/silo/v1/ui_pb";
import { patch, reload } from "../query";

// The Secrets tab's list and add form. The list reloads each time the tab
// opens; a half-typed secret survives leaving the tab.
export function useSecrets(botId: string | undefined, open: boolean, onError: (message: string) => void) {
  const q = useQuery(UI.method.listSecrets, botId && open ? { botId } : skipToken);
  const saver = useSave();
  const [name, setName] = useState("");
  const [value, setValue] = useState("");

  useEffect(() => {
    if (q.error) onError(fail(q.error));
  }, [q.error]);

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
  };
}

export type SecretsState = ReturnType<typeof useSecrets>;
