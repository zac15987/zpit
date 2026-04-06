//go:build windows

package notify

import (
	"fmt"
	"os/exec"
)

const toastMaxRunes = 100

func sendToast(projectName, questionText string) error {
	questionText = truncateRunes(questionText, toastMaxRunes)

	script := fmt.Sprintf(`
[Windows.UI.Notifications.ToastNotificationManager, Windows.UI.Notifications, ContentType = WindowsRuntime] | Out-Null
$template = [Windows.UI.Notifications.ToastNotificationManager]::GetTemplateContent([Windows.UI.Notifications.ToastTemplateType]::ToastText02)
$textNodes = $template.GetElementsByTagName("text")
$textNodes.Item(0).AppendChild($template.CreateTextNode("%s - Agent waiting")) | Out-Null
$textNodes.Item(1).AppendChild($template.CreateTextNode("%s")) | Out-Null
$toast = [Windows.UI.Notifications.ToastNotification]::new($template)
$toast.Tag = "agent-waiting"
[Windows.UI.Notifications.ToastNotificationManager]::CreateToastNotifier("Zpit").Show($toast)
`, escapePS(projectName), escapePS(questionText))

	cmd := exec.Command("powershell", "-NoProfile", "-Command", script)
	return cmd.Run()
}

// escapePS escapes characters that are special in PowerShell double-quoted strings.
func escapePS(s string) string {
	var out []byte
	for i := range len(s) {
		switch s[i] {
		case '"':
			out = append(out, '`', '"')
		case '`':
			out = append(out, '`', '`')
		case '$':
			out = append(out, '`', '$')
		default:
			out = append(out, s[i])
		}
	}
	return string(out)
}

func truncateRunes(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen]) + "..."
}
