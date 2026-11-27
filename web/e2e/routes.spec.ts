import { expect, test, type Page } from "@playwright/test";

// Every documented URL keeps working: each one loads, stays where it is, and
// lights the right places. No message is sent, so this costs no tokens.

const email = process.env.SILO_BOOTSTRAP_EMAIL ?? "admin@local";
const password = process.env.SILO_BOOTSTRAP_PASSWORD ?? "silo-dev-pass";

async function signIn(page: Page) {
  await page.fill("#silo-email", email);
  await page.fill('input[type="password"]', password);
  await page.getByRole("button", { name: /^sign in$/i }).click();
}

// No page may throw, or log a React error, while it is walked.
let thrown: string[] = [];
test.beforeEach(({ page }) => {
  thrown = [];
  page.on("pageerror", (e) => thrown.push(e.message));
  page.on("console", (m) => {
    if (m.type() === "error" && /react|router|query|hook|render/i.test(m.text())) thrown.push(m.text());
  });
});
test.afterEach(() => expect(thrown).toEqual([]));

// Whatever is lit carries data-tab-on: a tab of the bar (every page of a Bot
// opens as one) and a section tab by their label.
const lit = (page: Page, name: string) => page.locator(`[data-tab-on][title="${name}"], [data-tab-on]:has-text("${name}")`).first();

const pages: [path: string, lit: string[]][] = [
  // A machine pane as a page of its own is a tab.
  ["desktop", ["Desktop"]],
  ["console", ["Console"]],
  ["files", ["Files"]],
  ["changes", ["Changes"]],
  // Channels is a tab; Settings is one, and lights its open section tab.
  ["channels", ["Channels"]],
  ["settings", ["Settings", "General"]],
  ["people", ["Settings", "People"]],
  ["rules", ["Settings", "Rules"]],
  ["secrets", ["Settings", "Secrets"]],
  ["tunnels", ["Settings", "Tunnels & Links"]],
  ["container", ["Settings", "Containers"]],
  // The pages beside the conversations are tabs too.
  ["automations", ["Automations"]],
  ["artifacts", ["Artifacts"]],
  ["memories", ["Memories"]],
  ["knowledge", ["Knowledge"]],
  ["feed", ["Feed"]],
  // Runs opens from the closed chats' menu, as a tab.
  ["runs", ["Runs"]],
];

