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
  Send,
  SquareTerminal,
  Timer,
  Wrench,
  type LucideIcon,
} from "lucide-react";
import { useEffect, useState, type ReactNode } from "react";
import { ui } from "../api";

// The Python mark, one ink like the Lucide glyphs beside it.
function PythonMark({ size = 14, className }: { size?: number; className?: string }) {
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" fill="currentColor" className={className} aria-hidden>
      <path d="M14.25.18l.9.2.73.26.59.3.45.32.34.34.25.34.16.33.1.3.04.26.02.2-.01.13V8.5l-.05.63-.13.55-.21.46-.26.38-.3.31-.33.25-.35.19-.35.14-.33.1-.3.07-.26.04-.21.02H8.77l-.69.05-.59.14-.5.22-.41.27-.33.32-.27.35-.2.36-.15.37-.1.35-.07.32-.04.27-.02.21v3.06H3.17l-.21-.03-.28-.07-.32-.12-.35-.18-.36-.26-.36-.36-.35-.46-.32-.59-.28-.73-.21-.88-.14-1.05-.05-1.23.06-1.22.16-1.04.24-.87.32-.71.36-.57.4-.44.42-.33.42-.24.4-.16.36-.1.32-.05.24-.01h.16l.06.01h8.16v-.83H6.18l-.01-2.75-.02-.37.05-.34.11-.31.17-.28.25-.26.31-.23.38-.2.44-.18.51-.15.58-.12.64-.1.71-.06.77-.04.84-.02 1.27.05zm-6.3 1.98l-.23.33-.08.41.08.41.23.34.33.22.41.09.41-.09.33-.22.23-.34.08-.41-.08-.41-.23-.33-.33-.22-.41-.09-.41.09zm13.09 3.95l.28.06.32.12.35.18.36.27.36.35.35.47.32.59.28.73.21.88.14 1.04.05 1.23-.06 1.23-.16 1.04-.24.86-.32.71-.36.57-.4.45-.42.33-.42.24-.4.16-.36.09-.32.05-.24.02-.16-.01h-8.22v.82h5.84l.01 2.76.02.36-.05.34-.11.31-.17.29-.25.25-.31.24-.38.2-.44.17-.51.15-.58.13-.64.09-.71.07-.77.04-.84.01-1.27-.04-1.07-.14-.9-.2-.73-.25-.59-.3-.45-.33-.34-.34-.25-.34-.16-.33-.1-.3-.04-.25-.02-.2.01-.13v-5.34l.05-.64.13-.54.21-.46.26-.38.3-.32.33-.24.35-.2.35-.14.33-.1.3-.06.26-.04.21-.02.13-.01h5.84l.69-.05.59-.14.5-.21.41-.28.33-.32.27-.35.2-.36.15-.36.1-.35.07-.32.04-.28.02-.21V6.07h2.09l.14.01zm-6.47 14.25l-.23.33-.08.41.08.41.23.33.33.23.41.08.41-.08.33-.23.23-.33.08-.41-.08-.41-.23-.33-.33-.23-.41-.08-.41.08z" />
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
  memory: Brain,
  core_memory: Brain,
  remember: Brain,
  recall: Brain,
  forget: Brain,
  feed: Inbox,
  skill: BookOpen,
  artifact: Package,
  web_search: Globe,
  channel: Send,
  chats: MessagesSquare,
  list_models: Cpu,
  switch_model: Cpu,
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
  if (name === "exec_python") {
    inner = <PythonMark size={14} />;
  } else if (name === "call" || name.includes(".") || builtinCallIcons[name]) {
    const slug = name.split(".")[0];
    const mark = marks?.get(slug);
    const Icon = builtinCallIcons[name] ?? builtinCallIcons[slug] ?? Plug;
    inner =
      mark?.hasImage ? (
        <img src={`/connectors/${mark.id}/image`} alt="" className="h-4 w-4 rounded-[3px] object-cover" />
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
