package main

import (
	"os"
	"os/exec"
	"testing"
)

func TestMain_VersionFlag(t *testing.T) {
	if os.Getenv("GO_TEST_MAIN") == "1" {
		os.Args = []string{"jumpgate", "version"}
		main()
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestMain_VersionFlag")
	cmd.Env = append(os.Environ(), "GO_TEST_MAIN=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("main() exited with error: %v\noutput: %s", err, out)
	}
}

func TestMain_HelpFlag(t *testing.T) {
	if os.Getenv("GO_TEST_MAIN") == "1" {
		os.Args = []string{"jumpgate", "--help"}
		main()
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestMain_HelpFlag")
	cmd.Env = append(os.Environ(), "GO_TEST_MAIN=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("main() exited with error: %v\noutput: %s", err, out)
	}
}
