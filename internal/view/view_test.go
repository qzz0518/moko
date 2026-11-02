package view

import (
	"fmt"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/qzz0518/moko/internal/device"
	"github.com/qzz0518/moko/internal/history"
	"github.com/qzz0518/moko/internal/homekit"
	"github.com/qzz0518/moko/internal/i18n"
	"github.com/qzz0518/moko/internal/sharp"
	"github.com/qzz0518/moko/internal/weather"
)

var testNow = time.Date(2026, 11, 2, 21, 0, 0, 0, time.Local)

// online is the state read from the real device on 2026-11-02.
func online() device.Snapshot {
	return device.Snapshot{
		Phase:    device.PhaseOnline,
		HasState: true,
		Info:     device.Info{IP: "192.168.1.20", Model: "KISX100", Module: "HW-A04", Firmware: "104", NodeID: "fe000005"},
		State: device.State{
			On: true, Mode: sharp.ModeAuto, Humidify: true, Humidifier: sharp.HumidifyActive,
			WaterOK: true, StatusOK: true, SensorsOK: true,
			Sensors: sharp.Sensors{TempC: 24, TempOK: true, Humidity: 51, HumidityOK: true, PM25: 0, PM25OK: true, AirCleaned: 1722029,
				Used: sharp.Supplies{Dust: 83, Odor: 83, Humidifying: 86, Silver: 86, Ion: 14317}},
			Limits: sharp.Limits{Dust: 3000, Odor: 3000, Humidifying: 1250, Silver: 225, Ion: 19000}, LimitsOK: true,
			NeedsCleaning: true,
			Watts:         13, WattsOK: true, EnergyWh: 126886, EnergyOK: true,
		},
		UpdatedAt: testNow,
	}
}

// sampleHistory makes up a month of readings, to see the charts with.
func sampleHistory() []history.Sample {
	var out []history.Sample
	used := 0.0
	start := testNow.Add(-30 * 24 * time.Hour)
	for i := 0; i < 30*24*60; i += 5 {
		at := start.Add(time.Duration(i) * time.Minute)
		x := float64(i) / 60
		pm := 4 + 3*math.Sin(x/5) + 1.5*math.Sin(x/1.3)
		if h := at.Hour(); h >= 18 && h < 20 { // cooking
			pm += 22 * math.Sin(float64(at.Sub(at.Truncate(24*time.Hour)).Minutes())/120*math.Pi)
		}
		watts := 13 + 6*math.Sin(x/3.1)
		used += watts * 5 / 60 * (1 + 0.5*math.Sin(x/40))
		energy := 120000 + int64(used)
		out = append(out, history.Sample{
			At: at, PM25: int(math.Max(pm, 0)), TempC: int(23 + 2*math.Sin(x/12)),
			Humidity: int(50 + 6*math.Sin(x/8)), Watts: int(watts), EnergyWh: energy,
			AirM3: 1700000 + int64(float64(i)*(2.5+math.Sin(x/40))),
		})
	}
	return out
}

// sampleWeather makes up a report, to see the weather with.
func sampleWeather() *weather.Report {
	r := &weather.Report{
		Place: weather.Place{Name: "东京"}, At: testNow, TempC: 12, Humidity: 82, Kind: weather.PartlyCloudy, Daytime: true,
	}
	kinds := []weather.Kind{weather.PartlyCloudy, weather.Clear, weather.Clear, weather.Cloudy, weather.Drizzle, weather.Rain, weather.Rain, weather.Cloudy}
	for i, k := range kinds {
		r.Hours = append(r.Hours, weather.Hour{At: testNow.Add(time.Duration(3*i+1) * time.Hour), TempC: 13 + float64(i%4), Kind: k})
	}
	for i, k := range []weather.Kind{weather.Rain, weather.Snow, weather.Clear} {
		r.Days = append(r.Days, weather.Day{Date: testNow.AddDate(0, 0, i+1), MaxC: 15 + 4*float64(i), MinC: 11 + float64(i), Kind: k, RainPct: 70 - 25*i})
	}
	return r
}

