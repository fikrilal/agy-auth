package verify

import (
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/fikrilal/agy-auth/internal/harness/config"
	"github.com/fikrilal/agy-auth/internal/harness/contracts"
	"github.com/fikrilal/agy-auth/internal/harness/docs"
	"github.com/fikrilal/agy-auth/internal/harness/gitcheck"
	"github.com/fikrilal/agy-auth/internal/harness/lint"
	"github.com/fikrilal/agy-auth/internal/harness/repo"
	"github.com/fikrilal/agy-auth/internal/harness/runner"
	"github.com/fikrilal/agy-auth/internal/harness/secret"
)

type Env struct {
	Root    string
	Out     io.Writer
	Err     io.Writer
	Runner  *runner.Runner
	GoFiles []string
	Tracked []string
}

type Step struct {
	ID    string
	Title string
	Run   func(*Env) error
}

type Profile struct {
	ID          string
	Description string
	Steps       []Step
}

type ProfileInfo struct {
	ID          string
	Description string
}

func NewEnv(root string, out, errOut io.Writer, processRunner *runner.Runner) (*Env, error) {
	goFiles, err := repo.GoFiles(root)
	if err != nil {
		return nil, err
	}
	tracked, err := repo.TrackedFiles(root)
	if err != nil {
		return nil, err
	}
	return &Env{
		Root:    root,
		Out:     out,
		Err:     errOut,
		Runner:  processRunner,
		GoFiles: goFiles,
		Tracked: tracked,
	}, nil
}

func fastSteps() []Step {
	return []Step{
		{ID: "format", Title: "Format", Run: stepFormat},
		{ID: "lint", Title: "Custom lints", Run: stepLint},
		{ID: "secrets", Title: "Secret scan", Run: stepSecrets},
		{ID: "docs", Title: "Docs and version", Run: stepDocs},
		{ID: "contracts", Title: "Contract fixtures", Run: stepContracts},
		{ID: "vet", Title: "go vet", Run: stepVet},
		{ID: "build", Title: "go build", Run: stepBuild},
		{ID: "test-short", Title: "Unit tests", Run: stepTestShort},
	}
}

func fullSteps() []Step {
	return append(fastSteps(),
		Step{ID: "test-race", Title: "Race tests", Run: stepTestRace},
		Step{ID: "npm-current", Title: "Build current npm target", Run: stepNPMCurrent},
		Step{ID: "npm-shim", Title: "Node shim smoke test", Run: stepNPMShim},
		Step{ID: "npm-pack-dry", Title: "npm pack dry run", Run: stepNPMPackDry},
	)
}

func ciSteps() []Step {
	return append(fastSteps(),
		Step{ID: "test-ci", Title: "Fresh shuffled race tests", Run: stepTestCI},
		Step{ID: "npm-current", Title: "Build current npm target", Run: stepNPMCurrent},
		Step{ID: "npm-shim", Title: "Node shim smoke test", Run: stepNPMShim},
		Step{ID: "npm-all", Title: "Build all npm targets", Run: stepNPMAll},
		Step{ID: "npm-pack", Title: "npm pack", Run: stepNPMPack},
	)
}

var Profiles = map[string]Profile{
	"fast": {
		ID:          "fast",
		Description: "Static checks, contracts, build, and short tests",
		Steps:       fastSteps(),
	},
	"full": {
		ID:          "full",
		Description: "Fast profile plus race tests and current-target packaging",
		Steps:       fullSteps(),
	},
	"ci": {
		ID:          "ci",
		Description: "Fast plus fresh shuffled race tests and all-target packaging",
		Steps:       ciSteps(),
	},
}

