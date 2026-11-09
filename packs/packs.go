// Package packs is clingon's curated packs: a folder each, with a _pack.md
// saying how the pack looks and a Markdown file for each module. They're
// built into clingon, which copies one to ~/.clingon/packs/remote when it's
// added (`clingon pack add`) and again when it's updated.
package packs

import "embed"

// FS holds every pack, a folder each at its root. (A pattern that names the
// files keeps the _pack.md files, which embedding a folder would leave out.)
//
//go:embed */*.md
var FS embed.FS