func newView(snap device.Snapshot) (*View, *Model) {
	m := &Model{
		Snap: snap, Version: "0.1.0", Firewall: FirewallAllowed, Price: 31, WaterAlert: true, History: sampleHistory(),
		Weather: sampleWeather(), Place: "东京",
	}
	v := New(m)
	v.Now = func() time.Time { return testNow }
	v.Theme = ui.DarkTheme()
	return v, m
}

func tester(t *testing.T, view func(*ui.Context), w, h int) *ui.Tester {
	t.Helper()
	i18n.Set("zh", "")
	return ui.NewTester(view, w, h)
}

func click(t *testing.T, tt *ui.Tester, label string) {
	t.Helper()
	if err := tt.Click(label); err != nil {
		t.Errorf("click %q: %v (showing %q)", label, err, tt.Texts())
	}
}

func TestOverviewShowsTheReadings(t *testing.T) {
	v, _ := newView(online())
	tt := tester(t, v.Main, 1040, 700)
	for _, s := range []string{"空气很清新", "PM2.5", "24", "°C", "51", "13", "运行中", "加湿中 · 水箱有水", "自动", "夜间", "花粉", "效果实感", "飞沫粒子", "静音", "中", "强", "在线"} {
		if !tt.HasText(s) {
			t.Errorf("no %q", s)
		}
	}
}

func TestOverviewChoosingAModeCallsBack(t *testing.T) {
	v, m := newView(online())
	var got sharp.Mode
	m.SetMode = func(x sharp.Mode) { got = x }
	tt := tester(t, v.Main, 1040, 700)
	click(t, tt, "夜间")
	if got != sharp.ModeNight {
		t.Errorf("mode = %#x", got)
	}
	for label, want := range map[string]sharp.Mode{"强": sharp.ModeHigh, "效果实感": sharp.ModeBoost, "飞沫粒子": sharp.ModeDroplet} {
		got = 0
		click(t, tt, label)
		if got != want {
			t.Errorf("%s: mode = %#x", label, got)
		}
	}
}

// Drawing must never command the device: the fan speeds have no selection in auto,
// and a selection of -1 once became the first speed.
func TestDrawingSendsNoCommand(t *testing.T) {
	v, m := newView(online())
	fail := func(what string) { t.Errorf("drawing sent %s", what) }
	m.SetMode = func(sharp.Mode) { fail("a mode") }
	m.SetPower = func(bool) { fail("power") }
	m.SetHumidify = func(bool) { fail("humidify") }
	tt := tester(t, v.Main, 1040, 700)
	tt.Frame()
	tt.Frame()
	fly := tester(t, v.Flyout, FlyoutWidth, FlyoutHeight)
	fly.Frame()
	fly.Frame()
}

// The drifting clouds were once an element over Moko, which took its clicks.
func TestClickingMokoMakesItHop(t *testing.T) {
	v, m := newView(online())
	m.SetMode = func(sharp.Mode) { t.Error("a hop sent a command") }
	m.SetPower = func(bool) { t.Error("a hop sent a command") }
	tt := tester(t, v.Main, 1040, 700)
	tt.Frame()
	before := slices.Clone(tt.Image().Pix)
	tt.ClickAt(600, 200)
	if slices.Equal(before, tt.Image().Pix) {
		t.Error("Moko did not move")
	}
}

func TestClickingTheCurrentModeDoesNothing(t *testing.T) {
	v, m := newView(online())
	called := false
	m.SetMode = func(sharp.Mode) { called = true }
	tt := tester(t, v.Main, 1040, 700)
	click(t, tt, "自动")
	if called {
		t.Error("asked to change to the mode it is in")
	}
}

