package harness

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/fikrilal/agy-auth/internal/harness/commitmsg"
	"github.com/fikrilal/agy-auth/internal/harness/config"
	"github.com/fikrilal/agy-auth/internal/harness/doctor"
	"github.com/fikrilal/agy-auth/internal/harness/gitcheck"
	"github.com/fikrilal/agy-auth/internal/harness/lint"
	"github.com/fikrilal/agy-auth/internal/harness/repo"
	"github.com/fikrilal/agy-auth/internal/harness/runner"
	"github.com/fikrilal/agy-auth/internal/harness/verify"
)

type App struct {
	In     io.Reader
	Out    io.Writer
	Err    io.Writer
	Root   string
	Runner *runner.Runner
}

func New() (*App, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	root, err := repo.FindRoot(cwd)
	if err != nil {
		return nil, err
	}
	return NewWithIO(root, os.Stdin, os.Stdout, os.Stderr), nil
}

func NewWithIO(root string, in io.Reader, out, errOut io.Writer) *App {
	return &App{
		In:     in,
		Out:    out,
		Err:    errOut,
		Root:   root,
		Runner: &runner.Runner{Dir: root, Out: out, Err: errOut},
	}
}

func (a *App) Run(args []string) int {
	if len(args) == 0 {
		a.printHelp()
		return 0
	}
	command := args[0]
	rest := args[1:]
	switch command {
	case "help", "-h", "--help":
		if len(rest) > 0 {
			a.printCommandHelp(rest[0])
		} else {
			a.printHelp()
		}
		return 0
	case "version", "--version":
		return a.cmdVersion(rest)
	case "verify":
		return a.cmdVerify(rest)
	case "lint":
		return a.cmdLint(rest)
	case "secrets":
		return a.cmdSecrets(rest)
	case "docs":
		return a.cmdDocs(rest)
	case "contracts":
		return a.cmdContracts(rest)
	case "doctor":
		return a.cmdDoctor(rest)
	case "install-hooks":
		return a.cmdInstallHooks(rest)
	case "commit-msg":
		return a.cmdCommitMsg(rest)
	case "list":
		return a.cmdList(rest)
	default:
		return a.usage("unknown command %q", command)
	}
}

func (a *App) cmdVerify(args []string) int {
	profile, err := parseProfile(args)
	if err != nil {
		return a.usage("%v", err)
	}
	env, err := verify.NewEnv(a.Root, a.Out, a.Err, a.Runner)
	if err != nil {
		return a.fail("%v", err)
	}
	if err := verify.Run(profile, env); err != nil {
		return a.fail("%v", err)
	}
	return 0
}

func (a *App) cmdLint(args []string) int {
	if len(args) > 0 {
		return a.usage("lint takes no arguments")
	}
	env, err := verify.NewEnv(a.Root, a.Out, a.Err, a.Runner)
	if err != nil {
		return a.fail("%v", err)
	}
	if err := verify.Lint(env); err != nil {
		return a.fail("%v", err)
	}
	return 0
}

func (a *App) cmdSecrets(args []string) int {
	if len(args) > 0 {
		return a.usage("secrets takes no arguments")
	}
	env, err := verify.NewEnv(a.Root, a.Out, a.Err, a.Runner)
	if err != nil {
		return a.fail("%v", err)
	}
	if err := verify.Secrets(env); err != nil {
		return a.fail("%v", err)
	}
	return 0
}

func (a *App) cmdDocs(args []string) int {
	if len(args) > 0 {
		return a.usage("docs takes no arguments")
	}
	env, err := verify.NewEnv(a.Root, a.Out, a.Err, a.Runner)
	if err != nil {
		return a.fail("%v", err)
	}
	if err := verify.Docs(env); err != nil {
		return a.fail("%v", err)
	}
	return 0
}

func (a *App) cmdContracts(args []string) int {
	if len(args) > 0 {
		return a.usage("contracts takes no arguments")
	}
	env, err := verify.NewEnv(a.Root, a.Out, a.Err, a.Runner)
	if err != nil {
		return a.fail("%v", err)
	}
	if err := verify.Contracts(env); err != nil {
		return a.fail("%v", err)
	}
	return 0
}

func (a *App) cmdDoctor(args []string) int {
	if len(args) > 0 {
		return a.usage("doctor takes no arguments")
	}
	if err := doctor.Run(a.Root, a.Out, a.Runner); err != nil {
		return a.fail("%v", err)
	}
	return 0
}

func (a *App) cmdInstallHooks(args []string) int {
	if len(args) > 0 {
		return a.usage("install-hooks takes no arguments")
	}
	if err := gitcheck.InstallHooks(a.Root); err != nil {
		return a.fail("%v", err)
	}
	fmt.Fprintln(a.Out, "hooks installed: core.hooksPath=.githooks")
	return 0
}

func (a *App) cmdCommitMsg(args []string) int {
	if len(args) != 1 {
		return a.usage("commit-msg requires a message file")
	}
	raw, err := os.ReadFile(args[0])
	if err != nil {
		return a.fail("%v", err)
	}
	findings := commitmsg.Validate(string(raw))
	for _, finding := range findings {
		fmt.Fprintf(a.Err, "commit-msg: %s\n", finding)
	}
	if len(findings) > 0 {
		return 1
	}
	return 0
}

