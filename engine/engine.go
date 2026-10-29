// Package engine drives one browser page: compact snapshots, stable locators,
// and actions that behave like a user (real mouse clicks and text input).
package engine

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"
)

//go:embed inject.js
var injectJS string

// ErrStaleRef means the ref no longer points at an element (page changed); take a new snapshot.
var ErrStaleRef = errors.New("ref not found on the page; run snap again")

// Locator describes an element so it can be found again after the page reloads.
// Fields are tried in order: testid, id, nameAttr, role+name, placeholder, css.
type Locator struct {
	TestIDAttr  string `json:"testidAttr,omitempty"`
	TestID      string `json:"testid,omitempty"`
	ID          string `json:"id,omitempty"`
	NameAttr    string `json:"nameAttr,omitempty"`
	Role        string `json:"role,omitempty"`
	Name        string `json:"name,omitempty"`
	Placeholder string `json:"placeholder,omitempty"`
	Tag         string `json:"tag,omitempty"`
	CSS         string `json:"css,omitempty"`
	// Via is the strategy that matched when the locator was recorded.
	Via string `json:"via,omitempty"`
}

// String is a short human label, e.g. `textbox "Product name"`.
func (l *Locator) String() string {
	if l == nil {
		return ""
	}
	s := l.Role
	if s == "" {
		s = l.Tag
	}
	if l.Name != "" {
		s += fmt.Sprintf(" %q", l.Name)
	}
	return s
}

// Snapshot is the compact, LLM-friendly view of a page.
type Snapshot struct {
	URL   string   `json:"url"`
	Title string   `json:"title"`
	Lines []string `json:"lines"`
}

func (s *Snapshot) String() string {
	return fmt.Sprintf("page: %s — %s\n%s", s.Title, s.URL, strings.Join(s.Lines, "\n"))
}

// Page wraps a chromedp context that points at one tab.
type Page struct {
	ctx context.Context
	// Timeout bounds waiting for an element or text to appear.
	Timeout time.Duration
	// Quiet is how long the DOM must stay unchanged after an action. The next step
	// waits for its own element anyway, so this only needs to cover short re-renders.
	Quiet time.Duration

	mu      sync.Mutex
	dialogs []string // messages of alert/confirm/prompt dialogs, auto-accepted
}

// New wraps ctx. It accepts JavaScript dialogs (alert, confirm, prompt) as they open,
// so a page never blocks the run; their text counts for ExpectText.
func New(ctx context.Context) *Page {
	p := &Page{ctx: ctx, Timeout: 5 * time.Second, Quiet: 50 * time.Millisecond}
	events := chromedp.Events(ctx, page.JavascriptDialogOpening)
	go func() {
		for ev, err := range events {
			if err != nil {
				return
			}
			p.mu.Lock()
			p.dialogs = append(p.dialogs, ev.Message)
			p.mu.Unlock()
			_, _ = chromedp.Call(ctx, page.HandleJavaScriptDialog, page.HandleJavaScriptDialogParams{Accept: true})
		}
	}()
	return p
}

func (p *Page) sawDialog(text string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, m := range p.dialogs {
		if strings.Contains(m, text) {
			return true
		}
	}
	return false
}

// js builds a call like __xuse.fn("a", 1) with JSON-encoded args.
func js(fn string, args ...any) string {
	parts := make([]string, len(args))
	for i, a := range args {
		b, _ := json.Marshal(a)
		parts[i] = string(b)
	}
	return fmt.Sprintf("__xuse.%s(%s)", fn, strings.Join(parts, ","))
}

func eval[T any](p *Page, expr string) (T, error) {
	return chromedp.Run(p.ctx, chromedp.Evaluate[T](injectJS+";\n"+expr, chromedp.EvalAwaitPromise))
}

func (p *Page) timeoutMS() int { return int(p.Timeout / time.Millisecond) }

// Goto navigates and waits for the page to settle.
func (p *Page) Goto(url string) error {
	p.mu.Lock()
	p.dialogs = nil // a new page starts with no dialogs seen
	p.mu.Unlock()
	if _, err := chromedp.Run(p.ctx, chromedp.Navigate(url)); err != nil {
		return err
	}
	return p.Settle()
}

// Snapshot returns visible interactive elements and headings, each element with a ref.
func (p *Page) Snapshot() (*Snapshot, error) {
	return eval[*Snapshot](p, js("snapshot", 300))
}

