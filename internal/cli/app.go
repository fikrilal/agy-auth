package cli

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/fikrilal/agy-auth/internal/agy"
	"github.com/fikrilal/agy-auth/internal/registry"
	"golang.org/x/term"
)

var Version = "0.1.0"

type App struct {
	In         io.Reader
	Out        io.Writer
	Err        io.Writer
	Store      *registry.Store
	Storage    *agy.Storage
	Now        func() time.Time
	TTY        bool
	OutTTY     bool
	JSON       bool
	NoColor    bool
	StdinFile  *os.File
	StdoutFile *os.File
	StderrFile *os.File
	reader     *bufio.Reader
}

func New() (*App, error) {
	store, err := registry.DefaultStore()
	if err != nil {
		return nil, err
	}
	return &App{
		In:         os.Stdin,
		Out:        os.Stdout,
		Err:        os.Stderr,
		Store:      store,
		Storage:    agy.DefaultStorage(),
		Now:        time.Now,
		TTY:        term.IsTerminal(int(os.Stdin.Fd())),
		OutTTY:     term.IsTerminal(int(os.Stdout.Fd())),
		StdinFile:  os.Stdin,
		StdoutFile: os.Stdout,
		StderrFile: os.Stderr,
	}, nil
}

func (a *App) Run(args []string) int {
	args = a.extractGlobals(args)
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
	case "version", "--version", "-v":
		return a.cmdVersion(rest)
	case "list", "ls":
		return a.cmdList(rest)
	case "add":
		return a.cmdAdd(rest)
	case "login":
		return a.cmdLogin(rest)
	case "switch", "use":
		return a.cmdSwitch(rest)
	case "remove", "rm":
		return a.cmdRemove(rest)
	case "alias":
		return a.cmdAlias(rest)
	case "status":
		return a.cmdStatus(rest)
	case "current":
		return a.cmdCurrent(rest)
	case "import":
		return a.cmdImport(rest)
	case "export":
		return a.cmdExport(rest)
	case "path", "paths":
		return a.cmdPath(rest)
	default:
		fmt.Fprintf(a.Err, "error: unknown command %q\n\n", command)
		a.printHelp()
		return 2
	}
}

func (a *App) extractGlobals(args []string) []string {
	result := make([]string, 0, len(args))
	for _, arg := range args {
		switch arg {
		case "--json":
			a.JSON = true
		case "--no-color":
			a.NoColor = true
		default:
			result = append(result, arg)
		}
	}
	return result
}

func (a *App) newFlagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(a.Err)
	fs.Usage = func() { a.printCommandHelp(name) }
	return fs
}

func (a *App) parseFlags(fs *flag.FlagSet, args []string) (bool, int) {
	if err := fs.Parse(reorderArgs(fs, args)); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return false, 0
		}
		return false, 2
	}
	return true, 0
}

// reorderArgs moves flags before positional arguments. Go's flag package stops
// parsing at the first positional, but users naturally write
// "import file.json --alias work".
func reorderArgs(fs *flag.FlagSet, args []string) []string {
	flags := make([]string, 0, len(args))
	positional := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			positional = append(positional, arg)
			positional = append(positional, args[i+1:]...)
			break
		}
		if len(arg) > 1 && arg[0] == '-' && arg != "-" {
			flags = append(flags, arg)
			name := strings.TrimLeft(arg, "-")
			hasValue := false
			if eq := strings.IndexByte(name, '='); eq >= 0 {
				name = name[:eq]
				hasValue = true
			}
			definition := fs.Lookup(name)
			if definition != nil && !hasValue && !isBoolFlag(definition) && i+1 < len(args) {
				i++
				flags = append(flags, args[i])
			}
			continue
		}
		positional = append(positional, arg)
	}
	return append(flags, positional...)
}

func isBoolFlag(definition *flag.Flag) bool {
	type boolFlag interface{ IsBoolFlag() bool }
	value, ok := definition.Value.(boolFlag)
	return ok && value.IsBoolFlag()
}

func (a *App) fail(format string, args ...any) int {
	fmt.Fprintf(a.Err, "error: "+format+"\n", args...)
	return 1
}

func (a *App) warn(format string, args ...any) {
	fmt.Fprintf(a.Err, "warning: "+format+"\n", args...)
}

func (a *App) now() time.Time {
	if a.Now != nil {
		return a.Now()
	}
	return time.Now()
}

func (a *App) promptWriter() io.Writer {
	if a.JSON {
		return a.Err
	}
	return a.Out
}

func (a *App) cmdVersion(args []string) int {
	if a.JSON {
		_ = a.emitJSON(map[string]string{"version": Version})
		return 0
	}
	fmt.Fprintf(a.Out, "agy-auth %s\n", Version)
	return 0
}
