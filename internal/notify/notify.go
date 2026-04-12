package notify

import (
	"fmt"
	"os/exec"
	"strings"
)

// hasTerminalNotifier caches the lookup for terminal-notifier.
var hasTerminalNotifier *bool

func checkTerminalNotifier() bool {
	if hasTerminalNotifier != nil {
		return *hasTerminalNotifier
	}
	_, err := exec.LookPath("terminal-notifier")
	found := err == nil
	hasTerminalNotifier = &found
	return found
}

// Send sends a macOS notification with the given title and body.
// Uses terminal-notifier if available (supports icons, click actions),
// falls back to osascript.
func Send(title, body string, opts ...Option) {
	cfg := options{}
	for _, o := range opts {
		o(&cfg)
	}

	if checkTerminalNotifier() {
		sendTerminalNotifier(title, body, cfg)
	} else {
		sendOsascript(title, body)
	}
}

func sendTerminalNotifier(title, body string, cfg options) {
	args := []string{
		"-title", "ghq",
		"-subtitle", title,
		"-message", body,
		"-group", cfg.group,
	}
	if cfg.url != "" {
		args = append(args, "-open", cfg.url)
	}
	if cfg.appIcon != "" {
		args = append(args, "-appIcon", cfg.appIcon)
	}
	cmd := exec.Command("terminal-notifier", args...)
	_ = cmd.Start()
	// Don't wait — fire and forget.
}

func sendOsascript(title, body string) {
	// Escape quotes for AppleScript.
	title = strings.ReplaceAll(title, `"`, `\"`)
	body = strings.ReplaceAll(body, `"`, `\"`)
	script := fmt.Sprintf(`display notification "%s" with title "ghq" subtitle "%s"`, body, title)
	cmd := exec.Command("osascript", "-e", script)
	_ = cmd.Start()
}

// Option configures a notification.
type Option func(*options)

type options struct {
	url     string
	group   string
	appIcon string
}

// WithURL sets a URL to open when the notification is clicked.
func WithURL(url string) Option {
	return func(o *options) { o.url = url }
}

// WithGroup sets a group ID for notification deduplication.
func WithGroup(group string) Option {
	return func(o *options) { o.group = group }
}

// WithAppIcon sets a custom icon path for terminal-notifier.
func WithAppIcon(path string) Option {
	return func(o *options) { o.appIcon = path }
}
