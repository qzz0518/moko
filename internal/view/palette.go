package view

import (
	_ "embed"
	"log"

	"github.com/egoist/mygo/ui"

	"github.com/qzz0518/moko/internal/i18n"
)

// Nunito draws Latin letters and digits, for its round shapes; the system's fonts draw
// the rest.
//
//go:embed fonts/Nunito.ttf
var nunito []byte

func init() {
	if err := ui.RegisterFont(nunito, "Nunito"); err != nil {
		log.Printf("register font: %v", err)
	}
}

// pal is the app's colors, one set for each appearance: paper and ink, with no tint in
// them, and the cobalt of the icon as the one color that is not a state. The sky brings
// the rest.
type pal struct {
	dark                  bool
	bg, side, card, card2 ui.Color
	line                  ui.Color
	border, hover         ui.Color
	text, muted, faint    ui.Color
	accent, ink, soft     ui.Color // ink is the accent as text and icons
	onAccent              ui.Color
	capsule, onCapsule    ui.Color // what is selected, and its text
	water, warm, power    ui.Color
	warn, bad, ok, off    ui.Color
}

func palette(dark bool) pal {
	if dark {
		accent := ui.Oklch(0.68, 0.17, 255)
		return pal{dark: true,
			bg: ui.Oklch(0.17, 0.003, 85), side: ui.Oklch(0.21, 0.003, 85), card: ui.Oklch(0.215, 0.003, 85), card2: ui.Oklch(0.27, 0.003, 85),
			line: ui.RGBA(255, 255, 255, 0.08), border: ui.RGBA(255, 255, 255, 0.14), hover: ui.RGBA(255, 255, 255, 0.06),
			text: ui.Oklch(0.96, 0.003, 85), muted: ui.Oklch(0.74, 0.004, 85), faint: ui.Oklch(0.66, 0.004, 85),
			accent: accent, ink: ui.Oklch(0.8, 0.12, 255), soft: accent.Alpha(0.18), onAccent: ui.Oklch(0.17, 0.02, 255),
			capsule: ui.Oklch(0.96, 0.003, 85), onCapsule: ui.Oklch(0.17, 0.003, 85),
			water: ui.Oklch(0.76, 0.11, 215), warm: ui.Oklch(0.74, 0.14, 40), power: ui.Oklch(0.8, 0.14, 80),
			warn: ui.Oklch(0.82, 0.14, 80), bad: ui.Oklch(0.72, 0.17, 25), ok: ui.Oklch(0.78, 0.14, 160), off: ui.Oklch(0.46, 0.004, 85),
		}
	}
	accent := ui.Oklch(0.56, 0.2, 255)
	return pal{
		bg: ui.Oklch(0.955, 0.005, 85), side: ui.Oklch(0.925, 0.006, 85), card: ui.RGB(255, 255, 255), card2: ui.Oklch(0.94, 0.005, 85),
		line: ui.RGBA(20, 18, 12, 0.08), border: ui.RGBA(20, 18, 12, 0.16), hover: ui.RGBA(20, 18, 12, 0.05),
		text: ui.Oklch(0.2, 0.005, 85), muted: ui.Oklch(0.46, 0.006, 85), faint: ui.Oklch(0.52, 0.006, 85),
		accent: accent, ink: ui.Oklch(0.5, 0.2, 255), soft: accent.Alpha(0.1), onAccent: ui.RGB(255, 255, 255),
		capsule: ui.Oklch(0.2, 0.005, 85), onCapsule: ui.RGB(255, 255, 255),
		water: ui.Oklch(0.58, 0.12, 225), warm: ui.Oklch(0.62, 0.17, 38), power: ui.Oklch(0.68, 0.15, 70),
		warn: ui.Oklch(0.55, 0.13, 65), bad: ui.Oklch(0.55, 0.19, 25), ok: ui.Oklch(0.58, 0.14, 160), off: ui.Oklch(0.72, 0.005, 85),
	}
}

// sky is the backdrop of the air's card, one flat color for each state: a gradient
// from light down to dark makes colors this saturated look like a cartoon. What is on
// it takes its colors from it: the shadows are its own hue, darker, and not gray.
type sky struct {
	bg, fg      ui.Color
	shade, deep ui.Color // Moko's underside, and its shadow on the sky
}

var (
	airSkies = [...]sky{
		{bg: ui.Oklch(0.56, 0.2, 255), fg: mokoWhite, shade: mokoShade, deep: mokoDeep},                                               // fresh
		{bg: ui.Oklch(0.52, 0.1, 190), fg: mokoWhite, shade: ui.Oklch(0.88, 0.05, 185), deep: ui.Oklch(0.3, 0.07, 195)},               // good
		{bg: ui.Oklch(0.86, 0.15, 90), fg: ui.Oklch(0.27, 0.05, 75), shade: ui.Oklch(0.93, 0.07, 90), deep: ui.Oklch(0.55, 0.13, 70)}, // moderate
		{bg: ui.Oklch(0.58, 0.17, 45), fg: mokoWhite, shade: ui.Oklch(0.88, 0.05, 55), deep: ui.Oklch(0.36, 0.12, 40)},                // poor
		{bg: ui.Oklch(0.53, 0.18, 25), fg: mokoWhite, shade: ui.Oklch(0.85, 0.05, 25), deep: ui.Oklch(0.3, 0.12, 25)},                 // very poor
	}
	nightSky   = sky{bg: ui.Oklch(0.25, 0.05, 268), fg: mokoWhite, shade: ui.Oklch(0.82, 0.05, 268), deep: ui.Oklch(0.12, 0.04, 268)}
	unknownSky = sky{bg: ui.Oklch(0.5, 0.008, 85), fg: mokoWhite, shade: ui.Oklch(0.86, 0.005, 85), deep: ui.Oklch(0.28, 0.005, 85)}

	// Cards about something other than the air have colors of their own: the air's would
	// say something about the air. Home is the warm orange of the Home app, and a device
	// that answers is green.
	homeSky   = sky{bg: ui.Oklch(0.8, 0.15, 72), fg: ui.Oklch(0.27, 0.06, 60), shade: ui.Oklch(0.93, 0.06, 75), deep: ui.Oklch(0.56, 0.14, 60)}
	deviceSky = sky{bg: ui.Oklch(0.52, 0.11, 165), fg: mokoWhite, shade: ui.Oklch(0.88, 0.05, 165), deep: ui.Oklch(0.3, 0.07, 170)}
)

// airLevel is how a PM2.5 reading in µg/m³ reads: its band, from 0 for the
// cleanest, the same bands Apple Home gets.
func airLevel(pm int) int {
	switch {
	case pm <= 12:
		return 0
	case pm <= 35:
		return 1
	case pm <= 55:
		return 2
	case pm <= 150:
		return 3
	}
	return 4
}

func airLevelName(level int) string {
	switch level {
	case 0:
		return i18n.T("空气很清新", "空気きれい", "Air is fresh")
	case 1:
		return i18n.T("空气不错", "空気は良好", "Air is good")
	case 2:
		return i18n.T("空气一般", "空気はふつう", "Air is so-so")
	case 3:
		return i18n.T("空气有点差", "空気が悪い", "Air is poor")
	}
	return i18n.T("空气很差", "空気がとても悪い", "Air is very poor")
}

// airLevelHint is what to do about the air, when there is something to do.
func airLevelHint(level int) string {
	switch level {
	case 3:
		return i18n.T("可以调高风量试试", "風量を上げてみましょう", "Try a higher fan speed")
	case 4:
		return i18n.T("建议调到强档", "強運転がおすすめです", "High fan speed is a good idea")
	}
	return ""
}
