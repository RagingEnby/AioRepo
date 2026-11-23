package main

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite testdata/golden from the current parser")

// TestGolden parses trimmed copies of real sources in testdata/sources and
// compares the output with testdata/golden. After an intended change, run
// go test -run TestGolden -update and review the diff.
func TestGolden(t *testing.T) {
	files, err := filepath.Glob("testdata/sources/*.json")
	if err != nil || len(files) == 0 {
		t.Fatalf("no fixtures: %v", err)
	}
	for _, in := range files {
		name := filepath.Base(in)
		t.Run(strings.TrimSuffix(name, ".json"), func(t *testing.T) {
			data, err := os.ReadFile(in)
			if err != nil {
				t.Fatal(err)
			}
			seen := &seenState{Apps: map[string]string{}, Versions: map[string]string{}}
			src, err := newParser(testNow, seen.versionDate(testNow)).parseSource(data)
			if err != nil {
				t.Fatal(err)
			}
			r := &sourceResult{sourceURL: sourceURL{URL: name}, Status: statusOK, Parsed: src}
			apps := mergeApps([]*sourceResult{r}, nil)
			for _, a := range apps {
				fixCategory(a)
				a.attribute()
			}
			out := buildSource(kindMain, apps, buildOptions{now: testNow, sources: 1})
			got, err := encodeSource(out, true)
			if err != nil {
				t.Fatal(err)
			}
			links, _ := encodeSource(&Source{Name: "links", Apps: []*App{}, News: toAny(src.Links)}, true)
			got = append(got, links...)

			path := filepath.Join("testdata", "golden", name)
			if *update {
				if err := writeFileAtomic(path, got); err != nil {
					t.Fatal(err)
				}
				return
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("%v (run with -update to create it)", err)
			}
			if !bytes.Equal(got, want) {
				t.Errorf("output differs from %s; run go test -run TestGolden -update and review the diff", path)
			}
		})
	}
}

func toAny(s []string) []any {
	out := make([]any, len(s))
	for i, v := range s {
		out[i] = v
	}
	return out
}
