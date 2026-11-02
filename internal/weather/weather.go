// Package weather reads the weather outdoors from Open-Meteo, which needs no key. It
// also finds places by name, and estimates where the computer is from its public IP
// address when the user has not named a place.
//
// Open-Meteo's PM2.5 is left out: it is a model's estimate, and next to the reading of a
// sensor indoors it would look like a measurement.
package weather

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Place is somewhere to have the weather of.
type Place struct {
	Name   string  `json:"name"`
	Detail string  `json:"detail,omitempty"` // its region and country
	Lat    float64 `json:"lat"`
	Lon    float64 `json:"lon"`
}

// Kind is a kind of weather.
type Kind int

// The kinds of weather.
const (
	Clear Kind = iota
	PartlyCloudy
	Cloudy
	Fog
	Drizzle
	Rain
	Snow
	Thunder
)

// KindOf reads a WMO weather code.
func KindOf(code int) Kind {
	switch {
	case code == 0:
		return Clear
	case code == 1 || code == 2:
		return PartlyCloudy
	case code == 3:
		return Cloudy
	case code == 45 || code == 48:
		return Fog
	case code >= 51 && code <= 57:
		return Drizzle
	case code >= 61 && code <= 67, code >= 80 && code <= 82:
		return Rain
	case code >= 71 && code <= 77, code == 85, code == 86:
		return Snow
	case code >= 95:
		return Thunder
	}
	return Cloudy
}

// Hour is the forecast of an hour.
type Hour struct {
	At    time.Time
	TempC float64
	Kind  Kind
}

// Day is the forecast of a day.
type Day struct {
	Date       time.Time
	MaxC, MinC float64
	Kind       Kind
	RainPct    int // -1 when unknown
}

// Report is the weather of a place at a moment.
type Report struct {
	Place    Place
	At       time.Time
	TempC    float64
	Humidity int
	Kind     Kind
	Daytime  bool
	Hours    []Hour // the coming hours, every third one
	Days     []Day  // the days after today
}

const (
	hoursShown = 8
	daysShown  = 3
)

// Client talks to the services. The zero value is not usable: use New.
type Client struct {
	HTTP     *http.Client
	Forecast string
	Geocode  string
	Locators []string // tried in order
	Now      func() time.Time
}

// New returns a client of the real services.
func New() *Client {
	return &Client{
		HTTP:     &http.Client{Timeout: 15 * time.Second},
		Forecast: "https://api.open-meteo.com/v1/forecast",
		Geocode:  "https://geocoding-api.open-meteo.com/v1/search",
		Locators: []string{"https://ipwho.is/", "https://ipapi.co/json/"},
		Now:      time.Now,
	}
}

func (c *Client) get(ctx context.Context, base string, query url.Values, into any) error {
	u := base
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "Moko (https://github.com/qzz0518/moko)")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("weather: %s answered %s", req.URL.Host, resp.Status)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(into)
}

func coords(p Place) url.Values {
	return url.Values{
		"latitude":  {strconv.FormatFloat(p.Lat, 'f', 3, 64)},
		"longitude": {strconv.FormatFloat(p.Lon, 'f', 3, 64)},
		"timezone":  {"auto"},
	}
}

type forecastBody struct {
	UTCOffset int `json:"utc_offset_seconds"`
	Current   struct {
		Time     string  `json:"time"`
		Temp     float64 `json:"temperature_2m"`
		Humidity int     `json:"relative_humidity_2m"`
		Code     int     `json:"weather_code"`
		IsDay    int     `json:"is_day"`
	} `json:"current"`
	Hourly struct {
		Time []string  `json:"time"`
		Temp []float64 `json:"temperature_2m"`
		Code []int     `json:"weather_code"`
	} `json:"hourly"`
	Daily struct {
		Time []string  `json:"time"`
		Code []int     `json:"weather_code"`
		Max  []float64 `json:"temperature_2m_max"`
		Min  []float64 `json:"temperature_2m_min"`
		Rain []*int    `json:"precipitation_probability_max"`
	} `json:"daily"`
}

