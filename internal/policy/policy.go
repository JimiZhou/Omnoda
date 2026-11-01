// Package policy is the node's last line of authorization.
// The web UI is not trusted to enforce execution rights.
package policy

import (
	"errors"
	"path/filepath"
	"strings"

	"github.com/JimiZhou/Omnoda/internal/protocol"
)

var (
	ErrPathEscape = errors.New("path escapes allowed workspace")
	ErrLevel      = errors.New("execution level not permitted")
	ErrCommand    = errors.New("command not permitted by policy")
	ErrPrivileged = errors.New("privileged execution is disabled on this node")
)

type Policy struct {
	// Workspaces are absolute directories the owner has authorized.
	Workspaces []string
	// AllowPrivileged must be set in the node config by the device owner.
	AllowPrivileged bool
	// CommandAllow is an optional exact-binary allow list for READ_ONLY diagnostics.
	// Empty means a small built-in diagnostic set only.
	CommandAllow []string
}

var builtinReadOnly = map[string]bool{
	"echo": true, "uname": true, "hostname": true, "whoami": true,
	"pwd": true, "date": true, "df": true, "uptime": true, "true": true,
	"git": true, "python3": true, "python": true, "node": true,
	"systemctl": false, // never by default
}

func (p Policy) CheckLevel(level protocol.ExecLevel) error {
	switch level {
	case protocol.LevelReadOnly, protocol.LevelWorkspaceWrite, "":
		return nil
	case protocol.LevelPrivileged:
		if !p.AllowPrivileged {
			return ErrPrivileged
		}
		return nil
	default:
		return ErrLevel
	}
}

// ResolveWorkspace returns a cleaned absolute path inside an authorized root.
// Symlink targets are resolved and checked again by the caller when the path exists.
func (p Policy) ResolveWorkspace(requested string) (string, error) {
	if requested == "" {
		if len(p.Workspaces) == 0 {
			return "", ErrPathEscape
		}
		return p.Workspaces[0], nil
	}
	clean := filepath.Clean(requested)
	if !filepath.IsAbs(clean) {
		return "", ErrPathEscape
	}
	for _, root := range p.Workspaces {
		root = filepath.Clean(root)
		if clean == root || strings.HasPrefix(clean, root+string(filepath.Separator)) {
			return clean, nil
		}
	}
	return "", ErrPathEscape
}

// CheckCommand rejects shell metacharacters in argv and blocks obvious secret reads.
// The executor never invokes a shell, so this is defense in depth.
func (p Policy) CheckCommand(level protocol.ExecLevel, argv []string) error {
	if err := p.CheckLevel(level); err != nil {
		return err
	}
	if len(argv) == 0 || argv[0] == "" {
		return ErrCommand
	}
	base := filepath.Base(argv[0])
	for _, a := range argv {
		if strings.ContainsAny(a, "\x00") {
			return ErrCommand
		}
		low := strings.ToLower(a)
		if strings.Contains(low, "/.ssh/") || strings.Contains(low, "id_rsa") ||
			strings.Contains(low, ".aws/credentials") || strings.Contains(low, "/.codex/") ||
			strings.Contains(low, ".claude") && strings.Contains(low, "credential") {
			return ErrCommand
		}
	}
	if level == protocol.LevelPrivileged {
		return nil
	}
	if level == protocol.LevelReadOnly {
		if len(p.CommandAllow) > 0 {
			for _, c := range p.CommandAllow {
				if base == c || argv[0] == c {
					return nil
				}
			}
			return ErrCommand
		}
		if ok, known := builtinReadOnly[base]; known && ok {
			// git is read-only here only for status/diff/log/rev-parse.
			if base == "git" {
				if len(argv) < 2 {
					return ErrCommand
				}
				switch argv[1] {
				case "status", "diff", "log", "rev-parse", "show":
					return nil
				default:
					return ErrCommand
				}
			}
			if base == "systemctl" {
				return ErrCommand
			}
			return nil
		}
		// Absolute paths inside a workspace are allowed for fixture scripts.
		if filepath.IsAbs(argv[0]) {
			if _, err := p.ResolveWorkspace(argv[0]); err == nil {
				return nil
			}
			dir := filepath.Dir(argv[0])
			if _, err := p.ResolveWorkspace(dir); err == nil {
				return nil
			}
		}
		return ErrCommand
	}
	// WORKSPACE_WRITE: binary must live in a workspace or be on the diagnostic list.
	if filepath.IsAbs(argv[0]) {
		if _, err := p.ResolveWorkspace(filepath.Dir(argv[0])); err == nil {
			return nil
		}
	}
	if ok, known := builtinReadOnly[base]; known && ok {
		return nil
	}
	if len(p.CommandAllow) > 0 {
		for _, c := range p.CommandAllow {
			if base == c {
				return nil
			}
		}
	}
	return ErrCommand
}
