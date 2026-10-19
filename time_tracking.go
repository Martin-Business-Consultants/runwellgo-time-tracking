// Package timetracking is Time tracking, ported from the Rails plugin
// (github.com/Martin-Business-Consultants/runwell-time-tracking) onto the Go
// plugin API alone: minutes logged on clients, engagements and work (with
// the engagement and client kept alongside, so totals roll up), a Time tab
// on each, a weekly timesheet, and Log time in the quick actions. No live
// timers. Entries live in its own storage; records are read, never written.
package timetracking

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Martin-Business-Consultants/runwellgo/internal/plugin"
)

// Key is the plugin's key.
const Key = "time_tracking"

// everyone is the permission to see everyone's time, not only your own.
const everyone = Key + ":view_everyones_time"

// trackable are the records time is logged on (Entry::TRACKABLE_TYPES).
var trackable = []string{"Client", "Engagement", "Todo"}

var entryFields = []plugin.Field{
	{Name: "duration", Label: "Duration", Required: true, Placeholder: "1:30, 90 or 1.5h"},
	{Name: "worked_on", Label: "Worked on", Type: "date", Default: "today"},
	{Name: "note", Label: "Note", Placeholder: "What you did (optional)"},
}

// Module is the plugin, for the core to register.
func Module() *plugin.Module {
	logForm := &plugin.Form{Record: "entry", Save: "/entries", Submit: "Log time", Reset: true, Fields: entryFields}
	return &plugin.Module{
		Manifest: plugin.Manifest{
			Key:         Key,
			Name:        "Time tracking",
			Version:     "0.1.0",
			Author:      "Runwell",
			Homepage:    "https://github.com/Martin-Business-Consultants/runwell-time-tracking",
			Description: "Log time on clients, engagements and work, with totals and a weekly timesheet. Keeps its own storage and reads core records by id.",
			Permissions: plugin.Grants{Storage: true, RecordsRead: true},
			// A removed person's time stays theirs, under their name.
			Hooks: []string{"user.removed"},
			Capabilities: []plugin.Capability{
				{Key: everyone, Label: "See everyone’s time", Roles: []string{"owner", "manager"}},
			},
			Routes: []plugin.Route{
				{Method: "GET", Path: "/time", Export: "section"},
				{Method: "GET", Path: "/log", Export: "blank"},
				{Method: "POST", Path: "/entries", Export: "create", Tool: &plugin.Tool{
					Name: "log_time", Title: "Log time",
					Description: `record: "Type:id" for a Client, Engagement or Todo (a scope item goes to its engagement, a request to its client). duration like 1:30, 90 or 1.5h.`,
					Wrap:        "entry", Body: `{duration (required), worked_on: YYYY-MM-DD, note}`,
					Params: []plugin.ToolParam{{Name: "record", Description: `What it was spent on: "Client:12", "Engagement:5" or "Todo:40".`}},
				}},
				{Method: "DELETE", Path: "/entries", Export: "destroy", Tool: &plugin.Tool{
					Name: "delete_time", Title: "Delete time you logged",
					Query: []plugin.ToolParam{{Name: "id", Type: "integer", Description: "The entry's id.", Required: true}},
				}},
				{Method: "GET", Path: "/timesheet", Export: "timesheet", Tool: &plugin.Tool{
					Name: "show_timesheet", Title: "Show a weekly timesheet",
					Query: []plugin.ToolParam{
						{Name: "week", Description: "this (the default) or last."},
						{Name: "person", Description: "me (the default) or everyone, for those who may see everyone's time."},
					},
				}},
			},
			Screens: []plugin.Screen{
				{ID: "time", Title: "Time", Source: "/time", Stats: "totals", Form: logForm,
					Table: &plugin.Table{Rows: "rows", Empty: "No time logged yet.", Columns: []plugin.Column{
						{Key: "duration", Label: "Time"}, {Key: "who", Label: "Who"}, {Key: "worked_on", Label: "Date", Date: true},
						{Key: "on", Label: "On", Href: "path"}, {Key: "note", Label: "Note"},
					}, RowActions: []plugin.Action{
						{Label: "Delete", Post: "/entries", Method: "DELETE", Params: map[string]string{"id": "id"}, Confirm: "This time comes off the totals.", Danger: true, ShowIf: "mine"},
					}},
				},
				{ID: "timesheet", Title: "Time", Source: "/timesheet", Text: "range", Stats: "day_totals",
					Filters: []plugin.Filter{
						{Param: "week", Label: "Week", Options: []plugin.Option{{Value: "this", Label: "This week"}, {Value: "last", Label: "Last week"}}},
						{Param: "person", Label: "Whose", OptionsFrom: "people"},
					},
					Table: &plugin.Table{Rows: "rows", Empty: "No time logged this week.", Columns: []plugin.Column{
						{Key: "worked_on", Label: "Day", Date: true}, {Key: "on", Label: "On", Href: "path", Sub: "context"}, {Key: "duration", Label: "Time", Num: true},
					}, RowActions: []plugin.Action{
						{Label: "Delete", Post: "/entries", Method: "DELETE", Params: map[string]string{"id": "id"}, Confirm: "This time comes off the totals.", Danger: true, ShowIf: "mine"},
					}},
				},
				{ID: "log", Title: "Log time", Source: "/log", Form: logForm},
			},
			Nav: &plugin.NavPage{Label: "Time", Page: "timesheet", Icon: "clock"},
			Panels: []plugin.Panel{
				{Slot: "todo_panel", Label: "Time", Screen: "time"},
				{Slot: "engagement_panel", Label: "Time", Screen: "time"},
				{Slot: "client_panel", Label: "Time", Screen: "time"},
			},
			QuickActions: []plugin.QuickAction{{Label: "Log time", Icon: "history", Types: trackable, Screen: "log"}},
		},
		Handlers: map[string]plugin.Handler{"section": section, "blank": blank, "create": create, "destroy": destroy, "timesheet": timesheet},
		OnEvent:  onEvent,
	}
}

