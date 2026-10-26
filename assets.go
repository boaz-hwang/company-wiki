// Package companywiki embeds the files setup installs into ~/company-wiki, so the
// binary works without a clone of this repository.
package companywiki

import "embed"

// Assets holds the SCHEMA.md template and the agent skills.
//
//go:embed SCHEMA.md skills
var Assets embed.FS
