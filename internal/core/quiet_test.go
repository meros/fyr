package core

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
	_ "time/tzdata" // the Nix sandbox has no zoneinfo
)

// SPEC 0.5 V0.46: is a project quiet at a moment, and until when.
func TestQuietNow(t *testing.T) {
	sthlm, err := time.LoadLocation("Europe/Stockholm")
	if err != nil {
		t.Fatal(err)
	}
	at := func(y int, m time.Month, d, h, min int) time.Time { return time.Date(y, m, d, h, min, 0, 0, sthlm) }
	night := QuietHours{{From: "17:00", To: "06:00"}}
	day := QuietHours{{From: "09:00", To: "12:30"}}
	weekdays := QuietHours{{From: "17:00", To: "06:00", Days: []string{"mon", "tue", "wed", "thu", "fri"}}}
	allDay := QuietHours{{From: "00:00", To: "00:00", Days: []string{"sat", "sun"}}}
	// 2026-11-13 is a Friday.
	cases := []struct {
		name  string
		q     QuietHours
		t     time.Time
		want  bool
		until time.Time
	}{
		{"nil", nil, at(2026, 11, 13, 20, 0), false, time.Time{}},
		{"an empty window", QuietHours{{}}, at(2026, 11, 13, 20, 0), false, time.Time{}},
		{"empty", QuietHours{}, at(2026, 11, 13, 20, 0), false, time.Time{}},
		{"before the start", night, at(2026, 11, 13, 16, 59), false, time.Time{}},
		{"at the start", night, at(2026, 11, 13, 17, 0), true, at(2026, 11, 14, 6, 0)},
		{"evening", night, at(2026, 11, 13, 23, 30), true, at(2026, 11, 14, 6, 0)},
		{"after midnight", night, at(2026, 11, 14, 3, 0), true, at(2026, 11, 14, 6, 0)},
		{"at the end", night, at(2026, 11, 14, 6, 0), false, time.Time{}},
		{"day window inside", day, at(2026, 11, 13, 12, 29), true, at(2026, 11, 13, 12, 30)},
		{"day window outside", day, at(2026, 11, 13, 20, 0), false, time.Time{}},
		{"weekday friday night runs into saturday", weekdays, at(2026, 11, 14, 5, 0), true, at(2026, 11, 14, 6, 0)},
		{"weekday saturday evening", weekdays, at(2026, 11, 14, 18, 0), false, time.Time{}},
		{"weekday monday early: sunday not chosen", weekdays, at(2026, 11, 16, 3, 0), false, time.Time{}},
		{"weekday monday evening", weekdays, at(2026, 11, 16, 18, 0), true, at(2026, 11, 17, 6, 0)},
		{"whole day saturday runs on through sunday", allDay, at(2026, 11, 14, 13, 0), true, at(2026, 11, 16, 0, 0)},
		{"whole day sunday", allDay, at(2026, 11, 15, 23, 0), true, at(2026, 11, 16, 0, 0)},
		{"whole day friday", allDay, at(2026, 11, 13, 13, 0), false, time.Time{}},
		// DST ends 2026-10-25 03:00 CEST -> 02:00 CET: the night is an hour longer.
		{"dst end night", night, at(2026, 10, 25, 2, 30), true, at(2026, 10, 25, 6, 0)},
		// DST starts 2026-03-29 02:00 CET -> 03:00 CEST.
		{"dst start night", night, at(2026, 3, 29, 5, 59), true, at(2026, 3, 29, 6, 0)},
		{"dst start morning", night, at(2026, 3, 29, 6, 0), false, time.Time{}},
		// A window that starts at a wall time skipped by DST starts at once after the gap.
		{"dst gap start", QuietHours{{From: "02:30", To: "04:00"}}, at(2026, 3, 29, 3, 45), true, at(2026, 3, 29, 4, 0)},
	}
	for _, c := range cases {
		if got := QuietNow(c.q, c.t); got != c.want {
			t.Errorf("%s: QuietNow got %v want %v", c.name, got, c.want)
		}
		u, ok := QuietUntil(c.q, c.t)
		if ok != c.want || (ok && !u.Equal(c.until)) {
			t.Errorf("%s: QuietUntil got %v %v want %v", c.name, u, ok, c.until)
		}
	}
	if d := at(2026, 10, 25, 6, 0).Sub(at(2026, 10, 24, 17, 0)); d != 14*time.Hour {
		t.Errorf("the DST night is %v", d)
	}
}

