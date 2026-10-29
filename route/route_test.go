package route_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/manzil-infinity180/xuse/browser"
	"github.com/manzil-infinity180/xuse/engine"
	"github.com/manzil-infinity180/xuse/route"
)

func TestParameterizeAndRender(t *testing.T) {
	r := &route.Route{Name: "p", Steps: []route.Step{
		{Action: "fill", Target: &engine.Locator{Role: "textbox", Name: "Product name"}, Value: "RahulXF"},
		{Action: "expect", Text: "Created RahulXF at $10"},
	}}
	r.Parameterize(map[string]string{"name": "RahulXF", "price": "10"})
	if got := r.Steps[1].Text; got != "Created {{name}} at ${{price}}" {
		t.Fatalf("parameterized text = %q", got)
	}
	s, err := r.Render(r.Steps[1], map[string]string{"name": "blue t-shirt"})
	if err != nil {
		t.Fatal(err)
	}
	if s.Text != "Created blue t-shirt at $10" {
		t.Fatalf("rendered = %q (price should fall back to the recorded default)", s.Text)
	}
	if r.Steps[1].Text != "Created {{name}} at ${{price}}" {
		t.Fatal("Render must not modify the route")
	}
}

func TestRenderMissingParam(t *testing.T) {
	r := &route.Route{Steps: []route.Step{{Action: "expect", Text: "{{nope}}"}}}
	if _, err := r.Render(r.Steps[0], nil); err == nil {
		t.Fatal("want error for unknown param")
	}
}

// newPage serves testdata and returns a page on a fresh headless browser.
func newPage(t *testing.T) (*engine.Page, string) {
	t.Helper()
	srv := httptest.NewServer(http.FileServer(http.Dir("../testdata")))
	t.Cleanup(srv.Close)
	ctx, cancel := browser.Ephemeral(true)
	t.Cleanup(cancel)
	p := engine.New(ctx)
	p.Timeout = 3 * time.Second
	if err := p.Goto(srv.URL + "/products.html"); err != nil {
		t.Skipf("no usable Chrome: %v", err)
	}
	return p, srv.URL + "/products.html"
}

// refFor finds the snapshot ref of the line containing want.
func refFor(t *testing.T, p *engine.Page, want string) string {
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
	t.Fatalf("no %q in snapshot:\n%s", want, s)
	return ""
}

// TestRecordThenReplayWithNewValues drives the page like an LLM would (refs from a
// snapshot), records the steps, then replays them with a different product name.
func TestRecordThenReplayWithNewValues(t *testing.T) {
	p, url := newPage(t)

	r := &route.Route{Name: "create-product"}
	r.Steps = append(r.Steps, route.Step{Action: "goto", URL: url})
	live := func(action, want, value string) {
		ref := refFor(t, p, want)
		loc, err := p.LocatorOf(ref)
		if err != nil {
			t.Fatal(err)
		}
		if loc.Via != "testid" {
			t.Fatalf("%s: recorded via %q, want testid", want, loc.Via)
		}
		switch action {
		case "fill":
			err = p.Fill(ref, value)
		case "select":
			err = p.Select(ref, value)
		case "click":
			err = p.Click(ref)
		}
		if err != nil {
			t.Fatalf("%s %s: %v", action, want, err)
		}
		r.Steps = append(r.Steps, route.Step{Action: action, Target: loc, Value: value})
	}
	live("fill", `textbox "Product name"`, "RahulXF")
	live("fill", `spinbutton "Price"`, "10")
	live("select", `combobox "Status"`, "Active")
	live("click", `button "Save product"`, "")
	if err := p.ExpectText("Created RahulXF at $10 (active)"); err != nil {
		t.Fatal(err)
	}
	r.Steps = append(r.Steps, route.Step{Action: "expect", Text: "Created RahulXF at $10 (active)"})
	r.Parameterize(map[string]string{"name": "RahulXF", "price": "10"})

	var out bytes.Buffer
	err := route.Run(p, r, map[string]string{"name": "blue t-shirt", "price": "25"}, &out)
	t.Log("\n" + out.String())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "PASS 6/6") {
		t.Fatalf("unexpected output:\n%s", out.String())
	}
}