func TestPendingModeShowsAtOnce(t *testing.T) {
	v, m := newView(online())
	m.PendingMode = sharp.ModeHigh
	tt := tester(t, v.Main, 1040, 700)
	if !tt.HasText("强") {
		t.Fatal("no fan speed")
	}
	// The subtitle of the power row names the pending mode.
	if v.M.mode() != sharp.ModeHigh {
		t.Error("the pending mode does not show")
	}
}

func TestDeviceOff(t *testing.T) {
	snap := online()
	snap.State.On = false
	v, _ := newView(snap)
	tt := tester(t, v.Main, 1040, 700)
	for _, s := range []string{"Moko 睡着了", "选择模式即可开机", "未加湿"} {
		if !tt.HasText(s) {
			t.Errorf("no %q", s)
		}
	}
}

func TestOffline(t *testing.T) {
	snap := online()
	snap.Phase = device.PhaseOffline
	snap.UpdatedAt = testNow.Add(-5 * time.Minute)
	v, m := newView(snap)
	called := false
	m.SetMode = func(sharp.Mode) { called = true }
	tt := tester(t, v.Main, 1040, 700)
	if !tt.HasText("连接中断，正在重连 · 读数更新于 5 分钟前") {
		t.Error("no banner")
	}
	if !tt.HasText("24") {
		t.Error("the last readings should stay")
	}
	_ = tt.Click("夜间") // disabled
	if called {
		t.Error("a command while offline")
	}
}

func TestOutOfWater(t *testing.T) {
	snap := online()
	snap.State.Humidifier = sharp.HumidifyNoWater
	snap.State.WaterOK = false
	v, _ := newView(snap)
	tt := tester(t, v.Main, 1040, 700)
	for _, s := range []string{"缺水，请给水箱加水", "缺水"} {
		if !tt.HasText(s) {
			t.Errorf("no %q", s)
		}
	}
}

// Pollution is said only when there is some: "none detected" is not news.
func TestPollutionShowsOnlyWhenDetected(t *testing.T) {
	v, m := newView(online())
	tt := tester(t, v.Main, 1040, 700)
	if tt.HasText("检测到污染") {
		t.Error("nothing was detected")
	}
	m.Snap.State.Polluted = true
	tt.Frame()
	if !tt.HasText("检测到污染") {
		t.Error("no word of the pollution")
	}
}

func TestChoosingAReadingShowsItsChart(t *testing.T) {
	v, _ := newView(online())
	tt := tester(t, v.Main, 1040, 700)
	click(t, tt, "湿度")
	if v.metric != 1 {
		t.Errorf("metric = %d", v.metric)
	}
}

func TestFaultShows(t *testing.T) {
	snap := online()
	snap.State.Fault = true
	v, _ := newView(snap)
	if !tester(t, v.Main, 1040, 700).HasText("设备报告了故障，请查看机身面板") {
		t.Error("no banner")
	}
}

func TestUnknownReadingsShowADash(t *testing.T) {
	snap := online()
	snap.State.Sensors.PM25OK = false
	snap.State.Sensors.TempOK = false
	v, _ := newView(snap)
	tt := tester(t, v.Main, 1040, 700)
	if !tt.HasText("暂无读数") || !tt.HasText("—") {
		t.Error("an unknown reading must not show a number")
	}
}

func TestSearching(t *testing.T) {
	v, _ := newView(device.Snapshot{})
	tt := tester(t, v.Main, 1040, 700)
	if !tt.HasText("正在寻找你的净化器") || !tt.HasText("搜索中") {
		t.Error("no search")
	}
}

func TestNotFoundOffersAManualAddress(t *testing.T) {
	v, m := newView(device.Snapshot{Err: "no Sharp air purifier answered on the network"})
	var got *string
	m.SetManualIP = func(ip string) { got = &ip }
	tt := tester(t, v.Main, 1040, 700)
	if !tt.HasText("还没有找到净化器") {
		t.Fatal("no message")
	}
	click(t, tt, "重新搜索")
	if got == nil || *got != "" {
		t.Errorf("manual IP = %v", got)
	}
}

