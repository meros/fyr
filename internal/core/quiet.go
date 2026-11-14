package core

import (
	"fmt"
	"time"
)

// Quiet is a project's quiet hours (SPEC 0.5 V0.46): from and to are HH:MM
// in the host's local time; a window whose To is not after From crosses
// midnight (equal is a whole day). Days are the days a window starts on
// (mon … sun); none means every day.
type Quiet struct {
	From string   `json:"from"`
	To   string   `json:"to"`
	Days []string `json:"days,omitempty"`
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

// QuietNow reports whether q holds at t (in t's location: the host's).
func QuietNow(q *Quiet, t time.Time) bool {
	_, ok := q.window(t)
	return ok
}

// QuietUntil is the end of the quiet stretch that holds t; windows that
// follow on without a gap join it (a whole Saturday and Sunday end Monday
// 00:00). It looks at most a week ahead.
func QuietUntil(q *Quiet, t time.Time) (time.Time, bool) {
	end, ok := q.window(t)
	if !ok {
		return time.Time{}, false
	}
	for i := 0; i < 7; i++ {
		next, more := q.window(end)
		if !more {
			break
		}
		end = next
	}
	return end, true
}
