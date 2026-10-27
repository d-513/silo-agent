import { Link, useNavigate } from "@tanstack/react-router";
import { Plus, Settings } from "lucide-react";
import { useEffect, useRef, useState, type CSSProperties, type KeyboardEvent } from "react";
import { createPortal } from "react-dom";
import type { Space } from "../gen/silo/v1/ui_pb";
import { t } from "../i18n.ts";
import { useDismiss } from "../useMenu";
import { canAdmin } from "./spaces";

/** The letters of a Workspace on a `well` square: the rail's tile, the menu's rows. */
export function SpaceMark({ space, size = 32 }: { space: Pick<Space, "mark" | "name"> | undefined; size?: number }) {
  return (
    <span
      aria-hidden
      style={{ width: size, height: size, fontSize: size >= 32 ? 13 : 11 }}
      className="flex shrink-0 items-center justify-center rounded-sm bg-well font-semibold tracking-[0.02em] text-ink shadow-[inset_0_0_0_1px_var(--color-line)]"
    >
      {space?.mark ?? ""}
    </span>
  );
}

/** A Workspace role as a reader says it; the owner's goes unsaid. */
export function roleWord(role: string): string {
  switch (role) {
    case "admin":
      return t("Admin", { context: "role" });
    case "member":
      return t("Member", { context: "role" });
    case "guest":
      return t("Guest", { context: "role" });
  }
  return "";
}

// SpaceTile heads the rail: the current Workspace's letters, which open the
// switcher. The Silo word stays under it on a wide screen.
export function SpaceTile({ space, spaces }: { space: Space | undefined; spaces: Space[] | null }) {
  const nav = useNavigate();
  const [open, setOpen] = useState(false);
  const [active, setActive] = useState(0);
  const triggerRef = useRef<HTMLButtonElement>(null);
  const menuRef = useRef<HTMLDivElement>(null);
  const [box, setBox] = useState<CSSProperties>({});
  useDismiss(open, triggerRef, menuRef, () => setOpen(false));
  const list = spaces ?? [];

  useEffect(() => {
    if (!open) return;
    const r = triggerRef.current?.getBoundingClientRect();
    if (r) {
      // Beside the rail on a wide screen, under the top bar on a narrow one.
      const wide = window.innerWidth >= 960;
      setBox(wide ? { left: r.right + 10, top: r.top } : { left: Math.max(8, r.left), top: r.bottom + 6 });
    }
    setActive(Math.max(0, list.findIndex((s) => s.id === space?.id)));
    menuRef.current?.focus();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);

  function go(id: string) {
    setOpen(false);
    void nav({ to: "/w/$spaceId", params: { spaceId: id } });
  }

  function onKeyDown(e: KeyboardEvent) {
    if (e.key === "Escape") {
      setOpen(false);
      triggerRef.current?.focus();
    } else if (e.key === "ArrowDown") {
      e.preventDefault();
      setActive((i) => Math.min(i + 1, list.length - 1));
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      setActive((i) => Math.max(i - 1, 0));
    } else if (e.key === "Enter" && list[active]) {
      e.preventDefault();
      go(list[active].id);
    }
  }

  const label = space ? t("Workspace: {{name}}", { name: space.name }) : t("Workspaces");
  return (
    <div className="flex shrink-0 items-center gap-2 px-3 text-ink max-wide:h-12 wide:flex-col wide:gap-1 wide:px-2 wide:pt-4">
      <button
        ref={triggerRef}
        type="button"
        title={label}
        aria-label={label}
        aria-haspopup="menu"
        aria-expanded={open}
        onClick={() => setOpen((v) => !v)}
        className="rounded-sm transition-transform duration-[160ms] ease-quiet hover:-translate-y-px active:scale-[.94] active:duration-[70ms] aria-expanded:translate-y-0"
      >
        {space ? <SpaceMark space={space} /> : <span className="skeleton block h-8 w-8 rounded-sm" />}
      </button>
      <span className="font-semibold max-wide:hidden wide:text-[11px] wide:leading-4">{"Silo"}</span>
      {open
        ? createPortal(
            <div
              ref={menuRef}
              role="menu"
              tabIndex={-1}
              aria-label={t("Workspaces")}
              onKeyDown={onKeyDown}
              className="fixed z-50 w-[260px] rounded-control bg-surface p-1 shadow-slip outline-none"
              style={box}
            >
              <div className="rise">
                <div className="px-2 pt-1.5 pb-1 text-label-caps text-ink-3 uppercase">{t("Workspaces")}</div>
                {list.map((s, i) => {
                  const current = s.id === space?.id;
                  return (
                    <button
                      key={s.id}
                      type="button"
                      role="menuitemradio"
                      aria-checked={current}
                      onMouseEnter={() => setActive(i)}
                      onClick={() => go(s.id)}
                      className={`relative flex h-10 w-full items-center gap-2.5 rounded-sm px-2 text-left transition-colors duration-[160ms] ${
                        i === active ? "bg-well" : "hover:bg-well"
                      }`}
                    >
                      {current ? <span aria-hidden className="absolute inset-y-2 left-0 w-[3px] rounded-full bg-cobalt" /> : null}
                      <SpaceMark space={s} size={24} />
                      <span className="min-w-0 flex-1 truncate text-[13.5px] font-medium text-ink">{s.name}</span>
                      {roleWord(s.role) ? (
                        <span className="shrink-0 rounded-xs bg-well px-1.5 py-0.5 text-[11px] font-medium text-ink-2">{roleWord(s.role)}</span>
                      ) : null}
                    </button>
                  );
                })}
                <span aria-hidden className="mx-2 my-1 block h-px bg-line" />
                <Link
                  to="/w/new"
                  role="menuitem"
                  onClick={() => setOpen(false)}
                  className="flex h-9 items-center gap-2.5 rounded-sm px-2 text-[13px] text-ink-2 transition-colors hover:bg-well hover:text-ink"
                >
                  <Plus size={15} className="mx-[4.5px]" />
                  {t("New workspace")}
                </Link>
                {space && canAdmin(space.role) ? (
                  <Link
                    to="/w/$spaceId/settings"
                    params={{ spaceId: space.id }}
                    role="menuitem"
                    onClick={() => setOpen(false)}
                    className="flex h-9 items-center gap-2.5 rounded-sm px-2 text-[13px] text-ink-2 transition-colors hover:bg-well hover:text-ink"
                  >
                    <Settings size={15} className="mx-[4.5px]" />
                    {t("Workspace settings")}
                  </Link>
                ) : null}
              </div>
            </div>,
            document.body,
          )
        : null}
    </div>
  );
}