func TestFirewallAsksFirst(t *testing.T) {
	v, m := newView(device.Snapshot{})
	m.Firewall = FirewallMissing
	allowed, skipped := false, false
	m.AllowFirewall = func() { allowed = true }
	m.SkipFirewall = func() { skipped = true }
	tt := tester(t, v.Main, 1040, 700)
	if !tt.HasText("需要允许局域网访问") {
		t.Fatal("no card")
	}
	click(t, tt, "允许")
	click(t, tt, "跳过")
	if !allowed || !skipped {
		t.Errorf("allowed = %v, skipped = %v", allowed, skipped)
	}
}

func TestNavigation(t *testing.T) {
	v, _ := newView(online())
	tt := tester(t, v.Main, 1040, 700)
	for label, want := range map[string]string{
		"天气":       "显示室外天气",
		"趋势":       "应用运行时每分钟记录一次",
		"能耗":       "最近 30 天",
		"Apple 家庭": "在“家庭”App、Siri 和自动化里控制净化器",
		"设置":       "手动指定 IP",
		"概览":       "空气很清新",
	} {
		click(t, tt, label)
		if !tt.HasText(want) {
			t.Errorf("%s: no %q", label, want)
		}
	}
}

func TestTrendRanges(t *testing.T) {
	v, _ := newView(online())
	v.Page = PageTrends
	tt := tester(t, v.Main, 1040, 700)
	click(t, tt, "30 天")
	if v.rangeIdx != 2 {
		t.Errorf("range = %d", v.rangeIdx)
	}
	if !tt.HasText("最高") {
		t.Error("no statistics")
	}
}

func TestEnergyTotals(t *testing.T) {
	v, _ := newView(online())
	v.Page = PageEnergy
	tt := tester(t, v.Main, 1040, 700)
	for _, s := range []string{"今日", "本月", "设备累计", "126.89", "约 3,933 日元"} {
		if !tt.HasText(s) {
			t.Errorf("no %q", s)
		}
	}
}

func TestEmptyHistory(t *testing.T) {
	v, m := newView(online())
	m.History = nil
	tt := tester(t, v.Main, 1040, 700)
	if !tt.HasText("正在积累数据，稍后再来看") {
		t.Error("the overview's chart")
	}
	if !tt.HasText("正在统计") {
		t.Error("today's energy")
	}
	click(t, tt, "能耗")
	if !tt.HasText("正在积累数据，稍后再来看") {
		t.Error("the energy page's chart")
	}
}

func TestHomePage(t *testing.T) {
	v, m := newView(online())
	v.Page = PageHome
	var on *bool
	m.SetHomeEnabled = func(x bool) { on = &x }
	tt := tester(t, v.Main, 1040, 700)
	if !tt.HasText("打开后可以在“家庭”里看到") {
		t.Error("no introduction while off")
	}

	m.HomeEnabled = true
	m.Home = homekit.Status{Running: true, Code: "123-45-678", SetupURI: "X-HM://0023ISYWYABCD"}
	tt.Frame()
	for _, s := range []string{"123-45-678", "提示“未认证配件”时选“仍然添加”", "防止电脑自动睡眠"} {
		if !tt.HasText(s) {
			t.Errorf("no %q", s)
		}
	}

	if tt.HasText("解除配对") {
		t.Error("nothing to remove before pairing")
	}

	m.Home.Paired = true
	tt.Frame()
	for _, s := range []string{"已加入家庭", "空气净化", "滤网寿命", "防止电脑自动睡眠", "解除配对"} {
		if !tt.HasText(s) {
			t.Errorf("paired: no %q", s)
		}
	}
	if tt.HasText("123-45-678") {
		t.Error("paired: still the setup code")
	}
	_ = on
}

