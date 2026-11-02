package main

import (
	"testing"
	"time"

	"github.com/qzz0518/moko/internal/device"
	"github.com/qzz0518/moko/internal/sharp"
)

func TestSampleOfKeepsUnknownsUnknown(t *testing.T) {
	now := time.Now()
	s := sampleOf(now, device.State{})
	if s.PM25 != -1 || s.TempC != -1 || s.Humidity != -1 || s.Watts != -1 || s.EnergyWh != -1 {
		t.Errorf("sample = %+v", s)
	}
	s = sampleOf(now, device.State{
		SensorsOK: true,
		Sensors:   sharp.Sensors{TempC: 24, TempOK: true, Humidity: 51, HumidityOK: true, PM25: 0, PM25OK: true},
		Watts:     13, WattsOK: true, EnergyWh: 100, EnergyOK: true,
	})
	if s.PM25 != 0 || s.TempC != 24 || s.Humidity != 51 || s.Watts != 13 || s.EnergyWh != 100 {
		t.Errorf("sample = %+v", s)
	}
}
