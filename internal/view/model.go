// Package view draws the app in MyGo's native UI: the main window and the panel of
// the tray icon. It reads a Model and reports what the user does through the
// Model's functions; everything happens on the main thread.
package view

import (
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/qzz0518/moko/internal/device"
	"github.com/qzz0518/moko/internal/history"
	"github.com/qzz0518/moko/internal/homekit"
	"github.com/qzz0518/moko/internal/i18n"
	"github.com/qzz0518/moko/internal/sharp"
	"github.com/qzz0518/moko/internal/weather"
)

// Page is a page of the main window.
type Page int

// The pages.
const (
	PageOverview Page = iota
	PageWeather
	PageTrends
	PageEnergy
	PageHome
	PageSettings
)

// Firewall states of Windows Firewall towards the app. Elsewhere it is always
// FirewallAllowed.
const (
	FirewallChecking = "checking"
	FirewallAllowed  = "allowed"
	FirewallMissing  = "missing"
	FirewallBlocked  = "blocked"
)

// Model is what the views show and what they can ask for. The app fills it in.
type Model struct {
	Snap    device.Snapshot
	History []history.Sample

	// A change the device has not confirmed yet shows at once, and goes back if it
	// fails. Zero or nil for none.
	PendingMode     sharp.Mode
	PendingPower    *bool
	PendingHumidify *bool

	Notice string // shown once, as a toast

	Version     string
	ListenErr   string // the app could not open its network socket
	Firewall    string
	OpenAtLogin bool
	KeepAwake   bool
	WaterAlert  bool
	Language    string // "", "zh", "ja", "en"
	Appearance  string // "", "light", "dark"
	ManualIP    string
	Price       float64 // of a kWh, in yen

	HomeEnabled bool
	Home        homekit.Status

	Weather        *weather.Report // nil until there is one
	WeatherOff     bool
	Place          string          // the name of the place the weather is of
	PlaceFixed     bool            // the user chose it
	Places         []weather.Place // what a search found
	PlaceSearching bool
	PlaceNote      string // what became of a search

	SetPower       func(on bool)
	SetMode        func(m sharp.Mode)
	SetHumidify    func(on bool)
	SetHomeEnabled func(on bool)
	ResetHome      func()
	SetKeepAwake   func(on bool)
	SetOpenAtLogin func(on bool)
	SetWaterAlert  func(on bool)
	SetLanguage    func(lang string)
	SetAppearance  func(appearance string)
	SetWeatherOn   func(on bool)
	SearchPlace    func(query string)
	ChoosePlace    func(p *weather.Place) // nil goes back to estimating it
	CheckUpdate    func()
	SetManualIP    func(ip string)
	SetPrice       func(price float64)
	AllowFirewall  func()
	SkipFirewall   func()
	OpenNetwork    func() // the system's local network settings
	OpenMain       func()
	OpenURL        func(url string)
	Quit           func()
}

// on is the power as it should show.
func (m *Model) on() bool {
	if m.PendingPower != nil {
		return *m.PendingPower
	}
	return m.Snap.State.On
}

// mode is the mode as it should show.
func (m *Model) mode() sharp.Mode {
	if m.PendingMode != 0 {
		return m.PendingMode
	}
	return m.Snap.State.Mode
}

// humidify is whether humidifying should show as on.
func (m *Model) humidify() bool {
	if m.PendingHumidify != nil {
		return *m.PendingHumidify
	}
	return m.Snap.State.Humidify
}

// reachable is whether commands can reach the device.
func (m *Model) reachable() bool { return m.Snap.Phase == device.PhaseOnline }

// View holds what the views keep between frames.
type View struct {
	M    *Model
	Page Page

	// Now is the clock; tests hold it still.
	Now func() time.Time
	// Theme replaces the system's, for tests.
	Theme *ui.Theme

	metric     int // the overview's chart
	rangeIdx   int // the trend page's range
	lastPrice  float64
	manualIP   string
	ipEdited   bool
	askReset   bool
	placeQuery string
	series     seriesCache
}

// New makes the views of a model.
func New(m *Model) *View { return &View{M: m, Now: time.Now} }

// setup themes a window and returns its colors.
func (v *View) setup(c *ui.Context) pal {
	base := c.Theme()
	if v.Theme != nil {
		base = v.Theme
	}
	k := palette(base.Dark)
	t := *base
	t.Background, t.Text, t.TextMuted = k.bg, k.text, k.muted
	t.Surface, t.SurfaceHover, t.SurfacePressed = k.card2, k.card2.Mix(k.text, 0.05), k.card2.Mix(k.text, 0.1)
	t.Accent, t.AccentHover, t.AccentPressed, t.AccentText = k.accent, k.accent.Mix(k.onAccent, 0.1), k.accent.Mix(k.text, 0.1), k.onAccent
	t.Focus = k.accent
	t.Border = k.border
	t.Radius = 10
	t.Font = "Nunito"
	c.SetTheme(&t)
	if v.M.Notice != "" {
		c.Toast(v.M.Notice)
		v.M.Notice = ""
	}
	return k
}

// langTag tells the text engine the language, which picks the right forms of the
// characters Chinese and Japanese share.
func langTag() string {
	switch i18n.Lang() {
	case "ja":
		return "ja"
	case "en":
		return "en"
	}
	return "zh-Hans"
}

// modeName names a mode.
func modeName(m sharp.Mode) string {
	switch m {
	case sharp.ModeAuto:
		return i18n.T("自动", "自動", "Auto")
	case sharp.ModeNight:
		return i18n.T("夜间", "おやすみ", "Night")
	case sharp.ModePollen:
		return i18n.T("花粉", "花粉", "Pollen")
	case sharp.ModeSilent:
		return i18n.T("静音", "静音", "Quiet")
	case sharp.ModeMedium:
		return i18n.T("中", "中", "Medium")
	case sharp.ModeHigh:
		return i18n.T("强", "強", "High")
	case sharp.ModeAIAuto:
		return i18n.T("AI 自动", "AI 自動", "AI Auto")
	case sharp.ModeBoost:
		return i18n.T("效果实感", "効果実感", "Boost")
	case sharp.ModeDroplet:
		return i18n.T("飞沫粒子", "飛沫粒子", "Droplets")
	}
	return "—"
}
