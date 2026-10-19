package timetracking

import (
	"testing"

	"github.com/Martin-Business-Consultants/runwellgo/v3/plugin"
	"github.com/Martin-Business-Consultants/runwellgo/v3/plugintest"
)

// removedSetup installs the plugin, switched on with its grants, and
// answers the host (hooks run inline) and the database.
func removedSetup(t *testing.T) (*plugin.Host, *plugin.DB) {
	t.Helper()
	return plugintest.Setup(t, plugintest.Env{}, Module())
}

// emitRemoved delivers the core's user.removed as it would after commit.
func emitRemoved(h *plugin.Host, id, successor int64, name string) {
	plugintest.UserRemoved(h, id, successor, name, "Marcus")
}
