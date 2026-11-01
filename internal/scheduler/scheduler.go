// Package scheduler picks a live node and explains why.
package scheduler

import (
	"fmt"
	"strings"

	"github.com/JimiZhou/Omnoda/internal/protocol"
	"github.com/JimiZhou/Omnoda/internal/storage"
)

type Candidate struct {
	Node    storage.Node
	Running int
}

type Result struct {
	NodeID string
	Reason string
}

func Place(sel protocol.NodeSelector, nodes []Candidate) (Result, error) {
	if sel.NodeID != "" {
		for _, c := range nodes {
			if c.Node.ID == sel.NodeID {
				if c.Node.Status != protocol.NodeOnline {
					return Result{}, fmt.Errorf("node %s is %s", c.Node.DisplayName, c.Node.Status)
				}
				return Result{NodeID: c.Node.ID, Reason: fmt.Sprintf("Selected %s because the plan named this node and it is online.", c.Node.DisplayName)}, nil
			}
		}
		return Result{}, fmt.Errorf("named node %s is not registered", sel.NodeID)
	}
	var matches []Candidate
	for _, c := range nodes {
		if c.Node.Status != protocol.NodeOnline {
			continue
		}
		if sel.OS != "" && !strings.EqualFold(c.Node.OS, sel.OS) {
			continue
		}
		if sel.Arch != "" && !strings.EqualFold(c.Node.Arch, sel.Arch) {
			continue
		}
		if sel.Role != "" && !strings.EqualFold(c.Node.Labels["role"], sel.Role) {
			continue
		}
		if sel.Harness != "" && !hasHarness(c.Node, sel.Harness) {
			continue
		}
		ok := true
		for k, v := range sel.Labels {
			if c.Node.Labels[k] != v {
				ok = false
				break
			}
		}
		if !ok {
			continue
		}
		matches = append(matches, c)
	}
	if len(matches) == 0 {
		return Result{}, fmt.Errorf("no online node matches selector os=%s arch=%s role=%s harness=%s", sel.OS, sel.Arch, sel.Role, sel.Harness)
	}
	best := matches[0]
	for _, m := range matches[1:] {
		if m.Running < best.Running {
			best = m
		}
	}
	why := fmt.Sprintf("Selected %s because it is online", best.Node.DisplayName)
	if sel.Harness != "" {
		why += fmt.Sprintf(", has %s", sel.Harness)
	}
	if sel.Role != "" {
		why += fmt.Sprintf(", role=%s", sel.Role)
	}
	if sel.OS != "" {
		why += fmt.Sprintf(", os=%s", best.Node.OS)
	}
	if len(best.Node.Workspaces) > 0 {
		why += ", and has an authorized workspace"
	}
	why += fmt.Sprintf(". Current running tasks: %d.", best.Running)
	return Result{NodeID: best.Node.ID, Reason: why}, nil
}

func hasHarness(n storage.Node, id string) bool {
	for _, h := range n.Harnesses {
		if h.ID == id && h.Present {
			return true
		}
	}
	// native is always available
	if id == "native" || id == "builtin" {
		return true
	}
	return false
}
