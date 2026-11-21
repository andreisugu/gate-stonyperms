// Package web embeds the StonyPermsWeb static UI assets served by Gate.
package web

import "embed"

// FS holds the embedded static web application files.
//
//go:embed index.html styles.css app.js demo-data.js wiki-data.js favicon.svg
var FS embed.FS
