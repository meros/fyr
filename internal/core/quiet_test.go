package core

import (
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
	night := &Quiet{From: "17:00", To: "06:00"}
	day := &Quiet{From: "09:00", To: "12:30"}
	weekdays := &Quiet{From: "17:00", To: "06:00", Days: []string{"mon", "tue", "wed", "thu", "fri"}}
	allDay := &Quiet{From: "00:00", To: "00:00", Days: []string{"sat", "sun"}}
	// 2026-11-13 is a Friday.
	cases := []struct {
		name  string
		q     *Quiet
		t     time.Time
		want  bool
		until time.Time
	}{
		{"nil", nil, at(2026, 11, 13, 20, 0), false, time.Time{}},
		{"empty", &Quiet{}, at(2026, 11, 13, 20, 0), false, time.Time{}},
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
		{"dst gap start", &Quiet{From: "02:30", To: "04:00"}, at(2026, 3, 29, 3, 45), true, at(2026, 3, 29, 4, 0)},
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
