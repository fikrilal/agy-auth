package doctor

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/fikrilal/agy-auth/internal/harness/runner"
	"github.com/fikrilal/agy-auth/internal/harness/verify"
)

type Status string

const (
	Pass Status = "PASS"
	Warn Status = "WARN"
	Fail Status = "FAIL"
)

type Check struct {
	ID     string
	Status Status
	Detail string
}

// Run performs read-only prerequisite and policy checks. It never reads
// account credentials, talks to the network, or writes state.
func Run(root string, out io.Writer, processRunner *runner.Runner) error {
	var checks []Check
	add := func(id string, status Status, detail string) {
		checks = append(checks, Check{ID: id, Status: status, Detail: detail})
	}

	goDetail, goOK := checkGoVersion(root, processRunner)
	add("go.version", statusFor(goOK), goDetail)

	if detail, ok := checkCommand(processRunner, "git", "--version"); ok {
		add("git.version", Pass, detail)
	} else {
		add("git.version", Fail, "git is required")
	}

	nodeStatus, nodeDetail := checkNode(processRunner)
	add("node.version", nodeStatus, nodeDetail)
	if detail, ok := checkCommand(processRunner, "npm", "--version"); ok {
		add("npm.version", Pass, detail)
	} else {
		add("npm.version", Warn, "npm is required by full/ci packaging")
	}
	if _, err := exec.LookPath("agy"); err == nil {
		add("agy.executable", Pass, "found in PATH")
	} else {
		add("agy.executable", Warn, "not found; needed for manual login, not tests")
	}

	env, err := verify.NewEnv(root, io.Discard, io.Discard, processRunner)
	if err != nil {
		add("harness.env", Fail, err.Error())
	} else {
		add("harness.env", Pass, "file discovery ready")
		addResult := func(id string, err error) {
			if err != nil {
				add(id, Fail, err.Error())
				return
			}
			add(id, Pass, "ok")
		}
		addResult("lint", verify.Lint(env))
		addResult("secrets", verify.Secrets(env))
		addResult("docs.version", verify.Docs(env))
		addResult("contracts", verify.Contracts(env))
	}

	if config, err := processRunner.Output("git", "config", "--get", "core.hooksPath"); err == nil && strings.TrimSpace(config) == ".githooks" {
		add("hooks.installed", Pass, "core.hooksPath=.githooks")
	} else {
		add("hooks.installed", Warn, "run `make hooks` to install pre-commit and commit-msg hooks")
	}

	failures := 0
	for _, check := range checks {
		fmt.Fprintf(out, "%-4s %-20s %s\n", check.Status, check.ID, check.Detail)
		if check.Status == Fail {
			failures++
		}
	}
	if failures > 0 {
		return fmt.Errorf("%d doctor check(s) failed", failures)
	}
	return nil
}

func statusFor(ok bool) Status {
	if ok {
		return Pass
	}
	return Fail
}

func checkGoVersion(root string, processRunner *runner.Runner) (string, bool) {
	output, err := processRunner.Output("go", "version")
	if err != nil {
		return "go is required", false
	}
	current := regexp.MustCompile(`go(\d+)\.(\d+)`).FindStringSubmatch(output)
	if len(current) != 3 {
		return output, true
	}
	rawMod, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return output, true
	}
	required := regexp.MustCompile(`(?m)^go (\d+)\.(\d+)`).FindStringSubmatch(string(rawMod))
	if len(required) != 3 {
		return output, true
	}
	currentMajor, _ := strconv.Atoi(current[1])
	currentMinor, _ := strconv.Atoi(current[2])
	requiredMajor, _ := strconv.Atoi(required[1])
	requiredMinor, _ := strconv.Atoi(required[2])
	if currentMajor < requiredMajor || (currentMajor == requiredMajor && currentMinor < requiredMinor) {
		return fmt.Sprintf("%s (go.mod requires %s.%s)", output, required[1], required[2]), false
	}
	return output, true
}

func checkCommand(processRunner *runner.Runner, name string, args ...string) (string, bool) {
	output, err := processRunner.Output(name, args...)
	if err != nil {
		return "", false
	}
	return output, true
}

func checkNode(processRunner *runner.Runner) (Status, string) {
	output, err := processRunner.Output("node", "--version")
	if err != nil {
		return Warn, "node is required by full/ci packaging"
	}
	match := regexp.MustCompile(`v(\d+)`).FindStringSubmatch(output)
	if len(match) != 2 {
		return Pass, output
	}
	major, _ := strconv.Atoi(match[1])
	if major < 18 {
		return Warn, fmt.Sprintf("%s (Node 18+ recommended)", output)
	}
	return Pass, output
}