func TestResetAsksFirst(t *testing.T) {
	v, m := newView(online())
	v.Page = PageHome
	m.HomeEnabled = true
	m.Home = homekit.Status{Running: true, Paired: true, Code: "123-45-678", SetupURI: "X-HM://A"}
	reset := false
	m.ResetHome = func() { reset = true }
	tt := tester(t, v.Main, 1040, 700)
	click(t, tt, "解除")
	if reset {
		t.Fatal("reset without asking")
	}
	if !tt.HasText("解除配对？") {
		t.Fatal("no question")
	}
}

func TestFlyout(t *testing.T) {
	v, m := newView(online())
	var mode sharp.Mode
	opened := false
	m.SetMode = func(x sharp.Mode) { mode = x }
	m.OpenMain = func() { opened = true }
	tt := tester(t, v.Flyout, FlyoutWidth, FlyoutHeight)
	for _, s := range []string{"PM2.5", "空气很清新", "24°", "51%", "13 W", "加湿"} {
		if !tt.HasText(s) {
			t.Errorf("no %q", s)
		}
	}
	click(t, tt, "花粉")
	if mode != sharp.ModePollen {
		t.Errorf("mode = %#x", mode)
	}
	click(t, tt, "打开主窗口")
	if !opened {
		t.Error("the main window did not open")
	}
}

func TestFlyoutSearching(t *testing.T) {
	v, _ := newView(device.Snapshot{})
	if !tester(t, v.Flyout, FlyoutWidth, FlyoutHeight).HasText("正在寻找你的净化器") {
		t.Error("no search")
	}
}

func shows(tt *ui.Tester, want string) bool {
	return slices.Contains(tt.Texts(), want)
}

func TestSettingsOffersToApplyOnlyChanges(t *testing.T) {
	v, m := newView(online())
	v.Page = PageSettings
	tt := tester(t, v.Main, 1040, 700)
	if shows(tt, "重新搜索") || shows(tt, "连接") {
		t.Error("a working connection must not offer to search again")
	}
	m.ManualIP = "192.168.1.20"
	tt.Frame()
	if shows(tt, "连接") {
		t.Error("nothing to apply")
	}
}

func TestSettingsShowsTheConsumables(t *testing.T) {
	v, _ := newView(online())
	v.Page = PageSettings
	tt := tester(t, v.Main, 1040, 700)
	for _, s := range []string{"集尘滤网", "97%", "93%", "61%", "24%", "设备提示该清洁了", "1,722,029 m³"} {
		if !tt.HasText(s) {
			t.Errorf("no %q", s)
		}
	}
}

func TestSettingsUnknownConsumablesShowNoNumber(t *testing.T) {
	snap := online()
	snap.State.LimitsOK = false
	v, _ := newView(snap)
	v.Page = PageSettings
	tt := tester(t, v.Main, 1040, 700)
	if tt.HasText("97%") || !tt.HasText("还没有读到耗材状态") {
		t.Error("consumables without their limits must not show a number")
	}
}

func TestPriceSteps(t *testing.T) {
	v, m := newView(online())
	v.Page = PageSettings
	var got float64
	m.SetPrice = func(x float64) { got = x }
	tt := tester(t, v.Main, 1040, 700)
	click(t, tt, "增加")
	if got != 32 {
		t.Errorf("price = %v", got)
	}
	click(t, tt, "减少")
	if got != 30 {
		t.Errorf("price = %v", got)
	}
}

func TestInsideAgainstOutside(t *testing.T) {
	v, m := newView(online())
	tt := tester(t, v.Main, 1040, 700)
	for _, s := range []string{"室内", "24°C", "51%", "室外", "12°C", "82%"} {
		if !tt.HasText(s) {
			t.Errorf("no %q in %q", s, tt.Texts())
		}
	}
	click(t, tt, "室外 12°C 82% 晴间多云")
	if v.Page != PageWeather {
		t.Errorf("page = %v", v.Page)
	}
	v.Page = PageOverview

	old := *m.Weather
	old.At = testNow.Add(-7 * time.Hour)
	m.Weather = &old
	tt.Frame()
	if tt.HasText("室外") {
		t.Error("weather that old is not the weather now")
	}
	if !tt.HasText("室内") {
		t.Error("inside went with the weather")
	}
	m.Weather = sampleWeather()
	m.WeatherOff = true
	tt.Frame()
	if tt.HasText("室外") {
		t.Error("the weather was turned off")
	}
	m.WeatherOff = false
	m.Weather = nil
	tt.Frame()
	if tt.HasText("室外") {
		t.Error("no report yet, and still a line")
	}
	m.Snap.State.Sensors.TempOK, m.Snap.State.Sensors.HumidityOK = false, false
	tt.Frame()
	if tt.HasText("室内") {
		t.Error("nothing known inside, and still a line")
	}
}

