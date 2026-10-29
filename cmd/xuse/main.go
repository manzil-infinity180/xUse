// Command xuse drives a browser with small, fast commands and replays recorded
// flows without an LLM.
package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/manzil-infinity180/xuse/browser"
	"github.com/manzil-infinity180/xuse/engine"
	"github.com/manzil-infinity180/xuse/route"
)

const usage = `xuse — record a browser flow once, replay it with 0 LLM tokens

browser
  xuse start [--headless]          launch the browser (stays open)
  xuse stop                        close it

drive (add --snap to print a snapshot after an action)
  xuse goto <url>
  xuse snap                        compact list of elements with refs (e1, e2…)
  xuse click <ref>
  xuse fill <ref> <value>
  xuse select <ref> <value>
  xuse press <key>                 Enter, Tab, Escape, ArrowDown…
  xuse expect <text>               wait until the page shows text

record & replay
  xuse record start <name>         save every following action
  xuse record stop [--param k=v]…  write .xuse/routes/<name>.json; literal v becomes {{k}}
  xuse record status
  xuse run <route> [k=v]… [--new] [--headed]
                                   replay; --new uses a fresh headless browser
  xuse routes                      list saved routes
  xuse repair <route> <step> <ref> point a step at the element <ref> on the current page`

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		var exit *exitError
		if errors.As(err, &exit) {
			os.Exit(exit.code)
		}
		os.Exit(1)
	}
}

type exitError struct {
	code int
	err  error
}

func (e *exitError) Error() string { return e.err.Error() }

func routesDir() string {
	if d := os.Getenv("XUSE_ROUTES"); d != "" {
		return d
	}
	return filepath.Join(".xuse", "routes")
}

func recordingFile() string { return filepath.Join(browser.Home(), "recording.json") }

// takeFlag removes a boolean flag from args and reports whether it was there.
func takeFlag(args []string, name string) ([]string, bool) {
	out, found := args[:0:0], false
	for _, a := range args {
		if a == name {
			found = true
			continue
		}
		out = append(out, a)
	}
	return out, found
}

func need(args []string, n int, form string) error {
	if len(args) < n {
		return fmt.Errorf("usage: xuse %s", form)
	}
	return nil
}

func attach() (*engine.Page, error) {
	ctx, err := browser.Attach()
	if err != nil {
		return nil, err
	}
	return engine.New(ctx), nil
}

func run(args []string) error {
	args, snapAfter := takeFlag(args, "--snap")
	if len(args) == 0 || args[0] == "help" || args[0] == "-h" || args[0] == "--help" {
		fmt.Println(usage)
		return nil
	}
	cmd, args := args[0], args[1:]
	switch cmd {
	case "start":
		_, headless := takeFlag(args, "--headless")
		s, err := browser.Start(headless)
		if err != nil {
			return err
		}
		fmt.Println("browser ready:", s.WS)
		return nil
	case "stop":
		return browser.Stop()
	case "snap":
		p, err := attach()
		if err != nil {
			return err
		}
		return printSnap(p)
	case "goto", "click", "fill", "select", "press", "expect":
		return act(cmd, args, snapAfter)
	case "record":
		return record(args)
	case "run":
		return replay(args)
	case "routes":
		return listRoutes()
	case "repair":
		return repair(args)
	}
	return fmt.Errorf("unknown command %q\n\n%s", cmd, usage)
}

func printSnap(p *engine.Page) error {
	s, err := p.Snapshot()
	if err != nil {
		return err
	}
	fmt.Println(s)
	return nil
}

// act performs a live action, prints a one-line result, and records the step if a
// recording is active.
func act(cmd string, args []string, snapAfter bool) error {
	forms := map[string]string{
		"goto": "goto <url>", "click": "click <ref>", "fill": "fill <ref> <value>",
		"select": "select <ref> <value>", "press": "press <key>", "expect": "expect <text>",
	}
	n := map[string]int{"fill": 2, "select": 2}[cmd]
	if n == 0 {
		n = 1
	}
	if err := need(args, n, forms[cmd]); err != nil {
		return err
	}
	p, err := attach()
	if err != nil {
		return err
	}
	t := time.Now()
	step := route.Step{Action: cmd}
	switch cmd {
	case "goto":
		step.URL = args[0]
		err = p.Goto(args[0])
	case "press":
		step.Key = args[0]
		err = p.Press(args[0])
	case "expect":
		step.Text = strings.Join(args, " ")
		err = p.ExpectText(step.Text)
	default:
		ref := args[0]
		// Capture the locator before acting: a click may remove the element.
		if step.Target, err = p.LocatorOf(ref); err != nil {
			return err
		}
		switch cmd {
		case "click":
			err = p.Click(ref)
		case "fill":
			step.Value = args[1]
			err = p.Fill(ref, args[1])
		case "select":
			step.Value = args[1]
			err = p.Select(ref, args[1])
		}
	}
	if err != nil {
		return err
	}
	via := ""
	if step.Target != nil {
		via = "  [via " + step.Target.Via + "]"
		if step.Target.Via == "" {
			via = "  [warning: no unique locator; replay may fail]"
		}
	}
	rec := ""
	if appendRecording(step) {
		rec = "  (recorded)"
	}
	fmt.Printf("ok %s %s  %dms%s%s\n", cmd, step.Describe(), time.Since(t).Milliseconds(), via, rec)
	if snapAfter {
		return printSnap(p)
	}
	return nil
}

