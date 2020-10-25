package run

import (
	"bytes"
	"fmt"
	"io"
	"os/exec"
)

func Do(stdout io.Writer, command string) error {
	var out bytes.Buffer
	cmd := exec.Command("/bin/sh", "-c", command)
	cmd.Stdout = io.MultiWriter(stdout, &out)
	cmd.Stderr = io.MultiWriter(stdout, &out)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("Error running %q: %s\n=====Command output=====\n%s", command, err, string(out.Bytes()))
		return err
	}
	return nil
}