// Report reads the weather of a place.
func (c *Client) Report(ctx context.Context, p Place) (Report, error) {
	q := coords(p)
	q.Set("current", "temperature_2m,relative_humidity_2m,weather_code,is_day")
	q.Set("hourly", "temperature_2m,weather_code")
	q.Set("daily", "weather_code,temperature_2m_max,temperature_2m_min,precipitation_probability_max")
	q.Set("forecast_days", strconv.Itoa(daysShown+2))
	var body forecastBody
	if err := c.get(ctx, c.Forecast, q, &body); err != nil {
		return Report{}, err
	}
	now := c.Now()
	r := Report{
		Place: p, At: now, TempC: body.Current.Temp, Humidity: body.Current.Humidity,
		Kind: KindOf(body.Current.Code), Daytime: body.Current.IsDay != 0,
	}
	zone := time.FixedZone("", body.UTCOffset)
	h := body.Hourly
	if len(h.Temp) != len(h.Time) || len(h.Code) != len(h.Time) {
		return Report{}, errors.New("weather: the hourly forecast does not line up")
	}
	for i, s := range h.Time {
		at, err := time.ParseInLocation("2006-01-02T15:04", s, zone)
		if err != nil || !at.After(now) {
			continue
		}
		// Every third hour from the first to come.
		if len(r.Hours) > 0 && at.Sub(r.Hours[len(r.Hours)-1].At) < 3*time.Hour {
			continue
		}
		r.Hours = append(r.Hours, Hour{At: at, TempC: h.Temp[i], Kind: KindOf(h.Code[i])})
		if len(r.Hours) == hoursShown {
			break
		}
	}
	d := body.Daily
	if len(d.Code) != len(d.Time) || len(d.Max) != len(d.Time) || len(d.Min) != len(d.Time) {
		return Report{}, errors.New("weather: the daily forecast does not line up")
	}
	today := now.In(zone).Format("2006-01-02")
	for i, s := range d.Time {
		if s <= today {
			continue
		}
		date, err := time.ParseInLocation("2006-01-02", s, zone)
		if err != nil {
			continue
		}
		day := Day{Date: date, MaxC: d.Max[i], MinC: d.Min[i], Kind: KindOf(d.Code[i]), RainPct: -1}
		if i < len(d.Rain) && d.Rain[i] != nil {
			day.RainPct = *d.Rain[i]
		}
		r.Days = append(r.Days, day)
		if len(r.Days) == daysShown {
			break
		}
	}

	return r, nil
}

// Search finds places by name, in a language such as "zh", "ja" or "en".
func (c *Client) Search(ctx context.Context, query, lang string) ([]Place, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}
	var body struct {
		Results []struct {
			Name    string  `json:"name"`
			Admin1  string  `json:"admin1"`
			Country string  `json:"country"`
			Lat     float64 `json:"latitude"`
			Lon     float64 `json:"longitude"`
		} `json:"results"`
	}
	q := url.Values{"name": {query}, "count": {"5"}, "language": {lang}, "format": {"json"}}
	if err := c.get(ctx, c.Geocode, q, &body); err != nil {
		return nil, err
	}
	places := make([]Place, 0, len(body.Results))
	for _, r := range body.Results {
		var detail []string
		if r.Admin1 != "" && r.Admin1 != r.Name {
			detail = append(detail, r.Admin1)
		}
		if r.Country != "" {
			detail = append(detail, r.Country)
		}
		places = append(places, Place{Name: r.Name, Detail: strings.Join(detail, ", "), Lat: r.Lat, Lon: r.Lon})
	}
	return places, nil
}

// Locate estimates where this computer is from its public IP address, which the service
// asked sees. A VPN moves it. Names come in the language, such as "zh", "ja" or "en", if
// the service has them.
func (c *Client) Locate(ctx context.Context, lang string) (Place, error) {
	query := url.Values{"lang": {lang}}
	if lang == "zh" {
		query.Set("lang", "zh-CN")
	}
	var last error = errors.New("weather: nowhere to ask")
	for _, base := range c.Locators {
		var body struct {
			City    string   `json:"city"`
			Region  string   `json:"region"`
			Country string   `json:"country"`
			Lat     *float64 `json:"latitude"`
			Lon     *float64 `json:"longitude"`
		}
		if err := c.get(ctx, base, query, &body); err != nil {
			last = err
			continue
		}
		if body.Lat == nil || body.Lon == nil || body.City == "" {
			last = errors.New("weather: the location service had no answer")
			continue
		}
		return Place{Name: body.City, Detail: strings.Trim(body.Region+", "+body.Country, ", "), Lat: *body.Lat, Lon: *body.Lon}, nil
	}
	return Place{}, last
}
