package bootstrap

import (
	"testing"
)

func TestShortName(t *testing.T) {
	if got, want := shortName("Harbour City FC"), "Har"; got != want {
		t.Fatalf("shortName = %q, want %q", got, want)
	}
	if got, want := shortName("AC"), "AC"; got != want {
		t.Fatalf("shortName short = %q, want %q", got, want)
	}
}