test("every route loads, stays put, and lights its place", async ({ page }) => {
  await page.goto("/");
  await signIn(page);

  await page.getByRole("link", { name: "New Bot" }).first().click();
  await expect(page).toHaveURL(/\/new$/);
  await page.fill("#bot-name", `Routes ${Date.now()}`);
  await page.getByRole("button", { name: /^create bot$/i }).click();

  // A new Bot opens on its first chat.
  await expect(page).toHaveURL(/\/bots\/[^/]+\/run\/[^/]+$/, { timeout: 60_000 });
  const botId = page.url().match(/\/bots\/([^/]+)/)![1];
  const bot = `/bots/${botId}`;
  const chatUrl = page.url();
  await expect(page.getByPlaceholder(/Ask /)).toBeVisible();

  try {
    for (const [path, names] of pages) {
      await page.goto(`${bot}/${path}`);
      for (const name of names) await expect(lit(page, name), `${path}: ${name}`).toBeVisible();
      await expect(page, path).toHaveURL(new RegExp(`${bot}/${path}$`));
      await expect(page.getByText("This page hit a snag"), path).toHaveCount(0);
    }

    // Redirects.
    await page.goto(bot);
    await expect(page).toHaveURL(new RegExp(`${bot}/run`));
    await page.goto(`${bot}/nonsense`);
    await expect(page).toHaveURL(new RegExp(`${bot}/run`));
    await page.goto(`${bot}/run`);
    await expect(page).toHaveURL(chatUrl);
    // Anything else goes home: the Workspace's first Bot.
    await page.goto("/whatever");
    await expect(page).toHaveURL(/\/bots\/[^/]+\/run/);
    await page.goto("/admin");
    await expect(page).toHaveURL(/\/admin\/settings$/);

    // Sub-routes are real history entries: Back returns to the list.
    for (const [list, sub, label] of [
      ["channels", "channels/new", "Channels"],
      ["automations", "automations/new", "Automations"],
    ]) {
      await page.goto(`${bot}/${list}`);
      await expect(lit(page, label)).toBeVisible();
      await page.goto(`${bot}/${sub}`);
      await expect(page, sub).toHaveURL(new RegExp(`${bot}/${sub}$`));
      await expect(lit(page, label), sub).toBeVisible();
      await page.goBack();
      await expect(page, sub).toHaveURL(new RegExp(`${bot}/${list}$`));
    }
    await page.goto(`${bot}/channels/new/telegram`);
    await expect(page.getByText(/telegram/i).first()).toBeVisible();
    await expect(page).toHaveURL(new RegExp(`${bot}/channels/new/telegram$`));
    // Each sub-page is its own route with its own content.
    await page.goto(`${bot}/automations/new`);
    await expect(page.getByRole("heading", { name: "New automation" })).toBeVisible();
    await page.goto(`${bot}/automations/nope`);
    await expect(page.getByText("This automation is gone.")).toBeVisible();
    await expect(lit(page, "Automations")).toBeVisible();
    await page.goto(`${bot}/channels/nope/setup`);
    await expect(page.getByText("Opening…")).toBeVisible();
    await expect(lit(page, "Channels")).toBeVisible();
    // The pinned Heartbeat opens its log, and Back returns to the list.
    await page.goto(`${bot}/automations`);
    await page.getByRole("link", { name: /heartbeat/i }).first().click();
    await expect(page).toHaveURL(new RegExp(`${bot}/automations/[^/]+$`));
    await expect(page.getByText(/No runs yet/)).toBeVisible();
    await page.goBack();
    await expect(page.getByRole("heading", { name: "Automations" })).toBeVisible();

    await page.goto("/admin/connectors/new");
    await expect(page).toHaveURL(/\/admin\/connectors\/new$/);
    // The Workspace's settings: /skills lands on its Skills tab, the bare
    // address on its first tab, and every tab is a route of its own that
    // lights its tab.
    await page.goto("/skills");
    await expect(page).toHaveURL(/\/w\/[^/]+\/skills$/);
    const workspace = new URL(page.url()).pathname.replace(/\/skills$/, "");
    await page.goto(workspace);
    await expect(page).toHaveURL(new RegExp(`${workspace}/connectors$`));
    for (const [tab, label] of [
      ["connectors", "Connectors"],
      ["skills", "Skills"],
      ["drives", "Drives"],
      ["people", "People"],
      ["rules", "Rules"],
      ["activity", "Activity"],
      ["settings", "Settings"],
    ]) {
      await page.goto(`${workspace}/${tab}`);
      await expect(page, tab).toHaveURL(new RegExp(`${workspace}/${tab}$`));
      await expect(lit(page, label), tab).toBeVisible();
    }
    await page.goto(`${workspace}/drives/new/sftp`);
    await expect(page.getByText(/sftp/i).first()).toBeVisible();
    await page.goto(`${workspace}/drives/nope`);
    await expect(page.getByText("That drive is gone.")).toBeVisible();
    // A new room is a page of its own; a room that is not there says so.
    await page.goto(`${workspace}/rooms/new`);
    await expect(page.getByRole("heading", { name: "New room" })).toBeVisible();
    await page.goto("/rooms/nope");
    await expect(page.getByText("This room is not open to you")).toBeVisible();
    await page.goto("/account");
    await expect(page).toHaveURL(/\/account$/);

    // A query string survives.
    await page.goto(`${bot}/files?open=bot`);
    await expect(lit(page, "Files")).toBeVisible();
    await expect(page).toHaveURL(new RegExp(`${bot}/files\\?open=bot$`));

  } finally {
    await page.goto(`${bot}/settings`);
    await page.getByRole("button", { name: /^delete$/i }).click();
    await page.getByRole("button", { name: /^click again to delete$/i }).click();
    await expect(page).toHaveURL(/\/(w\/[^/]+\/new|bots\/[^/]+\/run.*)$/, { timeout: 30_000 });
  }
});

