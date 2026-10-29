import type { SelectOption } from "./Select";
import type { ModelOption } from "./gen/silo/v1/ui_pb";

// A model picker's rows: the friendly label, with the `provider/model` id as the
// hint when the label hides it. `live` words a live model's hint (a speech or
// transcription picker), since that is what tells two of them apart on a call.
export function modelOptions(models: ModelOption[], live?: string): SelectOption[] {
  return models.map((m) => {
    const id = m.label && m.label !== m.id ? m.id : "";
    const hint = [id, live && m.live ? live : ""].filter(Boolean).join(" · ");
    return { value: m.id, label: m.label || m.id, hint: hint || undefined };
  });
}
