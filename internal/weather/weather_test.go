package weather

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func forecastJSON(start time.Time) string {
	var hours []string
	var temps []float64
	var codes []int
	for i := range 48 {
		hours = append(hours, start.Add(time.Duration(i)*time.Hour).Format("2006-01-02T15:04"))
		temps = append(temps, 10+float64(i))
		codes = append(codes, 0)
	}
	days := []string{}
	for i := range 5 {
		days = append(days, start.AddDate(0, 0, i).Format("2006-01-02"))
	}
	b, _ := json.Marshal(map[string]any{
		"utc_offset_seconds": 9 * 3600,
		"current":            map[string]any{"time": "2026-11-03T02:45", "temperature_2m": 12.1, "relative_humidity_2m": 82, "weather_code": 61, "is_day": 0},
		"hourly":             map[string]any{"time": hours, "temperature_2m": temps, "weather_code": codes},
		"daily": map[string]any{"time": days, "weather_code": []int{0, 3, 61, 71, 95},
			"temperature_2m_max": []float64{19, 15, 18, 23, 20}, "temperature_2m_min": []float64{10, 13, 11, 14, 9},
			"precipitation_probability_max": []any{0, 70, 80, nil, 10}},
	})
	return string(b)
}

func testClient(t *testing.T) (*Client, *time.Time) {
	t.Helper()
	zone := time.FixedZone("", 9*3600)
	now := time.Date(2026, 11, 3, 2, 45, 0, 0, zone)
	mux := http.NewServeMux()
	mux.HandleFunc("/forecast", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("latitude") != "35.700" {
			t.Errorf("latitude = %q", r.URL.Query().Get("latitude"))
		}
		fmt.Fprint(w, forecastJSON(time.Date(2026, 11, 3, 0, 0, 0, 0, zone)))
	})
	mux.HandleFunc("/geo", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("name") != "Osaka" || r.URL.Query().Get("language") != "ja" {
			t.Errorf("query = %q", r.URL.RawQuery)
		}
		fmt.Fprint(w, `{"results":[{"name":"大阪市","admin1":"大阪府","country":"日本","latitude":34.7,"longitude":135.5},{"name":"大阪","admin1":"大阪","country":"日本","latitude":34.6,"longitude":135.4}]}`)
	})
	mux.HandleFunc("/down", func(w http.ResponseWriter, r *http.Request) { http.Error(w, "no", http.StatusTooManyRequests) })
	mux.HandleFunc("/empty", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{"success":false}`) })
	mux.HandleFunc("/ip", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("lang") != "zh-CN" {
			t.Errorf("lang = %q", r.URL.Query().Get("lang"))
		}
		fmt.Fprint(w, `{"city":"Tokyo","region":"Tokyo","country":"Japan","latitude":35.7,"longitude":139.7}`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return &Client{
		HTTP: srv.Client(), Forecast: srv.URL + "/forecast", Geocode: srv.URL + "/geo",
		Locators: []string{srv.URL + "/down", srv.URL + "/empty", srv.URL + "/ip"},
		Now:      func() time.Time { return now },
	}, &now
}

func TestReport(t *testing.T) {
	c, now := testClient(t)
	r, err := c.Report(context.Background(), Place{Name: "x", Lat: 35.7, Lon: 139.7})
	if err != nil {
		t.Fatal(err)
	}
	if r.TempC != 12.1 || r.Humidity != 82 || r.Kind != Rain || r.Daytime {
		t.Errorf("now = %+v", r)
	}
	if len(r.Hours) != hoursShown {
		t.Fatalf("%d hours", len(r.Hours))
	}
	if !r.Hours[0].At.After(*now) || r.Hours[1].At.Sub(r.Hours[0].At) != 3*time.Hour {
		t.Errorf("hours %v, %v", r.Hours[0].At, r.Hours[1].At)
	}
	if len(r.Days) != daysShown {
		t.Fatalf("%d days", len(r.Days))
	}
	if d := r.Days[0]; d.Date.Day() != 4 || d.MaxC != 15 || d.MinC != 13 || d.Kind != Cloudy || d.RainPct != 70 {
		t.Errorf("tomorrow = %+v", d)
	}
	if r.Days[2].RainPct != -1 {
		t.Errorf("an unknown chance of rain = %d", r.Days[2].RainPct)
	}
}

func TestSearch(t *testing.T) {
	c, _ := testClient(t)
	got, err := c.Search(context.Background(), " Osaka ", "ja")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Name != "大阪市" || got[0].Detail != "大阪府, 日本" {
		t.Errorf("places = %+v", got)
	}
	if got[1].Detail != "日本" {
		t.Errorf("a region named as the place is said twice: %q", got[1].Detail)
	}
	if got, err := c.Search(context.Background(), "  ", "ja"); err != nil || got != nil {
		t.Errorf("empty search = %v, %v", got, err)
	}
}

func TestLocateTriesTheNextService(t *testing.T) {
	c, _ := testClient(t)
	p, err := c.Locate(context.Background(), "zh")
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "Tokyo" || p.Detail != "Tokyo, Japan" || p.Lat != 35.7 {
		t.Errorf("place = %+v", p)
	}
	c.Locators = c.Locators[:2]
	if _, err := c.Locate(context.Background(), "zh"); err == nil {
		t.Error("no service answered, and no error")
	}
}

func TestKindOf(t *testing.T) {
	for code, want := range map[int]Kind{0: Clear, 2: PartlyCloudy, 3: Cloudy, 45: Fog, 53: Drizzle, 63: Rain, 81: Rain, 73: Snow, 86: Snow, 95: Thunder, 99: Thunder} {
		if got := KindOf(code); got != want {
			t.Errorf("code %d: %v, want %v", code, got, want)
		}
	}
}
