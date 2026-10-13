package agent

import "fmt"

// Effort levels for `ynh agent run --effort` and a harness's agent.effort.
//
// They are neutral: each backend maps them to its own setting, and the
// backends' wider sets (Claude Code's xhigh and max, Codex's minimal, xhigh
// and beyond) are deliberately not exposed, so a level means the same thing
// whichever backend runs it.
//
//	ynh      claude (--effort)   codex (-c model_reasoning_effort=)   cursor    opencode
//	low      low                 low                                  refused   refused
//	medium   medium              medium                               refused   refused
//	high     high                high                                 refused   refused
//
// Which levels a model accepts is the vendor's to decide: Claude Code and
// Codex both say their levels depend on the model. ynh passes the level and
// reports what the backend says it applied, so a level the vendor changed is
// visible in the result rather than assumed.
const (
	EffortLow    = "low"
	EffortMedium = "medium"
	EffortHigh   = "high"
)

// validateEffort rejects an effort level that is not one of the neutral
// three, or that the backend has no setting for. A backend that cannot honour
// a level refuses the run before it starts; it never silently ignores it.
func validateEffort(level, backend string) error {
	switch level {
	case "":
		return nil
	case EffortLow, EffortMedium, EffortHigh:
	default:
		return fmt.Errorf("unknown effort level %q (supported: low, medium, high)", level)
	}
	switch backend {
	case "claude", "codex":
		return nil
	case "cursor":
		// cursor-agent has no effort flag. Some models take an effort
		// parameter in the model name itself ('<model>[effort=high]'), which
		// is per model and so is the operator's to pass with --model.
		return fmt.Errorf("cursor has no reasoning effort setting, so effort %q cannot be honoured; "+
			"run without an effort, choose a model with --model, or use a different backend", level)
	case "opencode":
		// OpenCode has no effort flag: effort is a model variant written
		// into the model id ('-m provider/model#variant'). Variants are per
		// model and their names are the provider's, not a neutral low,
		// medium and high, so mapping a level onto '#low' would invent a
		// variant that most models do not have. The operator who wants one
		// passes it with --model.
		return fmt.Errorf("opencode has no reasoning effort setting, so effort %q cannot be honoured; "+
			"run without an effort, pick a model variant with --model (provider/model#variant), or use a different backend", level)
	default:
		return fmt.Errorf("effort is not supported by the %s backend (supported: claude, codex)", backend)
	}
}
