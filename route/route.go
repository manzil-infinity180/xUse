// Package route stores recorded flows and replays them without an LLM.
package route

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/manzil-infinity180/xuse/engine"
)

// Step is one recorded action. Values may hold {{param}} placeholders.
type Step struct {
	Action string          `json:"action"` // goto, click, fill, select, press, expect
	URL    string          `json:"url,omitempty"`
	Target *engine.Locator `json:"target,omitempty"`
	Value  string          `json:"value,omitempty"`
	Key    string          `json:"key,omitempty"`
	Text   string          `json:"text,omitempty"`
}

// Describe is a one-line label for logs.
func (s Step) Describe() string {
	switch s.Action {
	case "goto":
		return s.URL
	case "press":
		return s.Key
	case "expect":
		return fmt.Sprintf("%q", s.Text)
	case "fill", "select":
		return fmt.Sprintf("%s = %q", s.Target, s.Value)
	default:
		return s.Target.String()
	}
}

// Route is a named, replayable flow. Params maps each parameter to its recorded value,
// which is the default when a run does not pass it.
type Route struct {
	Name   string            `json:"name"`
	Params map[string]string `json:"params,omitempty"`
	Steps  []Step            `json:"steps"`
}

// Parameterize replaces each recorded literal with {{name}} everywhere in the route.
func (r *Route) Parameterize(params map[string]string) {
	names := make([]string, 0, len(params))
	for k := range params {
		names = append(names, k)
	}
	// Longest literal first so "blue t-shirt" wins over "blue".
	sort.Slice(names, func(i, j int) bool { return len(params[names[i]]) > len(params[names[j]]) })
	if r.Params == nil {
		r.Params = map[string]string{}
	}
	for _, k := range names {
		lit := params[k]
		r.Params[k] = lit
		if lit == "" {
			continue
		}
		ph := "{{" + k + "}}"
		for i := range r.Steps {
			eachString(&r.Steps[i], func(s string) string { return strings.ReplaceAll(s, lit, ph) })
		}
	}
}

var placeholder = regexp.MustCompile(`\{\{(\w+)\}\}`)

// Render fills placeholders using vars, falling back to the recorded defaults.
func (r *Route) Render(s Step, vars map[string]string) (Step, error) {
	var missing string
	out := s
	if s.Target != nil {
		t := *s.Target
		out.Target = &t
	}
	eachString(&out, func(str string) string {
		return placeholder.ReplaceAllStringFunc(str, func(m string) string {
			k := placeholder.FindStringSubmatch(m)[1]
			if v, ok := vars[k]; ok {
				return v
			}
			if v, ok := r.Params[k]; ok {
				return v
			}
			missing = k
			return m
		})
	})
	if missing != "" {
		return out, fmt.Errorf("no value for {{%s}}", missing)
	}
	return out, nil
}

// eachString rewrites every user-visible string of a step.
func eachString(s *Step, f func(string) string) {
	s.URL, s.Value, s.Text = f(s.URL), f(s.Value), f(s.Text)
	if t := s.Target; t != nil {
		t.TestID, t.ID, t.NameAttr, t.Name, t.Placeholder, t.CSS =
			f(t.TestID), f(t.ID), f(t.NameAttr), f(t.Name), f(t.Placeholder), f(t.CSS)
	}
}

// StepError is a replay failure at a step (1-based).
type StepError struct {
	Index int
	Step  Step
	Err   error
}

func (e *StepError) Error() string {
	return fmt.Sprintf("step %d (%s %s): %v", e.Index, e.Step.Action, e.Step.Describe(), e.Err)
}

func (e *StepError) Unwrap() error { return e.Err }

// Do runs one step and returns the locator strategy used, if any.
func Do(p *engine.Page, s Step) (via string, err error) {
	if s.Action == "goto" {
		return "", p.Goto(s.URL)
	}
	if s.Action == "press" {
		return "", p.Press(s.Key)
	}
	if s.Action == "expect" {
		return "", p.ExpectText(s.Text)
	}
	if s.Target == nil {
		return "", fmt.Errorf("step %q has no target", s.Action)
	}
	ref, via, err := p.Find(s.Target)
	if err != nil {
		return "", err
	}
	switch s.Action {
	case "click":
		err = p.Click(ref)
	case "fill":
		err = p.Fill(ref, s.Value)
	case "select":
		err = p.Select(ref, s.Value)
	default:
		err = fmt.Errorf("unknown action %q", s.Action)
	}
	return via, err
}

// Run replays the route on the page. It writes one line per step to out.
func Run(p *engine.Page, r *Route, vars map[string]string, out io.Writer) error {
	start := time.Now()
	for i, raw := range r.Steps {
		s, err := r.Render(raw, vars)
		if err != nil {
			return &StepError{i + 1, raw, err}
		}
		t := time.Now()
		via, err := Do(p, s)
		if err != nil {
			fmt.Fprintf(out, "%3d FAIL %-7s %s\n", i+1, s.Action, s.Describe())
			return &StepError{i + 1, s, err}
		}
		note := ""
		if via != "" && s.Target != nil && s.Target.Via != "" && via != s.Target.Via {
			note = "  (fallback: " + via + ")"
		}
		fmt.Fprintf(out, "%3d ok   %-7s %-50s %5dms%s\n", i+1, s.Action, s.Describe(), time.Since(t).Milliseconds(), note)
	}
	fmt.Fprintf(out, "PASS %d/%d steps in %dms · 0 LLM tokens\n", len(r.Steps), len(r.Steps), time.Since(start).Milliseconds())
	return nil
}

// Path returns the file for a route name inside dir. A name ending in .json is used as a path.
func Path(dir, name string) string {
	if strings.HasSuffix(name, ".json") {
		return name
	}
	return filepath.Join(dir, name+".json")
}

func Load(path string) (*Route, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var r Route
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &r, nil
}

func Save(path string, r *Route) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}