// Signing in returns to the page the session ended on (the /signin route's
// `from`), not home.
test("signing in comes back to the page you were on", async ({ page }) => {
  await page.goto("/account");
  await signIn(page);
  await expect(page).toHaveURL(/\/account$/);
  await page.getByTitle("Sign out").click();
  await expect(page).toHaveURL(/\/signin/);
  await signIn(page);
  await expect(page).toHaveURL(/\/account$/);
});

// A private tunnel sends the browser to /signin?next=<its handoff>: after
// signing in the SPA leaves for that server route instead of opening a page.
test("sign-in carries on to a tunnel handoff", async ({ page }) => {
  await page.goto(`/signin?next=${encodeURIComponent("/tunnels/auth?name=nope&rd=/")}`);
  const handoff = page.waitForRequest((r) => new URL(r.url()).pathname === "/tunnels/auth");
  await signIn(page);
  expect(new URL((await handoff).url()).searchParams.get("name")).toBe("nope");
});

// Moving around by clicking: the tabs, the right rail, the split, the
// sidebar, and back to the open chat.
test("tabs and chats navigate by click", async ({ page }) => {
  await page.goto("/new");
  await signIn(page);
  await expect(page).toHaveURL(/\/new$/);
  await page.fill("#bot-name", `Clicks ${Date.now()}`);
  await page.getByRole("button", { name: /^create bot$/i }).click();
  await expect(page).toHaveURL(/\/run\/[^/]+$/, { timeout: 60_000 });
  const first = page.url();
  const bot = first.match(/\/bots\/[^/]+/)![0];
  try {
    await page.getByRole("button", { name: "New chat", exact: true }).first().click();
    // New chat is an empty page: no chat exists until a message is sent.
    await expect(page).toHaveURL(/\/run\/new$/);
    await expect(page.getByPlaceholder(/Ask /)).toBeEnabled();
    const second = page.url();

    const rail = page.getByRole("complementary", { name: "Pages and machine" });
    // Settings, one of its tabs, and back: the tabs stay over every page.
    await rail.getByRole("button", { name: "Settings", exact: true }).click();
    await expect(page).toHaveURL(new RegExp(`${bot}/settings$`));
    await page.getByRole("link", { name: "Rules" }).click();
    await expect(page).toHaveURL(new RegExp(`${bot}/rules$`));
    await expect(lit(page, "Rules")).toBeVisible();
    await page.getByRole("link", { name: /^Main chat/ }).first().click();
    await expect(page).toHaveURL(first);
    await page.getByRole("button", { name: "New chat", exact: true }).first().click();
    await expect(page).toHaveURL(second);
    // The empty chat is a tab of its own; closing it opens its neighbour, the
    // Settings tab the rail opened before it.
    await page.locator("[data-tab-on]").getByRole("button", { name: "Close tab" }).click();
    await expect(page).toHaveURL(new RegExp(`${bot}/settings$`));
    await page.locator("[data-tab-on]").getByRole("button", { name: "Close tab" }).click();
    await expect(page).toHaveURL(first);
    await page.getByRole("link", { name: /^Dashboard/ }).first().click();
    await expect(page).toHaveURL(new RegExp(`${bot}/dashboard$`));
    await expect(lit(page, "Dashboard")).toBeVisible();
    await page.goto(second);

    // A machine pane opens as a tab, moves into the split beside the chat,
    // swaps back by its tab, and leaves the split.
    await rail.getByRole("button", { name: "Files", exact: true }).click();
    await expect(page).toHaveURL(new RegExp(`${bot}/files$`));
    await expect(lit(page, "Files")).toBeVisible();
    await page.getByRole("button", { name: "Open in split view" }).first().click();
    await expect(page).not.toHaveURL(new RegExp(`${bot}/files$`));
    const split = page.getByRole("region", { name: "Split view" });
    await expect(split).toBeVisible();
    await page.getByRole("link", { name: "Files", exact: true }).click();
    await expect(page).toHaveURL(new RegExp(`${bot}/files$`));
    await expect(split).toBeVisible();
    await split.getByRole("button", { name: "Close split view" }).click();
    await expect(split).toHaveCount(0);
    await page.goto(second);

    // A page beside the conversations opens as a tab from the rail.
    await rail.getByRole("button", { name: "Memories" }).click();
    await expect(page).toHaveURL(new RegExp(`${bot}/memories$`));
    await expect(lit(page, "Memories")).toBeVisible();
    await page.goto(second);
    await expect(page.getByPlaceholder(/Ask /)).toBeVisible();

    // A list that is written to shows the change without a reload.
    await page.goto(`${bot}/secrets`);
    await expect(page.getByText("No secrets on this Bot yet.")).toBeVisible();
    await page.getByPlaceholder("vendor_password").fill("e2e_secret");
    await page.locator('input[type="password"]').fill("hunter2");
    await page.getByRole("button", { name: /^add$/i }).click();
    await expect(page.getByText("e2e_secret")).toBeVisible();
    await page.getByRole("button", { name: /^delete$/i }).click();
    await page.getByRole("button", { name: /again/i }).click();
    await expect(page.getByText("No secrets on this Bot yet.")).toBeVisible();

    // Renaming the Bot reaches its row in the sidebar at once.
    await page.goto(`${bot}/settings`);
    const renamed = `Renamed ${Date.now()}`;
    await page.locator("input").first().fill(renamed);
    await page.getByRole("button", { name: /^save/i }).first().click();
    const row = page.getByRole("navigation", { name: "Bots" }).getByRole("link", { name: new RegExp(renamed) });
    await expect(row).toBeVisible();
    await page.goto(second);

    // The sidebar opens the Workspace's settings, and its row comes back to
    // the Bot.
    await page.getByRole("link", { name: "Workspace settings" }).click();
    await expect(page).toHaveURL(/\/w\/[^/]+\/connectors$/);
    await row.click();
    await expect(page).toHaveURL(new RegExp(`${bot}/run/[^/]+$`));
  } finally {
    await page.goto(`${bot}/settings`);
    await page.getByRole("button", { name: /^delete$/i }).click();
    await page.getByRole("button", { name: /^click again to delete$/i }).click();
    await expect(page).toHaveURL(/\/(w\/[^/]+\/new|bots\/[^/]+\/run.*)$/, { timeout: 30_000 });
  }
});

