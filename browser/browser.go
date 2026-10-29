// Package browser starts one long-lived Chrome and lets short CLI commands attach to it.
package browser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/chromedp/cdproto"
	"github.com/chromedp/cdproto/browser"
	"github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/remote"
)

// Session is the running browser, saved to disk so every command can attach.
type Session struct {
	WS       string `json:"ws"`
	Dir      string `json:"dir"`
	Headless bool   `json:"headless"`
}

// Home is the state directory: $XUSE_HOME or ~/.xuse.
func Home() string {
	if h := os.Getenv("XUSE_HOME"); h != "" {
		return h
	}
	d, err := os.UserHomeDir()
	if err != nil {
		return ".xuse"
	}
	return filepath.Join(d, ".xuse")
}

func sessionFile() string { return filepath.Join(Home(), "session.json") }

func options(headless bool) []chromedp.ExecAllocatorOption {
	opts := append([]chromedp.ExecAllocatorOption{}, chromedp.DefaultExecAllocatorOptions[:]...)
	if !headless {
		opts = append(opts, chromedp.Flag("headless", false))
	}
	if p := os.Getenv("XUSE_CHROME"); p != "" {
		opts = append(opts, chromedp.ExecPath(p))
	}
	return opts
}

// Start launches Chrome that outlives this process, or returns the running one.
func Start(headless bool) (*Session, error) {
	if s, err := Load(); err == nil && s.Alive() {
		return s, nil
	}
	dir := filepath.Join(Home(), "profile")
	opts := append(options(headless), chromedp.UserDataDir(dir), chromedp.KeepOpen, remote.WebSocket)
	alloc, _ := chromedp.NewExecAllocator(context.Background(), opts...)
	ctx, _ := chromedp.NewContext(alloc)
	if _, err := chromedp.Run(ctx, chromedp.Evaluate[chromedp.Void]("1")); err != nil {
		return nil, fmt.Errorf("start chrome: %w", err)
	}
	ws, _ := chromedp.KeptOpen(ctx)
	if ws == "" {
		return nil, errors.New("start chrome: no websocket address")
	}
	s := &Session{WS: ws, Dir: dir, Headless: headless}
	b, _ := json.MarshalIndent(s, "", "  ")
	if err := os.WriteFile(sessionFile(), b, 0o600); err != nil {
		return nil, err
	}
	return s, nil
}

// Load reads the saved session.
func Load() (*Session, error) {
	b, err := os.ReadFile(sessionFile())
	if err != nil {
		return nil, errors.New("no browser running; run: xuse start")
	}
	var s Session
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

// httpBase turns ws://127.0.0.1:PORT/devtools/browser/ID into http://127.0.0.1:PORT.
func (s *Session) httpBase() string {
	u, err := url.Parse(s.WS)
	if err != nil {
		return ""
	}
	return "http://" + u.Host
}

// Alive reports whether the browser still answers.
func (s *Session) Alive() bool {
	c := http.Client{Timeout: 500 * time.Millisecond}
	resp, err := c.Get(s.httpBase() + "/json/version")
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// pageTarget returns the first normal tab, opening one if none is left.
func (s *Session) pageTarget() (string, error) {
	c := http.Client{Timeout: 2 * time.Second}
	resp, err := c.Get(s.httpBase() + "/json/list")
	if err != nil {
		return "", fmt.Errorf("browser not reachable (%v); run: xuse start", err)
	}
	defer resp.Body.Close()
	var list []struct{ ID, Type, URL string }
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		return "", err
	}
	for _, t := range list {
		if t.Type == "page" {
			return t.ID, nil
		}
	}
	req, _ := http.NewRequest(http.MethodPut, s.httpBase()+"/json/new?about:blank", nil)
	resp2, err := c.Do(req)
	if err != nil {
		return "", err
	}
	defer resp2.Body.Close()
	var t struct{ ID string }
	err = json.NewDecoder(resp2.Body).Decode(&t)
	return t.ID, err
}

// Attach returns a context bound to the session's tab. Do not cancel it: cancelling the
// first context of a browser closes that browser. The connection ends with the process.
func Attach() (context.Context, error) {
	s, err := Load()
	if err != nil {
		return nil, err
	}
	id, err := s.pageTarget()
	if err != nil {
		return nil, err
	}
	alloc, _ := remote.NewAllocator(context.Background(), s.WS)
	ctx, _ := chromedp.NewContext(alloc, chromedp.WithTargetID(target.ID(id)))
	return ctx, nil
}

// Stop closes the session's browser and forgets it.
func Stop() error {
	s, err := Load()
	if err != nil {
		return err
	}
	defer os.Remove(sessionFile())
	if !s.Alive() {
		return nil
	}
	// chromedp will not close a browser it did not start, so send Browser.close directly.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := remote.DialContext(ctx, s.WS)
	if err != nil {
		return err
	}
	defer conn.Close()
	if err := conn.Write(ctx, &cdproto.Message{ID: 1, Method: browser.CommandClose}); err != nil {
		return err
	}
	var msg cdproto.Message
	return conn.Read(ctx, &msg)
}

// Ephemeral starts a throwaway browser for one run; cancel closes it.
func Ephemeral(headless bool) (context.Context, context.CancelFunc) {
	alloc, cancelAlloc := chromedp.NewExecAllocator(context.Background(), options(headless)...)
	ctx, cancel := chromedp.NewContext(alloc)
	return ctx, func() { cancel(); cancelAlloc() }
}
