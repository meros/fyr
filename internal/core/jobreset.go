package core

import (
	"strings"
	"time"
)

// The owner's Reset of a learned job class (decision 0029 S3, #187): the
// class's cost goes back to the prior until it is learned again. Owner only.
const (
	KindJobReset = "host.jobReset"
	// EffectJobReset forgets the class.
	EffectJobReset EffectKind = "jobReset"
)

func init() {
	RegisterKind(&InputKind{Name: KindJobReset, On: RecHost, From: ownerOnly, Await: true, Reduce: func(_ Rec, in Input, _ time.Time) (Result, error) {
		c, _ := mapArgs(in)["class"].(string)
		if c = strings.TrimSpace(c); c == "" || len(c) > 300 {
			return Result{}, reject(RejectBadArgs, "class must name a learned job class")
		}
		return Result{Effects: []Effect{{Kind: EffectJobReset, Request: &EffectReq{Action: KindJobReset, Args: map[string]any{"class": c}}}}}, nil
	}})
}
