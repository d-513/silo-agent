import { eq } from "./testing.ts";
import { mailNotice, rawMailHref, senderName, wakeListError } from "./mailbox.ts";

// What the page says when mail cannot be received: names the key to set, and
// only sends admins to the settings page.
eq(mailNotice("ok", true), null, "ok has no notice");
eq(mailNotice("off", false)?.admin, false, "off for a member");
eq(mailNotice("off", true)?.text.includes("mail.enabled"), true, "off names the key");
eq(mailNotice("no_domain", true), { text: mailNotice("no_domain", false)!.text, admin: true }, "admins get the link");
eq(mailNotice("no_domain", false)?.text.includes("mail.domain"), true, "no_domain names the key");

eq(senderName('"Ada Lovelace" <ada@example.com>', "ada@example.com"), "Ada Lovelace", "quoted name");
eq(senderName("Shop <shop@example.com>", "shop@example.com"), "Shop", "bare name");
eq(senderName("<ada@example.com>", "ada@example.com"), "ada@example.com", "address only");
eq(senderName("", ""), "Unknown sender", "nothing at all");
eq(senderName("a@example.com, b@example.com", ""), "a@example.com, b@example.com", "several authors as written");

eq(wakeListError(false, ""), "", "off and empty");
eq(wakeListError(true, "") !== "", true, "on for nobody");
eq(wakeListError(true, "  \n "), wakeListError(true, ""), "blank lines are nobody");
eq(wakeListError(true, "boss@example.com\n@partner.example.org, example.net"), "", "addresses and domains");
eq(wakeListError(true, "not an address").includes("not an address"), true, "names the bad entry");
eq(wakeListError(false, "a@b@c.com") !== "", true, "two @");
eq(wakeListError(true, "boss@localhost") !== "", true, "no real domain");

eq(rawMailHref("b 1", "m&2"), "/mail/raw?bot_id=b%201&id=m%262", "raw link is escaped");

console.log("ok");
