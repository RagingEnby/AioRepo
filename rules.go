package main

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
)

// Rule files. Apps matching spam.txt go to repo++.json. Apps matching
// block.txt are left out of every output.
const (
	spamFile  = "spam.txt"
	blockFile = "block.txt"
)

// rule matches one app field. A rule file has one rule per line:
//
//	<field> <op> <value>
//
// field is name, developer, subtitle, bundle, icon, download, source, or
// description. op is is, contains, prefix, suffix, or regex. Matching is
// case-insensitive; add (?-i) to a regex to turn that off.
type rule struct {
	field, op, value string
	re               *regexp.Regexp
	line             int
}

type ruleSet []rule

var ruleFields = map[string]func(*App) string{
	"name":        func(a *App) string { return a.Name },
	"developer":   func(a *App) string { return a.DeveloperName },
	"subtitle":    func(a *App) string { return a.Subtitle },
	"bundle":      func(a *App) string { return a.BundleIdentifier },
	"icon":        func(a *App) string { return a.IconURL },
	"download":    func(a *App) string { return a.DownloadURL },
	"source":      func(a *App) string { return a.sourceName },
	"description": func(a *App) string { return a.LocalizedDescription },
}

func parseRules(text string) (ruleSet, error) {
	var rs ruleSet
	sc := bufio.NewScanner(strings.NewReader(text))
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 3 {
			return nil, fmt.Errorf("line %d: want <field> <op> <value>", n)
		}
		field, op := strings.ToLower(parts[0]), strings.ToLower(parts[1])
		// The value is everything after the op, inner spaces kept.
		rest := strings.TrimSpace(line[len(parts[0]):])
		value := strings.TrimSpace(rest[len(parts[1]):])
		if _, ok := ruleFields[field]; !ok {
			return nil, fmt.Errorf("line %d: unknown field %q", n, field)
		}
		r := rule{field: field, op: op, value: strings.ToLower(value), line: n}
		switch op {
		case "is", "contains", "prefix", "suffix":
		case "regex":
			re, err := regexp.Compile("(?i)" + value)
			if err != nil {
				return nil, fmt.Errorf("line %d: %w", n, err)
			}
			r.re = re
		default:
			return nil, fmt.Errorf("line %d: unknown op %q", n, op)
		}
		rs = append(rs, r)
	}
	return rs, sc.Err()
}

func loadRules(path string) (ruleSet, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	rs, err := parseRules(string(data))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return rs, nil
}

func (r rule) match(a *App) bool {
	v := ruleFields[r.field](a)
	if r.re != nil {
		return r.re.MatchString(v)
	}
	v = strings.ToLower(v)
	switch r.op {
	case "is":
		return v == r.value
	case "contains":
		return strings.Contains(v, r.value)
	case "prefix":
		return strings.HasPrefix(v, r.value)
	case "suffix":
		return strings.HasSuffix(v, r.value)
	}
	return false
}

func (rs ruleSet) match(a *App) bool {
	for _, r := range rs {
		if r.match(a) {
			return true
		}
	}
	return false
}
