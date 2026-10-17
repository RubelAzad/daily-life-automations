// Package migrations embeds the versioned SQL files so the migrate command
// works from any working directory.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
