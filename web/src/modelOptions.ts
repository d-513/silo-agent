import type { SelectOption } from "./Select";
import type { ModelOption } from "./gen/silo/v1/ui_pb";

// A model picker's rows: the friendly label, with the `provider/model` id as the
// hint when the label hides it.
export function modelOptions(models: ModelOption[]): SelectOption[] {
  return models.map((m) => ({ value: m.id, label: m.label || m.id, hint: m.label && m.label !== m.id ? m.id : undefined }));
}
