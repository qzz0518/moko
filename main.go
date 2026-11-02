// Moko is a desktop app for Sharp's KI-SX100 air purifier and humidifier: it
// talks to the purifier directly over the local network, and connects it to Apple
// Home.
package main

import (
	"bytes"
	"context"
	_ "embed"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo"
	_ "github.com/egoist/mygo/locales/ja"
	_ "github.com/egoist/mygo/locales/zh-Hans"
	"github.com/egoist/mygo/plugins/updater"
	"github.com/egoist/mygo/plugins/updater/native"
	"github.com/egoist/mygo/ui"

	"github.com/qzz0518/moko/internal/device"
	"github.com/qzz0518/moko/internal/echonet"
	"github.com/qzz0518/moko/internal/firewall"
	"github.com/qzz0518/moko/internal/history"
	"github.com/qzz0518/moko/internal/homekit"
	"github.com/qzz0518/moko/internal/i18n"
	"github.com/qzz0518/moko/internal/sharp"
	"github.com/qzz0518/moko/internal/view"
	"github.com/qzz0518/moko/internal/weather"
)

// afterReady is a hook for the shot build.
var afterReady func(a *app)

//go:embed assets/tray.png
var trayPNG []byte

//go:embed assets/tray-alert.png
var trayAlertPNG []byte

// commandTimeout bounds a command from the UI: turning on and changing the mode, each
// read back.
const commandTimeout = 15 * time.Second

func main() {
	if !mygo.App.RequestSingleInstanceLock() {
		return
	}
	mygo.App.SetLanguages("zh-Hans", "ja", "en")
	a := &app{ctx: context.Background(), cfg: loadConfig()}
	a.ctx, a.cancel = context.WithCancel(a.ctx)
	mygo.Use(native.New(updater.Options{Language: localeOf(a.cfg.Language)}))
	mygo.App.SetActivationPolicy(mygo.ActivationPolicyAccessory)
	mygo.App.OnSecondInstance(func([]string, string) { mygo.App.WhenReady(a.openMain) })
	mygo.App.OnActivate(func(bool) { a.openMain() })
	mygo.App.OnBeforeQuit(func(*mygo.QuitEvent) { a.quitting = true })
	mygo.App.OnWillQuit(func(*mygo.QuitEvent) { a.shutdown() })
	mygo.App.WhenReady(a.ready)
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}

// app is the running app. Its fields belong to the main thread unless marked.
type app struct {
	ctx    context.Context
	cancel context.CancelFunc

	cfg  *config
	m    *view.Model
	v    *view.View
	win  *mygo.Window
	fly  *mygo.Window
	tray *mygo.Tray

	hk           *homekit.Service // safe from any goroutine
	store        atomic.Pointer[history.Store]
	dev          atomic.Pointer[device.Manager]
	client       atomic.Pointer[echonet.Client]
	lastSample   atomic.Int64 // Unix seconds, on the manager's goroutine
	netStarted   bool
	alerting     bool
	noWater      bool
	releaseAwake func()
	netTimer     *time.Timer
	quitting     bool
	weather      *weather.Client
	weatherKick  chan struct{}
	searchNo     int // the latest search for a place: what an older one finds is dropped
}

// localeOf is the language setting as MyGo names it, for its own dialogs and menus,
// and for the characters Chinese and Japanese share; "" follows the system.
func localeOf(setting string) string {
	if setting == "zh" {
		return "zh-Hans"
	}
	return setting
}

// themeOf is the appearance setting as MyGo names it: it decides the windows' own
// frames as well as what the views read of the theme.
func themeOf(setting string) mygo.ThemeSource {
	switch setting {
	case "light":
		return mygo.ThemeLight
	case "dark":
		return mygo.ThemeDark
	}
	return mygo.ThemeSystem
}

