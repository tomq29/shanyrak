// Package web holds the map page served by the HTTP API.
package web

import (
	_ "embed"
	"html/template"
)

//go:embed map.html
var mapHTML string

// MapPage parses the map template. The boot data is injected as JSON in a
// script context, so html/template escapes it for us.
func MapPage() (*template.Template, error) {
	return template.New("map").Parse(mapHTML)
}
