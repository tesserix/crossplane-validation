package git

import (
	"os/exec"
	"strings"
)

func CurrentBranch() (string, error) {
	out, err := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func HasRef(ref string) bool {
	err := exec.Command("git", "rev-parse", "--verify", ref).Run()
	return err == nil
}

func DefaultBranch() string {
	out, err := exec.Command("git", "symbolic-ref", "refs/remotes/origin/HEAD", "--short").Output()
	if err != nil {
		return "main"
	}
	parts := strings.Split(strings.TrimSpace(string(out)), "/")
	return parts[len(parts)-1]
}

func IsDirty() bool {
	out, err := exec.Command("git", "status", "--porcelain").Output()
	if err != nil {
		return false
	}
	return len(strings.TrimSpace(string(out))) > 0
}