func (a *app) ready() {
	a.setupLog()
	mygo.Theme.SetSource(themeOf(a.cfg.Appearance))
	i18n.Set(a.cfg.Language, mygo.App.PreferredLanguages()...)
	mygo.App.SetLocale(localeOf(a.cfg.Language))
	a.setupMenu()

	a.m = &view.Model{
		Version:     mygo.App.Version(),
		Firewall:    view.FirewallChecking,
		OpenAtLogin: mygo.App.OpenAtLogin(),
		KeepAwake:   a.cfg.KeepAwake,
		WaterAlert:  !a.cfg.NoWaterOff,
		Language:    a.cfg.Language,
		Appearance:  a.cfg.Appearance,
		ManualIP:    a.cfg.ManualIP,
		Price:       a.cfg.Price,
		HomeEnabled: a.cfg.HomeKit,
		WeatherOff:  a.cfg.WeatherOff,
		PlaceFixed:  a.cfg.PlaceFixed,
	}
	if a.cfg.Place != nil {
		a.m.Place = a.cfg.Place.Name
	}
	a.weather, a.weatherKick = weather.New(), make(chan struct{}, 1)
	a.bindActions()
	a.v = view.New(a.m)

	dir, err := mygo.App.Path(mygo.PathUserData)
	if err != nil {
		log.Fatal(err)
	}
	a.hk = homekit.New(filepath.Join(dir, "homekit"), a, func() { mygo.RunOnMain(a.refreshHome) })
	go a.openHistory(filepath.Join(dir, "history"))

	a.setupTray()
	if !mygo.App.WasOpenedAtLogin() {
		a.openMain()
	}
	mygo.Power.OnResume(a.networkChanged)
	mygo.Network.OnChanged(func(s mygo.NetworkStatus) {
		if s.Online {
			a.networkChanged()
		}
	})
	go a.watchHome()
	go a.checkFirewall()
	go a.watchWeather()
	if afterReady != nil {
		afterReady(a)
	}
}