// onEvent keeps a removed person's time as theirs: hours worked are a fact,
// not work to hand over, so they stay on the totals (and everyone's
// timesheet) under the name they logged them as; an entry saved without a
// name gets theirs. Nothing moves to their successor.
func onEvent(ctx context.Context, api *plugin.API, kind string, data json.RawMessage) error {
	if kind != "user.removed" {
		return nil
	}
	var ev struct {
		Payload struct {
			ID   int64  `json:"id"`
			Name string `json:"name"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(data, &ev); err != nil {
		return err
	}
	if ev.Payload.ID == 0 || ev.Payload.Name == "" {
		return nil
	}
	all, err := entries(ctx, api)
	if err != nil {
		return err
	}
	for _, e := range all {
		if e.UserID != ev.Payload.ID || e.UserName != "" {
			continue
		}
		e.UserName = ev.Payload.Name
		raw, err := json.Marshal(e)
		if err != nil {
			return err
		}
		if err := api.KVSet(ctx, entryKey(e.ID), string(raw)); err != nil {
			return err
		}
	}
	return nil
}

// Entry is minutes logged on a record (or none), by someone, on a day.
type Entry struct {
	ID           int64  `json:"id"`
	UserID       int64  `json:"user_id"`
	UserName     string `json:"user_name"`
	Trackable    string `json:"trackable,omitempty"` // "Todo:40", or blank
	Label        string `json:"label"`               // what it was on, as it was called then
	URL          string `json:"url,omitempty"`
	EngagementID int64  `json:"engagement_id,omitempty"`
	ClientID     int64  `json:"client_id,omitempty"`
	Minutes      int    `json:"minutes"`
	WorkedOn     string `json:"worked_on"` // YYYY-MM-DD
	Note         string `json:"note,omitempty"`
	CreatedAt    string `json:"created_at"`
}

func entryKey(id int64) string { return fmt.Sprintf("entry:%016d", id) }

// entries is every entry, oldest first.
func entries(ctx context.Context, api *plugin.API) ([]Entry, error) {
	kvs, err := api.KVValues(ctx, "entry:")
	if err != nil {
		return nil, err
	}
	out := make([]Entry, 0, len(kvs))
	for _, kv := range kvs {
		var e Entry
		if json.Unmarshal([]byte(kv.Value), &e) == nil {
			out = append(out, e)
		}
	}
	return out, nil
}

var (
	hoursMinutes = regexp.MustCompile(`^(\d+):(\d{1,2})$`)
	decimalHours = regexp.MustCompile(`^(\d+(?:\.\d+)?)\s*h$`)
	hAndM        = regexp.MustCompile(`^(?:(\d+)\s*h)?\s*(?:(\d+)\s*m)?$`)
)

// MinutesFrom reads "1:30", "90", "1.5h", "1h 30m" or "45m" as minutes; 0
// when it's none of them (Entry.minutes_from).
func MinutesFrom(text string) int {
	text = strings.ToLower(strings.TrimSpace(text))
	atoi := func(s string) int { n, _ := strconv.Atoi(s); return n }
	switch {
	case text == "":
		return 0
	case hoursMinutes.MatchString(text):
		m := hoursMinutes.FindStringSubmatch(text)
		return atoi(m[1])*60 + atoi(m[2])
	case decimalHours.MatchString(text):
		f, _ := strconv.ParseFloat(decimalHours.FindStringSubmatch(text)[1], 64)
		return int(f*60 + 0.5)
	case hAndM.MatchString(text):
		m := hAndM.FindStringSubmatch(text)
		return atoi(m[1])*60 + atoi(m[2])
	}
	return 0
}

// Duration is minutes as the plugin shows them: "1h 30m", "45m", "0m".
func Duration(minutes int) string {
	h, m := minutes/60, minutes%60
	var parts []string
	if h > 0 {
		parts = append(parts, fmt.Sprintf("%dh", h))
	}
	if m > 0 || h == 0 {
		parts = append(parts, fmt.Sprintf("%dm", m))
	}
	return strings.Join(parts, " ")
}

// today is the date in the install's time zone.
func today(ctx context.Context, api *plugin.API) time.Time {
	loc := time.UTC
	if s, err := api.Site(ctx); err == nil {
		if l, err := time.LoadLocation(s.TimeZone); err == nil {
			loc = l
		}
	}
	now := time.Now().In(loc)
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
}

// target is what time logged on record goes on (Entry.trackable_for): a
// client, engagement or todo as it is; a scope item's engagement; a
// request's client.
func target(ctx context.Context, api *plugin.API, record string) (map[string]any, error) {
	r, err := api.Record(ctx, record)
	if err != nil {
		return nil, err
	}
	switch r["type"] {
	case "ScopeItem":
		r, _ = r["engagement"].(map[string]any)
	case "Request":
		r, _ = r["client"].(map[string]any)
	}
	if r == nil || !slices.Contains(trackable, fmt.Sprint(r["type"])) {
		return nil, plugin.ErrNoRecord
	}
	return api.Record(ctx, fmt.Sprint(r["record"]))
}

func id(v any) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case float64:
		return int64(n)
	case int:
		return int64(n)
	}
	return 0
}

// covers is whether e counts towards record r (Entry.for): a client's
// time includes its engagements' and work's, an engagement's its work's.
func covers(r map[string]any, e Entry) bool {
	switch r["type"] {
	case "Client":
		return e.ClientID == id(r["id"])
	case "Engagement":
		return e.EngagementID == id(r["id"])
	}
	return e.Trackable == r["record"]
}

// row is an entry as the screens and agents show it.
func row(e Entry, me *plugin.Caller, withPerson bool) map[string]any {
	o := map[string]any{
		"id": e.ID, "minutes": e.Minutes, "duration": Duration(e.Minutes), "worked_on": e.WorkedOn, "note": e.Note,
		"person": map[string]any{"id": e.UserID, "name": e.UserName}, "who": e.UserName,
		"on": e.Label, "path": pathOf(e.URL), "mine": me != nil && e.UserID == me.ID,
	}
	if e.Trackable != "" {
		o["record"] = e.Trackable
	}
	if e.Label == "" {
		o["on"] = "No record"
	}
	var context []string
	if withPerson {
		context = append(context, e.UserName)
	}
	if e.Note != "" {
		context = append(context, e.Note)
	}
	o["context"] = strings.Join(context, " · ")
	return o
}

// pathOf is a record's address within the app (its url without the host).
func pathOf(url string) string {
	if i := strings.Index(url, "://"); i >= 0 {
		if j := strings.Index(url[i+3:], "/"); j >= 0 {
			return url[i+3+j:]
		}
	}
	return url
}

// section is the Time tab on a client, engagement or todo: the total, who
// logged it (not on a todo), the last 15 entries, and a form to log more.
func section(ctx context.Context, api *plugin.API, req *plugin.Request) (*plugin.Response, error) {
	r, err := api.Record(ctx, req.Query.Get("record"))
	if res := plugin.Answer(err); res != nil {
		return res, nil
	} else if err != nil {
		return nil, err
	}
	all, err := entries(ctx, api)
	if err != nil {
		return nil, err
	}
	var mine []Entry
	total := 0
	for _, e := range all {
		if covers(r, e) {
			mine = append(mine, e)
			total += e.Minutes
		}
	}
	slices.SortFunc(mine, func(a, b Entry) int { return cmp.Or(cmp.Compare(b.WorkedOn, a.WorkedOn), cmp.Compare(b.ID, a.ID)) })
	totals := []map[string]any{{"label": "Total", "value": Duration(total)}}
	if r["type"] != "Todo" {
		// Who logged it, in the order they first appear.
		var people []string
		byPerson := map[string]int{}
		for _, e := range mine {
			if _, ok := byPerson[e.UserName]; !ok {
				people = append(people, e.UserName)
			}
			byPerson[e.UserName] += e.Minutes
		}
		for _, p := range people {
			totals = append(totals, map[string]any{"label": p, "value": Duration(byPerson[p])})
		}
	}
	rows := []map[string]any{}
	for _, e := range mine[:min(len(mine), 15)] {
		o := row(e, req.Actor, false)
		if e.Trackable == r["record"] {
			o["on"], o["path"] = "", ""
		}
		rows = append(rows, o)
	}
	return plugin.JSON(http.StatusOK, map[string]any{
		"summary": fmt.Sprintf("%s on %s", Duration(total), r["display_name"]),
		"record":  r["record"], "total_minutes": total, "total": Duration(total),
		"totals": totals, "rows": rows, "entry": map[string]any{"worked_on": today(ctx, api).Format(time.DateOnly)},
	}), nil
}

// blank is Log time's form, empty, for the quick actions.
func blank(ctx context.Context, api *plugin.API, _ *plugin.Request) (*plugin.Response, error) {
	return plugin.JSON(http.StatusOK, map[string]any{"entry": map[string]any{"worked_on": today(ctx, api).Format(time.DateOnly)}}), nil
}

// create logs time (log_time), on the record named in the body or on the
// address, or on none.
func create(ctx context.Context, api *plugin.API, req *plugin.Request) (*plugin.Response, error) {
	var body struct {
		Record string `json:"record"`
		Entry  struct {
			Duration string `json:"duration"`
			WorkedOn string `json:"worked_on"`
			Note     string `json:"note"`
		} `json:"entry"`
	}
	if err := json.Unmarshal(req.Body, &body); err != nil {
		return plugin.JSON(http.StatusBadRequest, map[string]any{"error": "bad_request", "message": `Send {"record": "Todo:40", "entry": {"duration": "1:30"}}.`}), nil //nolint:nilerr // the caller's mistake, answered
	}
	record := cmp.Or(body.Record, req.Query.Get("record"))
	e := Entry{UserID: req.Actor.ID, UserName: req.Actor.Name, Minutes: MinutesFrom(body.Entry.Duration), Note: strings.TrimSpace(body.Entry.Note)}
	if e.Minutes <= 0 {
		return plugin.Invalid(map[string][]string{"duration": {"Enter a duration like 1:30, 90 or 1.5h."}}), nil
	}
	e.WorkedOn = today(ctx, api).Format(time.DateOnly)
	if w := strings.TrimSpace(body.Entry.WorkedOn); w != "" {
		if _, err := time.Parse(time.DateOnly, w); err != nil {
			return plugin.Invalid(map[string][]string{"worked_on": {"is not a date"}}), nil //nolint:nilerr // the caller's mistake, answered
		}
		e.WorkedOn = w
	}
	if record != "" {
		r, err := target(ctx, api, record)
		if res := plugin.Answer(err); res != nil {
			return res, nil
		} else if err != nil {
			return nil, err
		}
		e.Trackable, e.URL = fmt.Sprint(r["record"]), fmt.Sprint(r["url"])
		switch r["type"] {
		case "Todo":
			eng, _ := r["engagement"].(map[string]any)
			cl, _ := r["client"].(map[string]any)
			e.Label, e.EngagementID, e.ClientID = fmt.Sprint(r["title"]), id(eng["id"]), id(cl["id"])
		case "Engagement":
			cl, _ := r["client"].(map[string]any)
			e.Label, e.EngagementID, e.ClientID = fmt.Sprint(r["display_name"]), id(r["id"]), id(cl["id"])
		case "Client":
			e.Label, e.ClientID = fmt.Sprint(r["name"]), id(r["id"])
		}
	}
	now := time.Now().UTC()
	e.ID, e.CreatedAt = now.UnixMicro(), now.Format(time.RFC3339)
	raw, _ := json.Marshal(e)
	if err := api.KVSet(ctx, entryKey(e.ID), string(raw)); err != nil {
		return nil, err
	}
	msg := Duration(e.Minutes) + " logged."
	return plugin.JSON(http.StatusCreated, map[string]any{"summary": msg, "message": msg, "entry": row(e, req.Actor, false)}), nil
}

// destroy deletes time the caller logged (delete_time).
func destroy(ctx context.Context, api *plugin.API, req *plugin.Request) (*plugin.Response, error) {
	n, _ := strconv.ParseInt(req.Query.Get("id"), 10, 64)
	raw, ok, err := api.KVGet(ctx, entryKey(n))
	if err != nil {
		return nil, err
	}
	var e Entry
	if !ok || json.Unmarshal([]byte(raw), &e) != nil || e.UserID != req.Actor.ID {
		return plugin.JSON(http.StatusNotFound, map[string]any{"error": "not_found", "message": "Couldn't find that time of yours."}), nil //nolint:nilerr // unreadable is not found
	}
	if err := api.KVDelete(ctx, entryKey(n)); err != nil {
		return nil, err
	}
	return plugin.JSON(http.StatusOK, map[string]any{"summary": "Time removed.", "message": "Time removed."}), nil
}

// timesheet is a week's time, Monday to Sunday, yours or (for those who
// may) everyone's (show_timesheet).
func timesheet(ctx context.Context, api *plugin.API, req *plugin.Request) (*plugin.Response, error) {
	week := req.Query.Get("week")
	if week != "last" {
		week = "this"
	}
	person := req.Query.Get("person")
	if person != "everyone" || !req.Actor.Can(everyone) {
		person = "me"
	}
	day := today(ctx, api)
	start := day.AddDate(0, 0, -((int(day.Weekday()) + 6) % 7))
	if week == "last" {
		start = start.AddDate(0, 0, -7)
	}
	end := start.AddDate(0, 0, 6)
	all, err := entries(ctx, api)
	if err != nil {
		return nil, err
	}
	var in []Entry
	for _, e := range all {
		if e.WorkedOn >= start.Format(time.DateOnly) && e.WorkedOn <= end.Format(time.DateOnly) && (person == "everyone" || e.UserID == req.Actor.ID) {
			in = append(in, e)
		}
	}
	slices.SortFunc(in, func(a, b Entry) int { return cmp.Or(cmp.Compare(a.WorkedOn, b.WorkedOn), cmp.Compare(a.ID, b.ID)) })
	total := 0
	byDay := map[string]int{}
	rows, list := []map[string]any{}, []map[string]any{}
	for _, e := range in {
		total += e.Minutes
		byDay[e.WorkedOn] += e.Minutes
		r := row(e, req.Actor, person == "everyone")
		rows = append(rows, r)
		var on any
		if e.Trackable != "" {
			on = map[string]any{"record": e.Trackable, "display_name": e.Label, "url": e.URL}
		}
		list = append(list, map[string]any{"id": e.ID, "minutes": e.Minutes, "worked_on": e.WorkedOn, "note": e.Note,
			"duration": Duration(e.Minutes), "on": on, "person": map[string]any{"id": e.UserID, "name": e.UserName}})
	}
	days, dayTotals := []map[string]any{}, []map[string]any{}
	for d := start; !d.After(end); d = d.AddDate(0, 0, 1) {
		key := d.Format(time.DateOnly)
		days = append(days, map[string]any{"date": key, "total": Duration(byDay[key]), "minutes": byDay[key]})
		value := "—"
		if byDay[key] > 0 {
			value = Duration(byDay[key])
		}
		dayTotals = append(dayTotals, map[string]any{"label": d.Format("Mon Jan 2"), "value": value})
	}
	whose := "yours"
	if person == "everyone" {
		whose = "everyone"
	}
	people := []map[string]any{}
	if req.Actor.Can(everyone) {
		people = []map[string]any{{"value": "me", "label": "My time"}, {"value": "everyone", "label": "Everyone"}}
	}
	return plugin.JSON(http.StatusOK, map[string]any{
		"summary": fmt.Sprintf("%s %s week (%s)", Duration(total), week, whose),
		"week_of": start.Format(time.DateOnly), "week": week, "person": person,
		"days": days, "entries": list,
		"range":      fmt.Sprintf("%s – %s · %s", start.Format("Jan 2"), end.Format("Jan 2"), Duration(total)),
		"day_totals": dayTotals, "rows": rows, "people": people,
	}), nil
}
