import {
  BookOpen,
  Bot,
  Brain,
  Cpu,
  Eye,
  Feather,
  FileDiff,
  FilePen,
  FileSearch,
  FileText,
  FileX,
  Globe,
  Hourglass,
  Image as ImageIcon,
  Inbox,
  KeyRound,
  Keyboard,
  ListChecks,
  MessagesSquare,
  Monitor,
  Mouse,
  MousePointerClick,
  Package,
  Plug,
  Scale,
  Send,
  SquareTerminal,
  Timer,
  Wrench,
  type LucideIcon,
} from "lucide-react";
import { useEffect, useState, type ReactNode } from "react";
import { ui } from "../api";
import { connectorTool } from "./toolInfo";
import { PYTHON_MARK } from "./pythonMark.ts";

// The Python mark, one ink like the Lucide glyphs beside it.
function PythonMark({ size = 14, className }: { size?: number; className?: string }) {
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" fill="currentColor" className={className} aria-hidden>
      <path d={PYTHON_MARK} />
    </svg>
  );
}

const toolIcons: Record<string, LucideIcon> = {
  terminal: SquareTerminal,
  read: FileText,
  write: FilePen,
  patch: FileDiff,
  grep: FileSearch,
  delete: FileX,
  present: FileText,
  look: Eye,
  click: MousePointerClick,
  type: Keyboard,
  key: Keyboard,
  scroll: Mouse,
  soul: Feather,
  remember: Brain,
  recall: Brain,
  forget: Brain,
  feed: Inbox,
  skill: BookOpen,
  artifact: Package,
  web_search: Globe,
  web_read: Globe,
  channel: Send,
  chats: MessagesSquare,
  list_models: Cpu,
  switch_model: Cpu,
  decide: Scale,
  generate_image: ImageIcon,
  spawn_agent: Bot,
  agent_status: Bot,
  message_agent: Bot,
  stop_agent: Bot,
  sleep: Hourglass,
  task_add: ListChecks,
  task_list: ListChecks,
  task_done: ListChecks,
  task_reset: ListChecks,
};

// Built-in `import tools` slugs a Python `call` can hit (the rest are connectors).
const builtinCallIcons: Record<string, LucideIcon> = {
  desktop: Monitor,
  web: Globe,
  channels: Send,
  chats: MessagesSquare,
  artifact: Package,
  secrets: KeyRound,
  chromium: Globe,
  bot: Brain,
  "bot.feed": Inbox,
  automations: Timer,
  model: Cpu,
  "model.decide": Scale,
  "model.image": ImageIcon,
  tasks: ListChecks,
};

// slug → the attached connector's mark, for `<slug>.<action>` call rows.
export type ConnectorMarks = ReadonlyMap<string, { id: string; hasImage: boolean }>;

export function useConnectorMarks(botId: string): ConnectorMarks {
  const [marks, setMarks] = useState<ConnectorMarks>(new Map());
  useEffect(() => {
    let dead = false;
    ui.listBotConnectors({ botId })
      .then((r) => {
        if (dead) return;
        const m = new Map<string, { id: string; hasImage: boolean }>();
        for (const x of r.connectors) {
          const c = x.connector;
          if (c?.slug) m.set(c.slug, { id: c.id, hasImage: c.hasImage });
        }
        setMarks(m);
      })
      .catch(() => {
        /* rows fall back to the plug */
      });
    return () => {
      dead = true;
    };
  }, [botId]);
  return marks;
}

// 16px slot holding what the row is using: the Python mark, a Lucide glyph,
// or a connector's own image (a plug when it has none).
export function ToolIcon({ name, marks }: { name: string; marks?: ConnectorMarks }) {
  let inner: ReactNode;
  const direct = toolIcons[name] ? undefined : connectorTool(name);
  if (name === "exec_python") {
    inner = <PythonMark size={14} />;
  } else if (name === "call" || name.includes(".") || builtinCallIcons[name] || direct) {
    // A connector reached through Python is `<slug>.<action>`; called by the
    // model directly it is `<slug>__<action>`. Both wear the connector's mark.
    const slug = direct?.slug ?? name.split(".")[0];
    const mark = marks?.get(slug);
    const Icon = builtinCallIcons[name] ?? builtinCallIcons[slug] ?? Plug;
    inner =
      mark?.hasImage ? (
        <img src={`/connectors/${mark.id}/image`} alt="" className="silo-brand h-4 w-4 rounded-[3px] object-cover" />
      ) : (
        <Icon size={15} strokeWidth={1.75} />
      );
  } else {
    const Icon = toolIcons[name] ?? Wrench;
    inner = <Icon size={15} strokeWidth={1.75} />;
  }
  return (
    <span className="flex h-4 w-4 shrink-0 items-center justify-center text-ink-2" aria-hidden>
      {inner}
    </span>
  );
}