// The admin form edits on top of what the server holds: typing counts a change,
// it is still there after a look at another category, and Discard goes back to
// the saved value. Nothing is saved here.
test("admin settings form tracks and discards edits", async ({ page }) => {
  await page.goto("/admin/settings");
  await signIn(page);
  // Settings opens on its first category.
  await expect(page).toHaveURL(/\/admin\/settings\/models$/);
  const categories = page.getByRole("navigation", { name: "Settings" });
  await categories.getByRole("link", { name: "Server" }).click();
  await expect(page).toHaveURL(/\/admin\/settings\/server$/);
  const field = page.locator('main input[type="text"]:not([disabled])').first();
  await expect(field).toBeVisible();
  const saved = await field.inputValue();
  await field.fill(`${saved}x`);
  await expect(page.getByText("1 unsaved change")).toBeVisible();
  // Models has its pages as tabs in its own head.
  await categories.getByRole("link", { name: "Models" }).click();
  const models = page.getByRole("navigation", { name: "Models" });
  await models.getByRole("link", { name: "Providers" }).click();
  await expect(page).toHaveURL(/\/admin\/settings\/models\/providers$/);
  await expect(models.locator("[data-tab-on]")).toHaveText(/Providers/);
  await expect(page.getByText("1 unsaved change")).toBeVisible();
  await categories.getByRole("link", { name: "Server" }).click();
  await expect(field).toHaveValue(`${saved}x`);
  await page.getByRole("button", { name: /discard/i }).click();
  await expect(field).toHaveValue(saved);
  await expect(page.getByText("1 unsaved change")).toHaveCount(0);
  // Every category is a page of its own, and an unknown one falls back to the first.
  for (const s of ["search", "memory", "runs", "voice", "rules", "connectors", "tunnels", "mail", "network", "https", "signin", "server", "yaml"]) {
    await page.goto(`/admin/settings/${s}`);
    await expect(page).toHaveURL(new RegExp(`/admin/settings/${s}$`));
    await expect(categories.locator("[data-tab-on]")).toHaveCount(1);
    await expect(page.getByText("This page hit a snag")).toHaveCount(0);
  }
  // Sign-in shows the address the OIDC provider has to be told.
  await page.goto("/admin/settings/signin");
  await expect(page.getByText(/\/auth\/oidc\/callback$/)).toBeVisible();
  await page.goto("/admin/settings/nope");
  await expect(page).toHaveURL(/\/admin\/settings\/models$/);
  // Models opens on its jobs (the map), with Providers and Allowed models
  // under it; an unknown page lands on the jobs.
  for (const m of ["providers", "allowed"]) {
    await page.goto(`/admin/settings/models/${m}`);
    await expect(page).toHaveURL(new RegExp(`/admin/settings/models/${m}$`));
    await expect(categories.locator("[data-tab-on]")).toHaveCount(1);
    await expect(models.locator("[data-tab-on]")).toHaveCount(1);
    await expect(page.getByText("This page hit a snag")).toHaveCount(0);
  }
  // A job is picked on the map: pressing one opens the models it can be given.
  await page.goto("/admin/settings/models");
  await page.getByRole("button", { name: /^Chat/ }).first().click();
  await expect(page.getByRole("dialog", { name: "Chat" })).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(page.getByRole("dialog")).toHaveCount(0);
  // The addresses from before still land somewhere: a kind's page on its
  // allowlist, Voice on its category, the old Providers category under Models.
  for (const kind of ["text", "decision", "embedding", "image"]) {
    await page.goto(`/admin/settings/models/${kind}`);
    await expect(page).toHaveURL(new RegExp(`/admin/settings/models/allowed\\?kind=${kind}$`));
  }
  await page.goto("/admin/settings/models/voice");
  await expect(page).toHaveURL(/\/admin\/settings\/voice$/);
  await page.goto("/admin/settings/providers");
  await expect(page).toHaveURL(/\/admin\/settings\/models\/providers$/);
  await page.goto("/admin/settings/models/nope");
  await expect(page).toHaveURL(/\/admin\/settings\/models$/);
  // Search & Extract was a tab; its old address lands on the category.
  await page.goto("/admin/search-extract");
  await expect(page).toHaveURL(/\/admin\/settings\/search$/);
  // The other admin pages load their lists.
  for (const p of ["users", "users/invite", "connectors", "skills", "drives"]) {
    await page.goto(`/admin/${p}`);
    await expect(page).toHaveURL(new RegExp(`/admin/${p}$`));
    await expect(page.locator(".skeleton")).toHaveCount(0);
    await expect(page.getByText("This page hit a snag")).toHaveCount(0);
  }
});

