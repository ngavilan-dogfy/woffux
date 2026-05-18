package cmd

import "testing"

func TestParseDateList(t *testing.T) {
	got, err := parseDateList("2026-05-18, 2026-05-19")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 || got[0] != "2026-05-18" || got[1] != "2026-05-19" {
		t.Fatalf("unexpected dates: %#v", got)
	}

	for _, value := range []string{"", "2026-05-18,", "18-05-2026", "2026-99-18"} {
		t.Run(value, func(t *testing.T) {
			if _, err := parseDateList(value); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}
