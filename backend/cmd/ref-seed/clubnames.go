package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// clubNamesFile is the on-disk schema for data/clubs/clubnames.json — the
// bulk-ingest channel for the global club-name pools. The database
// (ref.club_name_parts) is the runtime source of truth; this file is loaded
// on demand, never authoritative at runtime.
type clubNamesFile struct {
	Version    int        `json:"version"`
	Provenance provenance `json:"provenance"`
	Stems      []string   `json:"stems"`
	Suffixes   []string   `json:"suffixes"`
}

// provenance records where a curated list came from, mirroring the player-name
// data contract (data/names/*.json).
type provenance struct {
	Source    string `json:"source"`
	License   string `json:"license"`
	CuratedBy string `json:"curated_by"`
	Date      string `json:"date"`
	Verified  bool   `json:"verified"`
}

// clubNames is the validated, deduplicated pool set.
type clubNames struct {
	Stems    []string
	Suffixes []string
}

// loadClubNames reads data/clubs/clubnames.json. A missing file, malformed
// JSON, missing provenance, an empty stem/suffix list, or a duplicate entry
// all fail loudly — seeding must never run with a silent "Unknown" fallback.
func loadClubNames(dataDir string) (*clubNames, error) {
	path := filepath.Join(dataDir, "clubnames.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read club name data %q: %w", path, err)
	}

	var f clubNamesFile
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, fmt.Errorf("parse %q: %w", path, err)
	}
	if f.Provenance.Source == "" || f.Provenance.License == "" {
		return nil, fmt.Errorf("%q: provenance.source and provenance.license are required", path)
	}
	if len(f.Stems) == 0 {
		return nil, fmt.Errorf("%q: stems is empty", path)
	}
	if len(f.Suffixes) == 0 {
		return nil, fmt.Errorf("%q: suffixes is empty", path)
	}

	stems, err := dedupe(f.Stems)
	if err != nil {
		return nil, fmt.Errorf("%q: stems: %w", path, err)
	}
	suffixes, err := dedupe(f.Suffixes)
	if err != nil {
		return nil, fmt.Errorf("%q: suffixes: %w", path, err)
	}
	return &clubNames{Stems: stems, Suffixes: suffixes}, nil
}

func dedupe(in []string) ([]string, error) {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, v := range in {
		if v == "" {
			return nil, fmt.Errorf("empty entry is not allowed")
		}
		if seen[v] {
			return nil, fmt.Errorf("duplicate entry %q", v)
		}
		seen[v] = true
		out = append(out, v)
	}
	return out, nil
}