func TestWeatherPage(t *testing.T) {
	v, m := newView(online())
	v.Page = PageWeather
	tt := tester(t, v.Main, 1040, 700)
	for _, s := range []string{"12°", "室外", "室内", "82%", "51%", "周二", "70%", "Open-Meteo"} {
		if !tt.HasText(s) {
			t.Errorf("no %q in %q", s, tt.Texts())
		}
	}
	m.WeatherOff = true
	tt.Frame()
	if tt.HasText("周二") || !tt.HasText("显示室外天气") {
		t.Error("off: no forecast, and the switch to turn it on")
	}
}

func TestChoosingAPlace(t *testing.T) {
	v, m := newView(online())
	v.Page = PageWeather
	m.Places = []weather.Place{{Name: "大阪", Detail: "大阪府, 日本", Lat: 34.7, Lon: 135.5}}
	var got *weather.Place
	m.ChoosePlace = func(p *weather.Place) { got = p }
	tt := tester(t, v.Main, 1040, 700)
	click(t, tt, "大阪")
	if got == nil || got.Lat != 34.7 {
		t.Errorf("chose %+v", got)
	}
	m.Places, m.PlaceFixed = nil, true
	tt.Frame()
	click(t, tt, "改回自动")
	if got != nil {
		t.Errorf("going back to automatic chose %+v", got)
	}
}

func TestAirCleanedOnTrends(t *testing.T) {
	v, _ := newView(online())
	v.Page = PageTrends
	if !tester(t, v.Main, 1040, 700).HasText("每日净化空气量") {
		t.Error("no air cleaned")
	}
}

func TestAppearanceAndLinks(t *testing.T) {
	v, m := newView(online())
	v.Page = PageSettings
	var look string
	var opened []string
	checked := false
	m.SetAppearance = func(a string) { look = a }
	m.OpenURL = func(u string) { opened = append(opened, u) }
	m.CheckUpdate = func() { checked = true }
	tt := tester(t, v.Main, 1040, 700)
	click(t, tt, "浅色")
	if look != "light" {
		t.Errorf("appearance = %q", look)
	}
	click(t, tt, "X @zerah_eth")
	click(t, tt, "GitHub")
	if !slices.Equal(opened, []string{authorX, repoLink}) {
		t.Errorf("opened %v", opened)
	}
	click(t, tt, "检查更新")
	if !checked {
		t.Error("no check for updates")
	}
}

func TestLanguages(t *testing.T) {
	v, _ := newView(online())
	for lang, want := range map[string]string{"zh": "运行中", "ja": "運転中", "en": "Running"} {
		i18n.Set(lang, "")
		tt := ui.NewTester(v.Main, 1040, 700)
		if !tt.HasText(want) {
			t.Errorf("%s: no %q", lang, want)
		}
	}
	i18n.Set("zh", "")
}

func TestAirLevels(t *testing.T) {
	for pm, want := range map[int]int{0: 0, 12: 0, 13: 1, 35: 1, 36: 2, 55: 2, 56: 3, 150: 3, 151: 4} {
		if got := airLevel(pm); got != want {
			t.Errorf("PM2.5 %d: level %d, want %d", pm, got, want)
		}
	}
}

