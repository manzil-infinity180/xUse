package engine

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"

	"github.com/chromedp/cdproto/accessibility"
	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/cdproto/storage"
	"github.com/chromedp/chromedp"
)

// ---- page size, as different agents would see it ---------------------------

// Stats compares what an agent would read for the same page.
type Stats struct {
	URL           string `json:"url"`
	SnapshotBytes int    `json:"snapshotBytes"` // xUse snapshot
	AXTreeBytes   int    `json:"axTreeBytes"`   // full CDP accessibility tree (JSON)
	HTMLBytes     int    `json:"htmlBytes"`     // document.documentElement.outerHTML
	Elements      int    `json:"elements"`      // snapshot lines
}

// Tokens is a rough token estimate (≈4 bytes per token).
func Tokens(bytes int) int { return (bytes + 3) / 4 }

func (p *Page) Stats() (*Stats, error) {
	s, err := p.Snapshot()
	if err != nil {
		return nil, err
	}
	ax, err := chromedp.Call(p.ctx, accessibility.GetFullAXTree, accessibility.GetFullAXTreeParams{})
	if err != nil {
		return nil, fmt.Errorf("accessibility tree: %w", err)
	}
	axJSON, _ := json.Marshal(ax.Nodes)
	html, err := eval[int](p, "document.documentElement.outerHTML.length")
	if err != nil {
		return nil, err
	}
	return &Stats{
		URL: s.URL, SnapshotBytes: len(s.String()), AXTreeBytes: len(axJSON),
		HTMLBytes: html, Elements: len(s.Lines),
	}, nil
}

// ---- element signatures (for maps and diffs) --------------------------------

var (
	refPrefix = regexp.MustCompile(`^\[e\d+\] `)
	valueAttr = regexp.MustCompile(` value="[^"]*"`)
)

// Signature turns a snapshot line into a stable description without refs or values,
// e.g. `[e3] textbox "Name" value="x"` → `textbox "Name"`.
func Signature(line string) string {
	return valueAttr.ReplaceAllString(refPrefix.ReplaceAllString(line, ""), "")
}

// Links returns same-origin link targets on the page (no fragments, deduplicated).
func (p *Page) Links() ([]string, error) {
	return eval[[]string](p, `(() => {
  const out = new Set();
  for (const a of document.querySelectorAll('a[href]')) {
    try {
      const u = new URL(a.getAttribute('href'), location.href);
      if (u.origin !== location.origin) continue;
      u.hash = '';
      out.add(u.href);
    } catch (_) {}
  }
  return [...out];
})()`)
}

// ---- login state (cookies + localStorage) -----------------------------------

// State is a saved login: cookies for every site plus localStorage per origin.
type State struct {
	Cookies []*network.CookieParam       `json:"cookies"`
	Origins map[string]map[string]string `json:"origins,omitempty"`
}

// SaveState captures all cookies and the localStorage of the current page's origin.
func (p *Page) SaveState() (*State, error) {
	res, err := chromedp.Call(p.ctx, storage.GetCookies, storage.GetCookiesParams{})
	if err != nil {
		return nil, err
	}
	st := &State{Origins: map[string]map[string]string{}}
	for _, c := range res.Cookies {
		cp := &network.CookieParam{
			Name: c.Name, Value: c.Value, Domain: c.Domain, Path: c.Path,
			Secure: c.Secure, HTTPOnly: c.HTTPOnly, SameSite: c.SameSite,
		}
		if !c.Session && c.Expires > 0 {
			cp.Expires = cdp.TimeSinceEpoch(c.Expires)
		}
		st.Cookies = append(st.Cookies, cp)
	}
	type origin struct {
		Origin string            `json:"origin"`
		Items  map[string]string `json:"items"`
	}
	o, err := eval[origin](p, `({origin: location.origin, items: Object.fromEntries(Object.entries(localStorage))})`)
	if err == nil && o.Origin != "null" && len(o.Items) > 0 {
		st.Origins[o.Origin] = o.Items
	}
	return st, nil
}

// LoadState sets the cookies now and restores localStorage before any page script
// runs on a matching origin. Call it before the first Goto.
func (p *Page) LoadState(st *State) error {
	if len(st.Cookies) > 0 {
		if _, err := chromedp.Call(p.ctx, storage.SetCookies, storage.SetCookiesParams{Cookies: st.Cookies}); err != nil {
			return fmt.Errorf("set cookies: %w", err)
		}
	}
	for origin, items := range st.Origins {
		if _, err := url.Parse(origin); err != nil {
			return fmt.Errorf("bad origin %q", origin)
		}
		o, _ := json.Marshal(origin)
		kv, _ := json.Marshal(items)
		src := fmt.Sprintf(`if (location.origin === %s) { for (const [k, v] of Object.entries(%s)) { if (localStorage.getItem(k) === null) localStorage.setItem(k, v); } }`, o, kv)
		if _, err := chromedp.Call(p.ctx, page.AddScriptToEvaluateOnNewDocument, page.AddScriptToEvaluateOnNewDocumentParams{Source: src}); err != nil {
			return fmt.Errorf("restore localStorage: %w", err)
		}
	}
	return nil
}
