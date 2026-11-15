package core

// The owner's look at the Changes tab and the Viewed marks (SPEC 0.5
// V0.65). Both change the owner's View (R19), never the thread.

import (
	"fmt"
	"regexp"
	"time"
)

// MaxLookFiles bounds a look and the Viewed marks (the host lists at most
// 1,000 files).
const MaxLookFiles = 1000

var objectID = regexp.MustCompile(`^([0-9a-f]{4,64})?$`)

func lookPath(p string) error {
	if p == "" || len(p) > 4096 {
		return fmt.Errorf("%w: a path is empty or longer than 4096 bytes", ErrBadArgs)
	}
	return nil
}

func init() {
	register(&Action{Name: "thread.changesSeen", Label: "Changes seen",
		Apply: func(_ *Thread, v *View, a Args, now time.Time) ([]Effect, error) {
			head := a.Str("head")
			if !objectID.MatchString(head) {
				return nil, fmt.Errorf("%w: head must be a commit id", ErrBadArgs)
			}
			raw, ok := a["files"].(map[string]any)
			if !ok {
				return nil, fmt.Errorf("%w: files must be {path: blob}", ErrBadArgs)
			}
			if len(raw) > MaxLookFiles {
				return nil, fmt.Errorf("%w: at most %d files", ErrBadArgs, MaxLookFiles)
			}
			files := make(map[string]string, len(raw))
			for p, b := range raw {
				s, ok := b.(string)
				if !ok || !objectID.MatchString(s) {
					return nil, fmt.Errorf("%w: the blob of %q must be a blob id or \"\"", ErrBadArgs, p)
				}
				if err := lookPath(p); err != nil {
					return nil, err
				}
				files[p] = s
			}
			// A new value, never an edit in place: v may share maps with the
			// stored View until the change is written.
			l := &ChangesLook{Head: head, At: now, Files: files}
			if v.ChangesLook != nil {
				for p, b := range v.ChangesLook.Viewed {
					if _, ok := files[p]; ok {
						if l.Viewed == nil {
							l.Viewed = map[string]string{}
						}
						l.Viewed[p] = b
					}
				}
			}
			v.ChangesLook = l
			return nil, nil
		}})

	register(&Action{Name: "thread.fileViewed", Label: "Viewed",
		Apply: func(_ *Thread, v *View, a Args, _ time.Time) ([]Effect, error) {
			p, blob := a.Str("path"), a.Str("blob")
			if err := lookPath(p); err != nil {
				return nil, err
			}
			if !objectID.MatchString(blob) {
				return nil, fmt.Errorf("%w: blob must be a blob id or \"\"", ErrBadArgs)
			}
			on, ok := a["viewed"].(bool)
			if !ok {
				return nil, fmt.Errorf("%w: viewed must be true or false", ErrBadArgs)
			}
			l := &ChangesLook{}
			if v.ChangesLook != nil {
				*l = *v.ChangesLook
			}
			viewed := make(map[string]string, len(l.Viewed)+1)
			for k, b := range l.Viewed {
				viewed[k] = b
			}
			if on {
				if _, had := viewed[p]; !had && len(viewed) >= MaxLookFiles {
					return nil, fmt.Errorf("%w: at most %d viewed files", ErrBadArgs, MaxLookFiles)
				}
				viewed[p] = blob
			} else {
				delete(viewed, p)
			}
			l.Viewed = viewed
			v.ChangesLook = l
			return nil, nil
		}})
}
