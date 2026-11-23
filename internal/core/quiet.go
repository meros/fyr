package core

import (
	"bytes"
	"encoding/json"
	"fmt"
	"time"
)

// Quiet is one window of a project's quiet hours (SPEC 0.5 V0.46): from and
// to are HH:MM in the host's local time; a window whose To is not after From
// crosses midnight, and From equal to To is a whole day (the client's "All
// day" stores 00:00–00:00). Days are the days a window starts on (mon …
// sun); none means every day.
type Quiet struct {
	From string   `json:"from"`
	To   string   `json:"to"`
	Days []string `json:"days,omitempty"`
}

// QuietHours is a project's quiet hours: any number of windows (decision
// 0014 addendum); empty is off. The project is quiet while any window holds.
type QuietHours []Quiet

// MaxQuietWindows bounds the list a client may set.
const MaxQuietWindows = 14

// UnmarshalJSON reads the list, and the single object stored before the
// addendum ({from, to, days?}) as a list of one.
func (qs *QuietHours) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	switch {
	case bytes.Equal(b, []byte("null")):
		*qs = nil
		return nil
	case len(b) > 0 && b[0] == '{':
		var q Quiet
		if err := json.Unmarshal(b, &q); err != nil {
			return err
		}
		*qs = QuietHours{q}
		return nil
	}
	var list []Quiet
	if err := json.Unmarshal(b, &list); err != nil {
		return err
	}
	*qs = list
	return nil
}

// Validate reports a bad window, by its place in the list.
func (qs QuietHours) Validate() error {
	if len(qs) > MaxQuietWindows {
		return fmt.Errorf("quiet holds at most %d windows, got %d", MaxQuietWindows, len(qs))
	}
	for i, q := range qs {
		if err := q.Validate(); err != nil {
			if len(qs) == 1 {
				return err
			}
			return fmt.Errorf("quiet window %d: %w", i+1, err)
		}
	}
	return nil
}

var weekdays = map[string]time.Weekday{"sun": time.Sunday, "mon": time.Monday, "tue": time.Tuesday, "wed": time.Wednesday, "thu": time.Thursday, "fri": time.Friday, "sat": time.Saturday}

func parseHM(s string) (h, m int, ok bool) {
	var extra string
	n, _ := fmt.Sscanf(s+" ", "%d:%d%s", &h, &m, &extra)
	if n < 2 || len(s) < 4 || len(s) > 5 || s[len(s)-3] != ':' {
		return 0, 0, false
	}
	return h, m, h >= 0 && h < 24 && m >= 0 && m < 60
}

// Validate reports a bad time or day.
func (q Quiet) Validate() error {
	if _, _, ok := parseHM(q.From); !ok {
		return fmt.Errorf("quiet from must be a time HH:MM, got %q", q.From)
	}
	if _, _, ok := parseHM(q.To); !ok {
		return fmt.Errorf("quiet to must be a time HH:MM, got %q", q.To)
	}
	for _, d := range q.Days {
		if _, ok := weekdays[d]; !ok {
			return fmt.Errorf("quiet days must be among mon tue wed thu fri sat sun, got %q", d)
		}
	}
	return nil
}

func (q *Quiet) startsOn(d time.Weekday) bool {
	if len(q.Days) == 0 {
		return true
	}
	for _, s := range q.Days {
		if weekdays[s] == d {
			return true
		}
	}
	return false
}

// window returns the end of the window that holds t, if one does. Times are
// read in t's location, so wall times hold across DST changes.
func (q *Quiet) window(t time.Time) (time.Time, bool) {
	if q == nil {
		return time.Time{}, false
	}
	fh, fm, ok1 := parseHM(q.From)
	th, tm, ok2 := parseHM(q.To)
	if !ok1 || !ok2 {
		return time.Time{}, false
	}
	span := 0
	if th*60+tm <= fh*60+fm {
		span = 1 // crosses midnight, or a whole day
	}
	y, mo, d := t.Date()
	loc := t.Location()
	for back := 1; back >= 0; back-- {
		day := time.Date(y, mo, d-back, 12, 0, 0, 0, loc)
		if !q.startsOn(day.Weekday()) {
			continue
		}
		start := time.Date(y, mo, d-back, fh, fm, 0, 0, loc)
		end := time.Date(y, mo, d-back+span, th, tm, 0, 0, loc)
		if !t.Before(start) && t.Before(end) {
			return end, true
		}
	}
	return time.Time{}, false
}

// window is the latest end among the windows that hold t.
func (qs QuietHours) window(t time.Time) (time.Time, bool) {
	var end time.Time
	found := false
	for i := range qs {
		if e, ok := qs[i].window(t); ok && (!found || e.After(end)) {
			end, found = e, true
		}
	}
	return end, found
}

// QuietUntil is the end of the quiet stretch that holds t. Windows that
// overlap it or follow on without a gap join it, whichever row they come
// from: weeknights 17:00–06:00 (Sun–Fri) and an all-day Saturday and Sunday
// run from Friday 17:00 to Monday 06:00. It looks at most a week ahead.
func QuietUntil(qs QuietHours, t time.Time) (time.Time, bool) {
	end, ok := qs.window(t)
	if !ok {
		return time.Time{}, false
	}
	for i := 0; i < 64 && end.Sub(t) < 7*24*time.Hour; i++ {
		next, more := qs.window(end)
		if !more || !next.After(end) {
			break
		}
		end = next
	}
	return end, true
}
