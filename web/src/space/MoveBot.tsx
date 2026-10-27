import { skipToken, useQuery } from "@connectrpc/connect-query";
import { useNavigate } from "@tanstack/react-router";
import { useState } from "react";
import { ui } from "../api";
import { fail } from "../errors";
import { ArmedButton } from "../Feedback";
import { SettingGroup, SettingRow } from "../Field";
import { UI, type Bot } from "../gen/silo/v1/ui_pb";
import { t } from "../i18n.ts";
import { reload } from "../query";
import { Select } from "../Select";
import { canBuild, useSpaces } from "./spaces";

// MoveBot is a Bot's move to another Workspace the manager may build in:
// what it loses first (grants, the Workspace's skills, the people not
// there), then a second click.
export function MoveBot({ bot }: { bot: Bot }) {
  const nav = useNavigate();
  const { spaces } = useSpaces();
  const targets = (spaces ?? []).filter((s) => s.id !== bot.spaceId && canBuild(s.role));
  const [to, setTo] = useState("");
  const [err, setErr] = useState("");
  const plan = useQuery(UI.method.planMoveBot, to ? { botId: bot.id, spaceId: to } : skipToken).data;
  if (bot.role !== "manage" || targets.length === 0) return null;
  const losses = plan ? [...plan.connectors, ...plan.drives, ...plan.skills] : [];
  async function move() {
    setErr("");
    try {
      await ui.moveBot({ botId: bot.id, spaceId: to });
      await Promise.all([reload(UI.method.listBots), reload(UI.method.listSpaces)]);
      void nav({ to: "/w/$spaceId", params: { spaceId: to } });
    } catch (ex) {
      setErr(fail(ex));
    }
  }
  return (
    <SettingGroup title={t("Workspace")}>
      <SettingRow
        label={t("Move to another workspace")}
        hint={
          plan
            ? [
                losses.length ? t("It loses what was its workspace's: {{what}}.", { what: losses.join(", ") }) : t("It takes nothing of its workspace's along."),
                plan.people.length ? t("These people are not in the new one and lose it: {{who}}.", { who: plan.people.join(", ") }) : "",
              ]
                .filter(Boolean)
                .join(" ")
            : t("Its machine, files, memories, chats, rules and secrets go with it.")
        }
        error={err}
      >
        <div className="flex flex-wrap items-center gap-2">
          <Select value={to} onChange={setTo} placeholder={t("Choose a workspace")} options={targets.map((s) => ({ value: s.id, label: s.name }))} />
          {to ? (
            <ArmedButton armedLabel={t("Click again to move")} onConfirm={() => void move()}>
              {t("Move")}
            </ArmedButton>
          ) : null}
        </div>
      </SettingRow>
    </SettingGroup>
  );
}
