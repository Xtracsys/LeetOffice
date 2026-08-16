// Command leetd is the LeetOffice daemon and CLI: an always-on, headless
// node owning the local store, git sync, MCP server, memory, RAG, and
// registry (RUNBOOK §3). Subcommands cover node lifecycle and one-shot
// operations; `leetd serve` runs the daemon loops.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"leetoffice/internal/config"
	"leetoffice/internal/daemon"
	"leetoffice/internal/memory"
	leetNet "leetoffice/internal/net"
	"leetoffice/internal/registry"
	"leetoffice/internal/store"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "check" {
		if err := runCheck(); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
		fmt.Println("store: OK")
		return
	}
	if len(os.Args) < 2 {
		// no arguments = run the node. First run opens the setup wizard;
		// after that it's the always-on daemon.
		if err := cmdServe(nil); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
		return
	}
	var err error
	switch os.Args[1] {
	case "init":
		err = cmdInit(os.Args[2:])
	case "serve":
		err = cmdServe(os.Args[2:])
	case "sync":
		err = cmdSync(os.Args[2:])
	case "mcp":
		err = cmdMCP(os.Args[2:])
	case "enroll":
		err = cmdEnroll(os.Args[2:])
	case "doc", "task":
		err = cmdDoc(os.Args[2:])
	case "audit":
		err = cmdAudit(os.Args[2:])
	case "memory":
		err = cmdMemory(os.Args[2:])
	case "digest":
		err = cmdDigest(os.Args[2:])
	case "hygiene":
		err = cmdHygiene(os.Args[2:])
	case "registry":
		err = cmdRegistry(os.Args[2:])
	case "install":
		err = cmdInstall(os.Args[2:])
	case "uninstall":
		err = cmdUninstall(os.Args[2:])
	case "mcp-install":
		err = cmdMCPInstall(os.Args[2:])
	case "version":
		cmdVersion()
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `leetd — LeetOffice node daemon

usage:
  leetd init [--store DIR] [--actor ID] [--coordinator] [--share URL]   create a node
  leetd serve [--config FILE]                                          run the daemon
  leetd sync   [--config FILE]                                         one sync cycle
  leetd mcp    [--config FILE] [--actor ID]                            MCP over stdio (agent clients)
  leetd enroll [--config FILE] --coordinator HOST:PORT --secret S      join a team (D8)
  leetd doc    add|edit|show|list ...                                  human store ops
  leetd audit  [--config FILE] [--doc SLUG] [--actor ID]               what changed, by whom
  leetd memory [--config FILE]                                         synthesize MEMORY.md now
  leetd digest [--config FILE]                                         write today's digest
  leetd hygiene [--config FILE]                                        run doc hygiene
  leetd registry list|use <name> [--ok|--fail]                         skills & tools registry
  leetd install [--config FILE]                                         register as always-on login service
  leetd uninstall                                                       remove the login service
  leetd mcp-install [--client claude] [--write]                         print/write MCP client config
  leetd version                                                        build + platform info
  leetd check                                                          store self-test

  running leetd with no arguments starts the node (first run opens the setup wizard).
`)
}

func loadConfig(args []string) (*config.Config, string) {
	fs := flag.NewFlagSet("config", flag.ContinueOnError)
	cfgPath := fs.String("config", config.DefaultPath(), "config file")
	_ = fs.Parse(args)
	cfg, err := config.Load(*cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "leetd: cannot load config %s (%v) — run `leetd init` first\n", *cfgPath, err)
		os.Exit(1)
	}
	return cfg, *cfgPath
}

func cmdInit(args []string) error {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	storeDir := fs.String("store", defaultStoreDir(), "store directory")
	actor := fs.String("actor", "", "actor id, e.g. human:josh")
	coordinator := fs.Bool("coordinator", false, "make this node the coordinator")
	share := fs.String("share", "", "main share URL (default: sibling main-share.git when coordinator)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *actor == "" {
		host, _ := os.Hostname()
		*actor = "human:" + host
	}
	cfg := config.Default(*storeDir, *actor)
	cfgPath := config.DefaultPath()

	if *coordinator {
		cfg.Role = "coordinator"
		if *share == "" {
			*share = "file://" + filepath.Join(filepath.Dir(*storeDir), "main-share.git")
		}
		cfg.MainShare = *share
		if _, err := daemon.InitBareShare(cfg); err != nil {
			return err
		}
		cfg.EnrollmentSecret = store.NewID()[:12] // printed once below (D8)
		fmt.Println("enrollment secret (share out of band, one-time pairing):", cfg.EnrollmentSecret)
	} else if *share != "" {
		cfg.MainShare = *share
	}

	if err := cfg.Save(cfgPath); err != nil {
		return err
	}
	if _, err := daemon.Start(cfg); err != nil {
		return err
	}
	fmt.Printf("node initialized: %s\n  config: %s\n  store:  %s\n  role:   %s\n  share:  %s\n",
		cfg.NodeID, cfgPath, cfg.StoreDir, cfg.Role, or(cfg.MainShare, "(none)"))
	return nil
}