func (a *App) cmdList(args []string) int {
	if len(args) > 0 {
		return a.usage("list takes no arguments")
	}
	fmt.Fprintln(a.Out, "Profiles:")
	for _, profile := range verify.List() {
		fmt.Fprintf(a.Out, "  %-6s %s\n", profile.ID, profile.Description)
	}
	fmt.Fprintln(a.Out, "\nLint rules:")
	for _, rule := range lint.Rules {
		fmt.Fprintf(a.Out, "  %-20s %s\n", rule.ID, rule.Description)
	}
	fmt.Fprintln(a.Out, "\nContract fixtures:")
	entries, err := os.ReadDir(filepath.Join(a.Root, "harness", "fixtures"))
	if err != nil {
		fmt.Fprintf(a.Err, "warning: %v\n", err)
		return 0
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	for _, name := range names {
		fmt.Fprintf(a.Out, "  %s\n", name)
	}
	return 0
}

func (a *App) cmdVersion(args []string) int {
	if len(args) > 0 {
		return a.usage("version takes no arguments")
	}
	version, err := config.PackageVersion(a.Root)
	if err != nil {
		return a.fail("%v", err)
	}
	fmt.Fprintf(a.Out, "harness %s\n", version)
	return 0
}

func parseProfile(args []string) (string, error) {
	profile := "fast"
	for index := 0; index < len(args); index++ {
		arg := args[index]
		switch {
		case arg == "--profile":
			if index+1 >= len(args) {
				return "", fmt.Errorf("--profile requires a value")
			}
			index++
			profile = args[index]
		case strings.HasPrefix(arg, "--profile="):
			profile = strings.TrimPrefix(arg, "--profile=")
		case strings.HasPrefix(arg, "--skip"):
			return "", fmt.Errorf("bypass flags are not supported")
		case strings.HasPrefix(arg, "-"):
			return "", fmt.Errorf("unknown flag %q", arg)
		default:
			return "", fmt.Errorf("unexpected argument %q", arg)
		}
	}
	switch profile {
	case "fast", "full", "ci":
		return profile, nil
	default:
		return "", fmt.Errorf("unknown profile %q; use fast, full, or ci", profile)
	}
}

func (a *App) fail(format string, args ...any) int {
	fmt.Fprintf(a.Err, "error: "+format+"\n", args...)
	return 1
}

func (a *App) usage(format string, args ...any) int {
	fmt.Fprintf(a.Err, "error: "+format+"\n", args...)
	fmt.Fprintln(a.Err, "hint: run `harness help` for usage")
	return 2
}

func (a *App) printHelp() {
	fmt.Fprint(a.Out, `harness - repository guardrails for agy-auth

Usage:
  harness <command> [options]

Commands:
  verify --profile fast|full|ci   Run a canonical verification profile
  lint                            Run custom Go AST lints and commit checks
  secrets                         Scan repository text for credential patterns
  docs                            Check relative links and version sync
  contracts                       Validate credential/registry fixtures
  doctor                          Inspect prerequisites and policy readiness
  install-hooks                   Enable .githooks for this checkout
  commit-msg <file>               Validate a commit message (used by the hook)
  list                            Show profiles, rules, and fixtures
  version                         Print the harness version
  help [command]                  Show help

Profiles:
  fast   static checks, contracts, build, short tests
  full   fast + race tests, current npm target, pack dry run
  ci     fast + fresh shuffled race tests, all npm targets, pack

No --skip-* flags are supported.
`)
}

func (a *App) printCommandHelp(command string) {
	help, ok := commandHelp[command]
	if !ok {
		a.printHelp()
		return
	}
	fmt.Fprint(a.Out, help)
}

var commandHelp = map[string]string{
	"verify": `Usage: harness verify [--profile fast|full|ci]

Runs the canonical verification profile. The default is fast.
There are no skip flags.`,
	"lint": `Usage: harness lint

Runs custom Go AST lints and scans commit history for bot co-author trailers.`,
	"secrets": `Usage: harness secrets

Scans non-ignored text files for high-confidence credential patterns.
Only rule, file, and position are printed; matched values are never shown.`,
	"docs": `Usage: harness docs

Checks relative Markdown links and keeps package.json, the Go CLI version,
and CHANGELOG.md in sync.`,
	"contracts": `Usage: harness contracts

Validates the credential and registry fixtures under harness/fixtures.`,
	"doctor": `Usage: harness doctor

Read-only prerequisite and policy inspection. Never reads account credentials.`,
	"install-hooks": `Usage: harness install-hooks

Sets core.hooksPath=.githooks and makes the pre-commit and commit-msg hooks executable.`,
	"commit-msg": `Usage: harness commit-msg <file>

Validates Conventional Commits and rejects bot co-author trailers. Used by the hook.`,
	"list": `Usage: harness list

Prints profiles, lint rules, and contract fixtures.`,
	"version": `Usage: harness version

Prints the harness version.`,
}
