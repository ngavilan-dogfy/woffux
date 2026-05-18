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
