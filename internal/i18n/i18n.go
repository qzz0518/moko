// Package i18n holds the app's language: Chinese, Japanese or English.
package i18n

import (
	"strings"
	"sync/atomic"
)

var current atomic.Value

// Set chooses the language from the setting ("zh", "ja", "en"; else the system's) and
// the languages the user reads, most preferred first, such as "zh-Hans-JP".
func Set(setting string, preferred ...string) {
	switch setting {
	case "zh", "ja", "en":
		current.Store(setting)
		return
	}
	for _, l := range preferred {
		for _, known := range []string{"zh", "ja", "en"} {
			if strings.HasPrefix(l, known) {
				current.Store(known)
				return
			}
		}
	}
	current.Store("en")
}

// Lang is the language in use.
func Lang() string {
	if s, ok := current.Load().(string); ok {
		return s
	}
	return "zh"
}

// T is the text in the language in use.
func T(zh, ja, en string) string {
	switch Lang() {
	case "ja":
		return ja
	case "en":
		return en
	}
	return zh
}
