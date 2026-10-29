package engine_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/manzil-infinity180/xuse/browser"
	"github.com/manzil-infinity180/xuse/engine"
)

func serve(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.FileServer(http.Dir("../testdata")))
	t.Cleanup(srv.Close)
	return srv.URL
}

func newPage(t *testing.T) *engine.Page {
	t.Helper()
	ctx, cancel := browser.Ephemeral(true)
	t.Cleanup(cancel)
	p := engine.New(ctx)
	p.Timeout = 2 * time.Second
	return p
}

func ref(t *testing.T, p *engine.Page, want string) string {
	t.Helper()
	s, err := p.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range s.Lines {
		if strings.Contains(l, want) && strings.HasPrefix(l, "[") {
			return l[1:strings.Index(l, "]")]
		}
	}
	t.Fatalf("no %q in:\n%s", want, s)
	return ""
}

// An alert must not block the page, and its text counts for ExpectText.
func TestDialogIsAcceptedAndExpectable(t *testing.T) {
	base := serve(t)
	p := newPage(t)
	if err := p.Goto(base + "/products.html"); err != nil {
		t.Fatal(err)
	}
	if err := p.Click(ref(t, p, `button "Save product"`)); err != nil {
		t.Fatal(err)
	}
	if err := p.ExpectText("name required"); err != nil {
		t.Fatal(err)
	}
	// The page still responds after the dialog.
	if err := p.Fill(ref(t, p, `textbox "Product name"`), "ok"); err != nil {
		t.Fatal(err)
	}
}

func TestFillRejectsWrongFields(t *testing.T) {
	base := serve(t)
	p := newPage(t)
	if err := p.Goto(base + "/products.html"); err != nil {
		t.Fatal(err)
	}
	if err := p.Fill(ref(t, p, `combobox "Status"`), "x"); err == nil || !strings.Contains(err.Error(), "not a text field") {
		t.Fatalf("fill on a select: got %v", err)
	}
	if err := p.Fill(ref(t, p, `spinbutton "Price"`), "abc"); err == nil {
		t.Fatal("text into a number field should fail, not pass silently")
	}
}

// A login saved from one browser restores cookies and localStorage in a fresh one.
func TestSaveAndLoadState(t *testing.T) {
	base := serve(t)
	a := newPage(t)
	if err := a.Goto(base + "/login.html"); err != nil {
		t.Fatal(err)
	}
	if err := a.Click(ref(t, a, `button "Log in"`)); err != nil {
		t.Fatal(err)
	}
	if err := a.ExpectText("Welcome back"); err != nil {
		t.Fatal(err)
	}
	st, err := a.SaveState()
	if err != nil {
		t.Fatal(err)
	}

	b := newPage(t)
	if err := b.LoadState(st); err != nil {
		t.Fatal(err)
	}
	if err := b.Goto(base + "/login.html"); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Welcome back", "theme: dark"} {
		if err := b.ExpectText(want); err != nil {
			t.Errorf("fresh browser with state: %v", err)
		}
	}
}

// TestSnapshotIsSmall logs how much an agent reads per page with each approach.
func TestSnapshotIsSmall(t *testing.T) {
	base := serve(t)
	p := newPage(t)
	for _, page := range []string{"/products.html", "/catalog.html"} {
		if err := p.Goto(base + page); err != nil {
			t.Fatal(err)
		}
		s, err := p.Stats()
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("%-15s elements=%3d  xuse=%6dB (~%5d tok)  ax-tree=%7dB (~%6d tok)  html=%7dB (~%6d tok)",
			page, s.Elements, s.SnapshotBytes, engine.Tokens(s.SnapshotBytes),
			s.AXTreeBytes, engine.Tokens(s.AXTreeBytes), s.HTMLBytes, engine.Tokens(s.HTMLBytes))
		if s.SnapshotBytes*5 > s.AXTreeBytes {
			t.Errorf("%s: snapshot (%dB) should be far smaller than the AX tree (%dB)", page, s.SnapshotBytes, s.AXTreeBytes)
		}
	}
}