// setupLog sends the log to a file in the system's place for logs as well.
func (a *app) setupLog() {
	dir, err := mygo.App.Path(mygo.PathLogs)
	if err != nil {
		return
	}
	path := filepath.Join(dir, "moko.log")
	if st, err := os.Stat(path); err == nil && st.Size() > 1<<20 {
		os.Remove(path)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	log.SetOutput(io.MultiWriter(os.Stderr, f))
}

func (a *app) openHistory(dir string) {
	store, err := history.Open(dir, time.Now())
	if err != nil {
		log.Printf("history: %v", err)
		return
	}
	a.store.Store(store)
	samples := store.Since(time.Now().Add(-history.Keep))
	mygo.RunOnMain(func() {
		a.m.History = append(samples, a.m.History...)
		a.invalidate()
	})
}

// checkFirewall starts the network parts once Windows Firewall lets them through.
func (a *app) checkFirewall() {
	status := firewall.Status()
	mygo.RunOnMain(func() {
		a.m.Firewall = status
		if status == firewall.Allowed {
			a.startNetwork()
		}
		a.invalidate()
	})
}

// startNetwork opens the socket and starts the device and Apple Home.
func (a *app) startNetwork() {
	if a.netStarted {
		return
	}
	a.netStarted = true
	if a.cfg.HomeKit {
		a.startHome()
	}
	cfg := *a.cfg
	go func() {
		for a.ctx.Err() == nil {
			client, err := echonet.Listen()
			if err == nil {
				a.client.Store(client)
				a.runDevice(client, cfg)
				return
			}
			log.Print(err)
			msg := err.Error()
			mygo.RunOnMain(func() {
				a.m.ListenErr = msg
				a.invalidate()
			})
			select {
			case <-time.After(5 * time.Second):
			case <-a.ctx.Done():
			}
		}
	}()
}

func (a *app) runDevice(client *echonet.Client, cfg config) {
	mygo.RunOnMain(func() { a.m.ListenErr = "" })
	dev := device.New(client, device.Config{
		ManualIP: cfg.ManualIP,
		LastIP:   cfg.LastIP,
		NodeID:   cfg.NodeID,
		OnChange: a.onSnapshot,
	})
	a.dev.Store(dev)
	mygo.RunOnMain(a.updateVisible)
	dev.Run(a.ctx)
}

// onSnapshot runs on the manager's goroutine.
func (a *app) onSnapshot(s device.Snapshot) {
	a.hk.Update(s)
	a.sample(s)
	mygo.RunOnMain(func() {
		a.m.Snap = s
		a.remember(s.Info)
		a.updateTray()
		a.alertWater(s)
		a.invalidate()
	})
}

// sample records a reading each minute, as long as the device answers.
func (a *app) sample(s device.Snapshot) {
	now := time.Now()
	if s.Phase != device.PhaseOnline || !s.HasState || now.Unix()-a.lastSample.Load() < 60 {
		return
	}
	a.lastSample.Store(now.Unix())
	smp := sampleOf(now, s.State)
	if store := a.store.Load(); store != nil {
		if err := store.Add(smp); err != nil {
			log.Printf("history: %v", err)
		}
	}
	mygo.RunOnMain(func() {
		a.m.History = append(a.m.History, smp)
		if cut := now.Add(-history.Keep); a.m.History[0].At.Before(cut) {
			a.m.History = a.m.History[1:]
		}
	})
}

// sampleOf is a state as history keeps it, with -1 for what is unknown.
func sampleOf(at time.Time, st device.State) history.Sample {
	smp := history.Sample{At: at, PM25: -1, TempC: -1, Humidity: -1, Watts: -1, EnergyWh: -1, AirM3: -1}
	if st.SensorsOK {
		if st.Sensors.PM25OK {
			smp.PM25 = st.Sensors.PM25
		}
		if st.Sensors.TempOK {
			smp.TempC = st.Sensors.TempC
		}
		if st.Sensors.HumidityOK {
			smp.Humidity = st.Sensors.Humidity
		}
	}
	if st.WattsOK {
		smp.Watts = st.Watts
	}
	if st.EnergyOK {
		smp.EnergyWh = int64(st.EnergyWh)
	}
	if st.SensorsOK {
		smp.AirM3 = int64(st.Sensors.AirCleaned)
	}
	return smp
}

// remember keeps where the device was found, for the next start.
func (a *app) remember(info device.Info) {
	if info.IP == "" || (info.IP == a.cfg.LastIP && info.NodeID == a.cfg.NodeID) {
		return
	}
	a.cfg.LastIP, a.cfg.NodeID = info.IP, info.NodeID
	a.cfg.save()
}

func (a *app) alertWater(s device.Snapshot) {
	st := s.State
	empty := st.StatusOK && st.On && st.Humidify && (st.Humidifier == sharp.HumidifyNoWater || !st.WaterOK)
	if empty && !a.noWater && a.m.WaterAlert && mygo.NotificationsSupported() {
		n := mygo.NewNotification(mygo.NotificationOptions{
			ID:    "water",
			Title: i18n.T("水箱没水了", "水がなくなりました", "Out of water"),
			Body:  i18n.T("请给 KI-SX100 的水箱加水。", "KI-SX100 に給水してください。", "Refill the tank of the KI-SX100."),
		})
		go func() {
			if err := n.Show(); err != nil {
				log.Printf("notification: %v", err)
			}
		}()
	}
	a.noWater = empty
}

func (a *app) invalidate() {
	if a.win != nil {
		a.win.Invalidate()
	}
	if a.fly != nil {
		a.fly.Invalidate()
	}
}

// The app is the Device of Apple Home, whether or not the network has started.

// SetPower implements homekit.Device.
func (a *app) SetPower(ctx context.Context, on bool) error {
	if d := a.dev.Load(); d != nil {
		return d.SetPower(ctx, on)
	}
	return device.ErrOffline
}

// SetMode implements homekit.Device.
func (a *app) SetMode(ctx context.Context, m sharp.Mode) error {
	if d := a.dev.Load(); d != nil {
		return d.SetMode(ctx, m)
	}
	return device.ErrOffline
}

// SetHumidify implements homekit.Device.
func (a *app) SetHumidify(ctx context.Context, on bool) error {
	if d := a.dev.Load(); d != nil {
		return d.SetHumidify(ctx, on)
	}
	return device.ErrOffline
}

// command runs a change in the background; done clears what the view shows meanwhile.
func (a *app) command(run func(ctx context.Context) error, done func()) {
	a.invalidate()
	go func() {
		ctx, cancel := context.WithTimeout(a.ctx, commandTimeout)
		defer cancel()
		err := run(ctx)
		mygo.RunOnMain(func() {
			done()
			if err != nil {
				log.Printf("command: %v", err)
				a.m.Notice = commandError(err)
			}
			a.invalidate()
		})
	}()
}

func commandError(err error) string {
	switch {
	case errors.Is(err, device.ErrOffline):
		return i18n.T("净化器不在线", "空気清浄機に接続できません", "The purifier is offline")
	case errors.Is(err, device.ErrUnconfirmed):
		return i18n.T("净化器没有确认这次操作", "空気清浄機が変更を確認しませんでした", "The purifier did not confirm the change")
	}
	return i18n.T("操作失败，请重试", "操作に失敗しました", "That did not work. Try again.")
}

func (a *app) bindActions() {
	m := a.m
	m.SetPower = func(on bool) {
		m.PendingPower = &on
		a.command(func(ctx context.Context) error { return a.SetPower(ctx, on) }, func() { m.PendingPower = nil })
	}
	// Choosing a mode or humidifying turns an off device on, so they show it on too.
	turningOn := func() *bool {
		if m.Snap.State.On {
			return nil
		}
		on := true
		return &on
	}
	m.SetMode = func(mode sharp.Mode) {
		m.PendingMode, m.PendingPower = mode, turningOn()
		a.command(func(ctx context.Context) error { return a.SetMode(ctx, mode) }, func() { m.PendingMode, m.PendingPower = 0, nil })
	}
	m.SetHumidify = func(on bool) {
		m.PendingHumidify = &on
		if on {
			m.PendingPower = turningOn()
		}
		a.command(func(ctx context.Context) error { return a.SetHumidify(ctx, on) }, func() { m.PendingHumidify, m.PendingPower = nil, nil })
	}
	m.SetHomeEnabled = a.setHomeEnabled
	m.ResetHome = a.resetHome
	m.SetKeepAwake = func(on bool) {
		a.cfg.KeepAwake, m.KeepAwake = on, on
		a.cfg.save()
		a.applyKeepAwake()
		a.invalidate()
	}
	m.SetOpenAtLogin = func(on bool) {
		if err := mygo.App.SetOpenAtLogin(on); err != nil {
			log.Printf("open at login: %v", err)
			m.Notice = i18n.T("设置失败：打包后的应用才能开机启动", "設定できませんでした", "Could not change that")
		}
		m.OpenAtLogin = mygo.App.OpenAtLogin()
		a.invalidate()
	}
	m.SetWaterAlert = func(on bool) {
		a.cfg.NoWaterOff, m.WaterAlert = !on, on
		a.cfg.save()
		a.invalidate()
	}
	m.SetLanguage = func(lang string) {
		a.cfg.Language, m.Language = lang, lang
		a.cfg.save()
		i18n.Set(lang, mygo.App.PreferredLanguages()...)
		mygo.App.SetLocale(localeOf(lang))
		a.setupMenu()
		a.invalidate()
	}
	m.SetAppearance = func(appearance string) {
		a.cfg.Appearance, m.Appearance = appearance, appearance
		a.cfg.save()
		mygo.Theme.SetSource(themeOf(appearance))
		a.invalidate()
	}
	m.CheckUpdate = updater.CheckForUpdates
	m.SetWeatherOn = func(on bool) {
		a.cfg.WeatherOff, m.WeatherOff = !on, !on
		a.cfg.save()
		a.kickWeather()
		a.invalidate()
	}
	m.SearchPlace = a.searchPlace
	m.ChoosePlace = func(p *weather.Place) {
		a.searchNo++
		m.Places, m.PlaceSearching, m.PlaceNote = nil, false, ""
		a.cfg.PlaceFixed, m.PlaceFixed = p != nil, p != nil
		if p != nil {
			a.cfg.Place, m.Place = p, p.Name
		}
		a.cfg.save()
		m.Weather = nil
		a.kickWeather()
		a.invalidate()
	}
	m.SetManualIP = func(ip string) {
		a.cfg.ManualIP, m.ManualIP = ip, ip
		a.cfg.save()
		if d := a.dev.Load(); d != nil {
			d.SetManualIP(ip)
		}
		a.invalidate()
	}
	m.SetPrice = func(price float64) {
		a.cfg.Price, m.Price = price, price
		a.cfg.save()
		a.invalidate()
	}
	m.AllowFirewall = a.allowFirewall
	m.SkipFirewall = func() {
		m.Firewall = view.FirewallAllowed
		a.startNetwork()
		a.invalidate()
	}
	m.OpenNetwork = func() {
		_ = mygo.Shell.OpenExternal("x-apple.systempreferences:com.apple.preference.security?Privacy_LocalNetwork")
	}
	m.OpenMain = func() {
		if a.fly != nil {
			a.fly.Close()
		}
		a.openMain()
	}
	m.OpenURL = func(url string) { _ = mygo.Shell.OpenExternal(url) }
	m.Quit = mygo.App.Quit
}

// weatherEvery is how often the weather is read. The forecast changes more slowly.
const weatherEvery = 20 * time.Minute

func (a *app) kickWeather() {
	select {
	case a.weatherKick <- struct{}{}:
	default:
	}
}

// watchWeather keeps the report up to date, and keeps the last one when it cannot.
func (a *app) watchWeather() {
	for {
		a.readWeather()
		select {
		case <-a.ctx.Done():
			return
		case <-a.weatherKick:
		case <-time.After(weatherEvery):
		}
	}
}

func (a *app) readWeather() {
	type wanted struct {
		off, fixed bool
		place      *weather.Place
	}
	got := make(chan wanted, 1)
	mygo.RunOnMain(func() { got <- wanted{a.cfg.WeatherOff, a.cfg.PlaceFixed, a.cfg.Place} })
	var w wanted
	select {
	case w = <-got:
	case <-a.ctx.Done():
		return
	}
	if w.off {
		return
	}
	ctx, cancel := context.WithTimeout(a.ctx, 40*time.Second)
	defer cancel()
	place := w.place
	if !w.fixed {
		// Each time, as a laptop moves; the last place stands in when this fails.
		if p, err := a.weather.Locate(ctx, i18n.Lang()); err == nil {
			place = &p
		} else {
			log.Printf("weather: locate: %v", err)
		}
	}
	if place == nil {
		return
	}
	r, err := a.weather.Report(ctx, *place)
	if err != nil {
		log.Printf("weather: %v", err)
		return
	}
	mygo.RunOnMain(func() {
		// The user may have chosen another place meanwhile.
		if a.cfg.WeatherOff || (a.cfg.PlaceFixed != w.fixed) || (w.fixed && a.cfg.Place != w.place) {
			return
		}
		a.cfg.Place, a.m.Place = place, place.Name
		a.cfg.save()
		a.m.Weather = &r
		a.invalidate()
	})
}

func (a *app) searchPlace(query string) {
	m := a.m
	a.searchNo++
	no := a.searchNo
	m.Places, m.PlaceNote, m.PlaceSearching = nil, "", true
	a.invalidate()
	go func() {
		ctx, cancel := context.WithTimeout(a.ctx, 20*time.Second)
		defer cancel()
		places, err := a.weather.Search(ctx, query, i18n.Lang())
		mygo.RunOnMain(func() {
			if no != a.searchNo {
				return
			}
			m.Places, m.PlaceSearching = places, false
			switch {
			case err != nil:
				log.Printf("weather: search: %v", err)
				m.PlaceNote = i18n.T("搜索失败，请检查网络", "検索できませんでした", "The search failed. Check your connection.")
			case len(places) == 0:
				m.PlaceNote = i18n.T("没有找到，换个名字试试（比如英文名）", "見つかりません。別の名前（英語名など）を試してください", "Nothing found. Try another name, such as in English.")
			}
			a.invalidate()
		})
	}()
}

func (a *app) allowFirewall() {
	go func() {
		err := firewall.Allow()
		status := firewall.Status()
		mygo.RunOnMain(func() {
			a.m.Firewall = status
			switch {
			case errors.Is(err, firewall.ErrDeclined):
			case err != nil:
				log.Printf("firewall: %v", err)
				a.m.Notice = i18n.T("放行失败，请重试", "許可できませんでした", "Could not allow it. Try again.")
			}
			if status == firewall.Allowed {
				a.startNetwork()
			}
			a.invalidate()
		})
	}()
}

// Apple Home.

func (a *app) setHomeEnabled(on bool) {
	a.cfg.HomeKit, a.m.HomeEnabled = on, on
	a.cfg.save()
	if on {
		a.startHome()
	} else {
		a.hk.Stop()
	}
	a.applyKeepAwake()
	a.refreshHome()
}

func (a *app) startHome() {
	if err := a.hk.Start(); err != nil {
		log.Print(err)
		a.m.Notice = i18n.T("Apple 家庭启动失败", "Apple ホームを開始できません", "Apple Home could not start")
	}
	a.applyKeepAwake()
	a.refreshHome()
}

func (a *app) resetHome() {
	go func() {
		err := a.hk.Reset()
		mygo.RunOnMain(func() {
			if err != nil {
				log.Print(err)
				a.m.Notice = i18n.T("解除配对失败", "解除できませんでした", "Could not remove it")
			} else if a.cfg.HomeKit {
				a.startHome()
			}
			a.refreshHome()
		})
	}()
}

func (a *app) refreshHome() {
	st := a.hk.Status()
	if st != a.m.Home {
		a.m.Home = st
		a.invalidate()
	}
}

// watchHome notices the accessory being paired or unpaired, which hap does not report.
func (a *app) watchHome() {
	tick := time.NewTicker(3 * time.Second)
	defer tick.Stop()
	var last homekit.Status
	for {
		select {
		case <-tick.C:
			if st := a.hk.Status(); st != last {
				last = st
				mygo.RunOnMain(a.refreshHome)
			}
		case <-a.ctx.Done():
			return
		}
	}
}

func (a *app) applyKeepAwake() {
	want := a.cfg.KeepAwake && a.cfg.HomeKit
	switch {
	case want && a.releaseAwake == nil:
		a.releaseAwake = mygo.Power.KeepAwake("Moko stays available in Apple Home", false)
	case !want && a.releaseAwake != nil:
		a.releaseAwake()
		a.releaseAwake = nil
	}
}

// networkChanged runs after the computer wakes or the network changes. The socket's
// multicast membership and Bonjour's announcements do not follow by themselves.
func (a *app) networkChanged() {
	if a.netTimer != nil {
		a.netTimer.Stop()
	}
	a.netTimer = time.AfterFunc(3*time.Second, func() {
		if c := a.client.Load(); c != nil {
			c.Rejoin()
		}
		if d := a.dev.Load(); d != nil {
			d.Refresh()
		}
		if err := a.hk.Restart(); err != nil {
			log.Print(err)
		}
		a.kickWeather()
	})
}

// Windows.

func (a *app) openMain() {
	if a.m == nil {
		return // not ready yet: ready opens it
	}
	if a.win == nil {
		a.win = mygo.NewWindow(mygo.WindowOptions{
			Title:         "Moko",
			Width:         1040,
			Height:        700,
			MinWidth:      960,
			MinHeight:     640,
			StateKey:      "main",
			TitleBarStyle: mygo.TitleBarHidden,
			Content:       ui.View(a.v.Main),
		})
		// Closing hides it: the app goes on running in the tray, for Apple Home.
		a.win.OnClose(func(e *mygo.CloseEvent) {
			if !a.quitting {
				e.PreventDefault()
				a.win.Hide()
			}
		})
		a.win.OnHide(func() {
			mygo.App.SetActivationPolicy(mygo.ActivationPolicyAccessory)
			a.updateVisible()
		})
		a.win.OnShow(a.updateVisible)
	}
	mygo.App.SetActivationPolicy(mygo.ActivationPolicyRegular)
	a.win.Show()
	a.win.Focus()
	mygo.App.Focus()
	a.updateVisible()
}

func (a *app) toggleFlyout() {
	if a.fly != nil {
		a.fly.Close()
		return
	}
	a.fly = mygo.NewFlyout(mygo.FlyoutOptions{
		Tray:      a.tray,
		Placement: mygo.PlacementBottom,
		Gap:       4,
		Width:     view.FlyoutWidth,
		Height:    view.FlyoutHeight,
		Focusable: true,
		Shadow:    true,
		Popover:   true,
		Content:   ui.View(a.v.Flyout),
	})
	a.fly.OnClosed(func() {
		a.fly = nil
		a.updateVisible()
	})
	a.updateVisible()
}

// updateVisible polls faster while the readings are on screen.
func (a *app) updateVisible() {
	if d := a.dev.Load(); d != nil {
		d.SetVisible(a.fly != nil || (a.win != nil && a.win.IsVisible()))
	}
}

func (a *app) setupTray() {
	tray, err := mygo.NewTray(mygo.TrayOptions{Icon: a.trayIcon(), IconIsTemplate: true, ToolTip: "Moko"})
	if err != nil {
		log.Fatal("tray: ", err)
	}
	a.tray = tray
	tray.OnClick(a.toggleFlyout)
	tray.OnRightClick(func() {
		if a.fly != nil {
			a.fly.Close()
		}
		tray.SetMenu(a.trayMenu())
		defer tray.SetMenu(nil)
		tray.PopUpMenu()
	})
	tray.OnAppearanceChange(func() { _ = tray.SetIcon(a.trayIcon(), true) })
}

func (a *app) trayMenu() *mygo.Menu {
	power := i18n.T("开机", "運転", "Turn On")
	if a.m.Snap.State.On {
		power = i18n.T("关机", "停止", "Turn Off")
	}
	return mygo.NewMenu([]*mygo.MenuItem{
		{Label: i18n.T("打开主窗口", "メインウインドウを開く", "Open Main Window"), Click: func(*mygo.MenuItem, *mygo.Window) { a.openMain() }},
		{Label: power, Disabled: a.m.Snap.Phase != device.PhaseOnline, Click: func(*mygo.MenuItem, *mygo.Window) { a.m.SetPower(!a.m.Snap.State.On) }},
		mygo.Separator(),
		{Label: i18n.T("退出", "終了", "Quit"), Click: func(*mygo.MenuItem, *mygo.Window) { mygo.App.Quit() }},
	})
}

// updateTray shows trouble on the icon, and the readings in its tooltip.
func (a *app) updateTray() {
	if a.tray == nil {
		return
	}
	s := a.m.Snap
	st := s.State
	alert := s.Phase == device.PhaseOnline && (st.Fault || (st.StatusOK && st.On && st.Humidify && (st.Humidifier == sharp.HumidifyNoWater || !st.WaterOK)))
	if alert != a.alerting {
		a.alerting = alert
		_ = a.tray.SetIcon(a.trayIcon(), true)
	}
	tip := "Moko"
	if s.HasState && st.SensorsOK {
		tip = fmt.Sprintf("Moko · PM2.5 %d · %d°C · %d%%", st.Sensors.PM25, st.Sensors.TempC, st.Sensors.Humidity)
	}
	a.tray.SetToolTip(tip)
}

// trayIcon is black on transparent, which macOS tints. Other systems show it as it is,
// so it turns white on a dark bar.
func (a *app) trayIcon() []byte {
	src := trayPNG
	if a.alerting {
		src = trayAlertPNG
	}
	if runtime.GOOS == "darwin" || a.tray == nil || !a.tray.Dark() {
		return src
	}
	img, err := png.Decode(bytes.NewReader(src))
	if err != nil {
		return src
	}
	r := img.Bounds()
	out := image.NewNRGBA(r)
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			_, _, _, al := img.At(x, y).RGBA()
			out.SetNRGBA(x, y, color.NRGBA{R: 255, G: 255, B: 255, A: uint8(al >> 8)})
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, out); err != nil {
		return src
	}
	return b.Bytes()
}

