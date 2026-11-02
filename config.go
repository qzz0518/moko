package main

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"

	"github.com/egoist/mygo"

	"github.com/qzz0518/moko/internal/weather"
)

// defaultPrice is Sharp's own figure for its product pages, in yen per kWh.
const defaultPrice = 31

// config is what the app remembers.
type config struct {
	Language   string  `json:"language,omitempty"`   // "", "zh", "ja", "en"
	Appearance string  `json:"appearance,omitempty"` // "", "light", "dark"
	ManualIP   string  `json:"manualIP,omitempty"`
	LastIP     string  `json:"lastIP,omitempty"`
	NodeID     string  `json:"nodeID,omitempty"`
	HomeKit    bool    `json:"homeKit,omitempty"`
	KeepAwake  bool    `json:"keepAwake,omitempty"`
	Price      float64 `json:"price,omitempty"`
	NoWaterOff bool    `json:"noWaterAlertOff,omitempty"`

	WeatherOff bool           `json:"weatherOff,omitempty"`
	Place      *weather.Place `json:"place,omitempty"`
	PlaceFixed bool           `json:"placeFixed,omitempty"` // the user chose it, rather than its being estimated
}

func configPath() string {
	dir, err := mygo.App.Path(mygo.PathUserData)
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "config.json")
}

func loadConfig() *config {
	cfg := &config{Price: defaultPrice}
	path := configPath()
	if path == "" {
		return cfg
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return cfg
	}
	if err := json.Unmarshal(b, cfg); err != nil {
		log.Printf("config: %v", err)
		return &config{Price: defaultPrice}
	}
	if cfg.Price <= 0 {
		cfg.Price = defaultPrice
	}
	return cfg
}

// save writes the file whole, by way of another name, so that a crash cannot leave it
// half written.
func (c *config) save() {
	path := configPath()
	if path == "" {
		return
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		log.Printf("config: %v", err)
		return
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		log.Printf("config: %v", err)
		return
	}
	if err := os.Rename(tmp, path); err != nil {
		log.Printf("config: %v", err)
	}
}
