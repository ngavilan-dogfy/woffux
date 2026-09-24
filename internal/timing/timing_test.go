package timing

import (
	"testing"
	"time"
)

var day = []Event{{510, true}, {810, false}, {855, true}, {1050, false}} // 08:30 13:30 14:15 17:30

func dateN(n int) time.Time {
	return time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC).AddDate(0, 0, n)
}

func TestTargetsStayInsideWindows(t *testing.T) {
	s := Settings{InEarly: 6, InLate: 1, OutLate: 8, Seed: "abc"}
	for n := 0; n < 400; n++ {
		d := dateN(n)
		got := s.Targets(d, day)
		for i, e := range day {
			planned := time.Date(d.Year(), d.Month(), d.Day(), 0, e.Minute, 0, 0, time.UTC)
			off := got[i].Sub(planned)
			early, late := s.InEarly, s.InLate
			if !e.In {
				early, late = s.OutEarly, s.OutLate
			}
			if off < -time.Duration(early)*time.Minute || off > time.Duration(late)*time.Minute {
				t.Fatalf("%s event %d offset %v outside [-%d,+%d]m", d.Format("2006-01-02"), i, off, early, late)
			}
		}
	}
}

func TestBlocksNeverShrink(t *testing.T) {
	s := Settings{InEarly: 3, InLate: 10, OutEarly: 5, OutLate: 4, Seed: "shrink"}
	for n := 0; n < 400; n++ {
		d := dateN(n)
		got := s.Targets(d, day)
		for i := 0; i+1 < len(day); i += 2 {
			planned := time.Duration(day[i+1].Minute-day[i].Minute) * time.Minute
			if got[i+1].Sub(got[i]) < planned {
				t.Fatalf("%s block %d is %v, shorter than planned %v", d.Format("2006-01-02"), i/2, got[i+1].Sub(got[i]), planned)
			}
		}
	}
}

func TestTargetsAreDeterministicAndVaried(t *testing.T) {
	s := Settings{InEarly: 6, InLate: 1, OutLate: 8, Seed: "seed-1"}
	a := s.Targets(dateN(3), day)
	b := s.Targets(dateN(3), day)
	for i := range a {
		if !a[i].Equal(b[i]) {
			t.Fatal("same seed and day must give the same moments (all signers agree)")
		}
	}
	seen := map[string]bool{}
	for n := 0; n < 30; n++ {
		seen[s.Targets(dateN(n), day)[0].Format("15:04:05")] = true
	}
	if len(seen) < 20 {
		t.Fatalf("only %d distinct IN moments over 30 days — not natural", len(seen))
	}
	other := Settings{InEarly: 6, InLate: 1, OutLate: 8, Seed: "seed-2"}.Targets(dateN(3), day)
	if other[0].Equal(a[0]) && other[1].Equal(a[1]) {
		t.Fatal("different seeds should give different moments")
	}
}

func TestExactIsExact(t *testing.T) {
	got := Settings{}.Targets(dateN(0), day)
	if got[0].Format("15:04:05") != "08:30:00" || got[3].Format("15:04:05") != "17:30:00" {
		t.Fatalf("exact timing moved signs: %v", got)
	}
}

func TestEncodeDecode(t *testing.T) {
	s := Settings{InEarly: 6, InLate: 1, OutLate: 8, Seed: "xyz"}
	back, err := Decode(s.Encode())
	if err != nil || back != s {
		t.Fatalf("round trip: %+v, %v", back, err)
	}
	if empty, err := Decode(""); err != nil || empty.Active() {
		t.Fatal("empty means exact")
	}
	if _, err := Decode("1,2,3"); err == nil {
		t.Fatal("malformed timing must fail")
	}
}

func TestPresetKey(t *testing.T) {
	for _, p := range Presets {
		if got := (Settings{}).WithPreset(p).PresetKey(); got != p.Key {
			t.Fatalf("preset %s detected as %s", p.Key, got)
		}
	}
}

func TestTargetsNeverCrossMidnight(t *testing.T) {
	s := Settings{OutLate: 15, Seed: "late"}
	late := []Event{{Minute: 22*60 + 55, In: true}, {Minute: 23*60 + 55, In: false}}
	for n := 0; n < 100; n++ {
		d := dateN(n)
		for _, m := range s.Targets(d, late) {
			if m.Day() != d.Day() {
				t.Fatalf("moment %v left the day", m)
			}
		}
	}
}

func TestTargetsRespectDST(t *testing.T) {
	madrid, err := time.LoadLocation("Europe/Madrid")
	if err != nil {
		t.Skip("no tzdata")
	}
	dstDay := time.Date(2026, 3, 29, 0, 0, 0, 0, madrid) // clocks go forward at 02:00
	got := Settings{}.Targets(dstDay, []Event{{Minute: 8*60 + 30, In: true}})
	if got[0].Format("15:04") != "08:30" {
		t.Fatalf("08:30 on a DST day became %s", got[0].Format("15:04"))
	}
}
