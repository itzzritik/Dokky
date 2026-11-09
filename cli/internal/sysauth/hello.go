package sysauth

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"unicode/utf16"
)

const reasonEnv = "DOKKY_AUTH_REASON"

// only Windows PowerShell 5.1 has the WinRT projection; Add-Type against System32\WinMetadata fails on current Windows.
// exit 2 means WinRT is not usable here
const helloPrelude = `
$ErrorActionPreference = 'Stop'
try {
  Add-Type -AssemblyName System.Runtime.WindowsRuntime
  $null = [Windows.Security.Credentials.UI.UserConsentVerifier, Windows.Security.Credentials.UI, ContentType = WindowsRuntime]
  $asTask = [System.WindowsRuntimeSystemExtensions].GetMethods() |
    Where-Object { $_.Name -eq 'AsTask' -and $_.IsGenericMethod -and $_.GetParameters().Count -eq 1 -and $_.GetParameters()[0].ParameterType.Name -eq 'IAsyncOperation` + "`" + `1' } |
    Select-Object -First 1
  if ($null -eq $asTask) { exit 2 }
} catch {
  exit 2
}
function Wait-WinRT($operation, [Type]$resultType) {
  $task = $asTask.MakeGenericMethod($resultType).Invoke($null, @($operation))
  $task.Wait(-1) | Out-Null
  return $task.Result
}
$verifier = [Windows.Security.Credentials.UI.UserConsentVerifier]
`

// exit 0 available, 1 busy for now, 4 not set up on this computer
const helloCheckScript = helloPrelude + `
try {
  $availability = Wait-WinRT ($verifier::CheckAvailabilityAsync()) ([Windows.Security.Credentials.UI.UserConsentVerifierAvailability])
} catch {
  exit 1
}
switch ($availability.ToString()) {
  'Available' { exit 0 }
  'DeviceBusy' { exit 1 }
  'RetriesExhausted' { exit 1 }
  'Canceled' { exit 1 }
  default { exit 4 }
}
`

// the reason comes from the environment, so no text can change what runs. exit 0 verified, 2 unavailable, 3 cancelled
const helloPromptScript = helloPrelude + `
try {
  $availability = Wait-WinRT ($verifier::CheckAvailabilityAsync()) ([Windows.Security.Credentials.UI.UserConsentVerifierAvailability])
  if ($availability.ToString() -ne 'Available') { exit 2 }
  $result = Wait-WinRT ($verifier::RequestVerificationAsync([string]$env:DOKKY_AUTH_REASON)) ([Windows.Security.Credentials.UI.UserConsentVerificationResult])
} catch {
  exit 1
}
switch ($result.ToString()) {
  'Verified' { exit 0 }
  'Canceled' { exit 3 }
  'DeviceNotPresent' { exit 2 }
  'NotConfiguredForUser' { exit 2 }
  'DisabledByPolicy' { exit 2 }
  default { exit 1 }
}
`

// an absolute path: a powershell.exe found on PATH could answer "verified" for anyone
func powerShellPath(systemDir string) string {
	return filepath.Join(systemDir, "WindowsPowerShell", "v1.0", "powershell.exe")
}

func helloCommand(shell, script, reason string) command {
	c := command{
		path: shell,
		args: []string{"-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-EncodedCommand", encodeCommand(script)},
	}
	if reason != "" {
		c.env = []string{reasonEnv + "=" + reason}
		c.raise = true
	}
	return c
}

// -EncodedCommand takes base64 of UTF-16LE, which sidesteps every quoting rule of the command line
func encodeCommand(script string) string {
	units := utf16.Encode([]rune(script))
	b := make([]byte, 0, 2*len(units))
	for _, u := range units {
		b = append(b, byte(u), byte(u>>8))
	}
	return base64.StdEncoding.EncodeToString(b)
}

func helloAvailable(ctx context.Context, shell string) (string, bool) {
	res, err := run(ctx, helloCommand(shell, helloCheckScript, ""))
	if err != nil || res.code != 0 {
		return "", false
	}
	return "Windows Hello", true
}

func helloPrompt(ctx context.Context, shell, reason string) error {
	res, err := run(ctx, helloCommand(shell, helloPromptScript, reason))
	return parseHello(res, err)
}

func parseHello(res result, err error) error {
	switch {
	case err != nil && stopped(err):
		return ErrCancelled
	case errors.Is(err, os.ErrNotExist):
		return fmt.Errorf("%w: Windows PowerShell is missing", ErrUnavailable)
	case err != nil:
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	switch res.code {
	case 0:
		return nil
	case 2:
		return fmt.Errorf("%w: Windows Hello is not set up on this computer", ErrUnavailable)
	case 3:
		return ErrCancelled
	}
	return fmt.Errorf("could not confirm it is you with Windows Hello (exit %d)", res.code)
}
