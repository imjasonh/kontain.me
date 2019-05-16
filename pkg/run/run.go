package run

import (
	"io"
	"os/exec"
)

func Do(stdout io.Writer, command string) error {
	cmd := exec.Command("/bin/sh", "-c", command)
	cmd.Stdout = stdout
	cmd.Stderr = stdout
	return cmd.Run()
}
