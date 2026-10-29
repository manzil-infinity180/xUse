# Plan 2 — next slices (not started)

Ordered by value. Each is a separate small slice.

1. **Benchmark vs Chrome DevTools MCP / Playwright MCP** — same flow, measure wall time
   and tokens per step (snapshot bytes) for: first run (LLM-driven) and replay.
   Publish the numbers in the README, honestly.
2. **Reusable login** — keep a browser profile / storage state per app so routes start
   logged in; a `login` route that runs first when the session is stale.
3. **Data-driven runs** — `xuse run route --vars cases.csv` runs one route over many inputs
   (good + bad cases), reports a table.
4. **Repair with lessons** — when a locator falls back or a repair happens, store a lesson
   (`what broke → what fixed it`) and prefer that strategy next time.
5. **Explore + app map** — LLM explores at the frontier only; screens and actions become a
   state graph; routes are paths in that graph.
6. **Map diff** — compare the map of two builds (main vs branch) to spot UI changes.
7. **MCP wrapper** — expose the same commands as MCP tools for clients without a shell.
8. **Packs** — app-specific add-ons (auth, routes, verifiers) live in their own repos and
   import xUse as a Go module.
