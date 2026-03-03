// Package migrations embeds all SQL migration files into the binary.
// This allows golang-migrate to run migrations from a FROM scratch Docker image
// without a filesystem. Use migrations.FS as the source in db.runMigrations.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