func TestKeepsWithinTheMinimumWindow(t *testing.T) {
	v, _ := newView(online())
	for _, lang := range []string{"zh", "ja", "en"} {
		i18n.Set(lang, "")
		tt := ui.NewTester(v.Main, 960, 640)
		tt.Frame()
	}
	i18n.Set("zh", "")
}

// Set MOKO_RENDER to a directory to draw every screen into it:
//
//	MOKO_RENDER=$PWD/docs/assets go test ./internal/view -run TestRender
func TestRender(t *testing.T) {
	dir := os.Getenv("MOKO_RENDER")
	if dir == "" {
		t.Skip("MOKO_RENDER is not set")
	}
	i18n.Set("zh", "")
	save := func(name string, view func(*ui.Context), w, h int) {
		f, err := os.Create(filepath.Join(dir, name+".png"))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		tt := ui.NewTester(view, w, h)
		tt.SetScale(2)
		tt.Advance(2 * time.Second)
		tt.Frame()
		if err := png.Encode(f, tt.Image()); err != nil {
			t.Fatal(err)
		}
	}
	for _, theme := range []struct {
		name string
		t    *ui.Theme
	}{{"dark", ui.DarkTheme()}, {"light", ui.LightTheme()}} {
		show := func(name string, page Page, edit func(*Model)) {
			v, m := newView(online())
			v.Theme, v.Page = theme.t, page
			if edit != nil {
				edit(m)
			}
			save(name+"-"+theme.name, v.Main, 1040, 700)
		}
		show("overview", PageOverview, nil)
		show("weather", PageWeather, nil)
		show("trends", PageTrends, nil)
		show("energy", PageEnergy, nil)
		show("settings", PageSettings, nil)
		show("home", PageHome, func(m *Model) {
			m.HomeEnabled = true
			m.Home = homekit.Status{Running: true, Code: "123-45-678", SetupURI: "X-HM://0023ISYWYABCD"}
		})
		show("home-paired", PageHome, func(m *Model) {
			m.HomeEnabled = true
			m.Home = homekit.Status{Running: true, Paired: true, Code: "123-45-678", SetupURI: "X-HM://0023ISYWYABCD"}
		})
		show("search", PageOverview, func(m *Model) { m.Snap = device.Snapshot{Err: "not found"} })
		show("asleep", PageOverview, func(m *Model) { m.Snap.State.On = false })
		show("night", PageOverview, func(m *Model) { m.Snap.State.Mode = sharp.ModeNight })
		show("dry", PageOverview, func(m *Model) {
			m.Snap.State.Humidifier, m.Snap.State.WaterOK = sharp.HumidifyNoWater, false
		})
		for level, pm := range []int{20, 45, 90, 200} {
			show(fmt.Sprintf("air%d", level+1), PageOverview, func(m *Model) { m.Snap.State.Sensors.PM25 = pm })
		}
		v, _ := newView(online())
		v.Theme = theme.t
		save("flyout-"+theme.name, v.Flyout, FlyoutWidth, FlyoutHeight)
		v, m := newView(online())
		v.Theme = theme.t
		m.Snap.State.Sensors.PM25 = 45
		save("flyout-moderate-"+theme.name, v.Flyout, FlyoutWidth, FlyoutHeight)
	}
}