func loadRecording() (*route.Route, error) { return route.Load(recordingFile()) }

func appendRecording(s route.Step) bool {
	r, err := loadRecording()
	if err != nil {
		return false
	}
	r.Steps = append(r.Steps, s)
	return route.Save(recordingFile(), r) == nil
}

func record(args []string) error {
	if err := need(args, 1, "record start|stop|status"); err != nil {
		return err
	}
	switch args[0] {
	case "start":
		if err := need(args, 2, "record start <name>"); err != nil {
			return err
		}
		if err := os.MkdirAll(browser.Home(), 0o755); err != nil {
			return err
		}
		if err := route.Save(recordingFile(), &route.Route{Name: args[1], Steps: []route.Step{}}); err != nil {
			return err
		}
		fmt.Printf("recording %q — every action is saved until: xuse record stop\n", args[1])
		return nil
	case "status":
		r, err := loadRecording()
		if err != nil {
			fmt.Println("not recording")
			return nil
		}
		fmt.Printf("recording %q, %d steps\n", r.Name, len(r.Steps))
		for i, s := range r.Steps {
			fmt.Printf("%3d %-7s %s\n", i+1, s.Action, s.Describe())
		}
		return nil
	case "stop":
		r, err := loadRecording()
		if err != nil {
			return errors.New("not recording")
		}
		params := map[string]string{}
		rest := args[1:]
		for i := 0; i < len(rest); i++ {
			if rest[i] != "--param" || i+1 >= len(rest) {
				return fmt.Errorf("unexpected %q; use --param k=v", rest[i])
			}
			k, v, ok := strings.Cut(rest[i+1], "=")
			if !ok || k == "" {
				return fmt.Errorf("bad --param %q; use k=v", rest[i+1])
			}
			params[k] = v
			i++
		}
		r.Parameterize(params)
		path := route.Path(routesDir(), r.Name)
		if err := route.Save(path, r); err != nil {
			return err
		}
		os.Remove(recordingFile())
		fmt.Printf("saved %s (%d steps", path, len(r.Steps))
		if len(params) > 0 {
			keys := make([]string, 0, len(params))
			for k := range params {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			fmt.Printf(", params: %s", strings.Join(keys, ", "))
		}
		fmt.Println(")")
		return nil
	}
	return fmt.Errorf("unknown: record %s", args[0])
}

func replay(args []string) error {
	args, fresh := takeFlag(args, "--new")
	args, headed := takeFlag(args, "--headed")
	if err := need(args, 1, "run <route> [k=v]…"); err != nil {
		return err
	}
	r, err := route.Load(route.Path(routesDir(), args[0]))
	if err != nil {
		return err
	}
	vars := map[string]string{}
	for _, a := range args[1:] {
		k, v, ok := strings.Cut(a, "=")
		if !ok {
			return fmt.Errorf("bad var %q; use k=v", a)
		}
		vars[k] = v
	}

	var p *engine.Page
	if s, lerr := browser.Load(); !fresh && lerr == nil && s.Alive() {
		if p, err = attach(); err != nil {
			return err
		}
	} else {
		ctx, cancel := browser.Ephemeral(!headed)
		defer cancel()
		p = engine.New(ctx)
	}

	fmt.Printf("route %s (%d steps)\n", r.Name, len(r.Steps))
	err = route.Run(p, r, vars, os.Stdout)
	var se *route.StepError
	if errors.As(err, &se) {
		fmt.Println("\nreplay stopped. current page:")
		_ = printSnap(p)
		fmt.Printf("\nfix the locator with: xuse repair %s %d <ref>\n", args[0], se.Index)
		return &exitError{2, err}
	}
	return err
}

func listRoutes() error {
	files, _ := filepath.Glob(filepath.Join(routesDir(), "*.json"))
	if len(files) == 0 {
		fmt.Println("no routes in", routesDir())
		return nil
	}
	for _, f := range files {
		r, err := route.Load(f)
		if err != nil {
			fmt.Printf("%s  (unreadable: %v)\n", f, err)
			continue
		}
		var ps []string
		for k, v := range r.Params {
			ps = append(ps, fmt.Sprintf("%s=%q", k, v))
		}
		sort.Strings(ps)
		fmt.Printf("%-24s %2d steps  %s\n", r.Name, len(r.Steps), strings.Join(ps, " "))
	}
	return nil
}

func repair(args []string) error {
	if err := need(args, 3, "repair <route> <step> <ref>"); err != nil {
		return err
	}
	path := route.Path(routesDir(), args[0])
	r, err := route.Load(path)
	if err != nil {
		return err
	}
	i, err := strconv.Atoi(args[1])
	if err != nil || i < 1 || i > len(r.Steps) {
		return fmt.Errorf("step must be 1..%d", len(r.Steps))
	}
	if r.Steps[i-1].Target == nil {
		return fmt.Errorf("step %d (%s) has no element to repair", i, r.Steps[i-1].Action)
	}
	p, err := attach()
	if err != nil {
		return err
	}
	l, err := p.LocatorOf(args[2])
	if err != nil {
		return err
	}
	old := r.Steps[i-1].Target.String()
	// Only how the element is found changes; values and expectations stay as recorded.
	r.Steps[i-1].Target = l
	r.Parameterize(r.Params) // keep recorded values as {{params}}
	if err := route.Save(path, r); err != nil {
		return err
	}
	fmt.Printf("step %d: %s → %s [via %s]\n", i, old, l, l.Via)
	return nil
}
