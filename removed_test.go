package timetracking

import (
	"context"
	"encoding/json"
	"testing"
)

// A removed person's time stays theirs, named.
func TestUserRemovedKeepsTheirTime(t *testing.T) {
	h, db := removedSetup(t)
	ctx := context.Background()
	for _, e := range []Entry{{ID: 1, UserID: 2, UserName: "", Minutes: 30, WorkedOn: "2026-10-12"}, {ID: 2, UserID: 2, UserName: "Sarah", Minutes: 15, WorkedOn: "2026-10-12"},
		{ID: 3, UserID: 3, UserName: "", Minutes: 60, WorkedOn: "2026-10-12"}} {
		raw, _ := json.Marshal(e)
		if _, err := db.ExecContext(ctx, `INSERT INTO plugin_kv (plugin_key, key, value, updated_at) VALUES (?, ?, ?, '2026-10-12')`, Key, entryKey(e.ID), raw); err != nil {
			t.Fatal(err)
		}
	}
	emitRemoved(h, 2, 3, "Sarah Lee")
	rows, err := db.QueryContext(ctx, `SELECT value FROM plugin_kv WHERE plugin_key = ? ORDER BY key`, Key)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	want := []Entry{{ID: 1, UserID: 2, UserName: "Sarah Lee"}, {ID: 2, UserID: 2, UserName: "Sarah"}, {ID: 3, UserID: 3, UserName: ""}}
	i := 0
	for rows.Next() {
		var raw []byte
		_ = rows.Scan(&raw)
		var e Entry
		_ = json.Unmarshal(raw, &e)
		if e.ID != want[i].ID || e.UserID != want[i].UserID || e.UserName != want[i].UserName {
			t.Fatalf("entry %d: %+v, want %+v", i, e, want[i])
		}
		i++
	}
	if i != 3 {
		t.Fatalf("%d entries, want 3", i)
	}
}
