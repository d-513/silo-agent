import { useEffect, useState } from "react";
import { ui } from "../api";
import { fail } from "../errors";
import { useSave } from "../Feedback";
import type { SecretMeta } from "../gen/silo/v1/ui_pb";

// The Secrets tab's list and add form. The list reloads each time the tab
// opens; a half-typed secret survives leaving the tab.
export function useSecrets(botId: string | undefined, open: boolean, onError: (message: string) => void) {
  const [secrets, setSecrets] = useState<SecretMeta[] | null>(null);
  const saver = useSave();
  const [name, setName] = useState("");
  const [value, setValue] = useState("");

  useEffect(() => {
    if (!botId || !open) return;
    ui.listSecrets({ botId }).then((r) => setSecrets(r.secrets)).catch((e) => {
      setSecrets([]);
      onError(fail(e));
    });
  }, [botId, open]);

  return { secrets, setSecrets, saver, name, setName, value, setValue };
}

export type SecretsState = ReturnType<typeof useSecrets>;
