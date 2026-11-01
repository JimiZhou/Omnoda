package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/JimiZhou/Omnoda/internal/auth"
	"github.com/JimiZhou/Omnoda/internal/capabilities/executor"
	"github.com/JimiZhou/Omnoda/internal/capabilities/packaging"
	"github.com/JimiZhou/Omnoda/internal/capabilities/registry"
	"github.com/JimiZhou/Omnoda/internal/protocol"
	"github.com/JimiZhou/Omnoda/internal/seed"
)

func seedCmd(args []string) {
	fs := flag.NewFlagSet("seed", flag.ExitOnError)
	port := fs.Int("port", 8787, "local port, bound to 127.0.0.1")
	root := fs.String("root", "", "config root (default ~/.omnoda)")
	_ = fs.Parse(args)
	rt, err := seed.Bootstrap(*root)
	if err != nil {
		fmt.Fprintln(os.Stderr, "seed:", err)
		os.Exit(1)
	}
	defer rt.DB.Close()
	ensureAdmin(rt)
	fmt.Printf("Omnoda seed %s\n", protocol.Version)
	fmt.Printf("console http://127.0.0.1:%d\n", *port)
	fmt.Println("bind 127.0.0.1 only; no remote control plane")
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := rt.Serve(ctx, *port); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func ensureAdmin(rt *seed.Runtime) {
	n, err := rt.DB.UserCount()
	if err != nil || n > 0 {
		return
	}
	fmt.Println("First start. Create the local administrator. The password stays on this machine.")
	in := bufio.NewReader(os.Stdin)
	fmt.Print("username [admin]: ")
	user, _ := in.ReadString('\n')
	user = strings.TrimSpace(user)
	if user == "" {
		user = "admin"
	}
	fmt.Print("password (10+ characters): ")
	pw, _ := in.ReadString('\n')
	pw = strings.TrimSpace(pw)
	if len(pw) < 10 {
		fmt.Fprintln(os.Stderr, "password too short; set it in the web console")
		return
	}
	hash, err := auth.HashPassword(pw)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return
	}
	if _, err := rt.DB.CreateUser(user, hash); err != nil {
		fmt.Fprintln(os.Stderr, err)
	}
}

func seedStatus() {
	st := seed.ReadStatus("")
	fmt.Printf("version %s\nroot %s\n", st.Version, st.Root)
	if st.Running {
		fmt.Printf("running %s\n", st.Addr)
	} else {
		fmt.Println("stopped")
	}
	rt, err := seed.Bootstrap("")
	if err != nil {
		return
	}
	defer rt.DB.Close()
	rows, _ := rt.Reg.List()
	fmt.Printf("capabilities %d\n", len(rows))
	mc, enc, _ := rt.DB.LoadModel()
	if enc != "" && mc.BaseURL != "" {
		fmt.Println("model configured")
	} else {
		fmt.Println("model not configured; autonomous generation is unavailable")
	}
}

func capCmd(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: omnoda cap list|inspect|enable|disable|test|install|versions|rollback|export|import")
		os.Exit(2)
	}
	rt, err := seed.Bootstrap("")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer rt.DB.Close()
	switch args[0] {
	case "list":
		rows, err := rt.Reg.List()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Print(registry.Summary(rows))
	case "inspect":
		need(args, 2)
		rec, err := rt.Reg.Get(args[1], "")
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Printf("%s %s %s %s\n", rec.CapabilityID, rec.Version, rec.ActivationStatus, rec.ContentHash)
	case "enable", "disable":
		need(args, 2)
		st := registry.StatusEnabled
		if args[0] == "disable" {
			st = registry.StatusDisabled
		}
		ver := ""
		if len(args) > 2 {
			ver = args[2]
		}
		rec, err := rt.Reg.SetStatus(args[1], ver, st)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Println(rec.ActivationStatus, rec.Version)
	case "versions":
		need(args, 2)
		rows, err := rt.Reg.Versions(args[1])
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		for _, rec := range rows {
			fmt.Printf("%s %s %s\n", rec.Version, rec.ActivationStatus, rec.ContentHash[:12])
		}
	case "rollback":
		need(args, 3)
		rec, err := rt.Reg.Rollback(args[1], args[2])
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Println("enabled", rec.Version)
	case "install":
		need(args, 2)
		rec, err := rt.Reg.Install(args[1], "local", "operator", registry.VerifyNone)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Println("installed", rec.CapabilityID, rec.Version, "not enabled")
	case "test":
		need(args, 2)
		ex := executor.Executor{Reg: rt.Reg}
		res := ex.Invoke(context.Background(), executor.Request{ID: args[1], Input: mapArg(args[2:])})
		fmt.Printf("ok=%v exit=%d sandbox=%s\n%s\n", res.OK, res.ExitCode, res.Backend, res.Stdout)
		if res.Error != "" {
			fmt.Fprintln(os.Stderr, res.Error)
			os.Exit(1)
		}
	case "export":
		need(args, 3)
		rec, err := rt.Reg.Get(args[1], "")
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		if err := packaging.Export(rec, args[2]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Println("exported", args[2])
	case "import":
		need(args, 2)
		rec, err := packaging.Import(rt.Reg, args[1])
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Println("imported", rec.CapabilityID, rec.Version, "not enabled")
	default:
		fmt.Fprintln(os.Stderr, "unknown cap command")
		os.Exit(2)
	}
}

func chatCmd(args []string) {
	fmt.Println("Model chat requires a configured provider. Without one, Omnoda will not invent an answer.")
	fmt.Println("Use the local console Chat page, or omnoda cap list for capabilities that work with no model.")
	_ = args
}

func need(args []string, n int) {
	if len(args) < n {
		fmt.Fprintln(os.Stderr, "missing argument")
		os.Exit(2)
	}
}

func mapArg(args []string) map[string]any {
	out := map[string]any{}
	if len(args) > 0 {
		out["input_file"] = args[0]
	}
	return out
}
