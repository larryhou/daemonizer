package daemonizer

import (
	"bytes"
	"fmt"
	"html"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"text/template"
	"unicode/utf16"
)

// Logon trigger + interactive token: the service runs in the user's desktop
// session. On failure it is retried every minute (PT1M is the smallest value
// the schema allows), up to 255 times (the schema maximum).
var taskTmpl = template.Must(template.New("task").Funcs(template.FuncMap{"x": html.EscapeString}).Parse(`<?xml version="1.0" encoding="UTF-16"?>
<Task version="1.2" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task">
  <RegistrationInfo>
    <Description>{{x .Description}}</Description>
  </RegistrationInfo>
  <Triggers>
    <LogonTrigger>
      <Enabled>true</Enabled>
      <UserId>{{x .User}}</UserId>
    </LogonTrigger>
  </Triggers>
  <Principals>
    <Principal id="Author">
      <UserId>{{x .User}}</UserId>
      <LogonType>InteractiveToken</LogonType>
      <RunLevel>LeastPrivilege</RunLevel>
    </Principal>
  </Principals>
  <Settings>
    <MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy>
    <DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries>
    <StopIfGoingOnBatteries>false</StopIfGoingOnBatteries>
    <ExecutionTimeLimit>PT0S</ExecutionTimeLimit>
    <Hidden>true</Hidden>
    <RestartOnFailure>
      <Interval>PT1M</Interval>
      <Count>255</Count>
    </RestartOnFailure>
    <Enabled>true</Enabled>
  </Settings>
  <Actions Context="Author">
    <Exec>
      <Command>{{x .Exe}}</Command>
{{- if .Args}}
      <Arguments>{{x .Args}}</Arguments>
{{- end}}
    </Exec>
  </Actions>
</Task>
`))

// windowsQuote quotes one argument following CommandLineToArgvW rules:
// backslashes are literal unless they precede a double quote, in which case
// they (and the quote) must be escaped.
func windowsQuote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	slashes := 0
	for _, r := range s {
		switch r {
		case '\\':
			slashes++
		case '"':
			b.WriteString(strings.Repeat(`\`, slashes*2+1))
			b.WriteByte('"')
			slashes = 0
		default:
			b.WriteString(strings.Repeat(`\`, slashes))
			slashes = 0
			b.WriteRune(r)
		}
	}
	// Trailing backslashes precede our closing quote, so they must be doubled.
	b.WriteString(strings.Repeat(`\`, slashes*2))
	b.WriteByte('"')
	return b.String()
}

func renderTaskXML(s Spec, userName string) (string, error) {
	quoted := make([]string, len(s.Args))
	for i, a := range s.Args {
		quoted[i] = windowsQuote(a)
	}
	desc := s.Description
	if desc == "" {
		desc = s.Name
	}
	var b bytes.Buffer
	err := taskTmpl.Execute(&b, map[string]any{
		"Description": desc, "User": userName, "Exe": s.Exe, "Args": strings.Join(quoted, " "),
	})
	return b.String(), err
}

// encodeUTF16LE converts to UTF-16 LE with a BOM, as Task Scheduler requires.
func encodeUTF16LE(s string) []byte {
	out := []byte{0xFF, 0xFE}
	for _, w := range utf16.Encode([]rune(s)) {
		out = append(out, byte(w), byte(w>>8))
	}
	return out
}

// schtasksPath resolves schtasks.exe even when PATH lacks System32.
func schtasksPath() string {
	if sr := os.Getenv("SystemRoot"); sr != "" {
		return filepath.Join(sr, "System32", "schtasks.exe")
	}
	return `C:\Windows\System32\schtasks.exe`
}

func installSchtasks(s Spec, run runner) error {
	u, err := user.Current()
	if err != nil {
		return fmt.Errorf("daemonizer: cannot determine current user: %w", err)
	}
	xmlText, err := renderTaskXML(s, u.Username)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp("", "daemonizer_*.xml")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // the XML may contain secrets; remove it as soon as schtasks has read it
	_, werr := tmp.Write(encodeUTF16LE(xmlText))
	if cerr := tmp.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		return werr
	}

	st := schtasksPath()
	_, _ = run(st, "/End", "/TN", s.Name)
	if out, err := run(st, "/Create", "/XML", tmp.Name(), "/TN", s.Name, "/F"); err != nil {
		return fmt.Errorf("daemonizer: schtasks /Create: %v\n%s", err, out)
	}
	if out, err := run(st, "/Run", "/TN", s.Name); err != nil {
		return fmt.Errorf("daemonizer: schtasks /Run: %v\n%s", err, out)
	}
	return nil
}

func uninstallSchtasks(name string, run runner) error {
	st := schtasksPath()
	if _, err := run(st, "/Query", "/TN", name); err != nil {
		return fmt.Errorf("%w: task %q", ErrNotInstalled, name)
	}
	_, _ = run(st, "/End", "/TN", name)
	if out, err := run(st, "/Delete", "/TN", name, "/F"); err != nil {
		return fmt.Errorf("daemonizer: schtasks /Delete: %v\n%s", err, out)
	}
	return nil
}
