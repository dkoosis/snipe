package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"text/tabwriter"

	"github.com/dkoosis/atomicfile"
)

// writeReport reads out/calls.jsonl and writes out/report.txt: accuracy and
// mean input tokens per arm per model, then the cases each arm missed.
func writeReport(out string, stdout io.Writer) error {
	//nolint:gosec // operator-supplied path in a developer benchmark tool, not served input
	f, err := os.Open(filepath.Join(out, "calls.jsonl"))
	if err != nil {
		return err
	}
	defer f.Close()
	var rows []call
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		var c call
		if err := json.Unmarshal(sc.Bytes(), &c); err != nil {
			return fmt.Errorf("calls.jsonl: %w", err)
		}
		rows = append(rows, c)
	}
	if err := sc.Err(); err != nil {
		return err
	}
	text := report(rows)
	if err := atomicfile.WriteFile(filepath.Join(out, "report.txt"), []byte(text), 0o600); err != nil {
		return err
	}
	_, err = io.WriteString(stdout, text)
	return err
}

type cell struct {
	n, right, errs, tokens int
	samples                map[string]int // per case
	missed                 map[string]int // per case
}

func report(rows []call) string {
	type key struct{ model, arm string }
	cells := map[key]*cell{}
	var models []string
	ids := map[string]string{}
	for _, r := range rows {
		k := key{r.Model, r.Arm}
		c := cells[k]
		if c == nil {
			c = &cell{samples: map[string]int{}, missed: map[string]int{}}
			cells[k] = c
		}
		if !slices.Contains(models, r.Model) {
			models = append(models, r.Model)
		}
		if r.ModelID != "" {
			ids[r.Model] = r.ModelID
		}
		c.n++
		c.samples[r.Case]++
		switch {
		case r.Err != "":
			c.errs++
			c.missed[r.Case]++
		case r.Correct:
			c.right++
		default:
			c.missed[r.Case]++
		}
		if r.Err == "" {
			c.tokens += r.InputTokens
		}
	}
	name := func(m string) string {
		if id := ids[m]; id != "" {
			return id
		}
		return m
	}
	var b strings.Builder
	b.WriteString("SUMMARY — accuracy and mean input tokens per answered call (prompt overhead is the same in every arm)\n")
	tw := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "model\tarm\tright\taccuracy\tmean input tokens\tmin samples/case\terrors")
	for _, m := range models {
		for _, arm := range arms {
			c := cells[key{m, arm}]
			if c == nil {
				continue
			}
			minS := c.n
			for _, s := range c.samples {
				minS = min(minS, s)
			}
			fmt.Fprintf(tw, "%s\t%s\t%d/%d\t%.0f%%\t%d\t%d\t%d\n", name(m), arm, c.right, c.n, 100*float64(c.right)/float64(c.n), c.tokens/max(c.n-c.errs, 1), minS, c.errs)
		}
	}
	tw.Flush()
	b.WriteString("\nMISSED — case: wrong samples, per model and arm\n")
	for _, m := range models {
		for _, arm := range arms {
			c := cells[key{m, arm}]
			if c == nil || len(c.missed) == 0 {
				continue
			}
			var ms []string
			for _, cs := range slices.Sorted(maps.Keys(c.missed)) {
				ms = append(ms, fmt.Sprintf("%s %d/%d", cs, c.missed[cs], c.samples[cs]))
			}
			fmt.Fprintf(&b, "%s %s: %s\n", name(m), arm, strings.Join(ms, ", "))
		}
	}
	return b.String()
}
