# daemonizer

Register a Go program as a per-user background service that starts automatically
and is restarted when it exits. Standard library only, no root needed.

| OS | Mechanism | Registration |
|---|---|---|
| macOS | launchd | `~/Library/LaunchAgents/<name>.plist` |
| Linux | systemd user | `~/.config/systemd/user/<name>.service` |
| Windows | Task Scheduler | task `<name>`, logon trigger, retried every minute on failure |

## Usage

```go
import "github.com/larryhou/daemonizer"

err := daemonizer.Install(daemonizer.Spec{
    Name:        "com.example.myapp",
    Description: "My app",
    Args:        []string{"run", "--token", "s3cret"}, // passed verbatim
    LogFile:     "/Users/me/.config/myapp/myapp.log",  // absolute path
})

err = daemonizer.Uninstall("com.example.myapp")
if errors.Is(err, daemonizer.ErrNotInstalled) { /* nothing to remove */ }
```

`Install` starts the service immediately and replaces any earlier registration
with the same `Name`. `Exe` defaults to the running executable.

Names may contain letters, digits, `.`, `_`, `-` and must start with a letter or
digit. The same name is used as the launchd label, systemd unit name and task name.

Typical pattern: add a `--daemon` flag to your program that calls `Install`
with the program's own arguments (minus `--daemon`) and exits.

## Notes

- **Secrets:** `Args` are written to the service definition, so files are created
  with mode `0600`. Keep that in mind for tokens on the command line.
- **Quoting** is handled per platform (XML escaping for plist/task XML,
  `systemd.syntax` quoting including `%` and `$`, `CommandLineToArgvW` rules on
  Windows); arguments may contain spaces and special characters.
- **LogFile:** macOS and Linux (systemd >= 240). Task Scheduler cannot redirect
  output, so on Windows the program must write its own log file.
- **Linux boot:** user services start at login. To start at boot without a login
  run `loginctl enable-linger <user>`.
- **Binary path:** the service points at the executable path used at install time.
  Re-run `Install` after moving or upgrading the binary.
- **Windows:** runs in the user's interactive session (logon trigger), not as a
  Session 0 service.

## Testing

Rendering and command sequencing are tested on every OS with a fake command
runner. Real registration has been exercised on macOS only.

## License

MIT
