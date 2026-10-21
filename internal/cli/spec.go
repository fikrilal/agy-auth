package cli

type OptionSpec struct {
	Flag        string
	Description string
}

type CommandSpec struct {
	Name        string
	Aliases     []string
	Group       string
	Synopsis    string
	Summary     string
	Usage       []string
	Description string
	Options     []OptionSpec
	Examples    []string
	Notes       []string
}

var commandSpecs = []CommandSpec{
	{
		Name:     "list",
		Aliases:  []string{"ls"},
		Group:    "Account management",
		Synopsis: "list, ls",
		Summary:  "List stored accounts and the live agy account",
		Usage:    []string{"agy-auth list [--active] [--no-sync]"},
		Description: "List stored accounts. The live agy credential is marked with *.\n" +
			"The active account snapshot is refreshed automatically when agy has rotated\n" +
			"its tokens; pass --no-sync to keep this command read-only.",
		Options: []OptionSpec{
			{Flag: "--active", Description: "Only show the active account"},
			{Flag: "--no-sync", Description: "Do not update the stored snapshot of the live account"},
		},
		Examples: []string{"agy-auth list", "agy-auth list --active"},
	},
	{
		Name:     "add",
		Group:    "Account management",
		Synopsis: "add",
		Summary:  "Save the account currently signed in to agy",
		Usage:    []string{"agy-auth add [--alias <alias>]"},
		Description: "Save the account currently signed in to agy. This does not change the active\n" +
			"account or contact Google. Use \"agy-auth login\" to add another account.",
		Options: []OptionSpec{
			{Flag: "--alias <alias>", Description: "Short name used by switch/remove queries"},
		},
	},
	{
		Name:     "login",
		Group:    "Account management",
		Synopsis: "login",
		Summary:  "Add an account through Google OAuth",
		Usage: []string{
			"agy-auth login [--alias <alias>] [--auth-method <method>]",
			"               [--no-browser] [--via-agy] [--agy <path>]",
		},
		Description: "Add an account through Google OAuth. The default flow prints an authorization\n" +
			"URL, opens a browser when possible, and asks for the code shown by\n" +
			"antigravity.google/oauth-callback.",
		Options: []OptionSpec{
			{Flag: "--alias <alias>", Description: "Short name used by switch/remove queries"},
			{Flag: "--auth-method <name>", Description: "Stored auth_method value (default \"consumer\")"},
			{Flag: "--no-browser", Description: "Only print the authorization URL"},
			{Flag: "--via-agy", Description: "Clear agy credentials and let agy perform sign-in"},
			{Flag: "--agy <path>", Description: "agy executable for OAuth client discovery or --via-agy (default: search PATH)"},
		},
		Examples: []string{"agy-auth login --alias work"},
	},
	{
		Name:     "switch",
		Aliases:  []string{"use"},
		Group:    "Account management",
		Synopsis: "switch, use [query]",
		Summary:  "Switch the active agy account",
		Usage:    []string{"agy-auth switch [<query>]"},
		Description: "Switch the active agy credential. Without a query, an interactive picker is\n" +
			"shown. Queries accept a row number from \"agy-auth list\", an alias, an exact\n" +
			"email, an email substring, or an account ID prefix.",
		Examples: []string{"agy-auth switch work", "agy-auth switch 2"},
		Notes: []string{
			"Restart agy after switching; an already-running agy process keeps the old",
			"credential in memory.",
		},
	},
	{
		Name:     "remove",
		Aliases:  []string{"rm"},
		Group:    "Account management",
		Synopsis: "remove, rm [query]",
		Summary:  "Remove stored accounts",
		Usage:    []string{"agy-auth remove [<query>...] [--all] [--logout] [--yes]"},
		Description: "Remove stored account snapshots. Removing an account does not sign agy out\n" +
			"unless --logout is passed. With no query, an interactive prompt is shown.",
		Options: []OptionSpec{
			{Flag: "--all", Description: "Remove every stored account"},
			{Flag: "--logout", Description: "Also delete the live agy credential when removing the active account"},
			{Flag: "--yes", Description: "Skip the confirmation prompt"},
		},
		Examples: []string{"agy-auth remove work", "agy-auth remove --all --yes"},
	},
	{
		Name:     "alias",
		Group:    "Account management",
		Synopsis: "alias set|clear",
		Summary:  "Manage account aliases",
		Usage: []string{
			"agy-auth alias set <query> <alias>",
			"agy-auth alias clear <query>",
		},
		Description: "Aliases are short, whitespace-free names used by switch and remove.",
		Examples:    []string{"agy-auth alias set work 2", "agy-auth alias clear work"},
	},
	{
		Name:        "current",
		Group:       "Account management",
		Synopsis:    "current",
		Summary:     "Print the live agy account",
		Usage:       []string{"agy-auth current"},
		Description: "Print the account currently stored in agy's credential store.",
	},
	{
		Name:     "status",
		Group:    "Account management",
		Synopsis: "status",
		Summary:  "Show credential storage health",
		Usage:    []string{"agy-auth status"},
		Description: "Show the live agy account, token expiry, registry paths, keyring/file mirror\n" +
			"state, and whether agy is currently running.",
	},
	{
		Name:     "import",
		Group:    "Import and export",
		Synopsis: "import <path>",
		Summary:  "Import credential JSON file(s)",
		Usage:    []string{"agy-auth import <file-or-directory> [--alias <alias>] [--activate]"},
		Description: "Import agy credential JSON files (the same format as\n" +
			"~/.gemini/antigravity-cli/antigravity-oauth-token). --alias is only valid when\n" +
			"importing a single file. Imported accounts do not become active unless\n" +
			"--activate is passed (the first account is always selected).",
		Options: []OptionSpec{
			{Flag: "--alias <alias>", Description: "Alias for a single imported file"},
			{Flag: "--activate", Description: "Activate a single imported file"},
		},
		Examples: []string{"agy-auth import ./token.json --alias work"},
	},
	{
		Name:     "export",
		Group:    "Import and export",
		Synopsis: "export [--dir <dir>]",
		Summary:  "Export stored credential JSON files",
		Usage:    []string{"agy-auth export [--dir <directory>] [<query>...]"},
		Description: "Write stored credential JSON files to a directory (default: current directory)\n" +
			"with 0600 permissions. Export every account when no query is given.",
		Options: []OptionSpec{
			{Flag: "--dir <directory>", Description: "Destination directory (default: current directory)"},
		},
		Examples: []string{"agy-auth export --dir ./backup"},
	},
	{
		Name:        "path",
		Aliases:     []string{"paths"},
		Group:       "Other",
		Synopsis:    "path, paths",
		Summary:     "Print storage paths",
		Usage:       []string{"agy-auth path"},
		Description: "Print the registry, credential file, and keyring locations.",
	},
	{
		Name:        "version",
		Group:       "Other",
		Synopsis:    "version",
		Summary:     "Print the version",
		Usage:       []string{"agy-auth version"},
		Description: "Print the agy-auth version.",
	},
	{
		Name:        "help",
		Group:       "Other",
		Synopsis:    "help [command]",
		Summary:     "Show help",
		Usage:       []string{"agy-auth help [command]"},
		Description: "Show top-level help, or help for one command.",
	},
}

var specIndex = buildSpecIndex()

func buildSpecIndex() map[string]int {
	index := make(map[string]int, len(commandSpecs))
	for i := range commandSpecs {
		index[commandSpecs[i].Name] = i
		for _, alias := range commandSpecs[i].Aliases {
			index[alias] = i
		}
	}
	return index
}

func specForName(name string) *CommandSpec {
	index, ok := specIndex[name]
	if !ok {
		return nil
	}
	return &commandSpecs[index]
}
