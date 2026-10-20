package agentactivity

import "testing"

func TestDisplay(t *testing.T) {
	tests := []struct {
		name      string
		lifecycle string
		a         *Activity
		key, text string
	}{
		{"starting wins", "starting", &Activity{State: Working}, "starting", "Starting"},
		{"stopping wins", "stopping", &Activity{State: Waiting}, "stopping", "Stopping"},
		{"exited wins over idle", "exited", &Activity{State: Idle}, "exited", "Exited"},
		{"failed wins", "failed", &Activity{State: Working}, "failed", "Failed"},
		{"exited without activity", "exited", nil, "exited", "Exited"},
		{"running without activity", "running", nil, "running", "Running"},
		{"empty lifecycle without activity", "", nil, "running", "Running"},
		{"unknown state", "running", &Activity{State: Unknown}, "running", "Running"},
		{"unrecognized state", "running", &Activity{State: "future"}, "running", "Running"},
		{"empty state", "running", &Activity{}, "running", "Running"},
		{"working", "running", &Activity{State: Working}, "working", "Working"},
		{"stalled", "running", &Activity{State: Working, Stalled: true}, "stalled", "Stalled"},
		{"idle", "running", &Activity{State: Idle, Reason: ReasonTurnComplete}, "idle", "Idle"},
		{"waiting permission", "running", &Activity{State: Waiting, Reason: ReasonPermission}, "waiting", "Needs input (permission)"},
		{"waiting plan", "running", &Activity{State: Waiting, Reason: ReasonPlan}, "waiting", "Needs input (plan)"},
		{"waiting no reason", "running", &Activity{State: Waiting}, "waiting", "Needs input"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key, label := Display(tt.lifecycle, tt.a)
			if key != tt.key || label != tt.text {
				t.Fatalf("Display = (%q, %q), want (%q, %q)", key, label, tt.key, tt.text)
			}
		})
	}
}
