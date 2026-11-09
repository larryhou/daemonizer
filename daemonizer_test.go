package daemonizer

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// fakeRunner records every external command instead of running it.
type fakeRunner struct {
	calls   []string
	failOn  string // fail when the joined command line contains this
	failOut string
}

func (f *fakeRunner) run(name string, args ...string) ([]byte, error) {
	line := filepath.Base(name) + " " + strings.Join(args, " ")
	f.calls = append(f.calls, line)
	if f.failOn != "" && strings.Contains(line, f.failOn) {
		return []byte(f.failOut), errors.New("exit status 1")
	}
	return nil, nil
}

var tricky = Spec{
	Name:        "demo.svc-1",
	Description: "Demo\nInjected=1",
	Exe:         "/usr/local/bin/demo",
	Args:        []string{"client", "-s", `h:1/to"k<>&%$en`, "with space", `back\slash`},
	LogFile:     "/tmp/a&b%.log",
}

func TestValidateName(t *testing.T) {
	for _, ok := range []string{"a", "portal-8080", "com.example.app", "A_b.c-1"} {
		if err := ValidateName(ok); err != nil {
			t.Errorf("%q should be valid: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "-a", ".a", "a b", "a/b", `a\b`, "../x", "a;b", "名字"} {
		if err := ValidateName(bad); err == nil {
			t.Errorf("%q should be rejected", bad)
		}
	}
}

func TestInstallRejectsBadSpec(t *testing.T) {
	for name, s := range map[string]Spec{
		"bad name":     {Name: "a/b", Exe: "/x"},
		"relative exe": {Name: "a", Exe: "x"},
		"relative log": {Name: "a", Exe: "/x", LogFile: "log.txt"},
	} {
		if err := Install(s); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
	if err := Uninstall("a/b"); err == nil {
		t.Error("Uninstall should validate name")
	}
}

func TestRenderPlist(t *testing.T) {
	out, err := renderPlist(tricky)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"<string>demo.svc-1</string>",
		"<string>/usr/local/bin/demo</string>",
		"<string>h:1/to&#34;k&lt;&gt;&amp;%$en</string>",
		"<string>back\\slash</string>",
		"<string>/tmp/a&amp;b%.log</string>",
		"<key>KeepAlive</key>",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
	noLog := tricky
	noLog.LogFile = ""
	if out, _ := renderPlist(noLog); strings.Contains(out, "StandardOutPath") {
		t.Error("no LogFile => no StandardOutPath")
	}
}

func TestRenderSystemdUnit(t *testing.T) {
	out := renderSystemdUnit(tricky)
	want := `ExecStart="/usr/local/bin/demo" "client" "-s" "h:1/to\"k<>&%%$$en" "with space" "back\\slash"`
	if !strings.Contains(out, want) {
		t.Errorf("ExecStart\nwant %s\ngot\n%s", want, out)
	}
	if strings.Contains(out, "\nInjected=1") {
		t.Errorf("newline in Description must not create a new directive:\n%s", out)
	}
	for _, w := range []string{"Restart=always", "StandardOutput=append:/tmp/a&b%%.log", "WantedBy=default.target"} {
		if !strings.Contains(out, w) {
			t.Errorf("missing %q in\n%s", w, out)
		}
	}
}

func TestWindowsQuote(t *testing.T) {
	for in, want := range map[string]string{
		`plain`:      `"plain"`,
		`with space`: `"with space"`,
		`say "hi"`:   `"say \"hi\""`,
		`C:\dir\`:    `"C:\dir\\"`, // trailing backslashes doubled before closing quote
		`a\"b`:       `"a\\\"b"`,   // backslash before quote: 2n+1
		`C:\a\b`:     `"C:\a\b"`,   // inner backslashes untouched
		``:           `""`,
	} {
		if got := windowsQuote(in); got != want {
			t.Errorf("windowsQuote(%q) = %s, want %s", in, got, want)
		}
	}
}

func TestRenderTaskXML(t *testing.T) {
	s := tricky
	s.Exe = `C:\Program Files\demo.exe`
	out, err := renderTaskXML(s, `PC\me`)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`<Command>C:\Program Files\demo.exe</Command>`,
		`<UserId>PC\me</UserId>`,
		`&lt;&gt;&amp;%$en`,
		"<RestartOnFailure>",
		"<LogonTrigger>",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
	s.Args = nil
	if out, _ := renderTaskXML(s, "u"); strings.Contains(out, "<Arguments>") {
		t.Error("no args => no <Arguments>")
	}
	if b := encodeUTF16LE("A\U0001F600"); len(b) != 2+2+4 || b[0] != 0xFF || b[1] != 0xFE {
		t.Errorf("bad utf16le (surrogate pair): % x", b)
	}
}

func TestLaunchdLifecycle(t *testing.T) {
	home := t.TempDir()
	f := &fakeRunner{}
	if err := installLaunchd(tricky, home, f.run); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, "Library", "LaunchAgents", "demo.svc-1.plist")
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && fi.Mode().Perm() != 0o600 {
		t.Errorf("plist mode = %v, want 0600", fi.Mode().Perm())
	}
	// unload (old) must precede load (new)
	if len(f.calls) != 2 || !strings.HasPrefix(f.calls[0], "launchctl unload") || !strings.HasPrefix(f.calls[1], "launchctl load") {
		t.Errorf("calls = %v", f.calls)
	}

	if err := uninstallLaunchd("demo.svc-1", home, f.run); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("plist should be removed")
	}
	if err := uninstallLaunchd("demo.svc-1", home, f.run); !errors.Is(err, ErrNotInstalled) {
		t.Errorf("second uninstall = %v, want ErrNotInstalled", err)
	}

	bad := &fakeRunner{failOn: "load", failOut: "boom"}
	if err := installLaunchd(tricky, home, bad.run); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Errorf("load failure should surface output, got %v", err)
	}
}

func TestSystemdLifecycle(t *testing.T) {
	home := t.TempDir()
	f := &fakeRunner{}
	if err := installSystemd(tricky, home, f.run); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, ".config", "systemd", "user", "demo.svc-1.service")
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(f.calls, "\n")
	for _, w := range []string{"daemon-reload", "enable --now demo.svc-1.service", "restart demo.svc-1.service"} {
		if !strings.Contains(joined, w) {
			t.Errorf("missing call %q in\n%s", w, joined)
		}
	}
	if err := uninstallSystemd("demo.svc-1", home, f.run); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("unit should be removed")
	}
	if err := uninstallSystemd("demo.svc-1", home, f.run); !errors.Is(err, ErrNotInstalled) {
		t.Errorf("second uninstall = %v, want ErrNotInstalled", err)
	}
}

func TestSchtasksLifecycle(t *testing.T) {
	s := tricky
	s.Exe = `C:\demo.exe`
	f := &fakeRunner{}
	if err := installSchtasks(s, f.run); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(f.calls, "\n")
	for _, w := range []string{"schtasks.exe /End /TN demo.svc-1", "/Create /XML", "/TN demo.svc-1 /F", "/Run /TN demo.svc-1"} {
		if !strings.Contains(joined, w) {
			t.Errorf("missing %q in\n%s", w, joined)
		}
	}
	// temp XML (may hold secrets) must be gone after install
	for _, c := range f.calls {
		if strings.Contains(c, "/Create") {
			xmlPath := strings.Fields(c)[3]
			if _, err := os.Stat(xmlPath); !os.IsNotExist(err) {
				t.Errorf("temp xml %s should be removed", xmlPath)
			}
		}
	}

	f = &fakeRunner{}
	if err := uninstallSchtasks("demo.svc-1", f.run); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(f.calls, "\n"), "/Delete /TN demo.svc-1 /F") {
		t.Errorf("calls = %v", f.calls)
	}
	missing := &fakeRunner{failOn: "/Query"}
	if err := uninstallSchtasks("nope", missing.run); !errors.Is(err, ErrNotInstalled) {
		t.Errorf("= %v, want ErrNotInstalled", err)
	}
}
