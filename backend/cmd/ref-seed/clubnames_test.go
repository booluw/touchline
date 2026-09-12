package main

import (
	"os"
	"path/filepath"
	"testing"
)

func writeClubFile(t *testing.T, dir, content string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "clubnames.json"), []byte(content), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
}

func validClubJSON() string {
	return `{
	  "version": 1,
	  "provenance": {"source":"test","license":"mit","curated_by":"t","date":"2026-09-12","verified":false},
	  "stems": ["Athletic", "City"],
	  "suffixes": ["FC", "Rovers"]
	}`
}

func TestLoadClubNamesValid(t *testing.T) {
	dir := t.TempDir()
	writeClubFile(t, dir, validClubJSON())
	got, err := loadClubNames(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(got.Stems) != 2 || got.Stems[0] != "Athletic" {
		t.Fatalf("stems = %v, want [Athletic City]", got.Stems)
	}
	if len(got.Suffixes) != 2 || got.Suffixes[1] != "Rovers" {
		t.Fatalf("suffixes = %v, want [FC Rovers]", got.Suffixes)
	}
}

func TestLoadClubNamesMissingFile(t *testing.T) {
	if _, err := loadClubNames(t.TempDir()); err == nil {
		t.Fatal("expected error for missing clubnames.json")
	}
}

func TestLoadClubNamesMalformedJSON(t *testing.T) {
	dir := t.TempDir()
	writeClubFile(t, dir, `{not json`)
	if _, err := loadClubNames(dir); err == nil {
		t.Fatal("expected error for malformed JSON")
	}
}

func TestLoadClubNamesMissingProvenance(t *testing.T) {
	dir := t.TempDir()
	writeClubFile(t, dir, `{"version":1,"stems":["a"],"suffixes":["b"]}`)
	if _, err := loadClubNames(dir); err == nil {
		t.Fatal("expected error for missing provenance")
	}
}

func TestLoadClubNamesEmptyLists(t *testing.T) {
	dir := t.TempDir()
	writeClubFile(t, dir, `{"provenance":{"source":"s","license":"l"},"stems":[],"suffixes":["b"]}`)
	if _, err := loadClubNames(dir); err == nil {
		t.Fatal("expected error for empty stems")
	}
	dir2 := t.TempDir()
	writeClubFile(t, dir2, `{"provenance":{"source":"s","license":"l"},"stems":["a"],"suffixes":[]}`)
	if _, err := loadClubNames(dir2); err == nil {
		t.Fatal("expected error for empty suffixes")
	}
}

func TestLoadClubNamesDuplicatesAndBlanks(t *testing.T) {
	dir := t.TempDir()
	writeClubFile(t, dir, `{"provenance":{"source":"s","license":"l"},"stems":["a","a"],"suffixes":["b"]}`)
	if _, err := loadClubNames(dir); err == nil {
		t.Fatal("expected error for duplicate stem")
	}
	dir2 := t.TempDir()
	writeClubFile(t, dir2, `{"provenance":{"source":"s","license":"l"},"stems":[""],"suffixes":["b"]}`)
	if _, err := loadClubNames(dir2); err == nil {
		t.Fatal("expected error for blank stem")
	}
}
