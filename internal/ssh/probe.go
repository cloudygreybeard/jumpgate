package ssh

import (
	"context"
	"log/slog"
	"os/exec"
	"strings"
)

func Probe(ctx context.Context, host, ccFile string) bool {
	args := []string{
		"-o", "ConnectTimeout=5",
		"-o", "BatchMode=yes",
		host,
		"true",
	}
	cmd := exec.CommandContext(ctx, "ssh", args...)
	if ccFile != "" {
		cmd.Env = append(cmd.Environ(), "KRB5CCNAME=FILE:"+ccFile)
	}
	err := cmd.Run()
	slog.Debug("probe", "host", host, "reachable", err == nil)
	return err == nil
}

func ProbeHostname(ctx context.Context, host, ccFile string) (string, bool) {
	args := []string{
		"-o", "ConnectTimeout=5",
		"-o", "BatchMode=yes",
		host,
		"hostname",
	}
	cmd := exec.CommandContext(ctx, "ssh", args...)
	if ccFile != "" {
		cmd.Env = append(cmd.Environ(), "KRB5CCNAME=FILE:"+ccFile)
	}
	out, err := cmd.Output()
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(string(out)), true
}
