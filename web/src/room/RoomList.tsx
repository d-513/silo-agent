import { useQuery } from "@connectrpc/connect-query";
import { Link } from "@tanstack/react-router";
import { Plus } from "lucide-react";
import { ActiveBar, UnreadBadge } from "../bot/SideNav";
import { Crest } from "../Crest";
import { UI, type Room } from "../gen/silo/v1/ui_pb";
import { locale, t } from "../i18n.ts";
import { Lamp } from "../Lamp";
import { shortWhen } from "../nav/botOrder";
import { hit } from "../nav/chrome";

// RoomList is the sidebar's Rooms: the rooms of the Workspace the caller is
// in, the newest first, each with its Bots' crests, its last line and what is
// unread. The plus makes a new one. Below 960 the sheet puts itself away
// when a row is pressed (NavSheet).
export function RoomList({ spaceId, activeRoomId, newOn }: { spaceId: string; activeRoomId?: string; newOn: boolean }) {
  const rooms = useQuery(UI.method.listRooms, { spaceId }, { refetchInterval: 5000 }).data?.rooms;
  return (
    <section className="mt-3" aria-label={t("Rooms")}>
      <div className="flex h-8 items-center gap-2 pr-0 pl-2">
        <h2 className="min-w-0 flex-1 truncate text-section text-ink-3">{t("Rooms")}</h2>
        <Link to="/w/$spaceId/rooms/new" params={{ spaceId }} title={t("New room")} aria-label={t("New room")} className={hit(newOn)}>
          <Plus size={17} />
        </Link>
      </div>
      <div className="space-y-0.5">
        {(rooms ?? []).map((r) => (
          <RoomRow key={r.id} room={r} on={r.id === activeRoomId} />
        ))}
        {rooms && rooms.length === 0 ? <p className="px-2 py-1 text-meta leading-[18px] text-ink-3">{t("Bring several Bots and people into one conversation.")}</p> : null}
      </div>
    </section>
  );
}

function RoomRow({ room, on }: { room: Room; on: boolean }) {
  const working = room.bots.some((b) => b.working);
  const needs = room.bots.some((b) => b.needsYou);
  const when = shortWhen(room.lastAt, new Date(), locale());
  const unread = Number(room.unread);
  return (
    <div className={`group relative rounded-control transition-[background-color,box-shadow] duration-[160ms] ease-quiet ${on ? "bg-surface shadow-card" : "hover:bg-pressed"}`}>
      <ActiveBar on={on} />
      <Link
        to="/rooms/$roomId"
        params={{ roomId: room.id }}
        aria-current={on ? "page" : undefined}
        className="blink flex min-w-0 items-center gap-3 rounded-control py-2 pr-3 pl-3 outline-offset-[-2px]"
      >
        <span className="relative flex h-[36px] w-[36px] shrink-0 items-center justify-center">
          {room.bots.slice(0, 2).map((b, i) => (
            <span key={b.botId} className={`absolute rounded-full ${i === 0 ? "top-0 left-0" : "right-0 bottom-0 ring-2 ring-well"}`}>
              <Crest index={b.crest} size={room.bots.length > 1 ? 24 : 36} />
            </span>
          ))}
          {working || needs ? <Lamp status={needs ? "needs_you" : "working"} onCrest={on ? "surface" : "well"} className="absolute -top-0.5 -right-0.5" /> : null}
        </span>
        <span className="min-w-0 flex-1">
          <span className="flex min-w-0 items-baseline gap-2">
            <span className="min-w-0 flex-1 truncate text-row leading-5 font-medium text-ink">{room.title}</span>
            {when ? <span className="shrink-0 text-micro leading-4 text-ink-3 tabular-nums">{when}</span> : null}
          </span>
          <span className="flex min-w-0 items-center gap-2">
            <span className="min-w-0 flex-1 truncate text-meta leading-[18px] text-ink-3">
              {room.lastMessage ? `${room.lastAuthor}: ${room.lastMessage}` : t("No messages yet")}
            </span>
            {unread > 0 && !on ? <UnreadBadge n={unread} /> : null}
          </span>
        </span>
      </Link>
    </div>
  );
}
