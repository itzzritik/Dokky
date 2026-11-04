package daemon

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"os"
	"strings"
)

const (
	launchdLabel = "me.ritik.dokky"
	systemdUnit  = "dokky.service"
)

func xmlEscape(s string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

func renderLaunchAgent(bin string) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>%s</string>
    <key>ProgramArguments</key>
    <array>
        <string>%s</string>
        <string>daemon</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <dict>
        <key>SuccessfulExit</key>
        <false/>
    </dict>
    <key>ThrottleInterval</key>
    <integer>10</integer>
    <key>ProcessType</key>
    <string>Interactive</string>
    <key>ExitTimeOut</key>
    <integer>60</integer>
    <key>StandardOutPath</key>
    <string>/dev/null</string>
    <key>StandardErrorPath</key>
    <string>/dev/null</string>
</dict>
</plist>
`, launchdLabel, xmlEscape(bin))
}

func renderSystemdUnit(bin string) string {
	return fmt.Sprintf(`[Unit]
Description=dokky daemon
StartLimitIntervalSec=300
StartLimitBurst=10

[Service]
ExecStart="%s" daemon
Restart=on-failure
RestartSec=5
TimeoutStopSec=60

[Install]
WantedBy=default.target
`, systemdQuote(bin))
}

func systemdQuote(s string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`, "%", "%%").Replace(s)
}

func renderTaskXML(bin, user string) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-16"?>
<Task version="1.2" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task">
  <RegistrationInfo>
    <Description>dokky daemon</Description>
  </RegistrationInfo>
  <Triggers>
    <LogonTrigger>
      <Enabled>true</Enabled>
      <UserId>%[1]s</UserId>
    </LogonTrigger>
  </Triggers>
  <Principals>
    <Principal id="Author">
      <UserId>%[1]s</UserId>
      <LogonType>InteractiveToken</LogonType>
      <RunLevel>LeastPrivilege</RunLevel>
    </Principal>
  </Principals>
  <Settings>
    <MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy>
    <DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries>
    <StopIfGoingOnBatteries>false</StopIfGoingOnBatteries>
    <StartWhenAvailable>true</StartWhenAvailable>
    <AllowStartOnDemand>true</AllowStartOnDemand>
    <Enabled>true</Enabled>
    <Hidden>true</Hidden>
    <ExecutionTimeLimit>PT0S</ExecutionTimeLimit>
    <RestartOnFailure>
      <Interval>PT1M</Interval>
      <Count>3</Count>
    </RestartOnFailure>
  </Settings>
  <Actions Context="Author">
    <Exec>
      <Command>conhost.exe</Command>
      <Arguments>--headless "%[2]s" daemon</Arguments>
    </Exec>
  </Actions>
</Task>
`, xmlEscape(user), xmlEscape(bin))
}

func windowsTaskName(username string) string {
	if i := strings.LastIndexAny(username, `\/`); i >= 0 {
		username = username[i+1:]
	}
	return "dokky-" + username
}

var (
	serviceInstalled = ServiceInstalled
	serviceCurrent   = ServiceCurrent
	reloadService    = refreshService
)

// an update can ship a new service file, but launchd and systemd only read it when the job is reloaded
func refreshServiceFile(bin string) (restart bool, err error) {
	if !serviceInstalled() || serviceCurrent(bin) {
		return false, nil
	}
	exe, err := executable()
	if err != nil {
		return false, nil
	}
	a, aerr := os.Stat(exe)
	b, berr := os.Stat(bin)
	if aerr != nil || berr != nil || !os.SameFile(a, b) {
		return false, nil
	}
	return reloadService(bin)
}
