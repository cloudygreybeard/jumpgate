package connect

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/cloudygreybeard/jumpgate/internal/config"
	internalssh "github.com/cloudygreybeard/jumpgate/internal/ssh"
)

type StatusInfo struct {
	ContextName string       `json:"context" yaml:"context"`
	IsLocal     bool         `json:"is_local,omitempty" yaml:"is_local,omitempty"`
	IsRemote    bool         `json:"is_remote,omitempty" yaml:"is_remote,omitempty"`
	Gate        GateStatus   `json:"gate,omitempty" yaml:"gate,omitempty"`
	Auth        AuthStatus   `json:"auth" yaml:"auth"`
	Remote      RemoteStatus `json:"remote,omitempty" yaml:"remote,omitempty"`
	Relay       RelayStatus  `json:"relay,omitempty" yaml:"relay,omitempty"`
	SSH         SSHStatus    `json:"ssh,omitempty" yaml:"ssh,omitempty"`
}

type GateStatus struct {
	Connected bool   `json:"connected" yaml:"connected"`
	Detail    string `json:"detail,omitempty" yaml:"detail,omitempty"`
}

type AuthStatus struct {
	Valid  bool   `json:"valid" yaml:"valid"`
	CCFile string `json:"cc_file,omitempty" yaml:"cc_file,omitempty"`
	Detail string `json:"detail,omitempty" yaml:"detail,omitempty"`
}

type RemoteStatus struct {
	Reachable bool   `json:"reachable" yaml:"reachable"`
	Detail    string `json:"detail,omitempty" yaml:"detail,omitempty"`
}

type RelayStatus struct {
	Active     bool   `json:"active" yaml:"active"`
	RemotePort int    `json:"remote_port,omitempty" yaml:"remote_port,omitempty"`
	Detail     string `json:"detail,omitempty" yaml:"detail,omitempty"`
}

type SSHStatus struct {
	Running bool `json:"running" yaml:"running"`
}

func CollectStatus(ctx context.Context, rc *config.ResolvedContext) StatusInfo {
	if rc.IsLocal() {
		return collectStatusLocal(ctx, rc)
	}
	return collectStatusRemote(ctx, rc)
}

func collectStatusLocal(ctx context.Context, rc *config.ResolvedContext) StatusInfo {
	ccFile := rc.Context.Auth.CCFile

	info := StatusInfo{ContextName: rc.Name, IsLocal: true}

	// Gate
	if rc.Derived.GateSocket != "" && socketExists(rc.Derived.GateSocket) {
		if internalssh.Check(ctx, rc.Derived.GateHost) == nil {
			info.Gate = GateStatus{Connected: true, Detail: "session active"}
		} else {
			info.Gate = GateStatus{Connected: false, Detail: "socket exists but check failed"}
		}
	} else {
		info.Gate = GateStatus{Connected: false, Detail: "no session"}
	}

	// Auth -- check only the configured ccache
	info.Auth = collectTicketStatus(ctx, ccFile)

	// Remote
	if !info.Gate.Connected {
		info.Remote = RemoteStatus{Reachable: false, Detail: "gate not connected"}
	} else if result := internalssh.Probe(ctx, rc.Derived.RemoteHost, ccFile); result.Reachable {
		info.Remote = RemoteStatus{Reachable: true}
	} else if result.Detail != "" {
		info.Remote = RemoteStatus{Reachable: false, Detail: result.Detail}
	} else {
		info.Remote = RemoteStatus{Reachable: false, Detail: "relay may be down"}
	}

	return info
}

func collectStatusRemote(ctx context.Context, rc *config.ResolvedContext) StatusInfo {
	socketPath := rc.Derived.RelaySocket
	relayHost := rc.Derived.RelayHost

	info := StatusInfo{ContextName: rc.Name, IsRemote: true}

	// Relay
	if socketExists(socketPath) && internalssh.CheckSocket(ctx, relayHost, socketPath) == nil {
		info.Relay = RelayStatus{
			Active:     true,
			RemotePort: rc.Context.Relay.RemotePort,
		}
	} else {
		info.Relay = RelayStatus{Active: false}
	}

	// Auth
	info.Auth = collectTicketStatus(ctx, "")

	// SSH service
	cmd := exec.CommandContext(ctx, "systemctl", "is-active", "--quiet", "ssh")
	info.SSH = SSHStatus{Running: cmd.Run() == nil}

	return info
}