// SPEC 0.5 V0.46: bad times and days are refused.
func TestQuietValid(t *testing.T) {
	good := []Quiet{{From: "17:00", To: "06:00"}, {From: "0:00", To: "23:59", Days: []string{"mon", "sun"}}}
	bad := []Quiet{{From: "", To: "06:00"}, {From: "24:00", To: "06:00"}, {From: "17:60", To: "06:00"}, {From: "5pm", To: "06:00"}, {From: "17:00", To: "06:00", Days: []string{"monday"}}}
	for _, q := range good {
		if err := q.Validate(); err != nil {
			t.Errorf("%+v: %v", q, err)
		}
	}
	for _, q := range bad {
		if q.Validate() == nil {
			t.Errorf("%+v accepted", q)
		}
	}
}

// Decision 0014 addendum: several windows. The project is quiet while any
// holds, and windows that overlap or follow on join into one stretch.
func TestQuietWindows(t *testing.T) {
	sthlm, err := time.LoadLocation("Europe/Stockholm")
	if err != nil {
		t.Fatal(err)
	}
	at := func(y int, m time.Month, d, h, min int) time.Time { return time.Date(y, m, d, h, min, 0, 0, sthlm) }
	// The presets: weeknights 17:00–06:00 Sun–Fri, and all weekend.
	weeknights := Quiet{From: "17:00", To: "06:00", Days: []string{"sun", "mon", "tue", "wed", "thu", "fri"}}
	weekend := Quiet{From: "00:00", To: "00:00", Days: []string{"sat", "sun"}}
	both := QuietHours{weeknights, weekend}
	// Monday to Friday nights only: Sunday night is not quiet.
	monFri := QuietHours{{From: "17:00", To: "06:00", Days: []string{"mon", "tue", "wed", "thu", "fri"}}, weekend}
	lunch := Quiet{From: "12:00", To: "13:00"}
	overlap := QuietHours{{From: "09:00", To: "12:00"}, {From: "11:00", To: "14:00"}, lunch}
	gap := QuietHours{{From: "09:00", To: "11:00"}, {From: "11:30", To: "12:00"}}
	// 2026-11-13 is a Friday.
	cases := []struct {
		name  string
		q     QuietHours
		t     time.Time
		want  bool
		until time.Time
	}{
		{"friday evening joins the weekend and monday morning", both, at(2026, 11, 13, 17, 0), true, at(2026, 11, 16, 6, 0)},
		{"saturday noon", both, at(2026, 11, 14, 12, 0), true, at(2026, 11, 16, 6, 0)},
		{"sunday night", both, at(2026, 11, 15, 23, 0), true, at(2026, 11, 16, 6, 0)},
		{"monday early", both, at(2026, 11, 16, 5, 59), true, at(2026, 11, 16, 6, 0)},
		{"monday working hours", both, at(2026, 11, 16, 6, 0), false, time.Time{}},
		{"monday evening ends tuesday", both, at(2026, 11, 16, 20, 0), true, at(2026, 11, 17, 6, 0)},
		{"friday afternoon", both, at(2026, 11, 13, 16, 59), false, time.Time{}},
		{"mon-fri nights stop at the end of sunday", monFri, at(2026, 11, 13, 20, 0), true, at(2026, 11, 16, 0, 0)},
		{"mon-fri nights: monday 03:00 is not quiet", monFri, at(2026, 11, 16, 3, 0), false, time.Time{}},
		{"overlapping windows end at the later end", overlap, at(2026, 11, 13, 10, 0), true, at(2026, 11, 13, 14, 0)},
		{"inside the second of two overlapping", overlap, at(2026, 11, 13, 13, 30), true, at(2026, 11, 13, 14, 0)},
		{"a gap keeps windows apart", gap, at(2026, 11, 13, 10, 0), true, at(2026, 11, 13, 11, 0)},
		{"in the gap", gap, at(2026, 11, 13, 11, 15), false, time.Time{}},
		{"after the gap", gap, at(2026, 11, 13, 11, 45), true, at(2026, 11, 13, 12, 0)},
		// DST ends Sunday 2026-10-25: the joined weekend is an hour longer and still ends Monday 06:00.
		{"dst end weekend", both, at(2026, 10, 23, 18, 0), true, at(2026, 10, 26, 6, 0)},
		// DST starts Sunday 2026-03-29.
		{"dst start weekend", both, at(2026, 3, 29, 2, 30), true, at(2026, 3, 30, 6, 0)},
		{"all day every day stops a week ahead", QuietHours{{From: "00:00", To: "00:00"}}, at(2026, 11, 13, 13, 0), true, at(2026, 11, 21, 0, 0)},
	}
	for _, c := range cases {
		if got := QuietNow(c.q, c.t); got != c.want {
			t.Errorf("%s: QuietNow got %v want %v", c.name, got, c.want)
		}
		u, ok := QuietUntil(c.q, c.t)
		if ok != c.want || (ok && !u.Equal(c.until)) {
			t.Errorf("%s: QuietUntil got %v %v want %v", c.name, u, ok, c.until)
		}
	}
	if d := at(2026, 10, 26, 6, 0).Sub(at(2026, 10, 23, 17, 0)); d != 62*time.Hour {
		t.Errorf("the DST weekend is %v", d)
	}
}

