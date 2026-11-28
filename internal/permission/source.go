package permission

// Source says where a resolved level came from: the value of
// `auto_approve_from` on the wire (delegate_status, the delegate result,
// `ynh info`, `ynh ls`, `ynh trust show`). It is a wire contract, defined here
// and nowhere else; the schema enums repeat these words and a test keeps them
// in step (ynh #1058).
type Source string

const (
	// SourceCall is a per-call level on delegate_to.
	SourceCall Source = "call"
	// SourceEntry is the parent's delegates_to entry.
	SourceEntry Source = "entry"
	// SourceFlag is `--auto-approve` on the run.
	SourceFlag Source = "flag"
	// SourceHarness is the harness's own declared auto_approve, on any run.
	SourceHarness Source = "harness"
	// SourceDelegateDefault is the delegate harness's own declared level, as
	// the ceiling a delegate session opens under.
	SourceDelegateDefault Source = "delegate_default"
	// SourceCallerCap is the level of the session that called the delegate.
	SourceCallerCap Source = "caller_cap"
	// SourceProject is the working folder's own, narrower permission mode.
	SourceProject Source = "project"
	// SourceDefault is the default for an accepted harness that declares no
	// level.
	SourceDefault Source = "default"
	// SourceInteractive is a task queued to a live interactive session: a
	// person approves each action there, so no level is recorded for it. It is
	// only the source of a delegation's record, never of a run's level.
	SourceInteractive Source = "interactive"
)

// Sources lists every Source, in the order the schemas list them.
var Sources = []Source{SourceCall, SourceEntry, SourceFlag, SourceHarness, SourceDelegateDefault, SourceCallerCap, SourceProject, SourceDefault, SourceInteractive}