func defaultStoreDir() string {
	wd, err := os.Getwd()
	if err != nil {
		return "leetoffice-store"
	}
	return filepath.Join(wd, "leetoffice-store")
}

func or(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

func cmdServe(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	cfgPath := fs.String("config", config.DefaultPath(), "config file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if _, err := os.Stat(*cfgPath); err != nil {
		fmt.Println("no configuration yet — first-run wizard:")
	}
	fmt.Println("leetd: UI on http://127.0.0.1:7667 (Ctrl+C to stop)")
	return daemon.ListenAndServe(context.Background(), *cfgPath)
}

func cmdSync(args []string) error {
	cfg, _ := loadConfig(args)
	node, err := daemon.Start(cfg)
	if err != nil {
		return err
	}
	res, err := node.SyncOnce()
	if res != nil {
		fmt.Printf("pulled=%v pushed=%v merged=%v conflicts=%d\n",
			res.Pulled, res.Pushed, res.Merged, len(res.Conflicts))
		for _, c := range res.Conflicts {
			fmt.Printf("  conflict: %s block %s (%s)\n", c.Slug, c.BlockID, c.Resolved)
		}
	}
	return err
}

func cmdMCP(args []string) error {
	fs := flag.NewFlagSet("mcp", flag.ContinueOnError)
	cfgPath := fs.String("config", config.DefaultPath(), "config file")
	actor := fs.String("actor", "", "actor id, e.g. agent:hermes (overrides config)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := config.Load(*cfgPath)
	if err != nil {
		return err
	}
	if *actor != "" {
		cfg.Actor = *actor
	}
	node, err := daemon.Start(cfg)
	if err != nil {
		return err
	}
	return node.MCP.ServeStdio(os.Stdin, os.Stdout)
}

func cmdDoc(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: leetd doc add|edit|show|list")
	}
	fs := flag.NewFlagSet("doc", flag.ContinueOnError)
	_ = fs.Parse(args[1:])
	sub, rest := args[0], args[1:]
	cfg, _ := loadConfig(fs.Args())
	_ = rest
	node, err := daemon.Start(cfg)
	if err != nil {
		return err
	}
	switch sub {
	case "list":
		docs, err := node.Store.List()
		if err != nil {
			return err
		}
		for _, d := range docs {
			fmt.Printf("%-8s %-28s %s (v%d, %s)\n", d.Type, d.Slug, d.Title, d.Version, strings.SplitN(d.Updated, "T", 2)[0])
		}
		return nil
	case "show":
		if len(rest) < 1 {
			return fmt.Errorf("show <slug>")
		}
		d, err := node.Store.Resolve(rest[0])
		if err != nil {
			return err
		}
		raw, err := d.Bytes()
		if err != nil {
			return err
		}
		fmt.Println(string(raw))
		return nil
	default:
		return fmt.Errorf("doc %s: not implemented (use the UI, MCP, or edit docs/<slug>.html via tools)", sub)
	}
}

func cmdAudit(args []string) error {
	fs := flag.NewFlagSet("audit", flag.ContinueOnError)
	doc := fs.String("doc", "", "filter by slug")
	actor := fs.String("actor", "", "filter by actor")
	cfgPath := fs.String("config", config.DefaultPath(), "config file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := config.Load(*cfgPath)
	if err != nil {
		return err
	}
	node, err := daemon.Start(cfg)
	if err != nil {
		return err
	}
	path := ""
	if *doc != "" {
		d, err := node.Store.Resolve(*doc)
		if err != nil {
			return err
		}
		path = "docs/" + d.Slug + ".html"
		if d.Type == store.TypeTask {
			path = "tasks/" + d.Slug + ".html"
		}
	}
	entries, err := node.Repo.AuditLog(path, time.Time{}, *actor, 30)
	if err != nil {
		return err
	}
	for _, e := range entries {
		fmt.Printf("%s %s %s\n  %s %s\n", e.When.Format("2006-01-02 15:04"), e.Actor, e.Commit[:7], e.Msg, strings.Join(e.Files, " "))
	}
	return nil
}

// runCheck is the Phase 1 store self-test (write → parse → render → rewrite).
func runCheck() error {
	d := store.NewDoc(store.TypeDoc, "imaging-runbook", "XtracBox Imaging Runbook")
	d.AddParagraph("Boot the target from the Ventoy USB.")
	b, err := d.Bytes()
	if err != nil {
		return err
	}
	parsed, err := store.ParseDoc(b)
	if err != nil {
		return err
	}
	page, err := store.RenderDoc(parsed)
	if err != nil {
		return err
	}
	reparsed, err := store.ExtractDoc(page)
	if err != nil {
		return err
	}
	if reparsed.ID != d.ID || len(reparsed.Blocks) != 1 {
		return fmt.Errorf("round-trip mismatch")
	}
	return nil
}

func cmdEnroll(args []string) error {
	fs := flag.NewFlagSet("enroll", flag.ContinueOnError)
	coordinator := fs.String("coordinator", "", "coordinator enrollment host:port")
	secret := fs.String("secret", "", "one-time enrollment secret")
	cfgPath := fs.String("config", config.DefaultPath(), "config file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := config.Load(*cfgPath)
	if err != nil {
		return err
	}
	if *coordinator == "" || *secret == "" {
		return fmt.Errorf("enroll needs --coordinator host:port and --secret")
	}
	id, gitAddr, err := leetNet.Enroll(*coordinator, cfg.NodeID, *secret, "")
	if err != nil {
		return fmt.Errorf("enrollment rejected: %w", err)
	}
	if err := id.Save(cfg.IdentityDir); err != nil {
		return err
	}
	// the share URL must target the git service (the coordinator tells us
	// where it is), not the enrollment port we just used
	if gitAddr == "" {
		host, _, splitErr := net.SplitHostPort(*coordinator)
		if splitErr != nil {
			host = *coordinator
		}
		gitAddr = net.JoinHostPort(host, fmt.Sprint(leetNet.DefaultPort))
	}
	cfg.MainShare = fmt.Sprintf("%s://%s/main.git", leetNet.Scheme, gitAddr)
	if err := cfg.Save(*cfgPath); err != nil {
		return err
	}
	fmt.Printf("enrolled: node %s (cert fingerprint %s)\nmain share: %s\n",
		id.NodeID(), id.Fingerprint(), cfg.MainShare)
	return nil
}

func cmdMemory(args []string) error {
	cfg, _ := loadConfig(args)
	node, err := daemon.Start(cfg)
	if err != nil {
		return err
	}
	if err := memory.Synthesize(node.Store, node.Repo, cfg.Actor); err != nil {
		return err
	}
	fmt.Println("MEMORY.md synthesized:", filepath.Join(cfg.StoreDir, "MEMORY.md"))
	return nil
}

func cmdDigest(args []string) error {
	cfg, _ := loadConfig(args)
	node, err := daemon.Start(cfg)
	if err != nil {
		return err
	}
	path, err := memory.DailyDigest(node.Store, node.Repo, time.Now().UTC(), cfg.Actor)
	if err != nil {
		return err
	}
	fmt.Println("digest written:", path)
	return nil
}

func cmdHygiene(args []string) error {
	cfg, _ := loadConfig(args)
	node, err := daemon.Start(cfg)
	if err != nil {
		return err
	}
	issues, err := memory.Hygiene(node.Store, 0)
	if err != nil {
		return err
	}
	if len(issues) == 0 {
		fmt.Println("hygiene: all clear")
		return nil
	}
	for _, i := range issues {
		fmt.Printf("%s: %s\n", i.Kind, i.Detail)
	}
	return nil
}

func cmdRegistry(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: leetd registry list | import <src-dir> | use <name> [--ok|--fail]")
	}
	sub := args[0]
	fs := flag.NewFlagSet("registry", flag.ContinueOnError)
	cfgPath := fs.String("config", config.DefaultPath(), "config file")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	cfg, err := config.Load(*cfgPath)
	if err != nil {
		return err
	}
	node, err := daemon.Start(cfg)
	if err != nil {
		return err
	}
	rest := fs.Args()
	switch sub {
	case "list":
		entries, err := registry.Load(repoRoot(cfg))
		if err != nil {
			return err
		}
		for _, e := range entries {
			thr := e.Manifest.Threshold
			if thr == 0 {
				thr = registry.DefaultThreshold
			}
			fmt.Printf("%-8s %-24s %-8s %-12s clean_uses=%d/%d\n",
				e.Manifest.Kind, e.Manifest.Name, e.Manifest.Version, e.Manifest.Stability,
				e.Manifest.CleanUses, thr)
		}
		return nil
	case "import":
		if len(rest) < 1 {
			return fmt.Errorf("import <src-dir> (a folder with manifest.json)")
		}
		e, err := registry.Import(repoRoot(cfg), rest[0], cfg.Actor, node.Repo)
		if err != nil {
			return err
		}
		fmt.Printf("imported %s %s@%s (%s)\n", e.Manifest.Kind, e.Manifest.Name, e.Manifest.Version, e.Manifest.Stability)
		return nil
	case "use":
		if len(rest) < 1 {
			return fmt.Errorf("use <name> [--ok|--fail]")
		}
		name := rest[0]
		success := true
		for _, a := range rest[1:] {
			if a == "--fail" {
				success = false
			}
		}
		e, err := registry.RecordUse(repoRoot(cfg), name, success, cfg.Actor, node.Repo)
		if err != nil {
			return err
		}
		fmt.Printf("%s: clean_uses=%d stability=%s\n", name, e.Manifest.CleanUses, e.Manifest.Stability)
		return nil
	default:
		return fmt.Errorf("unknown registry subcommand %q", sub)
	}
}

// repoRoot is where the registry folders live: alongside the store, matching
// the repo layout (skills/, tools/ at the workspace root).
func repoRoot(cfg *config.Config) string {
	return filepath.Join(cfg.StoreDir, "..")
}

func cmdInstall(args []string) error {
	fs := flag.NewFlagSet("install", flag.ContinueOnError)
	cfgPath := fs.String("config", config.DefaultPath(), "config file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := config.Load(*cfgPath)
	if err != nil {
		return fmt.Errorf("run the first-run wizard (just `leetd`) before installing the service: %w", err)
	}
	msg, err := daemon.InstallService(cfg)
	fmt.Println(msg)
	return err
}

func cmdUninstall(args []string) error {
	msg, err := daemon.UninstallService()
	fmt.Println(msg)
	return err
}

// cmdMCPInstall prints (or writes) the MCP client configuration for AI agents.
func cmdMCPInstall(args []string) error {
	fs := flag.NewFlagSet("mcp-install", flag.ContinueOnError)
	client := fs.String("client", "", "claude (writes .mcp.json in the current directory when combined with --write)")
	write := fs.Bool("write", false, "write the config file instead of printing")
	actor := fs.String("actor", "agent:hermes", "actor id for the agent")
	cfgPath := fs.String("config", config.DefaultPath(), "config file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	bin, err := os.Executable()
	if err != nil {
		bin = "leetd"
	}
	mcpArgs := []string{"mcp", "--config", *cfgPath, "--actor", *actor}
	cfgJSON, _ := json.MarshalIndent(map[string]any{
		"mcpServers": map[string]any{
			"leetoffice": map[string]any{"command": bin, "args": mcpArgs},
		},
	}, "", "  ")

	if !*write {
		fmt.Println(string(cfgJSON))
		fmt.Println("\nClaude Code:  claude mcp add leetoffice -- " + bin + " mcp --actor " + *actor)
		fmt.Println("HTTP:         point an MCP HTTP client at http://127.0.0.1:7667/mcp")
		fmt.Println("\nadd --write to create .mcp.json here for project-scoped use")
		return nil
	}
	switch *client {
	case "claude":
		if err := os.WriteFile(".mcp.json", append(cfgJSON, '\n'), 0o644); err != nil {
			return err
		}
		fmt.Println("wrote .mcp.json in the current directory (project-scoped for Claude Code)")
		return nil
	default:
		return fmt.Errorf("--write needs --client claude (other clients: copy the printed snippet)")
	}
}

// Build info, injected at release time (-ldflags "-X main.version=…").
var (
	version = "dev"
	commit  = "none"
)

func cmdVersion() {
	fmt.Printf("leetoffice %s (%s) %s/%s\n", version, commit, runtime.GOOS, runtime.GOARCH)
	fmt.Println("https://github.com/leetoffice/leetoffice · Apache-2.0 · 100% local, no egress")
}
