import { eq } from "../testing.ts";
import { contextFill, fmtTokens, growHeight, levelBars, micTitle, placeholderFor } from "./model.ts";

eq([fmtTokens(0), fmtTokens(850), fmtTokens(1500), fmtTokens(9999), fmtTokens(12000)], ["0", "850", "1.5k", "10.0k", "12k"], "token counts");

const fill = (input: number, output: number, window: number) => contextFill({ input, output, window });
eq(fill(100, 50, 1000).pct, 15, "percent of the window");
eq(fill(100, 50, 1000).tone, "text-ink-3", "calm below 70%");
eq([fill(700, 0, 1000).tone, fill(700, 0, 1000).barTone], ["text-ink", "bg-ink"], "darker from 70%");
eq([fill(900, 0, 1000).tone, fill(900, 0, 1000).barTone], ["text-vermilion", "bg-vermilion"], "red from 90%");
eq([fill(5000, 0, 1000).frac, fill(5000, 0, 1000).pct], [1, 100], "never past full");

eq(micTitle(false, "idle"), "Dictation needs https or localhost and a browser that can record", "unsupported wins");
eq([micTitle(true, "recording"), micTitle(true, "transcribing"), micTitle(true, "idle")], ["Stop and transcribe", "Transcribing…", "Dictate"], "by state");

eq(levelBars(0), [3, 3, 3, 3, 3], "silence is the floor");
eq(levelBars(1), [8, 14, 14, 14, 8], "loud caps at 14, the ends are smaller");
eq(levelBars(0.4).length, 5, "five bars");

eq([growHeight(0), growHeight(80), growHeight(500)], [26, 80, 168], "clamped to 26..168");

eq(placeholderFor(undefined, "Atlas"), "Select or create a chat to begin…", "no chat");
eq(placeholderFor("c1", "Atlas"), "Ask Atlas…", "named bot");
eq(placeholderFor("c1"), "Ask this Bot…", "unnamed bot");
