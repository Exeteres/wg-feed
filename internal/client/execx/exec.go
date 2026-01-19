package execx

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

type Runner struct{}

type Result struct {
	Stdout string
	Stderr string
}

func (Runner) Run(ctx context.Context, name string, args ...string) (Result, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return Result{Stdout: stdout.String(), Stderr: stderr.String()}, fmt.Errorf("exec %s: %w (stderr=%s)", shellQuote(name, args), err, strings.TrimSpace(stderr.String()))
	}
	return Result{Stdout: stdout.String(), Stderr: stderr.String()}, nil
}

func shellQuote(name string, args []string) string {
	parts := make([]string, 0, 1+len(args))
	parts = append(parts, name)
	for _, a := range args {
		if strings.ContainsAny(a, " \t\n\"'\\") {
			parts = append(parts, fmt.Sprintf("%q", a))
		} else {
			parts = append(parts, a)
		}
	}
	return strings.Join(parts, " ")
}
