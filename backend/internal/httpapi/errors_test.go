package httpapi

import (
	"fmt"
	"net/http"
	"regexp"
	"testing"

	internalboard "github.com/touchline/backend/internal/board"
)

func TestErrorCode(t *testing.T) {
	wrapped := fmt.Errorf("resolve: %w", internalboard.ErrMandateResolved)
	if got := errorCode(wrapped, http.StatusConflict); got != "mandate_resolved" {
		t.Fatalf("wrapped sentinel: got %q", got)
	}
	if got := errorCode(fmt.Errorf("unlisted"), http.StatusConflict); got != "conflict" {
		t.Fatalf("status fallback: got %q", got)
	}
}

// Codes are a client contract: snake_case, and one code per sentinel.
func TestSentinelCodesWellFormed(t *testing.T) {
	snake := regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	seen := map[error]bool{}
	for _, s := range sentinelCodes {
		if !snake.MatchString(s.code) {
			t.Errorf("code %q is not snake_case", s.code)
		}
		if seen[s.err] {
			t.Errorf("sentinel %v listed twice", s.err)
		}
		seen[s.err] = true
	}
}
