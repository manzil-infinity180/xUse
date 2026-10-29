package appmap_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/manzil-infinity180/xuse/appmap"
	"github.com/manzil-infinity180/xuse/browser"
	"github.com/manzil-infinity180/xuse/engine"
)

func crawl(t *testing.T, p *engine.Page, dir string) *appmap.Map {
	t.Helper()
	srv := httptest.NewServer(http.FileServer(http.Dir("../testdata/" + dir)))
	defer srv.Close()
	m, err := appmap.Crawl(p, srv.URL+"/index.html", 20, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestCrawlAndDiff(t *testing.T) {
	ctx, cancel := browser.Ephemeral(true)
	defer cancel()
	p := engine.New(ctx)

	v1 := crawl(t, p, "site-v1")
	v2 := crawl(t, p, "site-v2")
	if len(v1.Pages) != 3 {
		t.Fatalf("v1: want 3 same-origin pages, got %+v", v1.Pages)
	}

	var got []string
	for _, c := range appmap.Diff(v1, v2) {
		got = append(got, c.String())
	}
	joined := strings.Join(got, "\n")
	t.Log("\n" + joined)
	for _, want := range []string{
		"page+     /inventory.html  Inventory",
		`element-  /products.html  button "Add product"`,
		`element+  /products.html  button "New product"`,
		`element+  /  link "Inventory"`,
		`element-  /  link "Help"`,
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("diff missing %q", want)
		}
	}
	if strings.Contains(joined, "/orders.html") {
		t.Error("orders page did not change and should not be in the diff")
	}
}
