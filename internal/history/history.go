// Package history keeps a sample of the readings each minute, for the trend and
// energy pages. Samples go to one CSV file per month, small enough to read at
// start and to look into by hand.
package history

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Sample is one reading. A negative value is unknown.
type Sample struct {
	At       time.Time
	PM25     int
	TempC    int
	Humidity int
	Watts    int
	EnergyWh int64
	AirM3    int64 // the air the device has cleaned, in all
}

// Keep is how long samples stay in memory, the longest range the app shows.
const Keep = 31 * 24 * time.Hour

const keepFiles = 13 // months of files on disk

// Store is the samples, in order of time. Its methods are safe from any goroutine.
type Store struct {
	dir string

	mu      sync.Mutex
	samples []Sample
}

// Open reads what is on disk of the current and last month, and removes files
// too old to keep.
func Open(dir string, now time.Time) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	s := &Store{dir: dir}
	s.prune(now)
	cutoff := now.Add(-Keep)
	for _, month := range []time.Time{firstOfMonth(now).AddDate(0, -1, 0), now} {
		s.samples = append(s.samples, readFile(s.file(month), cutoff)...)
	}
	return s, nil
}

func firstOfMonth(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, t.Location())
}

func (s *Store) file(t time.Time) string {
	return filepath.Join(s.dir, t.Format("2006-01")+".csv")
}

func (s *Store) prune(now time.Time) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return
	}
	oldest := firstOfMonth(now).AddDate(0, -keepFiles, 0).Format("2006-01")
	for _, e := range entries {
		name, ok := strings.CutSuffix(e.Name(), ".csv")
		if ok && name < oldest {
			os.Remove(filepath.Join(s.dir, e.Name()))
		}
	}
}

// readFile reads samples since cutoff, skipping lines it cannot read: a line cut
// short by a crash must not lose the rest.
func readFile(path string, cutoff time.Time) []Sample {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []Sample
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if smp, ok := parseLine(sc.Text()); ok && smp.At.After(cutoff) {
			out = append(out, smp)
		}
	}
	return out
}

// parseLine reads a line of a file. Lines from before the air was recorded have no
// seventh field.
func parseLine(line string) (Sample, bool) {
	fields := strings.Split(line, ",")
	if len(fields) != 6 && len(fields) != 7 {
		return Sample{}, false
	}
	nums := [7]int64{6: -1}
	for i, f := range fields {
		n, err := strconv.ParseInt(f, 10, 64)
		if err != nil {
			return Sample{}, false
		}
		nums[i] = n
	}
	return Sample{At: time.Unix(nums[0], 0), PM25: int(nums[1]), TempC: int(nums[2]),
		Humidity: int(nums[3]), Watts: int(nums[4]), EnergyWh: nums[5], AirM3: nums[6]}, true
}

// Add records a sample.
func (s *Store) Add(smp Sample) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.samples = append(s.samples, smp)
	if cutoff := smp.At.Add(-Keep); len(s.samples) > 0 && s.samples[0].At.Before(cutoff) {
		i := sort.Search(len(s.samples), func(i int) bool { return !s.samples[i].At.Before(cutoff) })
		s.samples = append(s.samples[:0], s.samples[i:]...)
	}
	f, err := os.OpenFile(s.file(smp.At), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = fmt.Fprintf(f, "%d,%d,%d,%d,%d,%d,%d\n", smp.At.Unix(), smp.PM25, smp.TempC, smp.Humidity, smp.Watts, smp.EnergyWh, smp.AirM3)
	return err
}

// Since returns a copy of the samples since t.
func (s *Store) Since(t time.Time) []Sample {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := sort.Search(len(s.samples), func(i int) bool { return !s.samples[i].At.Before(t) })
	return append([]Sample(nil), s.samples[i:]...)
}

// Day is the energy used on one day.
type Day struct {
	Date   time.Time // midnight, local time
	Amount int64
}

// Energy is the device's counter of energy, for Daily.
func Energy(s Sample) int64 { return s.EnergyWh }

// Air is the device's counter of the air it has cleaned, for Daily.
func Air(s Sample) int64 { return s.AirM3 }

// Daily sums the increase of a counter by day, for the days since from. Each increase
// counts for the day it was seen on, so that what the device did while the app was
// closed lands on the day the app next ran. A negative value is unknown.
func Daily(samples []Sample, from time.Time, counter func(Sample) int64) []Day {
	var days []Day
	var last int64 = -1
	for _, smp := range samples {
		v := counter(smp)
		if v < 0 {
			continue
		}
		if last >= 0 && v > last && !smp.At.Before(from) {
			date := midnight(smp.At)
			if n := len(days); n > 0 && days[n-1].Date.Equal(date) {
				days[n-1].Amount += v - last
			} else {
				days = append(days, Day{Date: date, Amount: v - last})
			}
		}
		last = v
	}
	return days
}

func midnight(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}

// Bucket is a stretch of time with its samples averaged.
type Bucket struct {
	At    time.Time
	Value float64
	Empty bool
}

// Buckets averages a value of the samples into n stretches from start to end. A
// negative value is unknown and does not count; a stretch without any is Empty,
// which a chart shows as a gap.
func Buckets(samples []Sample, start, end time.Time, n int, value func(Sample) int) []Bucket {
	out := make([]Bucket, n)
	span := end.Sub(start)
	sum := make([]float64, n)
	count := make([]int, n)
	for _, smp := range samples {
		if smp.At.Before(start) || smp.At.After(end) {
			continue
		}
		v := value(smp)
		if v < 0 {
			continue
		}
		i := min(int(smp.At.Sub(start)*time.Duration(n)/span), n-1)
		sum[i] += float64(v)
		count[i]++
	}
	for i := range out {
		out[i].At = start.Add(span * time.Duration(i) / time.Duration(n))
		if count[i] == 0 {
			out[i].Empty = true
			continue
		}
		out[i].Value = sum[i] / float64(count[i])
	}
	return out
}