func collectTicketStatus(ctx context.Context, ccFile string) AuthStatus {
	cmd := exec.CommandContext(ctx, "klist", "-s")
	if ccFile != "" {
		cmd.Env = append(os.Environ(), "KRB5CCNAME=FILE:"+ccFile)
	}

	status := AuthStatus{CCFile: ccFile}

	if cmd.Run() == nil {
		status.Valid = true
		listCmd := exec.CommandContext(ctx, "klist")
		if ccFile != "" {
			listCmd.Env = append(os.Environ(), "KRB5CCNAME=FILE:"+ccFile)
		}
		if out, err := listCmd.Output(); err == nil {
			status.Detail = parseKlistOutput(string(out))
		}
	} else {
		status.Detail = "no valid ticket"
		if ccFile != "" {
			status.Detail = fmt.Sprintf("no valid ticket (checked %s)", ccFile)
		}
	}
	return status
}

// parseKlistOutput extracts the principal and first ticket's expiry from klist output.
// Example klist output:
//
//	Credentials cache: FILE:/tmp/krb5cc_work
//	        Principal: alice@EXAMPLE.COM
//
//	  Issued                Expires               Principal
//	Mar 23 08:45:03 2026  Mar 23 18:45:03 2026  krbtgt/EXAMPLE.COM@EXAMPLE.COM
func parseKlistOutput(out string) string {
	lines := strings.Split(out, "\n")
	var principal string
	var expires string

	headerSeen := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		lower := strings.ToLower(trimmed)

		if strings.HasPrefix(lower, "principal:") {
			principal = strings.TrimSpace(strings.SplitN(trimmed, ":", 2)[1])
			continue
		}

		if strings.Contains(lower, "issued") && strings.Contains(lower, "expires") {
			headerSeen = true
			continue
		}

		// First non-empty line after the header is the ticket data
		if headerSeen && trimmed != "" {
			// The line has: Issued-date  Expires-date  Service-principal
			// Fields are separated by 2+ spaces
			fields := splitOnMultipleSpaces(trimmed)
			if len(fields) >= 2 {
				expires = fields[1]
			}
			break
		}
	}

	var parts []string
	if principal != "" {
		parts = append(parts, principal)
	}
	if expires != "" {
		parts = append(parts, "expires "+expires)
	}
	return strings.Join(parts, ", ")
}

func splitOnMultipleSpaces(s string) []string {
	var fields []string
	var current strings.Builder
	spaceCount := 0
	for _, r := range s {
		if r == ' ' {
			spaceCount++
			if spaceCount == 1 {
				current.WriteRune(r)
			}
		} else {
			if spaceCount >= 2 && current.Len() > 0 {
				fields = append(fields, strings.TrimSpace(current.String()))
				current.Reset()
			}
			spaceCount = 0
			current.WriteRune(r)
		}
	}
	if current.Len() > 0 {
		fields = append(fields, strings.TrimSpace(current.String()))
	}
	return fields
}

// PrintStatus outputs human-readable status.
func PrintStatus(info StatusInfo) {
	if info.IsLocal {
		fmt.Printf("=== Gate [%s] ===\n", info.ContextName)
		if info.Gate.Connected {
			fmt.Println("  Session: UP")
		} else {
			fmt.Println("  Session: DOWN")
		}
		fmt.Println()
	}

	if info.IsRemote {
		fmt.Printf("=== Relay [%s] ===\n", info.ContextName)
		if info.Relay.Active {
			fmt.Println("  Status: UP")
			fmt.Printf("  RemoteForward: %d -> localhost:22\n", info.Relay.RemotePort)
		} else {
			fmt.Println("  Status: DOWN")
		}
		fmt.Println()
	}

	fmt.Println("=== Auth ===")
	if info.Auth.Valid {
		fmt.Println("  Ticket: VALID")
	} else {
		fmt.Println("  Ticket: NONE / EXPIRED")
	}
	if info.Auth.Detail != "" {
		fmt.Printf("  %s\n", info.Auth.Detail)
	}
	fmt.Println()

	if info.IsLocal {
		fmt.Printf("=== Remote [%s] ===\n", info.ContextName)
		if info.Remote.Reachable {
			fmt.Println("  Reachable: YES")
		} else {
			detail := "NO"
			if info.Remote.Detail != "" {
				detail += " (" + info.Remote.Detail + ")"
			}
			fmt.Printf("  Reachable: %s\n", detail)
		}
	}

	if info.IsRemote {
		fmt.Println()
		fmt.Println("=== SSH service ===")
		if info.SSH.Running {
			fmt.Println("  sshd: running")
		} else {
			fmt.Println("  sshd: stopped")
		}
	}
}

// PrintStatusJSON outputs JSON status.
func PrintStatusJSON(info StatusInfo) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(info)
}
