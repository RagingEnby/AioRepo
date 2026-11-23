package main

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata" // GitHub runners may lack a zoneinfo database.
)

// Dates without a time zone are read as New York time.
var localTZ = mustLoadLocation("America/New_York")

func mustLoadLocation(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		panic(err)
	}
	return loc
}

var (
	zonedLayouts = []string{
		time.RFC3339Nano,
		"2006-01-02T15:04:05.999999999Z0700",
		"2006-01-02T15:04Z07:00",
		"2006-01-02 15:04:05.999999999Z07:00",
		"2006-01-02 15:04:05.999999999Z0700",
		"2006-01-02 15:04:05.999999999 -0700",
		"2006-01-02 15:04:05.999999999 -0700 MST",
		time.RFC1123Z,
		time.RFC1123,
	}
	naiveLayouts = []string{
		"2006-01-02T15:04:05.999999999",
		"2006-01-02T15:04",
		"2006-01-02 15:04:05.999999999",
		"2006-01-02 15:04",
		"2006-01-02",
	}
	// Day-first and month-first fallbacks, tried on the date part only.
	dateOnlyLayouts = []string{"2006-01-02", "2006-02-01", "02-01-2006", "01-02-2006"}

	shortDatePart = regexp.MustCompile(`^(\d{4})-(\d{1,2})-(\d{1,2})`)
	compactOffset = regexp.MustCompile(`([+-]\d{2})(\d{2})$`)
)

// parseDate reads the many date styles found in sources: ISO 8601 with or
// without zone, unpadded months and days, day-first dates, and unix times.
func parseDate(v any) (time.Time, error) {
	var s string
	switch t := v.(type) {
	case string:
		s = strings.TrimSpace(t)
	case json.Number:
		s = t.String()
	default:
		return time.Time{}, fmt.Errorf("date is %T", v)
	}
	if s == "" {
		return time.Time{}, fmt.Errorf("empty date")
	}
	if n, err := strconv.ParseFloat(s, 64); err == nil && !strings.Contains(s, "-") {
		if n > 1e11 { // milliseconds
			n /= 1000
		}
		return time.Unix(int64(n), 0).In(localTZ), nil
	}

	candidates := []string{s}
	norm := strings.Replace(s, "Z", "+00:00", 1)
	norm = shortDatePart.ReplaceAllStringFunc(norm, func(m string) string {
		p := shortDatePart.FindStringSubmatch(m)
		return fmt.Sprintf("%s-%s-%s", p[1], pad2(p[2]), pad2(p[3]))
	})
	norm = compactOffset.ReplaceAllString(norm, "$1:$2")
	if norm != s {
		candidates = append(candidates, norm)
	}

	for _, c := range candidates {
		for _, layout := range zonedLayouts {
			if t, err := time.Parse(layout, c); err == nil {
				return t.In(localTZ), nil
			}
		}
		for _, layout := range naiveLayouts {
			if t, err := time.ParseInLocation(layout, c, localTZ); err == nil {
				return t, nil
			}
		}
	}

	datePart := s
	if i := strings.IndexAny(datePart, "T "); i >= 0 {
		datePart = datePart[:i]
	}
	for _, layout := range dateOnlyLayouts {
		if t, err := time.ParseInLocation(layout, datePart, localTZ); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unsupported date format: %q", s)
}

func pad2(s string) string {
	if len(s) == 1 {
		return "0" + s
	}
	return s
}
