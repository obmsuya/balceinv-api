package migrations

import "embed"

//go:embed postgres/*.sql
var Postgres embed.FS

//go:embed sqlite/*.sql
var Sqlite embed.FS
