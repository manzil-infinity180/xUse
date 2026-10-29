# Plan 1 — record once, replay at 0 tokens

Status: done (record, replay, params, fallback, repair; tested headless)

## Goal

A fast browser agent in Go. An LLM (e.g. Claude Code) drives the browser once
through small CLI commands. xUse records that as a **route**. Replaying the
route — even with different values — needs no LLM at all.

```
first time (LLM drives)                   later (no LLM)
xuse snap  → compact refs                 xuse run create-product name="blue t-shirt"
xuse fill e3 "RahulXF"                      ├─ find each element by its saved locator
xuse click e7                               ├─ act, auto-wait
xuse expect "Created RahulXF"               └─ PASS 5/5 in ~400ms · 0 tokens
xuse record stop --param name=RahulXF       on failure: print step + snapshot
        └─► .xuse/routes/create-product.json  → LLM picks the right ref → xuse repair
```

## Why it should beat Chrome DevTools MCP / Playwright MCP

- Snapshot is one line per interactive element (`[e3] textbox "Product name"`):
  no screenshots, no full accessibility tree.
- Actions do not return a snapshot unless asked.
- A Go binary attaches to an already-running Chrome over CDP: milliseconds per command.
- Replay runs no LLM at all.

## Scope

1. `xuse start [--headless]` / `xuse stop` — one long-lived Chrome; every command attaches to it.
2. `xuse snap` — visible interactive elements (shadow DOM too) + headings, with refs `e1, e2…`.
3. Actions: `goto`, `click`, `fill`, `select`, `press`, `expect <text>`. Real mouse
   events and text input; after each action wait until the DOM goes quiet.
4. `xuse record start <name>` / `record stop --param k=v` — every action is saved with a
   stable locator (data-testid → id → name attr → role+name → placeholder → css);
   recorded literals become `{{k}}`.
5. `xuse run <route> k=v …` — **replay**: wait for each element, act, settle; per-step timing;
   locator fallbacks; on failure exit 2 with the failing step and a snapshot.
6. `xuse repair <route> <step> <ref>` — re-point a step's locator. Never changes values or expectations.
7. Tests: headless Go tests on `testdata/products.html` (slow fake API): record → replay
   with a new name; a broken testid falls back to role+name.

## Layout

```
cmd/xuse/   CLI
browser/    start / attach / stop Chrome, session file
engine/     injected JS (snapshot, locators, find, settle) + actions
route/      route format, params, replay, repair
testdata/   fixture pages
```
