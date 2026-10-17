import { createElement, useEffect, useMemo, useState } from "react";
import { t } from "../i18n.ts";

// A glTF binary model the human can turn and zoom. It never moves by itself,
// and the decoders for compressed meshes are not fetched from anywhere: such a
// model says so instead.
export default function Model3D({ data }: { data: Uint8Array }) {
  const url = useMemo(() => URL.createObjectURL(new Blob([data.slice()], { type: "model/gltf-binary" })), [data]);
  const [ready, setReady] = useState(false);
  const [err, setErr] = useState("");
  useEffect(() => () => URL.revokeObjectURL(url), [url]);
  useEffect(() => {
    let dead = false;
    import("@google/model-viewer")
      .then((m) => {
        const el = m.ModelViewerElement as unknown as Record<string, string>;
        el.dracoDecoderLocation = "/_silo_no_decoder/";
        el.ktx2TranscoderLocation = "/_silo_no_decoder/";
        el.meshoptDecoderLocation = "/_silo_no_decoder/meshopt.js";
        if (!dead) setReady(true);
      })
      .catch(() => !dead && setErr(t("The 3D viewer could not load.")));
    return () => {
      dead = true;
    };
  }, []);
  if (err) return <p className="text-ink-2">{err}</p>;
  if (!ready) return <div className="skeleton h-[60vh] min-h-[22rem] rounded-sm" />;
  return createElement("model-viewer", {
    src: url,
    "camera-controls": "",
    "interaction-prompt": "none",
    "shadow-intensity": "0.6",
    style: { width: "100%", height: "60vh", minHeight: "22rem", background: "var(--color-well)", borderRadius: 8 },
    onError: () => setErr(t("This model could not be shown: it may use compression the preview cannot read.")),
  });
}
