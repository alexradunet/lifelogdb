// copyschema copies docs/schema/schema.sql into internal/db so the binary can embed it (go:embed cannot reach
// outside the module). Run by `go generate ./...`; internal/db's tests fail when the copy is stale.
package main

import (
	"log"
	"os"
)

func main() {
	if len(os.Args) != 3 {
		log.Fatal("usage: copyschema SRC DST")
	}
	b, err := os.ReadFile(os.Args[1])
	if err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile(os.Args[2], b, 0o644); err != nil {
		log.Fatal(err)
	}
}
