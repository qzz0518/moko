package history

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

var t0 = time.Date(2026, 11, 2, 12, 0, 0, 0, time.Local)

func sample(at time.Time, pm int, wh int64) Sample {
	return Sample{At: at, PM25: pm, TempC: 24, Humidity: 51, Watts: 13, EnergyWh: wh}
}

func TestAddSurvivesReopen(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir, t0)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Add(sample(t0, 3, 100)); err != nil {
		t.Fatal(err)
	}
	if err := s.Add(sample(t0.Add(time.Minute), 4, 101)); err != nil {
		t.Fatal(err)
	}
	s, err = Open(dir, t0.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	got := s.Since(t0.Add(-time.Hour))
	if len(got) != 2 || got[1].PM25 != 4 || got[1].EnergyWh != 101 || !got[0].At.Equal(t0) {
		t.Fatalf("samples = %+v", got)
	}
}

func TestReadsLastMonthAcrossTheBoundary(t *testing.T) {
	dir := t.TempDir()
	s, _ := Open(dir, t0)
	lastMonth := t0.AddDate(0, 0, -10) // October
	s.Add(sample(lastMonth, 1, 10))
	s.Add(sample(t0, 2, 20))
	s, _ = Open(dir, t0)
	if got := s.Since(t0.AddDate(0, 0, -20)); len(got) != 2 {
		t.Fatalf("samples = %+v", got)
	}
}

func TestCorruptLinesAreSkipped(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, t0.Format("2006-01")+".csv")
	good := "1793620000,1,24,51,13,100\n"
	if err := os.WriteFile(path, []byte(good+"garbage\n1793620060,2,24\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, _ := Open(dir, time.Unix(1793620100, 0))
	if got := s.Since(time.Time{}); len(got) != 1 || got[0].PM25 != 1 {
		t.Fatalf("samples = %+v", got)
	}
}

func TestOldFilesAreRemoved(t *testing.T) {
	dir := t.TempDir()
	old := filepath.Join(dir, "2020-01.csv")
	keep := filepath.Join(dir, "2026-10.csv")
	other := filepath.Join(dir, "notes.txt")
	for _, p := range []string{old, keep, other} {
		os.WriteFile(p, nil, 0o644)
	}
	if _, err := Open(dir, t0); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(old); err == nil {
		t.Error("old file stayed")
	}
	if _, err := os.Stat(keep); err != nil {
		t.Error("recent file went")
	}
	if _, err := os.Stat(other); err != nil {
		t.Error("removed a file that is not ours")
	}
}

func TestAddDropsWhatMemoryNeedNotKeep(t *testing.T) {
	s, _ := Open(t.TempDir(), t0)
	s.Add(sample(t0.Add(-Keep-time.Hour), 1, 1))
	s.Add(sample(t0, 2, 2))
	if got := s.Since(time.Time{}); len(got) != 1 || got[0].PM25 != 2 {
		t.Fatalf("samples = %+v", got)
	}
}

func TestDaily(t *testing.T) {
	day1 := time.Date(2026, 11, 1, 23, 50, 0, 0, time.Local)
	day2 := time.Date(2026, 11, 2, 0, 10, 0, 0, time.Local)
	got := Daily([]Sample{
		sample(day1, 0, 100),
		sample(day1.Add(5*time.Minute), 0, 110),
		{At: day1.Add(6 * time.Minute), EnergyWh: -1}, // unread: does not break the count
		sample(day2, 0, 130),
		sample(day2.Add(time.Hour), 0, 150),
	}, day1, Energy)
	if len(got) != 2 || got[0].Amount != 10 || got[1].Amount != 40 {
		t.Fatalf("days = %+v", got)
	}
}

func TestDailyIgnoresCounterReset(t *testing.T) {
	got := Daily([]Sample{sample(t0, 0, 1000), sample(t0.Add(time.Minute), 0, 5), sample(t0.Add(2*time.Minute), 0, 8)}, t0, Energy)
	if len(got) != 1 || got[0].Amount != 3 {
		t.Fatalf("days = %+v", got)
	}
}

func TestBuckets(t *testing.T) {
	end := t0.Add(4 * time.Hour)
	got := Buckets([]Sample{
		sample(t0.Add(10*time.Minute), 2, 0),
		sample(t0.Add(20*time.Minute), 4, 0),
		sample(t0.Add(3*time.Hour), -1, 0),
		sample(end, 8, 0),
	}, t0, end, 4, func(s Sample) int { return s.PM25 })
	if got[0].Value != 3 || got[0].Empty {
		t.Errorf("first = %+v", got[0])
	}
	if !got[1].Empty || !got[2].Empty {
		t.Errorf("gaps = %+v %+v", got[1], got[2])
	}
	if got[3].Value != 8 {
		t.Errorf("the end belongs to the last: %+v", got[3])
	}
}

// Files from before the air was recorded still read, with the air unknown.
func TestOldLinesHaveNoAir(t *testing.T) {
	old, ok := parseLine("1793600000,3,24,51,13,126886")
	if !ok || old.AirM3 != -1 || old.EnergyWh != 126886 {
		t.Errorf("old line = %+v, %v", old, ok)
	}
	line, ok := parseLine("1793600000,3,24,51,13,126886,1722000")
	if !ok || line.AirM3 != 1722000 {
		t.Errorf("line = %+v, %v", line, ok)
	}
	if _, ok := parseLine("1793600000,3,24"); ok {
		t.Error("a short line was read")
	}
}

func TestDailyAir(t *testing.T) {
	t0 := time.Date(2026, 11, 3, 8, 0, 0, 0, time.Local)
	got := Daily([]Sample{
		{At: t0, AirM3: -1, EnergyWh: 1},
		{At: t0.Add(time.Minute), AirM3: 1000, EnergyWh: 1},
		{At: t0.Add(2 * time.Minute), AirM3: 1003, EnergyWh: 1},
	}, t0, Air)
	if len(got) != 1 || got[0].Amount != 3 {
		t.Errorf("days = %+v", got)
	}
}
