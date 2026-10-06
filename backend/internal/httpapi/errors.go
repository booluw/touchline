package httpapi

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// errorBody is the one error shape every endpoint returns: a human-readable
// message plus a stable snake_case code clients can branch on. Messages may
// be reworded; codes must not change once shipped.
type errorBody struct {
	Error string `json:"error"`
	Code  string `json:"code"`
}

// respondError writes a fixed error message with an explicit code.
func respondError(c *gin.Context, status int, code, msg string) {
	c.JSON(status, errorBody{Error: msg, Code: code})
}

// abortError is respondError for middleware that must stop the chain.
func abortError(c *gin.Context, status int, code, msg string) {
	c.AbortWithStatusJSON(status, errorBody{Error: msg, Code: code})
}

// respondErr writes a client-safe domain error; its code comes from
// sentinelCodes (error_codes.go), falling back to the status code.
func respondErr(c *gin.Context, status int, err error) {
	c.JSON(status, errorBody{Error: err.Error(), Code: errorCode(err, status)})
}

func errorCode(err error, status int) string {
	for _, s := range sentinelCodes {
		if errors.Is(err, s.err) {
			return s.code
		}
	}
	return statusCode(status)
}

// statusCode turns an HTTP status into a code, e.g. 409 -> "conflict".
func statusCode(status int) string {
	text := http.StatusText(status)
	if text == "" {
		return "error"
	}
	return strings.ReplaceAll(strings.ToLower(text), " ", "_")
}
