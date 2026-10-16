Dashboard sweep. No human sent this; you are curating the owner's Dashboard.

1. **Read state first:**
   - Read `bot/DASHBOARD.md` with `read` (if it does not exist yet, create it with `write` using default local checks: workspace hygiene, recent workspace changes, unfinished tasks or threads in CORE MEMORY, and documentation drift — never assume external accounts unless configured there).
   - Call `silo_runtime.list_dashboard()` in `exec_python` to inspect the cards currently on the Dashboard and any recent dismissals.
2. **Diff against reality:**
   - Check the local sources and rules listed in `bot/DASHBOARD.md`.
   - Compare what you find against the notes in `bot/DASHBOARD.md` and the live cards from `list_dashboard()`.
   - Respect `recent_dismissals` and lessons in long-term memory: do not re-propose a card the human dismissed unless the underlying state materially changed, and never re-propose a suppressed topic.
3. **Edit gradually:**
   - Remove cards whose underlying issue is already resolved with `silo_runtime.remove_dashboard_card(id_or_key)`.
   - Update existing cards in place when numbers or details changed, or add new cards when something genuinely needs the human's decision, using `silo_runtime.upsert_dashboard_card(key=..., title=..., summary=..., priority=..., blocks=[...], submit_label=...)`.
   - Use modular display blocks (`markdown`, `kv`, `metrics`, `diff`) and interactive form controls (`switch`, `select`, `segmented`, `checkboxes`, `input`, `textarea`) plus `submit_label` so the human can configure options and send their decision in one click.
   - Leave unchanged cards alone. If nothing needs the human, leave the Dashboard empty.
4. **Save state:**
   - Update `bot/DASHBOARD.md` with `patch` (or `write`) to record what you checked, updated cursors/timestamps, and the rationale for any card changes.
   - Reply with just `DASHBOARD_OK` when done.
