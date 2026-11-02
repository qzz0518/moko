package view

import (
	"fmt"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/qzz0518/moko/internal/history"
	"github.com/qzz0518/moko/internal/i18n"
)

const energyDays = 30

func (v *View) energy(c *ui.Context, k pal) {
	m := v.M
	now := v.Now()
	y, mo, d := now.Date()
	today := time.Date(y, mo, d, 0, 0, 0, 0, now.Location())
	days := history.Daily(m.History, today.AddDate(0, 0, -energyDays+1), history.Energy)

	var todayWh, monthWh int64
	monthStart := time.Date(y, mo, 1, 0, 0, 0, 0, now.Location())
	for _, x := range days {
		if x.Date.Equal(today) {
			todayWh = x.Amount
		}
		if !x.Date.Before(monthStart) {
			monthWh += x.Amount
		}
	}
	pageTitle(c, k, i18n.T("能耗", "電力", "Energy"), fmt.Sprintf(i18n.T("电费按 %.0f 日元/kWh 估算，可在设置里修改", "電気代は %.0f 円/kWh で試算。設定で変更できます", "Costs use ¥%.0f per kWh, which Settings can change"), m.Price))
	ui.Row(c.Key("tiles")).Gap(12).Children(func() {
		val, unit := energyText(todayWh)
		tile(c.Key(0), k, i18n.T("今日", "今日", "Today"), val, unit, yenText(todayWh, m.Price))
		val, unit = energyText(monthWh)
		tile(c.Key(1), k, i18n.T("本月", "今月", "This month"), val, unit, yenText(monthWh, m.Price))
		total, totalUnit, note := "—", "", ""
		if m.Snap.State.EnergyOK {
			wh := int64(m.Snap.State.EnergyWh)
			total, totalUnit = energyText(wh)
			note = yenText(wh, m.Price)
		}
		tile(c.Key(2), k, i18n.T("设备累计", "本体の累計", "Device total"), total, totalUnit, note)
	})
	card(c.Key("bars"), k).Grow(1).MinHeight(200).Padding(16, 20, 12, 20).Gap(10).Children(func() {
		ui.Text(c, i18n.T("最近 30 天", "過去30日", "Last 30 days")).FontSize(13).FontWeight(600).TextColor(k.muted)
		dayBars(c, k, days, today, energyText)
		ui.Text(c, i18n.T("应用关闭期间的用电，会算在下次打开的那一天", "アプリを閉じていた間の電力量は、次に開いた日に計上されます", "What the device uses while the app is closed counts on the day the app next runs")).FontSize(11).TextColor(k.faint)
	})
}

// dayBars is a bar for each of the last 30 days, the hovered one saying how much, in the
// unit format chooses.
func dayBars(c *ui.Context, k pal, days []history.Day, today time.Time, format func(int64) (value, unit string)) {
	values := make([]int64, energyDays)
	var peak int64
	for _, x := range days {
		i := energyDays - 1 - int(today.Sub(x.Date).Hours()/24+0.5)
		if i >= 0 && i < energyDays {
			values[i] = x.Amount
			peak = max(peak, x.Amount)
		}
	}
	if peak == 0 {
		ui.Column(c.Key("empty")).Grow(1).Center().Children(func() {
			ui.Text(c, i18n.T("正在积累数据，稍后再来看", "データを集めています", "Collecting readings…")).FontSize(12.5).TextColor(k.faint)
		})
		return
	}
	box := ui.Box(c.Key("chart")).Grow(1).MinHeight(90)
	x, _, hovering := box.PointerPosition()
	box.Draw(func(p *ui.Painter, r ui.Rect) {
		const gap = 5
		w := (r.W - gap*(energyDays-1)) / energyDays
		chartH := r.H - 18
		hovered := -1
		if hovering {
			hovered = max(0, min(int(x/(w+gap)), energyDays-1)) // relative to the box
		}
		for i, amount := range values {
			bx := r.X + float32(i)*(w+gap)
			col := k.accent.Alpha(0.32)
			if i == hovered || (hovered < 0 && i == energyDays-1) {
				col = k.accent
			}
			if amount == 0 {
				p.Fill(ui.Rect{X: bx, Y: r.Y + chartH - 2, W: w, H: 2}, k.line, 1)
				continue
			}
			h := max(float32(amount)/float32(peak)*(chartH-18), 3)
			p.Fill(ui.Rect{X: bx, Y: r.Y + chartH - h, W: w, H: h}, col, w/2)
		}
		for _, i := range []int{0, 7, 14, 21, 29} {
			date := today.AddDate(0, 0, i-energyDays+1)
			label := ui.Span{Text: date.Format("1/2"), Size: 10.5, Color: k.faint, Features: "tnum"}
			lw, _ := p.MeasureText(0, label)
			lx := min(max(r.X+float32(i)*(w+gap)+w/2-lw/2, r.X), r.X+r.W-lw)
			p.RichText(lx, r.Y+chartH+4, 0, label)
		}
		if hovered >= 0 {
			date := today.AddDate(0, 0, hovered-energyDays+1)
			val, unit := format(values[hovered])
			label := ui.Span{Text: fmt.Sprintf("%s  %s %s", date.Format("1/2"), val, unit), Size: 11.5, Weight: 700, Color: k.bg, Features: "tnum"}
			lw, lh := p.MeasureText(0, label)
			lx := min(max(r.X+float32(hovered)*(w+gap)+w/2-lw/2, r.X+7), r.X+r.W-lw-7)
			p.Fill(ui.Rect{X: lx - 8, Y: r.Y - 4, W: lw + 16, H: lh + 8}, k.text, 8)
			p.RichText(lx, r.Y, 0, label)
		}
	})
}
