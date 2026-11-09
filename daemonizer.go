// Package daemonizer registers the current program as a per-user background
// service that starts automatically and is restarted when it exits:
//
//	macOS   launchd   ~/Library/LaunchAgents/<name>.plist
//	Linux   systemd   ~/.config/systemd/user/<name>.service   (systemctl --user)
//	Windows Task Scheduler task <name>, triggered at logon
//
// No root/Administrator rights are needed. The package has no dependencies
// outside the standard library.
package daemonizer

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
)

// Spec describes the service to register.
type Spec struct {
	// Name identifies the service on every platform: the launchd label, the
	// systemd unit name (without ".service") and the Windows task name.
	// Allowed characters: letters, digits, '.', '_' and '-'; it must start
	// with a letter or digit. Installing again with the same Name replaces
	// the previous registration.
	Name string

	// Description is shown by the service manager. Optional.
	Description string

	// Exe is the absolute path of the program to run. Optional: defaults to
	// the running executable with symlinks resolved.
	Exe string

	// Args are passed to Exe verbatim. Quoting for each platform is handled
	// here, so values may contain spaces and shell/XML special characters.
	Args []string

	// LogFile receives stdout and stderr of the service. Optional.
	// Supported on macOS and Linux (systemd >= 240). Windows Task Scheduler
	// cannot redirect output, so the program must write its own log file.
	// Must be an absolute path.
	LogFile string
}

var nameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// ValidateName reports whether name is usable as a service name.
func ValidateName(name string) error {
	if !nameRe.MatchString(name) {
		return fmt.Errorf("daemonizer: invalid name %q: use letters, digits, '.', '_', '-' and start with a letter or digit", name)
	}
	return nil
}

// Install registers the service and starts it immediately. Any existing
// registration with the same Name is replaced.
//
// The generated service definition contains Args verbatim (which may include
// secrets), so files are created with mode 0600.
func Install(s Spec) error {
	if err := ValidateName(s.Name); err != nil {
		return err
	}
	if s.Exe == "" {
		exe, err := os.Executable()
		if err != nil {
			return fmt.Errorf("daemonizer: cannot determine executable path: %w", err)
		}
		if exe, err = filepath.EvalSymlinks(exe); err != nil {
			return fmt.Errorf("daemonizer: %w", err)
		}
		s.Exe = exe
	}
	if !filepath.IsAbs(s.Exe) {
		return fmt.Errorf("daemonizer: Exe must be an absolute path, got %q", s.Exe)
	}
	if s.LogFile != "" {
		if !filepath.IsAbs(s.LogFile) {
			return fmt.Errorf("daemonizer: LogFile must be an absolute path, got %q", s.LogFile)
		}
		if err := os.MkdirAll(filepath.Dir(s.LogFile), 0o700); err != nil {
			return fmt.Errorf("daemonizer: create log dir: %w", err)
		}
	}
	return platformInstall(s)
}

// ErrNotInstalled is returned by Uninstall when no such service exists.
var ErrNotInstalled = errors.New("daemonizer: service not installed")

// Uninstall stops the service and removes its registration. It returns an
// error wrapping ErrNotInstalled if the service does not exist.
func Uninstall(name string) error {
	if err := ValidateName(name); err != nil {
		return err
	}
	return platformUninstall(name)
}

// ─── platform dispatch ───────────────────────────────────────────────────────

// runner executes an external command and returns its combined output.
// It is a variable so tests can substitute a fake.
type runner func(name string, args ...string) ([]byte, error)

func execRunner(name string, args ...string) ([]byte, error) {
	return exec.Command(name, args...).CombinedOutput()
}

var run runner = execRunner

func platformInstall(s Spec) error {
	switch runtime.GOOS {
	case "darwin", "linux":
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("daemonizer: cannot determine home dir: %w", err)
		}
		if runtime.GOOS == "darwin" {
			return installLaunchd(s, home, run)
		}
		return installSystemd(s, home, run)
	case "windows":
		return installSchtasks(s, run)
	}
	return fmt.Errorf("daemonizer: unsupported OS %q", runtime.GOOS)
}

func platformUninstall(name string) error {
	switch runtime.GOOS {
	case "darwin", "linux":
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("daemonizer: cannot determine home dir: %w", err)
		}
		if runtime.GOOS == "darwin" {
			return uninstallLaunchd(name, home, run)
		}
		return uninstallSystemd(name, home, run)
	case "windows":
		return uninstallSchtasks(name, run)
	}
	return fmt.Errorf("daemonizer: unsupported OS %q", runtime.GOOS)
}

// writePrivate writes a file readable only by its owner.
func writePrivate(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}
