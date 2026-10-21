package cli

import "fmt"

func (a *App) printHelp() {
	fmt.Fprintln(a.Out, "agy-auth - switch between Antigravity CLI (agy) accounts")
	fmt.Fprintln(a.Out)
	fmt.Fprintln(a.Out, "Usage:")
	fmt.Fprintln(a.Out, "  agy-auth <command> [options]")

	width := 20
	for _, spec := range commandSpecs {
		if len(spec.Synopsis) > width {
			width = len(spec.Synopsis)
		}
	}
	group := ""
	for _, spec := range commandSpecs {
		if spec.Group != group {
			fmt.Fprintf(a.Out, "\n%s:\n", spec.Group)
			group = spec.Group
		}
		fmt.Fprintf(a.Out, "  %-*s  %s\n", width, spec.Synopsis, spec.Summary)
	}

	fmt.Fprintln(a.Out)
	fmt.Fprintln(a.Out, "Global options:")
	fmt.Fprintf(a.Out, "  %-*s  %s\n", 12, "--json", "Machine-readable output")
	fmt.Fprintf(a.Out, "  %-*s  %s\n", 12, "--no-color", "Disable ANSI colors")

	fmt.Fprintln(a.Out)
	fmt.Fprintln(a.Out, "Examples:")
	for _, example := range []string{
		"agy-auth add",
		"agy-auth list",
		"agy-auth switch personal",
		"agy-auth login --alias work",
		"agy-auth switch 2",
	} {
		fmt.Fprintf(a.Out, "  %s\n", example)
	}

	fmt.Fprintln(a.Out)
	fmt.Fprint(a.Out, helpFooter)
}

func (a *App) printCommandHelp(command string) {
	spec := specForName(command)
	if spec == nil {
		a.printHelp()
		return
	}
	fmt.Fprintln(a.Out, "Usage:")
	for _, line := range spec.Usage {
		fmt.Fprintf(a.Out, "  %s\n", line)
	}
	if spec.Description != "" {
		fmt.Fprintln(a.Out)
		fmt.Fprintln(a.Out, spec.Description)
	}
	if len(spec.Options) > 0 {
		fmt.Fprintln(a.Out)
		fmt.Fprintln(a.Out, "Options:")
		width := optionWidth(spec.Options)
		for _, option := range spec.Options {
			fmt.Fprintf(a.Out, "  %-*s  %s\n", width, option.Flag, option.Description)
		}
	}
	if len(spec.Examples) > 0 {
		fmt.Fprintln(a.Out)
		fmt.Fprintln(a.Out, "Examples:")
		for _, example := range spec.Examples {
			fmt.Fprintf(a.Out, "  %s\n", example)
		}
	}
	if len(spec.Notes) > 0 {
		fmt.Fprintln(a.Out)
		fmt.Fprintln(a.Out, "Notes:")
		for _, note := range spec.Notes {
			fmt.Fprintf(a.Out, "  %s\n", note)
		}
	}
}

func (a *App) usage(spec *CommandSpec, format string, args ...any) int {
	if spec == nil {
		return a.usageTop(format, args...)
	}
	fmt.Fprintf(a.Err, "error: "+format+"\n\n", args...)
	fmt.Fprintln(a.Err, "Usage:")
	for _, line := range spec.Usage {
		fmt.Fprintf(a.Err, "  %s\n", line)
	}
	fmt.Fprintln(a.Err)
	fmt.Fprintf(a.Err, "hint: Run `agy-auth %s --help` for help and examples.\n", spec.Name)
	return 2
}

func (a *App) usageFor(name, format string, args ...any) int {
	return a.usage(specForName(name), format, args...)
}

func (a *App) usageTop(format string, args ...any) int {
	fmt.Fprintf(a.Err, "error: "+format+"\n\n", args...)
	fmt.Fprintln(a.Err, "Usage:")
	fmt.Fprintln(a.Err, "  agy-auth <command> [options]")
	fmt.Fprintln(a.Err)
	fmt.Fprintln(a.Err, "hint: Run `agy-auth help` for usage.")
	return 2
}

func optionWidth(options []OptionSpec) int {
	width := 12
	for _, option := range options {
		if len(option.Flag) > width {
			width = len(option.Flag)
		}
	}
	return width
}

const helpFooter = `Antigravity CLI keeps one credential in the OS keyring (service "gemini",
account "antigravity") with a file fallback at
~/.gemini/antigravity-cli/antigravity-oauth-token. agy-auth stores additional
accounts in its own registry and writes the selected account back to agy.
Restart agy after switching.
`