func List() []ProfileInfo {
	result := make([]ProfileInfo, 0, len(Profiles))
	for _, profile := range Profiles {
		result = append(result, ProfileInfo{ID: profile.ID, Description: profile.Description})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result
}

func Run(profileID string, env *Env) error {
	profile, ok := Profiles[profileID]
	if !ok {
		return fmt.Errorf("unknown profile %q", profileID)
	}
	fmt.Fprintf(env.Out, "harness verify: %s — %s\n", profile.ID, profile.Description)
	for _, step := range profile.Steps {
		fmt.Fprintf(env.Out, "\n==> %s\n", step.Title)
		started := time.Now()
		if err := step.Run(env); err != nil {
			return fmt.Errorf("step %s (%s): %w", step.ID, step.Title, err)
		}
		fmt.Fprintf(env.Out, "==> %s ok (%s)\n", step.Title, time.Since(started).Round(time.Millisecond))
	}
	fmt.Fprintf(env.Out, "\nharness verify: %s passed\n", profile.ID)
	return nil
}

func Lint(env *Env) error {
	findings, err := lint.Check(env.Root, env.GoFiles)
	if err != nil {
		return err
	}
	for _, finding := range findings {
		fmt.Fprintf(env.Out, "%s:%d:%d: %s: %s\n", finding.File, finding.Line, finding.Column, finding.Rule, finding.Message)
	}
	bots, err := gitcheck.CheckBotTrailers(env.Root)
	if err != nil {
		return err
	}
	for _, finding := range bots {
		fmt.Fprintf(env.Out, "%s: bot-coauthor: %s\n", finding.Commit, finding.Reason)
	}
	if len(findings)+len(bots) > 0 {
		return fmt.Errorf("%d lint finding(s)", len(findings)+len(bots))
	}
	return nil
}

func Secrets(env *Env) error {
	findings, err := secret.Scan(env.Root, env.Tracked)
	if err != nil {
		return err
	}
	for _, finding := range findings {
		fmt.Fprintf(env.Out, "%s:%d:%d: %s: %s\n", finding.File, finding.Line, finding.Column, finding.Rule, finding.Message)
	}
	if len(findings) > 0 {
		return fmt.Errorf("%d secret finding(s)", len(findings))
	}
	return nil
}

func Docs(env *Env) error {
	findings, err := docs.Check(env.Root)
	if err != nil {
		return err
	}
	for _, finding := range findings {
		fmt.Fprintln(env.Out, docs.Format(finding))
	}
	var errs []error
	if len(findings) > 0 {
		errs = append(errs, fmt.Errorf("%d documentation finding(s)", len(findings)))
	}
	if versionErr := config.Check(env.Root); versionErr != nil {
		fmt.Fprintf(env.Err, "version-sync: %v\n", versionErr)
		errs = append(errs, versionErr)
	}
	return errors.Join(errs...)
}

func Contracts(env *Env) error {
	if err := contracts.Check(env.Root); err != nil {
		return err
	}
	fmt.Fprintln(env.Out, "contracts: ok")
	return nil
}

func stepFormat(env *Env) error {
	if len(env.GoFiles) == 0 {
		return nil
	}
	result := env.Runner.Capture("gofmt", append([]string{"-l"}, env.GoFiles...)...)
	if result.Err != nil || result.ExitCode != 0 {
		return fmt.Errorf("gofmt failed: %s", strings.TrimSpace(result.Output))
	}
	if trimmed := strings.TrimSpace(result.Output); trimmed != "" {
		return fmt.Errorf("unformatted files:\n%s", trimmed)
	}
	return nil
}

func stepLint(env *Env) error      { return Lint(env) }
func stepSecrets(env *Env) error   { return Secrets(env) }
func stepDocs(env *Env) error      { return Docs(env) }
func stepContracts(env *Env) error { return Contracts(env) }

func stepVet(env *Env) error   { return env.Runner.Run("go", "vet", "./...") }
func stepBuild(env *Env) error { return env.Runner.Run("go", "build", "./...") }

func stepTestShort(env *Env) error { return env.Runner.Run("go", "test", "-short", "./...") }
func stepTestRace(env *Env) error  { return env.Runner.Run("go", "test", "-race", "./...") }
func stepTestCI(env *Env) error {
	return env.Runner.Run("go", "test", "-race", "-count=1", "-shuffle=on", "./...")
}

func stepNPMCurrent(env *Env) error {
	return env.Runner.Run("node", "scripts/build-npm.mjs", "--current")
}
func stepNPMAll(env *Env) error  { return env.Runner.Run("node", "scripts/build-npm.mjs", "--all") }
func stepNPMShim(env *Env) error { return env.Runner.Run("node", "npm/bin/agy-auth.js", "version") }
func stepNPMPackDry(env *Env) error {
	return env.Runner.Run("npm", "pack", "--dry-run", "--ignore-scripts")
}

func stepNPMPack(env *Env) error {
	dir, err := os.MkdirTemp("", "agy-auth-pack-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	if err := env.Runner.Run("npm", "pack", "--ignore-scripts", "--pack-destination", dir); err != nil {
		return err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".tgz") {
			fmt.Fprintf(env.Out, "package tarball: %s\n", entry.Name())
			return nil
		}
	}
	return errors.New("npm pack produced no tarball")
}
