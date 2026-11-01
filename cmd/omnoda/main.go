package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/JimiZhou/Omnoda/internal/node"
	"github.com/JimiZhou/Omnoda/internal/protocol"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "version":
		fmt.Println("omnoda", protocol.Version)
	case "seed":
		seedCmd(os.Args[2:])
	case "cap":
		capCmd(os.Args[2:])
	case "chat":
		chatCmd(os.Args[2:])
	case "join":
		join(os.Args[2:])
	case "start":
		start(os.Args[2:], false)
	case "stop":
		stop()
	case "status":
		if _, err := os.Stat(filepath.Join(dir(), "seed.pid")); err == nil {
			seedStatus()
		}
		status()
	case "doctor":
		for _, l := range cfgFrom(os.Args[2:]).Doctor() {
			fmt.Println(l)
		}
	case "agents":
		for _, l := range cfgFrom(os.Args[2:]).Doctor() {
			if strings.HasPrefix(l, "harness") {
				fmt.Println(l)
			}
		}
	case "logs":
		fmt.Println("logs follow the server task stream; local daemon logs go to stderr when running in the foreground")
	case "leave":
		leave()
	case "run":
		start(os.Args[2:], true)
	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, `Omnoda node %s
Usage:
  omnoda seed [--port 8787]
  omnoda cap list|inspect|install|enable|disable|test|versions|rollback|export|import
  omnoda chat
  omnoda join --server URL --code CODE [--name NAME] [--workspace DIR] [--label k=v]
  omnoda start
  omnoda stop
  omnoda status
  omnoda doctor
  omnoda agents
  omnoda logs
  omnoda leave
  omnoda version
`, protocol.Version)
}

type fileConfig struct {
	Server      string            `json:"server"`
	DisplayName string            `json:"display_name"`
	Workspaces  []string          `json:"workspaces"`
	Labels      map[string]string `json:"labels"`
	Privileged  bool              `json:"allow_privileged"`
}

func dir() string {
	if v := os.Getenv("OMNODA_CONFIG_DIR"); v != "" {
		return v
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".omnoda")
}

func loadFile() fileConfig {
	var c fileConfig
	b, err := os.ReadFile(filepath.Join(dir(), "config.json"))
	if err == nil {
		_ = json.Unmarshal(b, &c)
	}
	return c
}

func saveFile(c fileConfig) error {
	if err := os.MkdirAll(dir(), 0o700); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(c, "", "  ")
	return os.WriteFile(filepath.Join(dir(), "config.json"), b, 0o600)
}

func cfgFrom(args []string) *node.Daemon {
	fs := flag.NewFlagSet("cfg", flag.ContinueOnError)
	server := fs.String("server", "", "")
	name := fs.String("name", "", "")
	ws := fs.String("workspace", "", "")
	_ = fs.Parse(args)
	fc := loadFile()
	if *server != "" {
		fc.Server = *server
	}
	if *name != "" {
		fc.DisplayName = *name
	}
	workspaces := fc.Workspaces
	if *ws != "" {
		workspaces = append(workspaces, *ws)
	}
	return &node.Daemon{Cfg: node.Config{
		Server: fc.Server, ConfigDir: dir(), DisplayName: fc.DisplayName,
		Workspaces: workspaces, Labels: fc.Labels, AllowPrivileged: fc.Privileged,
		Version:      protocol.Version,
		ModelBaseURL: os.Getenv("OMNODA_MODEL_BASE_URL"),
		ModelName:    os.Getenv("OMNODA_MODEL"),
		ModelAPIKey:  os.Getenv("OMNODA_MODEL_API_KEY"),
	}}
}

