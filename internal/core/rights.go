package core

import (
	"fmt"
	"slices"
)

// Field rights (decision 0020 (2), #45): each field a reducer writes on
// behalf of a sender names who may write it. "Facts beat claims" (0018) and
// "the agent never marks a review or an approval ok" are rows here, not code
// in each tool.

// Field names a field of a record, or one value of it.
type Field string

const (
	FieldThreadTitle     Field = "thread.title"
	FieldThreadProject   Field = "thread.project" // filed by hand
	FieldThreadView      Field = "thread.view"    // the live view on or off
	FieldViewSeen        Field = "view.seen"      // the owner's seenAt
	FieldChangePhase     Field = "change.phase"
	FieldChangeCondition Field = "change.condition"
	// A condition type with one status: "change.condition.review=ok".
	FieldReviewOK   = FieldChangeCondition + "." + Field(CondReview) + "=" + Field(CondOK)
	FieldApprovalOK = FieldChangeCondition + "." + Field(CondApproval) + "=" + Field(CondOK)
)

// V0.116: a project's name, colour and image icon; the owner's is kept.
const (
	FieldProjectName  Field = "project.name"
	FieldProjectColor Field = "project.color"
	FieldProjectImage Field = "project.image"
)

// Right is one row of the table.
type Right struct {
	Field Field
	// By are the principals that may write it.
	By []string
	// Kept: a value one of these principals wrote is not changed by a
	// principal in Over ("facts beat claims": observed over agent).
	Kept, Over []string
	// Refuse is the reason given, with %s for the field's subject.
	Refuse string
}

// Rights is the one table.
var Rights = []Right{
	{Field: FieldThreadTitle, By: []string{ByOwner, ByAgent}, Kept: []string{ByOwner}, Over: agentOnly, Refuse: "the owner named %s; ask them to rename it"},
	{Field: FieldProjectName, By: []string{ByOwner, ByAgent}, Kept: ownerOnly, Over: agentOnly, Refuse: "the owner named %s"},
	{Field: FieldProjectColor, By: []string{ByOwner, ByAgent}, Kept: ownerOnly, Over: agentOnly, Refuse: "the owner chose %s"},
	{Field: FieldProjectImage, By: []string{ByOwner, ByAgent}, Kept: ownerOnly, Over: agentOnly, Refuse: "the owner set %s"},
	{Field: FieldThreadProject, By: ownerOnly, Refuse: "only the owner files %s in a project"},
	{Field: FieldThreadView, By: ownerOnly, Refuse: "only the owner turns the live view of %s on or off"},
	{Field: FieldViewSeen, By: ownerOnly, Refuse: "only the owner sees %s"},
	{Field: FieldChangePhase, By: anyone, Kept: observed, Over: agentOnly, Refuse: "Fyr observes %s; a report cannot change it"},
	// V0.72: the owner's end of a change is kept against the agent and the facts.
	{Field: FieldChangePhase, By: anyone, Kept: ownerOnly, Over: []string{ByAgent, ByObserved}, Refuse: "the owner ended %s; open a new change with another key"},
	{Field: FieldChangeCondition, By: anyone, Kept: observed, Over: agentOnly, Refuse: "Fyr observes %s; a report cannot change it"},
	{Field: FieldReviewOK, By: []string{ByOwner, ByObserved}, Refuse: "an agent cannot mark %s ok; ask the owner with change_request"},
	{Field: FieldApprovalOK, By: []string{ByOwner, ByObserved}, Refuse: "an agent cannot mark %s ok; ask the owner with change_request"},
}

// CanWrite checks that by may write field f whose current value cur wrote
// ("" for none): every row of f must allow it. subject names the field in
// the reason.
func CanWrite(f Field, by, cur, subject string) error {
	for _, r := range Rights {
		if r.Field != f {
			continue
		}
		if !slices.Contains(r.By, by) || (slices.Contains(r.Kept, cur) && slices.Contains(r.Over, by)) {
			return &Rejection{Code: RejectRights, Reason: fmt.Sprintf(r.Refuse, subject)}
		}
	}
	return nil
}

// condField is the row of one condition value, or "" when only the general
// condition row applies.
func condField(typ CondType, status CondStatus) Field {
	f := FieldChangeCondition + "." + Field(typ) + "=" + Field(status)
	for _, r := range Rights {
		if r.Field == f {
			return f
		}
	}
	return ""
}
