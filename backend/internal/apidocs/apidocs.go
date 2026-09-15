// Package apidocs serves the hand-authored Touchline API reference: the raw
// OpenAPI 3.1 document and the interactive Scalar web UI. The spec is kept in
// sync with the router by the route-coverage test in cmd/api.
package apidocs

import (
	_ "embed"

	"github.com/gin-gonic/gin"
)

// The single source of truth for the API surface. Every route in
// internal/httpapi/router.go must appear here (see cmd/api docs coverage test).
//
//go:embed openapi.yaml
var OpenAPIYAML []byte

// routers mounts the two doc endpoints on a route group. They are public like
// /health — no admin session is required to read the reference.
func routes(r *gin.RouterGroup) {
	r.GET("/openapi.yaml", func(c *gin.Context) {
		c.Header("Content-Type", "application/yaml; charset=utf-8")
		_, _ = c.Writer.Write(OpenAPIYAML)
	})
	r.GET("/docs", func(c *gin.Context) {
		c.Header("Content-Type", "text/html; charset=utf-8")
		_, _ = c.Writer.Write([]byte(docsHTML))
	})
}

// Mount registers the API reference routes on the router.
func Mount(r *gin.Engine) {
	routes(r.Group("/api"))
}

// docsHTML is the Scalar UI shell; a pinned CDN build renders the spec at
// /api/openapi.yaml. The client bundle is fetched from the pinned version's
// jsdelivr URL (no vendored assets, no build step).
const docsHTML = `<!doctype html>
<html lang="en">
  <head>
    <meta charset="utf-8">
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <title>Touchline API</title>
  </head>
  <body>
    <script id="api-reference" data-url="/api/openapi.yaml"></script>
    <script src="https://cdn.jsdelivr.net/npm/@scalar/api-reference@1"></script>
  </body>
</html>`