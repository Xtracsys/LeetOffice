// Always-on installation (D14): register leetd as a user service so the node
// survives reboots and never needs a terminal. macOS uses a launchd agent,
// Linux a systemd user unit. Exposed as `leetd install` / `leetd uninstall`
// and as one-click buttons in the UI (/service/install).
package daemon

import (
	"fmt"
	"html"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"text/template"

	"leetoffice/internal/config"
)

const serviceLabel = "dev.leetoffice.leetd"

// InstallService registers this binary to run at login and starts it now.
// Returns a human-readable result line for the UI/CLI.
func InstallService(cfg *config.Config) (string, error) {
	bin, err := os.Executable()
	if err != nil {
		return "", err
	}
	switch runtime.GOOS {
	case "darwin":
		return installLaunchd(bin, cfg)
	case "linux":
		return installSystemd(bin, cfg)
	default:
		return "", fmt.Errorf("automatic service install is not supported on %s — run `leetd serve` at login instead", runtime.GOOS)
	}
}

// UninstallService removes the registration.
func UninstallService() (string, error) {
	switch runtime.GOOS {
	case "darwin":
		return uninstallLaunchd()
	case "linux":
		return uninstallSystemd()
	default:
		return "", fmt.Errorf("unsupported on %s", runtime.GOOS)
	}
}

func homeRel(parts ...string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(append([]string{home}, parts...)...), nil
}

// --- launchd (macOS) ---------------------------------------------------------

func plistContent(label, bin, cfgPath string) (string, error) {
	const tpl = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key><string>{{.Label}}</string>
	<key>ProgramArguments</key>
	<array>
		<string>{{.Bin}}</string>
		<string>serve</string>
		<string>--config</string>
		<string>{{.Cfg}}</string>
	</array>
	<key>RunAtLoad</key><true/>
	<key>KeepAlive</key><true/>
	<key>ProcessType</key><string>Interactive</string>
	<key>StandardOutPath</key><string>{{.Home}}/Library/Logs/leetoffice.log</string>
	<key>StandardErrorPath</key><string>{{.Home}}/Library/Logs/leetoffice.log</string>
</dict>
</plist>
`
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	t, err := template.New("plist").Parse(tpl)
	if err != nil {
		return "", err
	}
	var out builder
	if err := t.Execute(&out, struct{ Label, Bin, Cfg, Home string }{label, bin, cfgPath, home}); err != nil {
		return "", err
	}
	return out.String(), nil
}

func installLaunchd(bin string, cfg *config.Config) (string, error) {
	plistPath, err := homeRel("Library", "LaunchAgents", serviceLabel+".plist")
	if err != nil {
		return "", err
	}
	content, err := plistContent(serviceLabel, bin, cfgPathFor(cfg))
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(plistPath), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(plistPath, []byte(content), 0o644); err != nil {
		return "", err
	}
	_ = exec.Command("launchctl", "unload", plistPath).Run() // reload if present
	if out, err := exec.Command("launchctl", "load", plistPath).CombinedOutput(); err != nil {
		return fmt.Sprintf("Registered at %s but launchctl failed: %v\n%s", plistPath, err, out), nil
	}
	return "Installed as a login service (launchd). LeetOffice now starts automatically and keeps running.", nil
}

func uninstallLaunchd() (string, error) {
	plistPath, err := homeRel("Library", "LaunchAgents", serviceLabel+".plist")
	if err != nil {
		return "", err
	}
	_ = exec.Command("launchctl", "unload", plistPath).Run()
	if err := os.Remove(plistPath); err != nil {
		if os.IsNotExist(err) {
			return "No installed service found.", nil
		}
		return "", err
	}
	return "Service removed — LeetOffice will not start at login.", nil
}

// --- systemd (Linux) ----------------------------------------------------------

func unitContent(label, bin, cfgPath string) string {
	return fmt.Sprintf(`[Unit]
Description=LeetOffice node daemon
After=network-online.target

[Service]
ExecStart=%s serve --config %s
Restart=on-failure

[Install]
WantedBy=default.target
`, bin, cfgPath)
}

func installSystemd(bin string, cfg *config.Config) (string, error) {
	unitPath, err := homeRel(".config", "systemd", "user", "leetoffice.service")
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(unitPath), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(unitPath, []byte(unitContent(serviceLabel, bin, cfgPathFor(cfg))), 0o644); err != nil {
		return "", err
	}
	_ = exec.Command("systemctl", "--user", "daemon-reload").Run()
	if out, err := exec.Command("systemctl", "--user", "enable", "--now", "leetoffice").CombinedOutput(); err != nil {
		return fmt.Sprintf("Unit written to %s but systemctl failed: %v\n%s", unitPath, err, out), nil
	}
	return "Installed as a systemd user service. LeetOffice now starts automatically and keeps running.", nil
}

func uninstallSystemd() (string, error) {
	_ = exec.Command("systemctl", "--user", "disable", "--now", "leetoffice").Run()
	unitPath, err := homeRel(".config", "systemd", "user", "leetoffice.service")
	if err != nil {
		return "", err
	}
	if err := os.Remove(unitPath); err != nil && !os.IsNotExist(err) {
		return "", err
	}
	return "Service removed — LeetOffice will not start at login.", nil
}

// cfgPathFor finds the config path for service args: the one this node was
// loaded from when available, otherwise the default location.
func cfgPathFor(cfg *config.Config) string {
	if cfg != nil && cfg.Path != "" {
		return cfg.Path
	}
	return config.DefaultPath()
}

type builder struct{ b []byte }

func (b *builder) Write(p []byte) (int, error) { b.b = append(b.b, p...); return len(p), nil }
func (b *builder) String() string              { return string(b.b) }

// --- HTTP endpoints (one-click from the UI) -----------------------------------

func handleServiceInstall(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		msg, err := InstallService(cfg)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, "<p>%s</p>", html.EscapeString(msg))
	}
}

func handleServiceUninstall() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		msg, err := UninstallService()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, "<p>%s</p>", html.EscapeString(msg))
	}
}
