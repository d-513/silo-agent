import type { SelectOption } from "./Select";
import type { ModelOption } from "./gen/silo/v1/ui_pb";

// A model picker's rows: the friendly label, with the `provider/model` id as the
// hint when the label hides it. `live` words a live model's hint (a speech or
// transcription picker), since that is what tells two of them apart on a call.
export function modelOptions(models: ModelOption[], live?: string): SelectOption[] {
  return models.map((m) => {
    const id = m.label && m.label !== m.id ? m.id : "";
    const hint = [id, live && m.live ? live : ""].filter(Boolean).join(" · ");
    const label = m.label || m.id;
    // A chip shows only the model's own name: "OpenRouter · anthropic/claude-haiku-4.5" is "claude-haiku-4.5".
    const name = label.includes(" · ") ? label.slice(label.lastIndexOf(" · ") + 3) : label;
    const short = name.split("/").pop()?.trim() || label;
    return { value: m.id, label, hint: hint || undefined, short: short === label ? undefined : short };
  });
}
