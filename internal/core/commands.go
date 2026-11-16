package core

// Command is one slash command or skill the agent CLI offers in a folder
// (SPEC 0.5 V0.79): a built-in, a custom command or a skill. The owner
// starts it by sending "/name args"; the CLI runs it.
type Command struct {
	Name         string `json:"name"`
	Description  string `json:"description,omitempty"`
	ArgumentHint string `json:"argumentHint,omitempty"`
}
