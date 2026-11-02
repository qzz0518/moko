// Package firewall lets the app through Windows Firewall. The purifier answers on
// UDP 3610 and iPhones connect to the Apple Home accessory, both inbound. Windows'
// own alert appears the first time the app listens, and canceling it leaves block
// rules behind that stop the app from then on, so the app asks first instead.
package firewall

import "errors"

// States of the firewall towards this program.
const (
	Allowed = "allowed"
	Missing = "missing"
	Blocked = "blocked"
)

// ErrDeclined is an administrator prompt the user said no to.
var ErrDeclined = errors.New("firewall: the administrator prompt was declined")
