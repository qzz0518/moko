//go:build !windows

package firewall

// Status is always Allowed: only Windows needs this.
func Status() string { return Allowed }

// Allow does nothing.
func Allow() error { return nil }
