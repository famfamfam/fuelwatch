// Package migrations содержит SQL-миграции (goose), встроенные в бинарник.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
