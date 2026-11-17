// Package docs embeds the user guide, served by the wiki at /_/help.
package docs

import _ "embed"

// Guide is the user guide (guide.md). Its "<!-- widgets -->" line is
// replaced by the documentation of the wiki's widgets.
//
//go:embed guide.md
var Guide string