// LocatorOf builds a stable locator for the element behind ref.
func (p *Page) LocatorOf(ref string) (*Locator, error) {
	l, err := eval[*Locator](p, js("locator", ref))
	if err == nil && l == nil {
		err = ErrStaleRef
	}
	return l, err
}

type findHit struct {
	Ref string `json:"ref"`
	Via string `json:"via"`
}

type point struct {
	X     float64 `json:"x"`
	Y     float64 `json:"y"`
	OnTop bool    `json:"onTop"`
}

// Find waits until the locator matches exactly one visible element and returns its ref
// and the strategy that matched.
func (p *Page) Find(l *Locator) (ref, via string, err error) {
	hit, err := eval[*findHit](p, js("find", l, p.timeoutMS()))
	if err != nil {
		return "", "", err
	}
	if hit == nil {
		return "", "", fmt.Errorf("element not found within %s: %s", p.Timeout, l)
	}
	return hit.Ref, hit.Via, nil
}

// Click clicks the element with a real mouse event at its centre; if something covers
// it, it falls back to a DOM click.
func (p *Page) Click(ref string) error {
	pt, err := eval[*point](p, js("point", ref))
	if err != nil {
		return err
	}
	if pt == nil {
		return ErrStaleRef
	}
	if pt.OnTop {
		_, err = chromedp.Run(p.ctx, chromedp.MouseClickXY(pt.X, pt.Y))
	} else {
		_, err = eval[bool](p, js("click", ref))
	}
	if err != nil {
		return err
	}
	return p.Settle()
}

// Fill replaces the field's value, typing it as real text input.
func (p *Page) Fill(ref, value string) error {
	msg, err := eval[string](p, js("clearAndFocus", ref))
	if err != nil {
		return err
	}
	if msg == "stale" {
		return ErrStaleRef
	}
	if msg != "" {
		return errors.New(msg)
	}
	if value != "" {
		if _, err := chromedp.Call(p.ctx, input.InsertText, input.InsertTextParams{Text: value}); err != nil {
			return err
		}
	}
	got, err := eval[*string](p, js("changed", ref))
	if err != nil {
		return err
	}
	// A field can reject input (e.g. text into type=number); say so instead of passing.
	if got != nil && *got != value {
		return fmt.Errorf("field holds %q after typing %q", *got, value)
	}
	return p.Settle()
}

// Select picks an option of a native <select> by value or visible text.
func (p *Page) Select(ref, value string) error {
	msg, err := eval[string](p, js("select", ref, value))
	if err != nil {
		return err
	}
	if msg != "" {
		return errors.New(msg)
	}
	return p.Settle()
}

var keys = map[string]string{
	"enter": kb.Enter, "tab": kb.Tab, "escape": kb.Escape, "esc": kb.Escape,
	"backspace": kb.Backspace, "arrowdown": kb.ArrowDown, "arrowup": kb.ArrowUp,
	"arrowleft": kb.ArrowLeft, "arrowright": kb.ArrowRight, "space": " ",
}

// Press sends a key to the focused element, e.g. "Enter", "Tab", "Escape".
func (p *Page) Press(key string) error {
	k, ok := keys[strings.ToLower(key)]
	if !ok {
		k = key
	}
	if _, err := chromedp.Run(p.ctx, chromedp.KeyEvent(k)); err != nil {
		return err
	}
	return p.Settle()
}

// ExpectText waits until the page body, or a dialog the page opened, contains text.
func (p *Page) ExpectText(text string) error {
	if p.sawDialog(text) {
		return nil
	}
	ok, err := eval[bool](p, js("hasText", text, p.timeoutMS()))
	if err != nil {
		return err
	}
	if !ok && !p.sawDialog(text) {
		return fmt.Errorf("text %q not on the page within %s", text, p.Timeout)
	}
	return nil
}

// Settle waits until the DOM has been quiet briefly. A click that navigates destroys
// the JS context mid-wait, so retry until the new document answers.
func (p *Page) Settle() error {
	deadline := time.Now().Add(10 * time.Second)
	for {
		_, err := eval[int](p, js("settle", int(p.Quiet/time.Millisecond), 3000))
		if err == nil {
			return nil
		}
		if time.Now().After(deadline) || !transient(err) {
			return err
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func transient(err error) bool {
	s := err.Error()
	for _, m := range []string{"context was destroyed", "Cannot find context", "navigated", "Inspected target", "uniqueContextId"} {
		if strings.Contains(s, m) {
			return true
		}
	}
	return false
}