// TestFallbackWhenTestIDChanges: a stale testid must fall back to role+name.
func TestFallbackWhenTestIDChanges(t *testing.T) {
	p, url := newPage(t)
	r := &route.Route{Name: "fallback", Steps: []route.Step{
		{Action: "goto", URL: url},
		{Action: "fill", Value: "mug", Target: &engine.Locator{
			TestIDAttr: "data-testid", TestID: "old-name-field", Role: "textbox", Name: "Product name", Via: "testid"}},
		{Action: "fill", Value: "5", Target: &engine.Locator{NameAttr: "price", Tag: "input", Via: "nameAttr"}},
		{Action: "click", Target: &engine.Locator{TestID: "gone", Role: "button", Name: "Save product", Via: "testid"}},
		{Action: "expect", Text: "Created mug at $5 (draft)"},
	}}
	var out bytes.Buffer
	err := route.Run(p, r, nil, &out)
	t.Log("\n" + out.String())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "fallback: role+name") {
		t.Fatalf("want a role+name fallback in:\n%s", out.String())
	}
}

// TestMissingElementFailsFast: an element that never appears fails the step with an error.
func TestMissingElementFailsFast(t *testing.T) {
	p, url := newPage(t)
	p.Timeout = 500 * time.Millisecond
	r := &route.Route{Steps: []route.Step{
		{Action: "goto", URL: url},
		{Action: "click", Target: &engine.Locator{Role: "button", Name: "Delete everything"}},
	}}
	err := route.Run(p, r, nil, &bytes.Buffer{})
	var se *route.StepError
	if err == nil || !asStep(err, &se) || se.Index != 2 {
		t.Fatalf("want failure at step 2, got %v", err)
	}
}

func asStep(err error, se **route.StepError) bool {
	e, ok := err.(*route.StepError)
	*se = e
	return ok
}

func TestCasesGoodAndBad(t *testing.T) {
	p, url := newPage(t)
	r := &route.Route{Name: "create-product", Params: map[string]string{"name": "x", "price": "1"}, Steps: []route.Step{
		{Action: "goto", URL: url},
		{Action: "fill", Value: "{{name}}", Target: &engine.Locator{TestIDAttr: "data-testid", TestID: "product-name", Via: "testid"}},
		{Action: "fill", Value: "{{price}}", Target: &engine.Locator{TestIDAttr: "data-testid", TestID: "product-price", Via: "testid"}},
		{Action: "click", Target: &engine.Locator{TestIDAttr: "data-testid", TestID: "save-product", Via: "testid"}},
		{Action: "expect", Text: "Created {{name}} at ${{price}}"},
	}}
	p.Timeout = time.Second
	cases, err := route.LoadCases("../testdata/cases.csv")
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	passed, failed := route.RunCases(p, r, cases, &out)
	t.Log("\n" + out.String())
	if passed != 4 || failed != 0 {
		t.Fatalf("want 4 passed (2 good, 2 rejected bad), got %d passed %d failed", passed, failed)
	}
}

// TestHealRewritesLocatorAndRecordsLesson: a stale testid falls back, gets rewritten to
// the element's current testid, and the next run needs no fallback.
func TestHealRewritesLocatorAndRecordsLesson(t *testing.T) {
	p, url := newPage(t)
	r := &route.Route{Name: "heal", Params: map[string]string{"name": "mug"}, Steps: []route.Step{
		{Action: "goto", URL: url},
		{Action: "fill", Value: "{{name}}", Target: &engine.Locator{
			TestIDAttr: "data-testid", TestID: "old-name", Role: "textbox", Name: "Product name", Via: "testid"}},
	}}
	healed, err := route.RunWith(p, r, map[string]string{"name": "lamp"}, route.Options{Heal: true})
	if err != nil {
		t.Fatal(err)
	}
	if healed != 1 || len(r.Lessons) != 1 {
		t.Fatalf("want 1 healed step and 1 lesson, got %d / %+v", healed, r.Lessons)
	}
	if got := r.Steps[1].Target.TestID; got != "product-name" {
		t.Fatalf("healed testid = %q, want product-name", got)
	}
	if r.Steps[1].Value != "{{name}}" {
		t.Fatalf("healing must not change values; got %q", r.Steps[1].Value)
	}
	var out bytes.Buffer
	if err := route.Run(p, r, nil, &out); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "fallback") {
		t.Fatalf("second run should not need a fallback:\n%s", out.String())
	}
}
