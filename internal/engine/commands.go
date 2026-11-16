package engine

import (
	"encoding/json"
	"log"
	"slices"

	"github.com/meros/fyr/internal/core"
)

// The agent's slash commands and skills (SPEC 0.5 V0.79): host facts, not
// thread state. The engine keeps each thread's list from its session's
// initialize answer in memory, and the newest list per provider and folder
// in the store, so a thread without a session (and New thread) has one too.
// Neither is in the thread DTO: a list is tens of entries and changes only
// when a session starts.

func commandsKey(profile, cwd string) string { return "commands:" + profile + ":" + cwd }

// SetCommands keeps a session's commands for its thread and as the newest
// list of its provider and folder. The agent manager calls it after
// initialize.
func (e *Engine) SetCommands(thread, profile, cwd string, cmds []core.Command) {
	cmds = slices.Clone(cmds)
	e.cmdMu.Lock()
	e.cmds[thread] = cmds
	e.cmdMu.Unlock()
	b, err := json.Marshal(cmds)
	if err != nil || cwd == "" {
		return
	}
	k := commandsKey(profile, cwd)
	if old, _ := e.st.GetKV(k); old == string(b) {
		return
	}
	if err := e.st.SetKV(k, string(b)); err != nil {
		log.Printf("commands: %v", err)
	}
}

// FolderCommands is the newest list seen for a provider and folder, empty
// when none was.
func (e *Engine) FolderCommands(profile, cwd string) []core.Command {
	out := []core.Command{}
	if cwd == "" {
		return out
	}
	raw, _ := e.st.GetKV(commandsKey(profile, cwd))
	if raw != "" {
		json.Unmarshal([]byte(raw), &out)
	}
	return out
}

// ThreadCommands is a thread's commands and where they come from: "thread"
// (its session's), "folder" (the newest for its provider and folder; a
// thread without a profile is on defaultProfile) or "" (none). ok is false
// for an unknown thread.
func (e *Engine) ThreadCommands(id, defaultProfile string) (cmds []core.Command, from string, ok bool) {
	t, ok := e.Thread(id)
	if !ok {
		return nil, "", false
	}
	e.cmdMu.Lock()
	own, has := e.cmds[id]
	e.cmdMu.Unlock()
	if has {
		return slices.Clone(own), "thread", true
	}
	p := t.Profile
	if p == "" {
		p = defaultProfile
	}
	if cs := e.FolderCommands(p, t.Cwd); len(cs) > 0 {
		return cs, "folder", true
	}
	return []core.Command{}, "", true
}
