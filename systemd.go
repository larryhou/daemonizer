package daemonizer

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// systemdQuote quotes one ExecStart word following systemd.syntax(7):
// wrapped in double quotes, with \ and " escaped. '%' (specifier) and '$'
// (environment expansion) are doubled so they are taken literally.
func systemdQuote(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, `%`, `%%`, `$`, `$$`).Replace(s) + `"`
}

func renderSystemdUnit(s Spec) string {
	words := []string{systemdQuote(s.Exe)}
	for _, a := range s.Args {
		words = append(words, systemdQuote(a))
	}
	desc := s.Description
	if desc == "" {
		desc = s.Name
	}
	// Description is a single line; strip newlines so it cannot inject directives.
	desc = strings.NewReplacer("\n", " ", "\r", " ").Replace(desc)

	var b strings.Builder
	fmt.Fprintf(&b, "[Unit]\nDescription=%s\nAfter=network-online.target\nWants=network-online.target\n\n", desc)
	fmt.Fprintf(&b, "[Service]\nExecStart=%s\nRestart=always\nRestartSec=5\n", strings.Join(words, " "))
	if s.LogFile != "" {
		// "append:" needs systemd >= 240.
		path := strings.NewReplacer("%", "%%").Replace(s.LogFile)
		fmt.Fprintf(&b, "StandardOutput=append:%s\nStandardError=append:%s\n", path, path)
	}
	b.WriteString("\n[Install]\nWantedBy=default.target\n")
	return b.String()
}

func unitPath(home, name string) string {
	return filepath.Join(home, ".config", "systemd", "user", name+".service")
}

func installSystemd(s Spec, home string, run runner) error {
	if err := writePrivate(unitPath(home, s.Name), []byte(renderSystemdUnit(s))); err != nil {
		return fmt.Errorf("daemonizer: write unit: %w", err)
	}
	_, _ = run("systemctl", "--user", "daemon-reload")
	// "restart" after "enable --now" makes re-installs with changed args take effect.
	if out, err := run("systemctl", "--user", "enable", "--now", s.Name+".service"); err != nil {
		return fmt.Errorf("daemonizer: systemctl enable: %v\n%s", err, out)
	}
	if out, err := run("systemctl", "--user", "restart", s.Name+".service"); err != nil {
		return fmt.Errorf("daemonizer: systemctl restart: %v\n%s", err, out)
	}
	return nil
}

func uninstallSystemd(name, home string, run runner) error {
	path := unitPath(home, name)
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("%w: %s", ErrNotInstalled, path)
	}
	_, _ = run("systemctl", "--user", "disable", "--now", name+".service")
	if err := os.Remove(path); err != nil {
		return err
	}
	_, _ = run("systemctl", "--user", "daemon-reload")
	return nil
}
