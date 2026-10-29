package route

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/manzil-infinity180/xuse/engine"
)

// Case is one row of inputs. The reserved column _expect=fail marks a bad case that
// must fail (e.g. invalid input that the app should reject); _name labels the row.
type Case map[string]string

// LoadCases reads a CSV whose header row names the params.
func LoadCases(path string) ([]Case, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	rows, err := csv.NewReader(f).ReadAll()
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if len(rows) < 2 {
		return nil, fmt.Errorf("%s: need a header row and at least one case", path)
	}
	var cases []Case
	for _, row := range rows[1:] {
		c := Case{}
		for i, h := range rows[0] {
			if i < len(row) {
				c[strings.TrimSpace(h)] = row[i]
			}
		}
		cases = append(cases, c)
	}
	return cases, nil
}

func (c Case) vars() map[string]string {
	v := map[string]string{}
	for k, val := range c {
		if !strings.HasPrefix(k, "_") {
			v[k] = val
		}
	}
	return v
}

func (c Case) label(i int) string {
	if n := c["_name"]; n != "" {
		return n
	}
	keys := make([]string, 0, len(c))
	for k := range c.vars() {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for j, k := range keys {
		parts[j] = fmt.Sprintf("%s=%q", k, c[k])
	}
	return fmt.Sprintf("#%d %s", i+1, strings.Join(parts, " "))
}

// RunCases replays the route once per case and prints a table. A good case passes when
// the route passes; a bad case (_expect=fail) passes when the route fails.
func RunCases(p *engine.Page, r *Route, cases []Case, out io.Writer) (passed, failed int) {
	start := time.Now()
	for i, c := range cases {
		wantFail := strings.EqualFold(c["_expect"], "fail")
		t := time.Now()
		err := Run(p, r, c.vars(), io.Discard)
		var se *StepError
		ok := err == nil
		detail := ""
		switch {
		case wantFail && err != nil && errors.As(err, &se):
			ok = true
			detail = fmt.Sprintf("rejected as expected at step %d", se.Index)
		case wantFail && err == nil:
			detail = "expected a failure, but the route passed"
		case err != nil:
			detail = err.Error()
		}
		status := "PASS"
		if !ok {
			status = "FAIL"
			failed++
		} else {
			passed++
		}
		kind := "good"
		if wantFail {
			kind = "bad "
		}
		fmt.Fprintf(out, "%s %s %-45s %5dms  %s\n", status, kind, c.label(i), time.Since(t).Milliseconds(), detail)
	}
	fmt.Fprintf(out, "%d/%d cases passed in %dms · 0 LLM tokens\n", passed, len(cases), time.Since(start).Milliseconds())
	return passed, failed
}
