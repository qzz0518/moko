package firewall

import "testing"

// Reads the rules on Windows, which needs no elevation, and changes nothing.
func TestStatusAnswers(t *testing.T) {
	switch s := Status(); s {
	case Allowed, Missing, Blocked:
	default:
		t.Errorf("status = %q", s)
	}
}
