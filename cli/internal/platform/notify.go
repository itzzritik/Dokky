package platform

import (
	"context"
	"os"
	"os/exec"
	"runtime"
	"time"
)

const notifyTimeout = 10 * time.Second

// the text arrives through the environment, so a file name can't break out of the script
const toastScript = `[void][Windows.UI.Notifications.ToastNotificationManager, Windows.UI.Notifications, ContentType = WindowsRuntime]
$xml = [Windows.UI.Notifications.ToastNotificationManager]::GetTemplateContent([Windows.UI.Notifications.ToastTemplateType]::ToastText02)
$text = $xml.GetElementsByTagName('text')
[void]$text.Item(0).AppendChild($xml.CreateTextNode($env:DOKKY_TITLE))
[void]$text.Item(1).AppendChild($xml.CreateTextNode($env:DOKKY_BODY))
$app = '{1AC14E77-02E7-4E5D-B744-2EB1AE5198B7}\WindowsPowerShell\v1.0\powershell.exe'
[Windows.UI.Notifications.ToastNotificationManager]::CreateToastNotifier($app).Show([Windows.UI.Notifications.ToastNotification]::new($xml))`

func Notify(title, body string) error {
	ctx, cancel := context.WithTimeout(context.Background(), notifyTimeout)
	defer cancel()
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.CommandContext(ctx, "osascript", "-e", "on run argv", "-e", "display notification (item 2 of argv) with title (item 1 of argv)", "-e", "end run", title, body)
	case "windows":
		cmd = exec.CommandContext(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command", toastScript)
		cmd.Env = append(os.Environ(), "DOKKY_TITLE="+title, "DOKKY_BODY="+body)
	default:
		cmd = exec.CommandContext(ctx, "notify-send", title, body)
	}
	cmd.WaitDelay = time.Second
	return cmd.Run()
}
