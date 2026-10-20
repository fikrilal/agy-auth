package cli

import "fmt"

func (a *App) printHelp() {
	fmt.Fprint(a.Out, `agy-auth - switch between Antigravity CLI (agy) accounts

Usage:
  agy-auth <command> [options]

Account management:
  list, ls              List stored accounts and the live agy account
  add                   Save the account currently signed in to agy
  login                 Add an account through Google OAuth
  switch, use [query]   Switch the active agy account
  remove, rm [query]    Remove stored accounts
  alias set|clear       Manage account aliases
  current               Print the live agy account
  status                Show credential storage health

Import and export:
  import <path>         Import credential JSON file(s)
  export [--dir <dir>]  Export stored credential JSON files

Other:
  path                  Print storage paths
  version               Print the version
  help [command]        Show help

Global options:
  --json                Machine-readable output
  --no-color            Disable ANSI colors

Examples:
  agy-auth add
  agy-auth list
  agy-auth switch personal
  agy-auth login --alias work
  agy-auth switch 2

Antigravity CLI keeps one credential in the OS keyring (service "gemini",
account "antigravity") with a file fallback at
~/.gemini/antigravity-cli/antigravity-oauth-token. agy-auth stores additional
accounts in its own registry and writes the selected account back to agy.
Restart agy after switching.
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
	"list": `Usage: agy-auth list [--active] [--no-sync]

List stored accounts. The live agy credential is marked with *.
The active account snapshot is refreshed automatically when agy has rotated
its tokens; pass --no-sync to keep this command read-only.

Options:
  --active    Only show the active account
  --no-sync   Do not update the stored snapshot of the live account
`,
	"add": `Usage: agy-auth add [--alias <alias>]

Save the account currently signed in to agy. This does not change the active
account or contact Google. Use "agy-auth login" to add another account.

Options:
  --alias <alias>   Short name used by switch/remove queries
`,
	"login": `Usage: agy-auth login [--alias <alias>] [--auth-method <method>]
                        [--no-browser] [--via-agy] [--agy <path>]

Add an account through Google OAuth. The default flow prints an authorization
URL, opens a browser when possible, and asks for the code shown by
antigravity.google/oauth-callback.

Options:
  --alias <alias>        Short name used by switch/remove queries
  --auth-method <name>   Stored auth_method value (default "consumer")
  --no-browser           Only print the authorization URL
  --via-agy              Clear agy credentials and let agy perform sign-in
  --agy <path>           agy executable for OAuth client discovery or --via-agy
                         (default: search PATH)
`,
	"switch": `Usage: agy-auth switch [<query>]

Switch the active agy credential. Without a query, an interactive picker is
shown. Queries accept a row number from "agy-auth list", an alias, an exact
email, an email substring, or an account ID prefix.

Restart agy after switching; an already-running agy process keeps the old
credential in memory.
`,
	"remove": `Usage: agy-auth remove [<query>...] [--all] [--logout] [--yes]

Remove stored account snapshots. Removing an account does not sign agy out
unless --logout is passed. With no query, an interactive prompt is shown.

Options:
  --all      Remove every stored account
  --logout   Also delete the live agy credential when removing the active account
  --yes      Skip the confirmation prompt
`,
	"alias": `Usage:
  agy-auth alias set <query> <alias>
  agy-auth alias clear <query>

Aliases are short, whitespace-free names used by switch and remove.
`,
	"status": `Usage: agy-auth status

Show the live agy account, token expiry, registry paths, keyring/file mirror
state, and whether agy is currently running.
`,
	"current": `Usage: agy-auth current

Print the account currently stored in agy's credential store.
`,
	"import": `Usage: agy-auth import <file-or-directory> [--alias <alias>] [--activate]

Import agy credential JSON files (the same format as
~/.gemini/antigravity-cli/antigravity-oauth-token). --alias is only valid when
importing a single file. Imported accounts do not become active unless
--activate is passed (the first account is always selected).
`,
	"export": `Usage: agy-auth export [--dir <directory>] [<query>...]

Write stored credential JSON files to a directory (default: current directory)
with 0600 permissions. Export every account when no query is given.
`,
	"path": `Usage: agy-auth path

Print the registry, credential file, and keyring locations.
`,
	"version": `Usage: agy-auth version

Print the agy-auth version.
`,
}
