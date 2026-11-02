package agentactivity

// Display collapses lifecycle and activity into the one status clients show.
// The lifecycle states starting, stopping, exited and failed win over any
// activity (a dead process is never "Idle"); otherwise the activity decides.
// Unknown, missing or unrecognized activity displays as plain "Running", never
// as an error, because hooks may be disabled or unsupported.
//
// Keys are stable identifiers for styling; labels are user text. A Stalled
// Working session has its own key.
func Display(lifecycle string, a *Activity) (key, label string) {
	switch lifecycle {
	case "starting":
		return "starting", "Starting"
	case "stopping":
		return "stopping", "Stopping"
	case "exited":
		return "exited", "Exited"
	case "failed":
		return "failed", "Failed"
	}
	if a == nil {
		return "running", "Running"
	}
	switch a.State {
	case Working:
		if a.Stalled {
			return "stalled", "Stalled"
		}
		return "working", "Working"
	case Waiting:
		if a.Reason != "" {
			return "waiting", "Needs input (" + string(a.Reason) + ")"
		}
		return "waiting", "Needs input"
	case Idle:
		return "idle", "Idle"
	}
	return "running", "Running"
}
