// Package appmap crawls an app into a map of pages and their elements, and diffs two
// maps (e.g. main vs a branch build) to show what changed in the UI.
package appmap

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"sort"
	"strings"

	"github.com/manzil-infinity180/xuse/engine"
)

// Page is one screen: its path relative to the root, title, and element signatures.
type Page struct {
	Path     string   `json:"path"`
	Title    string   `json:"title"`
	Elements []string `json:"elements"`
	Links    []string `json:"links"`
}

// Map is the crawled app. Pages are keyed by path so maps of different hosts compare.
type Map struct {
	Root  string `json:"root"`
	Pages []Page `json:"pages"`
}

// rel makes u relative to root ("/products.html", "/orders?id=1").
func rel(root, u *url.URL) string {
	p := u.Path
	if dir := root.Path[:strings.LastIndex(root.Path, "/")+1]; strings.HasPrefix(p, dir) {
		p = "/" + strings.TrimPrefix(p, dir)
	}
	if u.RawQuery != "" {
		p += "?" + u.RawQuery
	}
	return p
}

// Crawl visits same-origin links breadth-first from root, up to max pages.
// It only follows links, so it never submits forms or clicks buttons.
func Crawl(p *engine.Page, root string, max int, out io.Writer) (*Map, error) {
	ru, err := url.Parse(root)
	if err != nil {
		return nil, err
	}
	m := &Map{Root: root}
	seen := map[string]bool{root: true}
	queue := []string{root}
	for len(queue) > 0 && len(m.Pages) < max {
		u := queue[0]
		queue = queue[1:]
		if err := p.Goto(u); err != nil {
			fmt.Fprintf(out, "skip %s: %v\n", u, err)
			continue
		}
		snap, err := p.Snapshot()
		if err != nil {
			return nil, err
		}
		links, err := p.Links()
		if err != nil {
			return nil, err
		}
		pu, _ := url.Parse(snap.URL)
		pg := Page{Path: rel(ru, pu), Title: snap.Title}
		for _, l := range snap.Lines {
			if strings.HasPrefix(l, "… ") {
				continue
			}
			pg.Elements = append(pg.Elements, engine.Signature(l))
		}
		for _, l := range links {
			lu, err := url.Parse(l)
			if err != nil {
				continue
			}
			pg.Links = append(pg.Links, rel(ru, lu))
			if !seen[l] {
				seen[l] = true
				queue = append(queue, l)
			}
		}
		sort.Strings(pg.Links)
		m.Pages = append(m.Pages, pg)
		fmt.Fprintf(out, "%-30s %3d elements  %d links\n", pg.Path, len(pg.Elements), len(pg.Links))
	}
	sort.Slice(m.Pages, func(i, j int) bool { return m.Pages[i].Path < m.Pages[j].Path })
	return m, nil
}

// Change is a difference between two maps.
type Change struct {
	Kind string `json:"kind"` // page+, page-, element+, element-
	Path string `json:"path"`
	What string `json:"what,omitempty"`
}

func (c Change) String() string {
	if c.What == "" {
		return fmt.Sprintf("%-9s %s", c.Kind, c.Path)
	}
	return fmt.Sprintf("%-9s %s  %s", c.Kind, c.Path, c.What)
}

// Diff lists pages and elements added or removed going from a to b.
func Diff(a, b *Map) []Change {
	ia, ib := index(a), index(b)
	var out []Change
	for _, path := range keys(ia, ib) {
		pa, inA := ia[path]
		pb, inB := ib[path]
		switch {
		case !inA:
			out = append(out, Change{Kind: "page+", Path: path, What: pb.Title})
		case !inB:
			out = append(out, Change{Kind: "page-", Path: path, What: pa.Title})
		default:
			added, removed := setDiff(pa.Elements, pb.Elements)
			for _, e := range removed {
				out = append(out, Change{Kind: "element-", Path: path, What: e})
			}
			for _, e := range added {
				out = append(out, Change{Kind: "element+", Path: path, What: e})
			}
		}
	}
	return out
}

func index(m *Map) map[string]Page {
	ix := map[string]Page{}
	for _, p := range m.Pages {
		ix[p.Path] = p
	}
	return ix
}

func keys(a, b map[string]Page) []string {
	set := map[string]bool{}
	for k := range a {
		set[k] = true
	}
	for k := range b {
		set[k] = true
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// setDiff compares element lists as multisets (a page can have two "Edit" buttons).
func setDiff(a, b []string) (added, removed []string) {
	count := map[string]int{}
	for _, e := range a {
		count[e]++
	}
	for _, e := range b {
		count[e]--
	}
	for e, n := range count {
		for ; n > 0; n-- {
			removed = append(removed, e)
		}
		for ; n < 0; n++ {
			added = append(added, e)
		}
	}
	sort.Strings(added)
	sort.Strings(removed)
	return added, removed
}

func Load(path string) (*Map, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var m Map
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &m, nil
}

func Save(path string, m *Map) error {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}