// Set MOKO_ASSETS to a directory to draw the tray's icons into it:
//
//	MOKO_ASSETS=$PWD/assets go test ./internal/view -run TestAssets
func TestAssets(t *testing.T) {
	dir := os.Getenv("MOKO_ASSETS")
	if dir == "" {
		t.Skip("MOKO_ASSETS is not set")
	}
	const scale, size = 2, 18
	draw := func(name string, alert bool) {
		// Moko's head from the front, with a flat base, on a grid of 1200 by 1200 with its
		// origin between the eyes: the lean of the icon does not read at this size.
		s := float32(size) / 1200
		ox, oy := float32(592), float32(690)
		img := ui.Render(func(c *ui.Context) {
			c.Root().Background(ui.Transparent)
			ui.Box(c).Fill().Draw(func(p *ui.Painter, r ui.Rect) {
				var cloud ui.Path
				for _, o := range [...][3]float32{{-340, 85, 165}, {-235, -165, 185}, {30, -275, 215}, {295, -150, 185}, {430, 110, 195}, {40, 0, 300}} {
					cloud.Circle(r.X+(ox+o[0])*s, r.Y+(oy+o[1])*s, o[2]*s)
				}
				p.FillPath(&cloud, ui.RGB(0, 0, 0))
				p.Fill(ui.Rect{X: r.X + (ox-505)*s, Y: r.Y + (oy+90)*s, W: 1130 * s, H: 215 * s}, ui.RGB(0, 0, 0), 105*s)
			})
		}, size, size, scale)
		// A template icon only has alpha to work with, and no paint removes any, so what
		// is cut out of the cloud is cut out of its pixels: Moko's eyes, and a gap around
		// the dot that asks for attention.
		oval := func(cx, cy, rx, ry float64, set func(i int, cover float64)) {
			for y := range img.Rect.Dy() {
				for x := range img.Rect.Dx() {
					d := math.Hypot((float64(x)+0.5-cx)/rx, (float64(y)+0.5-cy)/ry)
					if cover := math.Max(0, math.Min(1, (1-d)*math.Min(rx, ry)+0.5)); cover > 0 {
						set(img.PixOffset(x, y), cover)
					}
				}
			}
		}
		disc := func(cx, cy, r float64, set func(i int, cover float64)) { oval(cx, cy, r, r, set) }
		erase := func(i int, cover float64) {
			for j := range 4 {
				img.Pix[i+j] = uint8(float64(img.Pix[i+j]) * (1 - cover))
			}
		}
		px := float64(s) * scale
		for _, eye := range [2]float64{-150, 86} {
			oval((float64(ox)+eye)*px, (float64(oy)-8)*px, 62*px, 110*px, erase)
		}
		if alert {
			const x, y, r = size*scale - 6.5, size*scale - 6.5, 6.0
			disc(x, y, r+2.5, erase)
			disc(x, y, r, func(i int, cover float64) {
				img.Pix[i+3] = uint8(math.Max(float64(img.Pix[i+3]), cover*255))
			})
		}
		f, err := os.Create(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if err := png.Encode(f, img); err != nil {
			t.Fatal(err)
		}
	}
	draw("tray.png", false)
	draw("tray-alert.png", true)

	// The art behind the disk image's window. scripts/make-dmg-background.swift adds its
	// words and arrow.
	art := ui.NewTester(func(c *ui.Context) {
		ui.Box(c).Fill().Background(airSkies[0].bg).Draw(func(p *ui.Painter, r ui.Rect) {
			y := r.Y + r.H - 64
			var gust, short ui.Path
			gust.MoveTo(r.X-10, y).LineTo(r.X+150, y).CubeTo(r.X+178, y, r.X+186, y-26, r.X+170, y-38).CubeTo(r.X+156, y-48, r.X+136, y-38, r.X+138, y-22)
			short.MoveTo(r.X-10, y+24).LineTo(r.X+78, y+24)
			p.StrokePath(gust.Cap(ui.RoundCap), 4, mokoWhite.Alpha(0.24))
			p.StrokePath(short.Cap(ui.RoundCap), 4, mokoWhite.Alpha(0.24))
			const size = 196
			p.Clip(r, 0, func() {
				drawMoko(p, ui.Rect{X: r.X + r.W - size + 8, Y: r.Y + r.H - size + 8, W: size, H: size}, calmMood(1), 0, 1, 0)
			})
		})
	}, 660, 400)
	art.SetScale(2)
	art.Frame()
	if err := os.MkdirAll(filepath.Join(dir, "dmg"), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(filepath.Join(dir, "dmg", "background-source.png"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, art.Image()); err != nil {
		t.Fatal(err)
	}
}
