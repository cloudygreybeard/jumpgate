package auth

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"

	"github.com/cloudygreybeard/jumpgate/internal/config"
	"github.com/cloudygreybeard/jumpgate/internal/hooks"
	internalssh "github.com/cloudygreybeard/jumpgate/internal/ssh"
)

// EnsureKerberos obtains a Kerberos ticket if auth.type is "kerberos".
// Skips if auth type is "key" or "none", or if a valid ticket already exists.
func EnsureKerberos(ctx context.Context, rc *config.ResolvedContext) error {
	authType := strings.ToLower(rc.Context.Auth.Type)
	if authType == "key" || authType == "none" || authType == "" {
		fmt.Printf("Auth: not required (type=%s)\n", rc.Context.Auth.Type)
		return nil
	}

	if hasValidTicket(ctx, rc.Context.Auth.CCFile) {
		fmt.Println("Auth: ticket valid")
		return nil
	}

	fwdSpec := rc.KDCForwardSpec()

	fmt.Println("Auth: starting KDC forward...")
	if err := internalssh.Forward(ctx, rc.Derived.GateHost, fwdSpec); err != nil {
		return fmt.Errorf("KDC port forward: %w", err)
	}

	password, err := hooks.RunRequired(ctx, rc, "get-krb-password")
	if err != nil {
		_ = internalssh.CancelForward(ctx, rc.Derived.GateHost, fwdSpec)
		return fmt.Errorf("get-krb-password: %w", err)
	}

	krb5Conf, err := generateKrb5Conf(rc.Context.Auth.Realm, rc.Context.Auth.KDCLocalPort)
	if err != nil {
		_ = internalssh.CancelForward(ctx, rc.Derived.GateHost, fwdSpec)
		return fmt.Errorf("generating krb5.conf: %w", err)
	}
	defer os.Remove(krb5Conf)

	fmt.Println("Auth: authenticating...")
	kinitErr := runKinit(ctx, rc.Context.Auth.Kinit, rc.Derived.AuthPrincipal, password, rc.Context.Auth.CCFile, krb5Conf)

	_ = internalssh.CancelForward(ctx, rc.Derived.GateHost, fwdSpec)

	if kinitErr != nil {
		return fmt.Errorf("kinit failed: %w", kinitErr)
	}

	fmt.Println("Auth: ticket acquired")
	return nil
}

// hasValidTicket checks only the configured ccache -- no system-cache fallback.
func hasValidTicket(ctx context.Context, ccFile string) bool {
	cmd := exec.CommandContext(ctx, "klist", "-s")
	if ccFile != "" {
		cmd.Env = append(os.Environ(), "KRB5CCNAME=FILE:"+ccFile)
	}
	if err := cmd.Run(); err != nil {
		slog.Info("no valid ticket", "ccache", ccFile)
		return false
	}
	return true
}

// generateKrb5Conf creates a temporary krb5.conf that directs the realm
// to the forwarded KDC on localhost. Without this, kinit uses DNS discovery
// and fails because the KDC is only reachable through the SSH port forward.
func generateKrb5Conf(realm string, kdcLocalPort int) (string, error) {
	content := fmt.Sprintf(`[libdefaults]
    default_realm = %s
    dns_lookup_realm = false
    dns_lookup_kdc = false
    udp_preference_limit = 1
    forwardable = true
    rdns = false

[realms]
    %s = {
        kdc = localhost:%d
    }
`, realm, realm, kdcLocalPort)

	f, err := os.CreateTemp("", "jumpgate-krb5-*.conf")
	if err != nil {
		return "", err
	}
	if _, err := f.WriteString(content); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", err
	}
	f.Close()
	return f.Name(), nil
}

// runKinit feeds the password to kinit via stdin pipe, replacing
// the fragile PTY-based approach. Most kinit implementations accept
// password on stdin when not connected to a terminal.
func runKinit(ctx context.Context, kinitPath, principal, password, ccFile, krb5Conf string) error {
	slog.Debug("kinit", "binary", kinitPath, "principal", principal, "krb5conf", krb5Conf)

	cmd := exec.CommandContext(ctx, kinitPath, principal)
	env := append(os.Environ(), "KRB5CCNAME=FILE:"+ccFile)
	if krb5Conf != "" {
		env = append(env, "KRB5_CONFIG="+krb5Conf)
	}
	cmd.Env = env
	cmd.Stderr = os.Stderr

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("creating stdin pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("starting kinit: %w", err)
	}

	if _, err := fmt.Fprintln(stdin, password); err != nil {
		stdin.Close()
		_ = cmd.Wait()
		return fmt.Errorf("writing password: %w", err)
	}
	stdin.Close()

	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("kinit: %w", err)
	}
	return nil
}