// The pages nobody is signed in for: an invite link that is not one says so,
// and a failed single sign-on comes back to Sign in with its reason in words.
test("signed-out pages", async ({ page }) => {
  await page.goto("/invite/not-a-real-link");
  await expect(page.getByRole("heading", { name: "This link has expired" })).toBeVisible();
  await page.getByRole("link", { name: "Go to sign in" }).click();
  await expect(page).toHaveURL(/\/signin$/);

  await page.goto("/signin?error=oidc_no_account");
  await expect(page.getByRole("alert")).toHaveText(/no Silo account for you yet/);
  // Only the server's codes are worded; a link cannot put its own text here.
  await page.goto("/signin?error=Call%20555-0100");
  await expect(page.getByRole("alert")).toHaveCount(0);
});

test("account page shows the session in use", async ({ page }) => {
  await page.goto("/account");
  await signIn(page);
  await expect(page).toHaveURL(/\/account$/);
  await expect(page.getByRole("heading", { name: "Sessions" })).toBeVisible();
  await expect(page.getByText("This device")).toBeVisible();
  await expect(page.getByRole("heading", { name: "Two-factor" })).toBeVisible();
  // The email field shows the account's address and saves only a change.
  await expect(page.getByRole("button", { name: "Change email" })).toBeDisabled();
});
