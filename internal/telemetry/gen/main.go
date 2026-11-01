// Command gen writes internal/telemetry/names_gen.go from telemetry/registry,
// ynh's Weaver registry. Run it with `go generate ./...`.
package main

import (
	"fmt"
	"os"

	"github.com/eyelock/ynh/internal/telemetry/weaver"
	"github.com/eyelock/ynh/telemetry/registry"
)

func main() {
	out := "names_gen.go"
	if len(os.Args) > 1 {
		out = os.Args[1]
	}
	r, err := weaver.Load(registry.FS, "")
	if err == nil {
		var src []byte
		if src, err = weaver.Generate(r, "telemetry"); err == nil {
			err = os.WriteFile(out, src, 0o644)
		}
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "gen:", err)
		os.Exit(1)
	}
}
