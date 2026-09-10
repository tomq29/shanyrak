// Package schema holds the SQLite schema shared by the Python scraper, which
// writes listings, and the Go server, which reads them and writes marks.
package schema

import _ "embed"

//go:embed schema.sql
var SQL string
