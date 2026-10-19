package timetracking

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/Martin-Business-Consultants/runwellgo/internal/mail"
	"github.com/Martin-Business-Consultants/runwellgo/internal/plugin"
	"github.com/Martin-Business-Consultants/runwellgo/internal/store"
)

// removedEnv is just enough Runwell to install the plugin and hand it events.
type removedEnv struct{}

func (removedEnv) Setting(context.Context, string) (map[string]any, error) {
	return map[string]any{}, nil
}
func (removedEnv) ReplaceSetting(context.Context, string, map[string]any) error { return nil }
func (removedEnv) Site(context.Context) (plugin.Site, error) {
	return plugin.Site{Name: "Runwell"}, nil
}
func (removedEnv) SendMail(context.Context, mail.Message) error { return nil }
func (removedEnv) Records(context.Context, plugin.Query) ([]map[string]any, error) {
	return nil, nil
}
func (removedEnv) Write(context.Context, plugin.Actor, string, any) (map[string]any, error) {
	return map[string]any{}, nil
}
func (removedEnv) Audit(context.Context, plugin.Actor, string, int64, string, map[string]any) {}
func (removedEnv) AttachFile(context.Context, string, int64, string, string, string, io.Reader) (*plugin.File, error) {
	return nil, errors.New("no")
}
func (removedEnv) Attachments(context.Context, string, int64, string) ([]plugin.File, error) {
	return nil, nil
}
func (removedEnv) OpenFile(context.Context, string, int64, int64) (io.ReadCloser, *plugin.File, error) {
	return nil, nil, errors.New("no")
}
func (removedEnv) DetachFile(context.Context, string, int64, int64) error         { return nil }
func (removedEnv) Tools(context.Context, plugin.Actor) ([]plugin.ToolInfo, error) { return nil, nil }
func (removedEnv) RunTool(context.Context, plugin.Actor, string, string, map[string]any) (map[string]any, error) {
	return nil, errors.New("no")
}
func (removedEnv) Seal(plain string) string            { return plain }
func (removedEnv) Unseal(sealed string) (string, bool) { return sealed, true }

// removedSetup installs the plugin, switched on with its grants, and
// answers the host (hooks run inline) and the database.
func removedSetup(t *testing.T) (*plugin.Host, *store.DB) {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "test.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	spawn := func(ctx context.Context, _ string, fn func(context.Context)) bool { fn(ctx); return true }
	h := plugin.New(removedEnv{}, db, slog.New(slog.NewTextHandler(io.Discard, nil)), spawn, Module())
	if err := h.LoadAll(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Install(t.Context(), nil, Key); err != nil {
		t.Fatal(err)
	}
	grants := Module().Manifest.Permissions
	on := true
	if _, err := h.Configure(t.Context(), nil, Key, &grants, &on); err != nil {
		t.Fatal(err)
	}
	return h, db
}

// emitRemoved delivers the core's user.removed as it would after commit.
func emitRemoved(h *plugin.Host, id, successor int64, name string) {
	h.Emit(context.Background(), "user.removed", map[string]any{"id": 99, "subject_type": "User", "subject_id": id, "kind": "user.removed",
		"payload": map[string]any{"id": id, "name": name, "successor_id": successor, "successor_name": "Marcus"}, "source": "app"})
}