func join(args []string) {
	fs := flag.NewFlagSet("join", flag.ExitOnError)
	server := fs.String("server", "", "control plane base URL")
	code := fs.String("code", "", "enrollment code")
	name := fs.String("name", "", "display name")
	ws := fs.String("workspace", "", "authorized workspace (repeat by joining again is not required; comma-separated)")
	label := fs.String("label", "", "k=v labels, comma-separated")
	_ = fs.Parse(args)
	if *server == "" || *code == "" {
		fmt.Fprintln(os.Stderr, "join requires --server and --code")
		os.Exit(2)
	}
	fc := loadFile()
	fc.Server = *server
	if *name != "" {
		fc.DisplayName = *name
	}
	if *ws != "" {
		for _, p := range strings.Split(*ws, ",") {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			abs, err := filepath.Abs(p)
			if err != nil {
				abs = p
			}
			fc.Workspaces = append(fc.Workspaces, abs)
		}
	}
	if fc.Labels == nil {
		fc.Labels = map[string]string{}
	}
	if *label != "" {
		for _, kv := range strings.Split(*label, ",") {
			k, v, ok := strings.Cut(kv, "=")
			if ok {
				fc.Labels[strings.TrimSpace(k)] = strings.TrimSpace(v)
			}
		}
	}
	if err := saveFile(fc); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	d := &node.Daemon{Cfg: node.Config{
		Server: fc.Server, ConfigDir: dir(), DisplayName: fc.DisplayName,
		Workspaces: fc.Workspaces, Labels: fc.Labels, Version: protocol.Version,
		EnrollmentCode: *code,
		ModelBaseURL:   os.Getenv("OMNODA_MODEL_BASE_URL"),
		ModelName:      os.Getenv("OMNODA_MODEL"),
		ModelAPIKey:    os.Getenv("OMNODA_MODEL_API_KEY"),
	}}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- d.Run(ctx) }()
	select {
	case err := <-done:
		if err != nil && ctx.Err() == nil {
			fmt.Fprintln(os.Stderr, "join failed:", err)
			os.Exit(1)
		}
	case <-time.After(8 * time.Second):
		cancel()
	}
	if err := d.Load(); err == nil {
		fmt.Println("joined", d.ID())
	}
}

func start(args []string, foreground bool) {
	fc := loadFile()
	d := &node.Daemon{Cfg: node.Config{
		Server: fc.Server, ConfigDir: dir(), DisplayName: fc.DisplayName,
		Workspaces: fc.Workspaces, Labels: fc.Labels, AllowPrivileged: fc.Privileged,
		Version:      protocol.Version,
		ModelBaseURL: os.Getenv("OMNODA_MODEL_BASE_URL"),
		ModelName:    os.Getenv("OMNODA_MODEL"),
		ModelAPIKey:  os.Getenv("OMNODA_MODEL_API_KEY"),
	}}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	pidPath := filepath.Join(dir(), "omnoda.pid")
	_ = os.WriteFile(pidPath, []byte(fmt.Sprint(os.Getpid())), 0o644)
	defer os.Remove(pidPath)
	if err := d.Run(ctx); err != nil && ctx.Err() == nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func stop() {
	pidPath := filepath.Join(dir(), "omnoda.pid")
	b, err := os.ReadFile(pidPath)
	if err != nil {
		fmt.Println("not running")
		return
	}
	var pid int
	fmt.Sscan(string(b), &pid)
	p, err := os.FindProcess(pid)
	if err != nil {
		fmt.Println("not running")
		return
	}
	_ = p.Signal(syscall.SIGTERM)
	fmt.Println("stopped", pid)
}

func status() {
	d := cfgFrom(nil)
	if err := d.Load(); err != nil {
		fmt.Println("not configured:", err)
		return
	}
	fmt.Println("node", d.ID())
	fc := loadFile()
	fmt.Println("server", fc.Server)
	if b, err := os.ReadFile(filepath.Join(dir(), "omnoda.pid")); err == nil {
		fmt.Println("pid", strings.TrimSpace(string(b)))
	} else {
		fmt.Println("daemon not running")
	}
}

func leave() {
	stop()
	fmt.Println("local identity kept at", dir(), "(delete that directory to discard the device key)")
}
