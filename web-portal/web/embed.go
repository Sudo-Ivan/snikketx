// Package web carries the HTML templates and the static assets of the portal,
// embedded into the binary at build time.
package web

import "embed"

// FS holds the templates and static assets served by the portal.
//
//go:embed all:templates all:static
var FS embed.FS
