package runner

import (
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"sync"
	"time"
)

// SeqKey is the key under which a message from a runner carries its
// sequence number; Link.Handled acks it.
const SeqKey = "_fyrSeq"

// Link is fyrd's end of the pipe to one agent process, whatever protocol
// the CLI speaks (the claude control protocol, ACP): it writes and logs
// JSON lines and keeps the ack state the runner needs (V0.90). An ack covers
// every line up to it, so it stops before the oldest message the consumer
// has not handled, and it keeps the CLI's requests that wait for an answer:
// the runner knows nothing of the protocol, so the next fyrd gets them only
// if this one says so. A protocol client embeds a Link.
type Link struct {
	pipe Pipe
	log  io.Writer // optional raw protocol log
	wmu  sync.Mutex

	amu      sync.Mutex
	unacked  []int64          // seqs on Out, not handled yet, in order
	open     map[string]int64 // the CLI's requests not answered → seq
	seen     int64            // the newest seq read
	lastAck  int64
	lastKeep []int64
}

// NewLink is the Link over p, logging to log when it is set.
func NewLink(p Pipe, log io.Writer) *Link {
	return &Link{pipe: p, log: log, open: map[string]int64{}}
}

// Pipe is the link to the process.
func (l *Link) Pipe() Pipe { return l.pipe }

// Pid is the process's pid.
func (l *Link) Pid() int { return l.pipe.Pid() }

// Logf writes one protocol line to the log, with its direction.
func (l *Link) Logf(dir string, line []byte) {
	if l.log != nil {
		fmt.Fprintf(l.log, "%s %s %s\n", time.Now().Format("15:04:05.000"), dir, line)
	}
}

// Write sends v as one JSON line.
func (l *Link) Write(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	l.Logf(">>", b)
	l.wmu.Lock()
	defer l.wmu.Unlock()
	return l.pipe.WriteLine(b)
}

// Handled says that the consumer of the client's output is done with m: a
// later fyrd does not get it again from the runner (V0.90).
func (l *Link) Handled(m map[string]any) {
	seq, ok := m[SeqKey].(int64)
	if !ok {
		return
	}
	l.amu.Lock()
	if i := slices.Index(l.unacked, seq); i >= 0 {
		l.unacked = slices.Delete(l.unacked, i, i+1)
	}
	l.amu.Unlock()
	l.Ack()
}

// Note records a line read: queued for the consumer (acked by Handled), or
// dispatched by the client itself.
func (l *Link) Note(seq int64, queued bool) {
	if seq == 0 {
		return
	}
	l.amu.Lock()
	l.seen = seq
	if queued {
		l.unacked = append(l.unacked, seq)
	}
	l.amu.Unlock()
}

// Ack acks every line before the oldest unhandled message, and keeps the
// requests still open.
func (l *Link) Ack() {
	l.amu.Lock()
	upTo := l.seen
	if len(l.unacked) > 0 {
		upTo = l.unacked[0] - 1
	}
	var keep []int64
	for _, seq := range l.open {
		if seq <= upTo {
			keep = append(keep, seq)
		}
	}
	slices.Sort(keep)
	same := upTo == l.lastAck && slices.Equal(keep, l.lastKeep)
	l.lastAck, l.lastKeep = upTo, keep
	l.amu.Unlock()
	if !same {
		l.pipe.Ack(upTo, keep)
	}
}

// Opened records a request of the CLI that waits for fyrd's answer: kept
// for the next fyrd until Answered.
func (l *Link) Opened(id string, seq int64) {
	if seq == 0 || id == "" {
		return
	}
	l.amu.Lock()
	l.open[id] = seq
	l.amu.Unlock()
}

// Answered says the request id needs no answer any more.
func (l *Link) Answered(id string) {
	l.amu.Lock()
	_, ok := l.open[id]
	delete(l.open, id)
	l.amu.Unlock()
	if ok {
		l.Ack()
	}
}
