package cli

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/fikrilal/agy-auth/internal/agy"
	"github.com/fikrilal/agy-auth/internal/oauth"
	"github.com/fikrilal/agy-auth/internal/registry"
)

func (a *App) cmdLogin(args []string) int {
	fs := a.newFlagSet("login")
	alias := fs.String("alias", "", "short name used by switch/remove queries")
	authMethod := fs.String("auth-method", "consumer", "stored auth_method value")
	noBrowser := fs.Bool("no-browser", false, "only print the authorization URL")
	viaAgy := fs.Bool("via-agy", false, "let agy perform the sign-in flow")
	agyPath := fs.String("agy", "", "agy executable for OAuth client discovery and --via-agy")
	if ok, code := a.parseFlags(fs, args); !ok {
		return code
	}
	if fs.NArg() > 0 {
		return a.usageFor("login", "login takes no positional arguments")
	}
	if *viaAgy {
		return a.loginViaAgy(*alias, *agyPath)
	}
	if err := oauth.EnsureClient(*agyPath); err != nil {
		return a.fail("%v", err)
	}
	return a.loginOAuth(*alias, *authMethod, *noBrowser)
}

func (a *App) loginOAuth(alias, authMethod string, noBrowser bool) int {
	state, err := oauth.NewState()
	if err != nil {
		return a.fail("%v", err)
	}
	verifier, err := oauth.NewVerifier()
	if err != nil {
		return a.fail("%v", err)
	}
	authURL := oauth.AuthURL(state, verifier)

	prompt := a.promptWriter()
	fmt.Fprintln(prompt, "Open this URL in a browser and approve access:")
	fmt.Fprintln(prompt)
	fmt.Fprintln(prompt, authURL)
	fmt.Fprintln(prompt)
	if !noBrowser {
		if err := openBrowser(authURL); err != nil {
			fmt.Fprintf(prompt, "Could not open a browser automatically (%v).\n", err)
		}
	}
	fmt.Fprintln(prompt, "After approving, antigravity.google/oauth-callback shows an authorization code.")
	input, err := a.readLine("Paste the authorization code shown on that page: ")
	if err != nil {
		return a.fail("read authorization code: %v", err)
	}
	code, err := oauth.NormalizeCode(input, state)
	if err != nil {
		return a.fail("%v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	response, err := oauth.Exchange(ctx, code, verifier)
	if err != nil {
		return a.fail("%v", err)
	}
	if response.IDToken == "" {
		return a.fail("Google did not return an id_token; the account cannot be identified")
	}

	tokenFile := oauth.TokenFromResponse(response, authMethod, a.now())
	var warnings []string
	if tokenFile.Token.RefreshToken == "" {
		if old := a.existingRefreshToken(tokenFile); old != "" {
			tokenFile.Token.RefreshToken = old
			warnings = append(warnings, "Google did not issue a new refresh token; reused the stored one")
		}
	}
	if tokenFile.Token.RefreshToken == "" {
		return a.fail("Google did not return a refresh token; re-run login and approve offline access")
	}

	raw, err := oauth.MarshalTokenFile(tokenFile)
	if err != nil {
		return a.fail("%v", err)
	}
	result, err := a.Store.Upsert(raw, alias, true)
	if err != nil {
		return a.fail("%v", err)
	}
	report := a.Storage.Write(raw)
	if !report.OK() {
		return a.fail("account was saved to the registry but agy credentials could not be written: %v\nrun \"agy-auth switch %s\" after fixing credential storage", report.Error(), result.Account.ID)
	}
	if err := report.Error(); err != nil {
		warnings = append(warnings, err.Error())
	}
	if result.Account.Email == "" {
		warnings = append(warnings, "the ID token did not include an email address")
	}
	return a.finishLogin(result, report, warnings)
}

func (a *App) loginViaAgy(alias, agyPath string) int {
	if !a.TTY {
		return a.fail("--via-agy requires an interactive terminal; use the default OAuth flow instead")
	}
	if read, err := a.Storage.Read(); err == nil {
		if result, err := a.Store.Upsert(read.Raw, "", true); err == nil {
			fmt.Fprintf(a.promptWriter(), "Saved current account %s.\n", result.Account.DisplayName())
		}
	}
	if count := runningAgyCount(); count > 0 {
		answer, err := a.readLine(fmt.Sprintf("%d agy process(es) are running. Close them before signing in. Continue? [y/N]: ", count))
		if err != nil || !isYes(answer) {
			return 1
		}
	}
	if err := a.Storage.Delete(); err != nil {
		a.warn("could not fully clear existing credentials: %v", err)
	}
	if remaining, err := a.Storage.Read(); err == nil {
		name := ""
		if tokenFile, parseErr := agy.ParseTokenFile(remaining.Raw); parseErr == nil {
			name = tokenFile.Email()
		}
		if name == "" {
			name = "(unknown account)"
		}
		return a.fail("agy is still signed in as %s and its credential could not be cleared; sign out in agy first", name)
	} else if !errors.Is(err, agy.ErrNoCredentials) {
		a.warn("could not verify that credentials were cleared: %v", err)
	}

	binary := agyPath
	if binary == "" {
		found, err := exec.LookPath("agy")
		if err != nil {
			return a.fail("agy executable not found in PATH; pass --agy <path>")
		}
		binary = found
	}

	if a.StdinFile == nil || a.StdoutFile == nil || a.StderrFile == nil {
		return a.fail("interactive terminal unavailable for --via-agy")
	}
	command := exec.Command(binary)
	command.Stdin = a.StdinFile
	command.Stdout = a.StdoutFile
	command.Stderr = a.StderrFile
	fmt.Fprintln(a.promptWriter(), "Starting agy. Sign in with the account you want to add, then quit agy.")
	runErr := command.Run()
	if runErr != nil {
		fmt.Fprintf(a.promptWriter(), "agy exited with an error (%v); checking for a credential anyway.\n", runErr)
	}

	read, err := a.Storage.Read()
	if errors.Is(err, agy.ErrNoCredentials) {
		return a.fail("agy did not leave a credential; the previous account is still stored and can be restored with \"agy-auth list\"")
	}
	if err != nil {
		return a.fail("%v", err)
	}
	result, err := a.Store.Upsert(read.Raw, alias, true)
	if err != nil {
		return a.fail("%v", err)
	}
	return a.finishLogin(result, agy.WriteReport{Written: []agy.Source{read.Source}}, nil)
}

func (a *App) existingRefreshToken(tokenFile agy.TokenFile) string {
	claims, err := tokenFile.Claims()
	if err != nil || claims.Subject == "" {
		return ""
	}
	reg, err := a.Store.Load()
	if err != nil {
		return ""
	}
	for i := range reg.Accounts {
		if reg.Accounts[i].Subject != claims.Subject {
			continue
		}
		raw, err := a.Store.LoadToken(reg.Accounts[i].ID)
		if err != nil {
			return ""
		}
		old, err := agy.ParseTokenFile(raw)
		if err != nil {
			return ""
		}
		return old.Token.RefreshToken
	}
	return ""
}

func (a *App) finishLogin(result registry.UpsertResult, report agy.WriteReport, warnings []string) int {
	if a.JSON {
		if err := a.emitJSON(map[string]any{
			"created":  result.Created,
			"account":  result.Account,
			"written":  report.Written,
			"warnings": warnings,
		}); err != nil {
			return a.fail("%v", err)
		}
		return 0
	}
	verb := "Updated"
	if result.Created {
		verb = "Added"
	}
	fmt.Fprintf(a.Out, "%s account %s [%s].\n", verb, result.Account.DisplayName(), result.Account.ID)
	if len(report.Written) > 0 {
		fmt.Fprintf(a.Out, "Wrote %s.\n", strings.Join(sourceNames(report.Written), " and "))
	}
	for _, warning := range warnings {
		fmt.Fprintf(a.Err, "warning: %s\n", warning)
	}
	fmt.Fprintln(a.Out, "Restart agy for the new account to take effect.")
	return 0
}

func sourceNames(sources []agy.Source) []string {
	names := make([]string, 0, len(sources))
	for _, source := range sources {
		if source == agy.SourceKeyring {
			names = append(names, "OS keyring")
		} else {
			names = append(names, "file mirror")
		}
	}
	return names
}

func isYes(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	return value == "y" || value == "yes"
}
