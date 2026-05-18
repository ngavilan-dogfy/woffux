package cmd

import (
	"testing"

	"github.com/ngavilan-dogfy/woffux/internal/woffu"
)

func TestParseExpectedSignAction(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		want    woffu.SignAction
		wantErr bool
	}{
		{name: "empty", value: "", want: ""},
		{name: "in", value: "in", want: woffu.SignActionIn},
		{name: "out", value: "out", want: woffu.SignActionOut},
		{name: "trim and lowercase", value: " OUT ", want: woffu.SignActionOut},
		{name: "invalid", value: "toggle", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseExpectedSignAction(tt.value)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}
