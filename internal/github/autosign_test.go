package github

import "testing"

func TestIsAutoManagedWorkflow(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{name: "Auto Sign", want: true},
		{name: "Keepalive", want: true},
		{name: "Manual Sign", want: false},
		{name: "Release", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isAutoManagedWorkflow(tt.name); got != tt.want {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAutoSignEnabledFromWorkflowsIgnoresAutoVersion(t *testing.T) {
	enabled, found := autoSignEnabledFromWorkflows([]WorkflowStatus{
		{Name: "Auto Version", State: "active"},
		{Name: "Auto Sign", State: "disabled_manually"},
	})

	if !found {
		t.Fatal("expected Auto Sign workflow to be found")
	}
	if enabled {
		t.Fatal("Auto Version must not make Auto Sign look enabled")
	}
}

func TestAutoSignEnabledFromWorkflowsRequiresExactWorkflow(t *testing.T) {
	enabled, found := autoSignEnabledFromWorkflows([]WorkflowStatus{
		{Name: "Auto Something Else", State: "active"},
	})

	if found || enabled {
		t.Fatalf("unexpected Auto Sign match: enabled=%v found=%v", enabled, found)
	}
}
