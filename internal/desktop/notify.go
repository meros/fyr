package desktop

import "sync"

// Notification is what the shell posts on org.freedesktop.Notifications.
type Notification struct {
	Summary  string
	Body     string
	Ref      string // the thread a click opens
	Urgency  byte   // 0 low, 1 normal, 2 critical
	Replaces uint32 // the id of this thread's notification on screen, 0 for none
}

// Notifications maps the feed to the desktop's notifications: one per
// thread, so a thread's newer notice replaces its older one (as Web Push's
// topic and tag do, V0.notification-polish), and a click maps back to the
// thread.
type Notifications struct {
	mu    sync.Mutex
	byRef map[string]uint32
	refOf map[uint32]string
	shown map[uint32]string // the state each notification was posted for
}

// Plan is the notification for n.
func (ns *Notifications) Plan(n Notice) Notification {
	ns.mu.Lock()
	defer ns.mu.Unlock()
	// Critical would pass Do Not Disturb, which quiet hours are for, so
	// everything is normal; a finished thread is low, as its push waits for
	// a sleeping phone (push.Urgency).
	urg := byte(1)
	if n.State == "done" {
		urg = 0
	}
	title := n.Title
	if title == "" {
		title = "Fyr"
	}
	return Notification{Summary: title, Body: n.Body, Ref: n.Ref, Urgency: urg, Replaces: ns.byRef[n.Ref]}
}

// Posted records the id the server gave the notification for ref, shown
// for state.
func (ns *Notifications) Posted(ref, state string, id uint32) {
	ns.mu.Lock()
	defer ns.mu.Unlock()
	if ns.byRef == nil {
		ns.byRef, ns.refOf, ns.shown = map[string]uint32{}, map[uint32]string{}, map[uint32]string{}
	}
	if old, ok := ns.byRef[ref]; ok && old != id {
		delete(ns.refOf, old)
		delete(ns.shown, old)
	}
	ns.shown[id] = state
	if ref != "" {
		ns.byRef[ref] = id
	}
	ns.refOf[id] = ref
}

// Clicked is the thread of notification id, and whether it is one of ours.
func (ns *Notifications) Clicked(id uint32) (string, bool) {
	ns.mu.Lock()
	defer ns.mu.Unlock()
	ref, ok := ns.refOf[id]
	return ref, ok
}

// Closed forgets notification id.
func (ns *Notifications) Closed(id uint32) {
	ns.mu.Lock()
	defer ns.mu.Unlock()
	if ref, ok := ns.refOf[id]; ok {
		delete(ns.refOf, id)
		delete(ns.shown, id)
		if ns.byRef[ref] == id {
			delete(ns.byRef, ref)
		}
	}
}

// Stale is the notifications to close now that thread threadID is in
// state: those shown for another state (web/src/push.ts
// notificationIsStale, which the page applies to the service worker's).
func (ns *Notifications) Stale(threadID, state string) []uint32 {
	ns.mu.Lock()
	defer ns.mu.Unlock()
	var out []uint32
	for ref, id := range ns.byRef {
		if RefHasThread(ref, threadID) && ns.shown[id] != state {
			out = append(out, id)
		}
	}
	return out
}