// Decision 0014 addendum: the stored single object reads as a list of one;
// the list, null and an empty list read as they are.
func TestQuietHoursJSON(t *testing.T) {
	cases := []struct {
		in   string
		want QuietHours
	}{
		{`{"from":"17:00","to":"06:00","days":["mon"]}`, QuietHours{{From: "17:00", To: "06:00", Days: []string{"mon"}}}},
		{`[{"from":"17:00","to":"06:00"},{"from":"00:00","to":"00:00","days":["sat","sun"]}]`, QuietHours{{From: "17:00", To: "06:00"}, {From: "00:00", To: "00:00", Days: []string{"sat", "sun"}}}},
		{`null`, nil},
		{`[]`, QuietHours{}},
	}
	for _, c := range cases {
		var got QuietHours
		if err := json.Unmarshal([]byte(c.in), &got); err != nil {
			t.Fatalf("%s: %v", c.in, err)
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: got %#v want %#v", c.in, got, c.want)
		}
	}
	// A project stored before the addendum keeps its window and writes the list.
	var p Project
	if err := json.Unmarshal([]byte(`{"id":"p_1","name":"Work","quiet":{"from":"17:00","to":"06:00"}}`), &p); err != nil {
		t.Fatal(err)
	}
	if len(p.Quiet) != 1 || p.Quiet[0].From != "17:00" {
		t.Fatalf("old shape: %+v", p.Quiet)
	}
	b, _ := json.Marshal(p)
	if !strings.Contains(string(b), `"quiet":[{"from":"17:00","to":"06:00"}]`) {
		t.Fatalf("written as %s", b)
	}
	if b, _ := json.Marshal(Project{ID: "p_2"}); strings.Contains(string(b), "quiet") {
		t.Fatalf("off is written as %s", b)
	}
	for _, bad := range []string{`"17:00"`, `[1]`, `{"from":1}`} {
		var q QuietHours
		if json.Unmarshal([]byte(bad), &q) == nil {
			t.Errorf("%s accepted", bad)
		}
	}
}

// Decision 0014 addendum: every window is checked, and the error names it.
func TestQuietHoursValid(t *testing.T) {
	ok := QuietHours{{From: "17:00", To: "06:00"}, {From: "00:00", To: "00:00", Days: []string{"sat", "sun"}}}
	if err := ok.Validate(); err != nil {
		t.Fatal(err)
	}
	if err := (QuietHours{}).Validate(); err != nil {
		t.Fatal(err)
	}
	err := QuietHours{{From: "17:00", To: "06:00"}, {From: "17:00", To: "6"}}.Validate()
	if err == nil || !strings.Contains(err.Error(), "window 2") {
		t.Fatalf("second window: %v", err)
	}
	many := make(QuietHours, MaxQuietWindows+1)
	for i := range many {
		many[i] = Quiet{From: "01:00", To: "02:00"}
	}
	if many.Validate() == nil {
		t.Fatal("too many windows accepted")
	}
}
