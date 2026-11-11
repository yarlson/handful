package db

import (
	"io/fs"
	"testing"
)

// The bridge release v0.13.0 is the last YarGPT release: Handful numbers its
// migrations above LastYarGPTSchema, so YarGPT must ship exactly that many.
func TestLastYarGPTSchemaIsTheNumberOfMigrations(t *testing.T) {
	names, err := fs.Glob(migrations, "migrations/*.sql")
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != LastYarGPTSchema {
		t.Fatalf("YarGPT embeds %d migrations, LastYarGPTSchema is %d", len(names), LastYarGPTSchema)
	}
}
