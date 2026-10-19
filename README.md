# Time tracking for Runwell

Minutes logged on clients, engagements and work (the engagement and client kept alongside, so totals roll up), a Time tab on each, a weekly timesheet, and Log time in the quick actions. Entries live in its own storage; records are read, never written. Its screens are described in its manifest, so it has no Svelte code.

A [Runwell](https://github.com/Martin-Business-Consultants/runwellgo) plugin: key `time_tracking`,
version 0.1.0. It's in the official Runwell binary; an owner adds it from
**Settings › Plugins › Add a plugin** and switches it on, which grants what it asks for.

## What it asks for

- `storage`: keeps values in its own key-value namespace
- `records:read`: reads clients, engagements, work and the other core records, as agents see them

The grants are a contract the host keeps: the plugin reaches Runwell only through the
`*plugin.API` it's handed, holding what an owner granted ([docs/plugins.md](https://github.com/Martin-Business-Consultants/runwellgo/blob/main/docs/plugins.md)).

## Its screens

It draws no Svelte code of its own: the screens its manifest describes (text, details, forms, tables) are drawn by the app.

## Building Runwell with it

The official binary (runwellgo's `cmd/runwell`) already has it. A Runwell of your own is a
`main.go` that hands `runwell.Main` its plugins:

```go
package main

import (
	"os"

	timetracking "github.com/Martin-Business-Consultants/runwellgo-time-tracking"
	"github.com/Martin-Business-Consultants/runwellgo/v3/runwell"
)

// version is set from the release tag: go build -ldflags "-X main.version=1.2.3".
var version = "dev"

func main() {
	os.Exit(runwell.Main(version, timetracking.Module()))
}
```

`go get github.com/Martin-Business-Consultants/runwellgo-time-tracking@v0.1.0` adds it. Runwell's app (embedded in the core module) already
carries this plugin's screens.

## Developing

```sh
go vet ./... && go test ./...
golangci-lint run ./...   # v2.15.0, .golangci.yml
```

The tests run the plugin on a real database through `plugintest` (a host, a temporary
database and a stand-in for the core). To try a change in the app, check this repository out
beside runwellgo and add it to runwellgo's `go.work` (`go work use ../runwellgo-time-tracking`): the build
and `make plugin-ui` then use your checkout.

This code moved here from runwellgo's `plugins/time_tracking` and `ui/src/plugins/time_tracking` with its
history (runwellgo 3.1.0).

## License

[FSL-1.1-MIT](LICENSE.md), like Runwell.
