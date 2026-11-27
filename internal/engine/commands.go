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

// newestKey is the newest list of a provider in any folder (V0.new-thread-skills):
// what New thread lists for a scratch folder or a folder no session of the
// provider has started in yet. The user's own skills and the CLI's commands are
// the same in every folder; only a folder's project skills differ.
func newestKey(profile string) string { return "commands-newest:" + profile }

// SetCommands keeps a session's commands for its thread and as the newest
// list of its provider and folder. The agent manager calls it after
// initialize.
func (e *Engine) SetCommands(thread, profile, cwd string, cmds []core.Command) {
	cmds = slices.Clone(cmds)
	e.cmdMu.Lock()
	e.cmds[thread] = cmds
	e.cmdMu.Unlock()
	b, err := json.Marshal(cmds)
	if err != nil {
		return
	}
	var keys []string
	if len(cmds) > 0 { // a session that listed nothing tells nothing about the provider's other folders
		keys = append(keys, newestKey(profile))
	}
	if cwd != "" {
		keys = append(keys, commandsKey(profile, cwd))
	}
	for _, k := range keys {
		if old, _ := e.st.GetKV(k); old == string(b) {
			continue
		}
		if err := e.st.SetKV(k, string(b)); err != nil {
			log.Printf("commands: %v", err)
		}
	}
}

// FolderCommands is the newest list seen for a provider and folder; for no
// folder (a scratch thread), or a folder none was seen in, the provider's
// newest in any folder; empty when the provider never started a session.
func (e *Engine) FolderCommands(profile, cwd string) []core.Command {
	out := []core.Command{}
	raw := ""
	if cwd != "" {
		raw, _ = e.st.GetKV(commandsKey(profile, cwd))
	}
	if raw == "" {
		raw, _ = e.st.GetKV(newestKey(profile))
	}
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
