package view

import (
	"fmt"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/qzz0518/moko/internal/history"
	"github.com/qzz0518/moko/internal/i18n"
)

// pageTitle heads a page.
func pageTitle(c *ui.Context, k pal, title, sub string) {
	ui.Column(c).Gap(3).Children(func() {
		ui.Text(c, title).FontSize(20).FontWeight(700)
		if sub != "" {
			ui.Text(c, sub).FontSize(12.5).TextColor(k.muted)
		}
	})
}

type trendRange struct {
	span   time.Duration
	points int
	format string
}

var trendRanges = []trendRange{
	{24 * time.Hour, 96, "15:04"},
	{7 * 24 * time.Hour, 168, "1/2 15:04"},
	{30 * 24 * time.Hour, 180, "1/2"},
}

func (v *View) trends(c *ui.Context, k pal) {
	ui.Row(c).AlignItems(ui.End).Children(func() {
		ui.Column(c).Grow(1).Children(func() {
			pageTitle(c, k, i18n.T("趋势", "推移", "Trends"), i18n.T("应用运行时每分钟记录一次", "アプリの実行中、1分ごとに記録します", "Recorded each minute while the app runs"))
		})
		ui.Box(c).Width(240).Children(func() {
			labels := []string{i18n.T("24 小时", "24時間", "24 hours"), i18n.T("7 天", "7日", "7 days"), i18n.T("30 天", "30日", "30 days")}
			opts := make([]choice, len(labels))
			for i, l := range labels {
				opts[i] = choice{label: l, on: i == v.rangeIdx}
			}
			chooser(c, k, "range", i18n.T("范围", "範囲", "Range"), false, opts, false, func(i int) { v.rangeIdx = i })
		})
	})
	r := trendRanges[v.rangeIdx]
	ui.Scroll(c.Key("charts")).Grow(1).Gap(12).Children(func() {
		v.trendCard(c.Key(0), k, metricPM25, r)
		v.trendCard(c.Key(1), k, metricHumidity, r)
		v.trendCard(c.Key(2), k, metricTemp, r)
		v.airCleanedCard(c.Key(3), k)
	})
}

// airCleanedCard is how much air the device cleaned each day, whatever the range.
func (v *View) airCleanedCard(c *ui.Context, k pal) {
	now := v.Now()
	y, mo, d := now.Date()
	today := time.Date(y, mo, d, 0, 0, 0, 0, now.Location())
	days := history.Daily(v.M.History, today.AddDate(0, 0, -energyDays+1), history.Air)
	cubic := func(m3 int64) (string, string) { return thousands(m3), "m³" }
	card(c, k).Height(204).Padding(16, 20, 12, 20).Gap(8).Children(func() {
		ui.Row(c).AlignItems(ui.End).Gap(5).Children(func() {
			ui.Text(c, i18n.T("每日净化空气量", "1日にきれいにした空気量", "Air cleaned each day")).FontSize(14).FontWeight(700).Grow(1)
			if n := len(days); n > 0 && days[n-1].Date.Equal(today) {
				ui.Text(c, i18n.T("今日", "今日", "Today")).FontSize(11).TextColor(k.faint)
				ui.Text(c, thousands(days[n-1].Amount)+" m³").FontSize(13.5).FontWeight(800).FontFeatures("tnum")
			}
		})
		dayBars(c.Key("bars"), k, days, today, cubic)
	})
}

func (v *View) trendCard(c *ui.Context, k pal, m metric, r trendRange) {
	cv := v.curveOf(m, r.span, r.points)
	card(c, k).Height(204).Padding(16, 20, 12, 20).Gap(8).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(18).Children(func() {
			ui.Row(c).Grow(1).Gap(5).AlignItems(ui.End).Children(func() {
				ui.Text(c, m.name()).FontSize(14).FontWeight(700)
				ui.Text(c, m.unit).FontSize(11).TextColor(k.faint)
			})
			if cv.count > 0 {
				for i, s := range []struct {
					label string
					value float64
				}{{i18n.T("最低", "最低", "Low"), cv.min}, {i18n.T("平均", "平均", "Avg"), cv.avg}, {i18n.T("最高", "最高", "High"), cv.max}} {
					ui.Row(c.Key(i)).Gap(5).AlignItems(ui.End).Children(func() {
						ui.Text(c, s.label).FontSize(11).TextColor(k.faint)
						ui.Text(c, fmt.Sprintf("%.0f", s.value)).FontSize(13.5).FontWeight(800).FontFeatures("tnum")
					})
				}
			}
		})
		bigChart(c, k, "chart", m, cv, r.format, r.span)
		format := "15:04"
		if r.span > 24*time.Hour {
			format = "1/2"
		}
		axisTimes(c, k, r.span, v.Now(), format)
	})
}
