package run

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
)

func Do(command string) error {
	var out bytes.Buffer
	cmd := exec.Command("sh", "-c", command)
	cmd.Stdout = io.MultiWriter(os.Stdout, &out)
	cmd.Stderr = io.MultiWriter(os.Stderr, &out)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("Error running %q: %s\n=====Command output=====\n%s", command, err, out.String())
	}
	return nil
}
