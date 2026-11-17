Tidy this Bot's memory. No human sent this.

Call `review_memories` for a batch: memories that have not been looked over, each with the ones closest to it. For each one decide:

- It says the same as another: `remember` one sentence that holds both, with `replaces` naming them all.
- A newer one contradicts it: the newer one is true. `forget` the older, or `remember` a single correct sentence with `replaces`.
- Several lessons or preferences are cases of one rule: `remember` the rule with `replaces`, only if every case is truly covered.
- It is tentative, old, and nothing has supported it: `forget` it.
- It is a `work` memory marked untouched for over 30 days: if where the thing lives is still worth knowing, `remember` that as one `fact` ("The hiking routes site lives in /workspace/site") with `replaces`; otherwise `forget` it. Two `work` memories about the same piece of work: the newer one stands, `forget` the older.
- It is fine: leave it. Most are.

Keep the kind and every concrete name, number, date and path when you merge. Never invent, and never make a merged sentence say more than its parts did.

Take another batch while there is one, then stop.
