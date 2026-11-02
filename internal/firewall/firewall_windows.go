//go:build windows

package firewall

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"unicode/utf16"
)

const (
	createNoWindow = 0x08000000
	ruleName       = "Moko"
)

// Status tells what Windows Firewall does with this program's inbound traffic on a
// private network. Reading the rules needs no elevation.
func Status() string {
	exe, err := os.Executable()
	if err != nil {
		return Missing
	}
	script := fmt.Sprintf(`$rules = Get-NetFirewallApplicationFilter -Program %s -ErrorAction SilentlyContinue |
  Get-NetFirewallRule -ErrorAction SilentlyContinue |
  Where-Object { $_.Direction -eq 'Inbound' -and $_.Enabled -eq 'True' -and ($_.Profile -match 'Private|Any') }
if ($rules | Where-Object Action -eq 'Block') { 'blocked' }
elseif ($rules | Where-Object Action -eq 'Allow') { 'allowed' }
else { 'missing' }`, psQuote(exe))
	out, err := powershell(script).Output()
	if err != nil {
		return Missing
	}
	switch s := strings.TrimSpace(string(out)); s {
	case Allowed, Blocked:
		return s
	}
	return Missing
}

// Allow asks for administrator rights once and replaces this program's inbound rules
// with one that allows it on private and domain networks. The rule covers every
// protocol: Apple Home uses TCP, Bonjour and ECHONET Lite UDP.
func Allow() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	inner := fmt.Sprintf(`$p = %s
Get-NetFirewallApplicationFilter -Program $p -ErrorAction SilentlyContinue |
  Get-NetFirewallRule -ErrorAction SilentlyContinue |
  Where-Object Direction -eq 'Inbound' | Remove-NetFirewallRule
New-NetFirewallRule -DisplayName %s -Direction Inbound -Program $p -Action Allow -Profile Private,Domain | Out-Null`,
		psQuote(exe), psQuote(ruleName))
	outer := fmt.Sprintf(`try {
  $p = Start-Process powershell -Verb RunAs -WindowStyle Hidden -Wait -PassThru -ArgumentList '-NoProfile','-NonInteractive','-EncodedCommand','%s'
  exit $p.ExitCode
} catch { exit 1223 }`, encodePS(inner))
	err = powershell(outer).Run()
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == 1223 {
		return ErrDeclined
	}
	return err
}

// powershell runs a script without a console window.
func powershell(script string) *exec.Cmd {
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-EncodedCommand", encodePS(script))
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
	return cmd
}

// encodePS is PowerShell's -EncodedCommand form: base64 of UTF-16LE.
func encodePS(script string) string {
	u := utf16.Encode([]rune(script))
	b := make([]byte, 2*len(u))
	for i, c := range u {
		b[2*i], b[2*i+1] = byte(c), byte(c>>8)
	}
	return base64.StdEncoding.EncodeToString(b)
}

// psQuote is a single-quoted PowerShell string literal.
func psQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }
