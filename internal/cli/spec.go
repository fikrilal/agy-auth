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
		Usage:    []string{"agy-auth list [--active] [--no-sync] [--no-usage] [--live]"},
		Description: "List stored accounts. The live agy credential is marked with *.\n" +
			"The active account snapshot is refreshed automatically when agy has rotated\n" +
			"its tokens; pass --no-sync to keep this command read-only. Plan and shared\n" +
			"5-hour/weekly quota are fetched when available; --no-usage skips that lookup.\n" +
			"--live opens a refreshing interactive view.",
		Options: []OptionSpec{
			{Flag: "--active", Description: "Only show the active account"},
			{Flag: "--no-sync", Description: "Do not update the stored snapshot of the live account"},
			{Flag: "--no-usage", Description: "Do not fetch plan and quota data"},
			{Flag: "--live", Description: "Interactive live view"},
		},
		Examples: []string{"agy-auth list", "agy-auth list --active", "agy-auth list --no-usage", "agy-auth list --live"},
	},
	{
		Name:     "add",
		Group:    "Account management",
		Synopsis: "add",
		Summary:  "Save the account currently signed in to agy",
		Usage:    []string{"agy-auth add [--alias <alias>] [--target <target>] [--gcp --project <id> [--region <region>]]"},
		Description: "Save the account currently signed in to agy and Antigravity Desktop. This does not change the active\n" +
			"account or contact Google. Pass --gcp and --project to tag the credential as a\n" +
			"Google Cloud project (Pay As You Go) account. Use \"agy-auth login\" to add another account.",
		Options: []OptionSpec{
			{Flag: "--alias <alias>", Description: "Short name used by switch/remove queries"},
			{Flag: "--target <target>", Description: "Capture target: all, cli, or desktop (default \"all\")"},
			{Flag: "--gcp", Description: "Save as a Google Cloud project (Pay As You Go) account"},
			{Flag: "--project <id>", Description: "Google Cloud project ID"},
			{Flag: "--region <region>", Description: "Google Cloud region: global, us, or eu (default \"global\")"},
		},
	},
	{
		Name:     "login",
		Group:    "Account management",
		Synopsis: "login",
		Summary:  "Add an account through Google OAuth",
		Usage: []string{
			"agy-auth login [--alias <alias>] [--auth-method <method>]",
			"               [--gcp --project <id> [--region <region>]]",
			"               [--no-browser] [--via-agy] [--agy <path>]",
		},
		Description: "Add an account through Google OAuth. The default flow prints an authorization\n" +
			"URL, opens a browser when possible, and asks for the code shown by\n" +
			"antigravity.google/oauth-callback. Pass --gcp and --project <id> (with optional\n" +
			"--region global|us|eu) to sign in with a Google Cloud project in Pay As You Go mode.",
		Options: []OptionSpec{
			{Flag: "--alias <alias>", Description: "Short name used by switch/remove queries"},
			{Flag: "--auth-method <name>", Description: "Stored auth_method value (default \"consumer\")"},
			{Flag: "--gcp", Description: "Sign in with a Google Cloud project (Pay As You Go)"},
			{Flag: "--project <id>", Description: "Google Cloud project ID for --gcp"},
			{Flag: "--region <region>", Description: "Google Cloud region for --gcp: global, us, or eu (default \"global\")"},
			{Flag: "--no-browser", Description: "Only print the authorization URL"},
			{Flag: "--via-agy", Description: "Run agy in an isolated login directory; import only on success"},
			{Flag: "--agy <path>", Description: "agy executable for OAuth client discovery or --via-agy (default: search PATH)"},
		},
		Examples: []string{
			"agy-auth login --alias personal",
			"agy-auth login --gcp --project sample-gcp-project --region global --alias work",
		},
	},
	{
		Name:     "switch",
		Aliases:  []string{"use"},
		Group:    "Account management",
		Synopsis: "switch, use [query]",
		Summary:  "Switch the active agy account",
		Usage:    []string{"agy-auth switch [<query>|-] [--live] [--target <target>] [--relaunch] [--close-desktop]"},
		Description: "Switch the active agy and Antigravity Desktop credentials. Without a query, an interactive picker is\n" +
			"shown. Queries accept a row number from \"agy-auth list\", an alias, an exact\n" +
			"project ID, an exact email, an email/project substring, or an account ID prefix.\n" +
			"--live opens a refreshing view and switches on Enter.",
		Options: []OptionSpec{
			{Flag: "--target <target>", Description: "Switch target: all, cli, or desktop (default \"all\")"},
			{Flag: "--relaunch", Description: "Relaunch Antigravity Desktop after switching"},
			{Flag: "--close-desktop", Description: "Gracefully close Antigravity Desktop before switching"},
			{Flag: "--live", Description: "Interactive live view; select and press Enter to switch"},
		},
		Examples: []string{
			"agy-auth switch work",
			"agy-auth switch work --relaunch",
			"agy-auth switch work --target=cli",
			"agy-auth switch 2",
			"agy-auth switch --live",
		},
		Notes: []string{
			"Restart agy after switching; an already-running agy process keeps the old",
			"credential in memory.",
		},
	},
	{
		Name:     "rotate",
		Group:    "Account management",
		Synopsis: "rotate",
		Summary:  "Automatically switch to an account with available quota",
		Usage:    []string{"agy-auth rotate [--target <target>] [--relaunch] [--close-desktop] [--force]"},
		Description: "Check 5-hour quota across all stored accounts and automatically switch to\n" +
			"the healthiest account with remaining quota when the active account is exhausted.\n" +
			"Use --force to switch even if the active account is not exhausted.",
		Options: []OptionSpec{
			{Flag: "--target <target>", Description: "Switch target: all, cli, or desktop (default \"all\")"},
			{Flag: "--relaunch", Description: "Relaunch Antigravity Desktop after switching"},
			{Flag: "--close-desktop", Description: "Gracefully close Antigravity Desktop before switching"},
			{Flag: "--force", Description: "Rotate even if the active account still has remaining quota"},
		},
		Examples: []string{
			"agy-auth rotate",
			"agy-auth rotate --relaunch",
			"agy-auth rotate --force",
			"agy-auth rotate --target=cli",
		},
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
		Usage:    []string{"agy-auth remove [<query>...] [--all] [--logout] [--yes] [--live]"},
		Description: "Remove stored account snapshots. Removing an account does not sign agy out\n" +
			"unless --logout is passed. With no query, an interactive prompt is shown.\n" +
			"--live opens a checkbox view and removes checked accounts on Enter.",
		Options: []OptionSpec{
			{Flag: "--all", Description: "Remove every stored account"},
			{Flag: "--logout", Description: "Also delete the live agy credential when removing the active account"},
			{Flag: "--yes", Description: "Skip the confirmation prompt"},
			{Flag: "--live", Description: "Interactive live view; Space toggles and Enter removes"},
		},
		Examples: []string{"agy-auth remove work", "agy-auth remove --all --yes", "agy-auth remove --live"},
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
		Name:     "usage",
		Group:    "Account management",
		Synopsis: "usage [query] [--all]",
		Summary:  "Show Antigravity quota groups",
		Usage: []string{
			"agy-auth usage [<query>]",
			"agy-auth usage --all",
		},
		Description: "Show the same quota groups as `agy /usage`: Gemini Models, Claude and GPT\n" +
			"models, and any tier-specific group such as Ultra. Each group shows its 5-hour\n" +
			"and weekly remaining percentage plus reset times. --all compares every stored\n" +
			"account; a query selects one account.",
		Options:  []OptionSpec{{Flag: "--all", Description: "Show every stored account"}},
		Examples: []string{"agy-auth usage", "agy-auth usage work", "agy-auth usage --all", "agy-auth usage --json"},
	},
	{
		Name:     "import",
		Group:    "Import and export",
		Synopsis: "import <path> | --purge",
		Summary:  "Import credential JSON or rebuild the registry",
		Usage: []string{
			"agy-auth import <file-or-directory> [--alias <alias>] [--activate] [--gcp --project <id> [--region <region>]]",
			"agy-auth import --purge",
		},
		Description: "Import agy credential JSON files (the same format as\n" +
			"~/.gemini/antigravity-cli/antigravity-oauth-token). Invalid files are skipped\n" +
			"and reported. --alias, --gcp, --project, and --region are only valid for a single\n" +
			"file. Imported accounts do not become active unless --activate is passed.\n" +
			"--purge rebuilds registry.json from stored account snapshots, preserving aliases\n" +
			"and active/previous where possible.",
		Options: []OptionSpec{
			{Flag: "--alias <alias>", Description: "Alias for a single imported file"},
			{Flag: "--activate", Description: "Activate a single imported file"},
			{Flag: "--gcp", Description: "Mark a single imported file as a Google Cloud project account"},
			{Flag: "--project <id>", Description: "Google Cloud project ID for a single imported file"},
			{Flag: "--region <region>", Description: "Google Cloud region: global, us, or eu (default \"global\")"},
			{Flag: "--purge", Description: "Rebuild the registry from stored account snapshots"},
		},
		Examples: []string{"agy-auth import ./token.json --alias work", "agy-auth import --purge"},
	},
	{
		Name:     "export",
		Group:    "Import and export",
		Synopsis: "export [--dir <dir>]",
		Summary:  "Export stored credential JSON files",
		Usage:    []string{"agy-auth export [--dir <directory>] [<query>...]"},
		Description: "Write stored credential JSON files to a directory (default: current directory)\n" +
			"with 0600 permissions. Export every account when no query is given. Per-file\n" +
			"results and a summary are printed.",
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
		Name:     "clean",
		Group:    "Other",
		Synopsis: "clean [--dry-run]",
		Summary:  "Prune managed backups and orphan snapshots",
		Usage:    []string{"agy-auth clean [--dry-run]"},
		Description: "Remove managed backup files beyond retention and snapshot files not\n" +
			"referenced by the registry. Snapshots are preserved when registry.json is\n" +
			"missing so the registry can be rebuilt.",
		Options: []OptionSpec{
			{Flag: "--dry-run", Description: "Show what would be removed"},
		},
		Examples: []string{"agy-auth clean --dry-run", "agy-auth clean"},
	},
	{
		Name:     "config",
		Group:    "Other",
		Synopsis: "config live [--interval <seconds>]",
		Summary:  "Show or set live refresh settings",
		Usage:    []string{"agy-auth config live [--interval <seconds>]"},
		Description: "Show the effective live refresh interval, or set it to 5-3600 seconds.\n" +
			"The setting is stored in registry.json and applies to the live refresh loop and\n" +
			"the quota cache.",
		Options:  []OptionSpec{{Flag: "--interval <seconds>", Description: "Live refresh interval (5-3600)"}},
		Examples: []string{"agy-auth config live", "agy-auth config live --interval 30"},
	},
	{
		Name:        "version",
		Group:       "Other",
		Synopsis:    "version [--check]",
		Summary:     "Print the version",
		Usage:       []string{"agy-auth version", "agy-auth version --check"},
		Description: "Print the agy-auth version. --check queries the npm registry for a newer release.",
		Options:     []OptionSpec{{Flag: "--check", Description: "Check the latest published version"}},
		Examples:    []string{"agy-auth version", "agy-auth version --check"},
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

func canonicalCommand(name string) string {
	switch name {
	case "--version", "-v":
		return "version"
	case "-h", "--help":
		return "help"
	}
	if spec := specForName(name); spec != nil {
		return spec.Name
	}
	return name
}
