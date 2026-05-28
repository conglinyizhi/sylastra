package promptfs

import "embed"

//go:embed prompts/zh/*.md
var Files embed.FS
