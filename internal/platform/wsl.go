// Package platform provides OS and environment detection utilities.
package platform

import (
	"os"
	"os/exec"
	"strings"
)

// IsWSL reports whether the current process is running inside WSL.
func IsWSL() bool {
	return os.Getenv("WSL_DISTRO_NAME") != ""
}

// HasWSL reports whether WSL is available on a Windows host by checking
// for wsl.exe in PATH.
func HasWSL() bool {
	_, err := exec.LookPath("wsl.exe")
	return err == nil
}

// WSLDistro returns the name of the default WSL distribution, or empty
// if WSL is not available. Runs "wsl.exe -l -q" and returns the first
// non-empty line.
func WSLDistro() string {
	out, err := exec.Command("wsl.exe", "--list", "--quiet").Output()
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		// wsl.exe output is UTF-16LE on some versions; strip NUL bytes
		line = strings.ReplaceAll(line, "\x00", "")
		if line != "" {
			return line
		}
	}
	return ""
}

// WSLHomePath returns the WSL user's home directory as seen from Windows
// (e.g. "\\wsl.localhost\Ubuntu\home\user"), or empty on failure.
// The returned path is a WSL-native path (e.g. "/home/user").
func WSLHomePath() string {
	out, err := exec.Command("wsl.exe", "-e", "sh", "-c", "echo $HOME").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// WSLHasJumpgate checks whether jumpgate is installed and in PATH inside WSL.
func WSLHasJumpgate() bool {
	err := exec.Command("wsl.exe", "-e", "sh", "-c", "command -v jumpgate >/dev/null 2>&1").Run()
	return err == nil
}

// WSLRun executes a command inside the default WSL distribution.
// Returns combined stdout and any error.
func WSLRun(command string) (string, error) {
	out, err := exec.Command("wsl.exe", "-e", "sh", "-c", command).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}
