package main

import (
	"runtime/debug"
	"strings"
)

// version is set by -ldflags "-X main.version=...". Empty/dev means
// fall back to the module version from `go install @v1.2.3` / `@latest`.
var version = "dev"

func versionString() string {
	if v := strings.TrimSpace(version); v != "" && v != "dev" {
		return stripV(v)
	}
	if bi, ok := debug.ReadBuildInfo(); ok {
		v := bi.Main.Version
		if v != "" && v != "(devel)" {
			return stripV(v)
		}
	}
	return "dev"
}

func stripV(v string) string {
	return strings.TrimPrefix(v, "v")
}
