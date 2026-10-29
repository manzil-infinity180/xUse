# xUse

**Record a browser flow once. Replay it with 0 LLM tokens.**

xUse is a small, fast browser driver in Go, built to be the hands of an LLM agent
(Claude Code, Cursor, any shell-capable agent). The agent explores a flow once with
short CLI commands. xUse records it as a **route**. After that, the flow replays
deterministically, with different values each time, and no model in the loop.

```
first time (agent drives)                 every time after (no LLM)
xuse snap                                 xuse run create-product name="blue t-shirt"
xuse fill e1 "RahulXF"                      1 ok goto   …/products        136ms
xuse click e4                               2 ok fill   textbox "Product name"
xuse expect "Created RahulXF"               …
xuse record stop --param name=RahulXF       PASS 6/6 steps in 864ms · 0 LLM tokens
```

## Why

Browser agents today pay for every step twice: an LLM call to decide, and a large page
dump (screenshot or full accessibility tree) to decide from. Most flows are the same
every time. xUse makes the first run cheap and every later run free:

- **Compact snapshots**: one line per interactive element, e.g. `[e3] textbox "Product name"`.
  No screenshots, and no snapshot after every action unless you ask (`--snap`).
- **Fast commands**: one long-lived Chrome; each command is a Go binary that attaches over
  CDP in milliseconds.
- **Stable locators**: every recorded step stores several ways to find the element
  (data-testid → id → name attribute → role + accessible name → placeholder → CSS), so
  replays survive UI changes and report when they fall back.
- **Real input**: clicks are real mouse events at the element's centre, and text goes in
  through `Input.insertText`, so frameworks see what a user would do.
- **Parameters**: recorded values become `{{params}}`; replay with any values.
- **Repair, not rewrite**: when a replay can't find an element it stops, prints the page,
  and `xuse repair` re-points only that locator. Values and expectations never change.

## Install

```bash
go install github.com/manzil-infinity180/xuse/cmd/xuse@latest
```

Needs Chrome or Chromium. Set `XUSE_CHROME` to use a specific binary.

## Quickstart

```bash
xuse start --headless                 # or without --headless to watch
xuse record start create-product
xuse goto https://example.test/products/new
xuse snap
# page: New product — https://example.test/products/new
# [e1] textbox "Product name"
# [e2] spinbutton "Price"
# [e3] combobox "Status" value="Draft"
# [e4] button "Save product"
xuse fill e1 RahulXF
xuse fill e2 10
xuse select e3 Active
xuse click e4
xuse expect "Created RahulXF"
xuse record stop --param name=RahulXF --param price=10

xuse run create-product name="blue t-shirt" price=25   # attaches to the running browser
xuse run create-product name="red mug" --new            # fresh headless browser (CI)
xuse stop
```

Routes are plain JSON in `.xuse/routes/`, so commit them with your project.

## Commands

| Command | What it does |
|---|---|
| `start [--headless]` / `stop` | launch / close the shared browser |
| `goto <url>` | navigate and wait for the page to settle |
| `snap` | compact snapshot with refs |
| `click <ref>` · `fill <ref> <value>` · `select <ref> <value>` · `press <key>` | act, then wait for the DOM to go quiet (`--snap` prints a snapshot after) |
| `expect <text>` | wait until the page shows text |
| `record start <name>` · `record stop [--param k=v]…` · `record status` | record actions into a route |
| `run <route> [k=v]… [--new] [--headed]` | replay; exit code 2 on a failed step |
| `routes` | list routes and their params |
| `repair <route> <step> <ref>` | re-point one step at an element on the current page |

## Status

Early. Plan 1 (record + replay) works and is tested headless (`go test ./...`).
Not benchmarked against Chrome DevTools MCP / Playwright MCP yet; that is the
next slice, and numbers will go here, honestly.

- [Plan 1: record and replay](docs/plans/plan-1-record-replay.md)
- [Plan 2: what's next](docs/plans/plan-2-next.md)

## License

Apache-2.0
