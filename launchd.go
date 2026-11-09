package daemonizer

import (
	"bytes"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"text/template"
)

var plistTmpl = template.Must(template.New("plist").Funcs(template.FuncMap{"x": html.EscapeString}).Parse(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>{{x .Name}}</string>
	<key>ProgramArguments</key>
	<array>
		<string>{{x .Exe}}</string>
{{- range .Args}}
		<string>{{x .}}</string>
{{- end}}
	</array>
	<key>RunAtLoad</key>
	<true/>
	<key>KeepAlive</key>
	<true/>
	<key>ThrottleInterval</key>
	<integer>5</integer>
{{- if .LogFile}}
	<key>StandardOutPath</key>
	<string>{{x .LogFile}}</string>
	<key>StandardErrorPath</key>
	<string>{{x .LogFile}}</string>
{{- end}}
</dict>
</plist>
`))

func renderPlist(s Spec) (string, error) {
	var b bytes.Buffer
	err := plistTmpl.Execute(&b, s)
	return b.String(), err
}

func plistPath(home, name string) string {
	return filepath.Join(home, "Library", "LaunchAgents", name+".plist")
}

func installLaunchd(s Spec, home string, run runner) error {
	content, err := renderPlist(s)
	if err != nil {
		return err
	}
	path := plistPath(home, s.Name)
	// Unload any previous version first so changed arguments take effect.
	_, _ = run("launchctl", "unload", "-w", path)
	if err := writePrivate(path, []byte(content)); err != nil {
		return fmt.Errorf("daemonizer: write plist: %w", err)
	}
	if out, err := run("launchctl", "load", "-w", path); err != nil {
		return fmt.Errorf("daemonizer: launchctl load: %v\n%s", err, out)
	}
	return nil
}

func uninstallLaunchd(name, home string, run runner) error {
	path := plistPath(home, name)
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("%w: %s", ErrNotInstalled, path)
	}
	_, _ = run("launchctl", "unload", "-w", path)
	return os.Remove(path)
}
