package main

import (
	"encoding/json"
	"errors"
	"os"
	"sync"
	"time"
)

// seenFile records when each app, and each version from a dateless format,
// first appeared. It is committed so the dates survive a lost CI cache.
const seenFile = "state/seen.json"

type seenState struct {
	mu sync.Mutex
	// Apps maps a bundle ID to the day it first appeared in repo.json
	// (YYYY-MM-DD). An empty day means the app was there before tracking
	// began.
	Apps map[string]string `json:"apps"`
	// Versions maps "bundleID@version" to an RFC 3339 time, for formats
	// without version dates.
	Versions map[string]string `json:"versions"`

	seeded bool // the file did not exist before this run
}

func loadSeen(path string) (*seenState, error) {
	s := &seenState{Apps: map[string]string{}, Versions: map[string]string{}}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		s.seeded = true
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, s); err != nil {
		return nil, err
	}
	if s.Apps == nil {
		s.Apps = map[string]string{}
	}
	if s.Versions == nil {
		s.Versions = map[string]string{}
	}
	return s, nil
}

// versionDate returns when bundleID@version was first seen, recording now
// if it is new.
func (s *seenState) versionDate(now time.Time) func(bundleID, version string) time.Time {
	return func(bundleID, version string) time.Time {
		key := bundleID + "@" + version
		s.mu.Lock()
		defer s.mu.Unlock()
		if v, ok := s.Versions[key]; ok {
			if t, err := time.Parse(time.RFC3339, v); err == nil {
				return t.In(localTZ)
			}
		}
		// On the first run the real age is unknown. An old date keeps the
		// whole Scarlet catalog from jumping to the top of the list.
		t := now.In(localTZ)
		if s.seeded {
			t = unknownDate
		}
		s.Versions[key] = t.Format(time.RFC3339)
		return t
	}
}

// unknownDate stands in for versions whose age is unknown.
var unknownDate = time.Date(2020, 1, 1, 0, 0, 0, 0, localTZ)

// markApps records today's date for every app not seen before. On the
// first run every app is recorded with an empty date, so the whole catalog
// is not announced as new.
func (s *seenState) markApps(apps []*App, now time.Time) {
	day := now.In(localTZ).Format(time.DateOnly)
	if s.seeded {
		day = ""
	}
	for _, a := range apps {
		if _, ok := s.Apps[a.BundleIdentifier]; !ok {
			s.Apps[a.BundleIdentifier] = day
		}
	}
}

func (s *seenState) save(path string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := json.MarshalIndent(s, "", " ")
	if err != nil {
		return err
	}
	return writeFileAtomic(path, append(data, '\n'))
}