func (a *app) setupMenu() {
	if runtime.GOOS != "darwin" {
		return // Windows' menu bars belong to windows, and this app's have none
	}
	mygo.App.SetMenu(mygo.NewMenu([]*mygo.MenuItem{
		{Role: mygo.RoleAppMenu, Submenu: []*mygo.MenuItem{
			{Role: mygo.RoleAbout, Label: i18n.T("关于 Moko", "Moko について", "About Moko")},
			updater.MenuItem(),
			mygo.Separator(),
			{Role: mygo.RoleHide, Label: i18n.T("隐藏 Moko", "Moko を隠す", "Hide Moko")},
			{Role: mygo.RoleHideOthers, Label: i18n.T("隐藏其他", "ほかを隠す", "Hide Others")},
			{Role: mygo.RoleUnhide, Label: i18n.T("全部显示", "すべてを表示", "Show All")},
			mygo.Separator(),
			{Role: mygo.RoleQuit, Label: i18n.T("退出 Moko", "Moko を終了", "Quit Moko")},
		}},
		{Role: mygo.RoleEditMenu, Label: i18n.T("编辑", "編集", "Edit"), Submenu: []*mygo.MenuItem{
			{Role: mygo.RoleUndo, Label: i18n.T("撤销", "取り消す", "Undo")},
			{Role: mygo.RoleRedo, Label: i18n.T("重做", "やり直す", "Redo")},
			mygo.Separator(),
			{Role: mygo.RoleCut, Label: i18n.T("剪切", "カット", "Cut")},
			{Role: mygo.RoleCopy, Label: i18n.T("拷贝", "コピー", "Copy")},
			{Role: mygo.RolePaste, Label: i18n.T("粘贴", "ペースト", "Paste")},
			{Role: mygo.RoleSelectAll, Label: i18n.T("全选", "すべてを選択", "Select All")},
		}},
		{Role: mygo.RoleWindowMenu, Label: i18n.T("窗口", "ウインドウ", "Window"), Submenu: []*mygo.MenuItem{
			{Role: mygo.RoleMinimize, Label: i18n.T("最小化", "しまう", "Minimize")},
			{Role: mygo.RoleClose, Label: i18n.T("关闭", "閉じる", "Close")},
		}},
	}))
}

func (a *app) shutdown() {
	a.hk.Stop()
	a.cancel()
	if c := a.client.Load(); c != nil {
		c.Close()
	}
	if a.releaseAwake != nil {
		a.releaseAwake()
	}
	if a.tray != nil {
		a.tray.Destroy()
	}
}
