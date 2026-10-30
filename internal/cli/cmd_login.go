package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/fikrilal/agy-auth/internal/agy"
	"github.com/fikrilal/agy-auth/internal/oauth"
	"github.com/fikrilal/agy-auth/internal/registry"
)

func (a *App) cmdLogin(args []string) int {
	fs := a.newFlagSet("login")
	alias := fs.String("alias", "", "short name used by switch/remove queries")
	authMethod := fs.String("auth-method", agy.AuthMethodConsumer, "stored auth_method value")
	gcp := fs.Bool("gcp", false, "sign in with a Google Cloud project (Pay As You Go)")
	project := fs.String("project", "", "Google Cloud project ID for --gcp")
	region := fs.String("region", "", "Google Cloud region for --gcp (global, us, or eu)")
	noBrowser := fs.Bool("no-browser", false, "only print the authorization URL")
	viaAgy := fs.Bool("via-agy", false, "let agy perform the sign-in flow")
	agyPath := fs.String("agy", "", "agy executable for OAuth client discovery and --via-agy")
	if ok, code := a.parseFlags(fs, args); !ok {
		return code
	}
	if fs.NArg() > 0 {
		return a.usageFor("login", "login takes no positional arguments")
	}
	if *alias != "" {
		if err := registry.ValidateAlias(*alias); err != nil {
			return a.usageFor("login", "%s", err)
		}
	}

	effectiveAuthMethod := strings.TrimSpace(*authMethod)
	if effectiveAuthMethod == "" {
		effectiveAuthMethod = agy.AuthMethodConsumer
	}
	isGCP := *gcp || strings.TrimSpace(*project) != "" || strings.EqualFold(effectiveAuthMethod, agy.AuthMethodGCP)
	if !isGCP && strings.TrimSpace(*region) != "" {
		return a.usageFor("login", "--region requires --gcp or --project")
	}

	var projectID, regionValue string
	if isGCP {
		effectiveAuthMethod = agy.AuthMethodGCP
		rawProject := strings.TrimSpace(*project)
		rawRegion := strings.TrimSpace(*region)
		if !*viaAgy && rawProject == "" {
			if a.TTY && !a.JSON {
				inputProject, err := a.readLine("Google Cloud project ID: ")
				if err != nil {
					return a.failCode("input_error", "Pass --project <id> or try again.", "could not read the Google Cloud project ID")
				}
				rawProject = strings.TrimSpace(inputProject)
				if rawRegion == "" {
					inputRegion, err := a.readLine("Google Cloud region [global/us/eu] (default: global): ")
					if err != nil {
						return a.failCode("input_error", "Pass --region <region> or try again.", "could not read the Google Cloud region")
					}
					rawRegion = strings.TrimSpace(inputRegion)
				}
			} else {
				return a.usageFor("login", "--gcp requires --project <id>")
			}
		}
		if rawProject != "" {
			validatedProject, err := agy.ValidateProjectID(rawProject)
			if err != nil {
				return a.usageFor("login", "%s", err)
			}
			projectID = validatedProject
		}
		if rawRegion != "" || projectID != "" {
			validatedRegion, err := agy.ValidateRegion(rawRegion)
			if err != nil {
				return a.usageFor("login", "%s", err)
			}
			regionValue = validatedRegion
		}
	}

	if *viaAgy {
		return a.loginViaAgy(*alias, *agyPath, isGCP, projectID, regionValue)
	}
	if err := oauth.EnsureClient(*agyPath); err != nil {
		return a.failCode("oauth_error", "Pass --agy <path>, or set AGY_AUTH_CLIENT_ID and AGY_AUTH_CLIENT_SECRET.", "could not discover the Antigravity OAuth client")
	}
	return a.loginOAuth(*alias, effectiveAuthMethod, projectID, regionValue, *noBrowser)
}

func (a *App) loginOAuth(alias, authMethod, projectID, region string, noBrowser bool) int {
	state, err := oauth.NewState()
	if err != nil {
		return a.failCode("oauth_error", "Retry the login flow.", "could not start the OAuth flow")
	}
	verifier, err := oauth.NewVerifier()
	if err != nil {
		return a.failCode("oauth_error", "Retry the login flow.", "could not start the OAuth flow")
	}
	authURL := oauth.AuthURLForMethod(state, verifier, authMethod)

	prompt := a.promptWriter()
	fmt.Fprintln(prompt, "Open this URL in a browser and approve access:")
	fmt.Fprintln(prompt)
	fmt.Fprintln(prompt, authURL)
	fmt.Fprintln(prompt)
	if !noBrowser {
		if err := openBrowser(authURL); err != nil {
			fmt.Fprintln(prompt, "Could not open a browser automatically; open the URL manually.")
		}
	}
	fmt.Fprintln(prompt, "After approving, antigravity.google/oauth-callback shows an authorization code.")
	input, err := a.readLine("Paste the authorization code shown on that page: ")
	if err != nil {
		return a.failCode("input_error", "Try again with the code from antigravity.google/oauth-callback.", "could not read the authorization code")
	}
	code, err := oauth.NormalizeCode(input, state)
	if err != nil {
		return a.failCode("oauth_error", "Copy the code shown after approving access and try again.", "the authorization code is invalid")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	response, err := oauth.ExchangeForMethod(ctx, code, verifier, authMethod)
	if err != nil {
		return a.failCode("oauth_error", "Check your network connection and try again.", "could not exchange the authorization code")
	}
	if response.IDToken == "" {
		return a.failCode("oauth_error", "Try signing in again; the account cannot be identified without an ID token.", "Google did not return an id_token")
	}

	tokenFile := oauth.TokenFromResponse(response, authMethod, a.now())
	tokenFile.ProjectID = projectID
	tokenFile.Region = region
	var warnings []string
	if tokenFile.Token.RefreshToken == "" {
		if old := a.existingRefreshToken(tokenFile); old != "" {
			tokenFile.Token.RefreshToken = old
			warnings = append(warnings, "Google did not issue a new refresh token; reused the stored one")
		}
	}
	if tokenFile.Token.RefreshToken == "" {
		return a.failCode("oauth_error", "Re-run login and approve offline access.", "Google did not return a refresh token")
	}

	raw, err := oauth.MarshalTokenFile(tokenFile)
	if err != nil {
		return a.failCode("oauth_error", "Try signing in again.", "could not prepare the credential document")
	}
	result, err := a.Store.Upsert(raw, alias, true)
	if err != nil {
		return a.registryFailure(err, "could not save the logged-in account")
	}
	report := a.Storage.Write(raw)
	if !report.OK() {
		if errors.Is(report.Error(), agy.ErrBackup) {
			return a.failCode("backup_error", fmt.Sprintf("The account is saved; fix backup permissions and run `agy-auth switch %s`.", result.Account.ID), "could not back up the current agy credential")
		}
		return a.errorDoc(1, ErrorDoc{
			Code:    "state_uncertain",
			Message: "the account was saved, but the agy credential could not be written",
			Hint:    fmt.Sprintf("Run `agy-auth switch %s` after fixing credential storage.", result.Account.ID),
		})
	}
	if err := a.Storage.SyncSettings(authMethod, projectID, region); err != nil {
		warnings = append(warnings, fmt.Sprintf("settings: %v", err))
	}
	if err := report.Error(); err != nil {
		warnings = append(warnings, err.Error())
	}
	if result.Account.Email == "" {
		warnings = append(warnings, "the ID token did not include an email address")
	}
	return a.finishLogin(result, report, warnings)
}

func (a *App) loginViaAgy(alias, agyPath string, isGCP bool, projectID, region string) int {
	if !a.TTY {
		return a.usageFor("login", "--via-agy requires an interactive terminal; use the default OAuth flow instead")
	}
	if read, err := a.Storage.Read(); err == nil {
		if result, err := a.Store.Upsert(read.Raw, "", true); err == nil {
			fmt.Fprintf(a.promptWriter(), "Saved current account %s.\n", result.Account.DisplayName())
		}
	}

	binary := agyPath
	if binary == "" {
		found, err := exec.LookPath("agy")
		if err != nil {
			return a.failCode("agy_not_found", "Pass --agy <path>.", "agy executable not found in PATH")
		}
		binary = found
	}
	if a.StdinFile == nil || a.StdoutFile == nil || a.StderrFile == nil {
		return a.usageFor("login", "interactive terminal unavailable for --via-agy")
	}

	home, err := os.MkdirTemp("", "agy-auth-login-*")
	if err != nil {
		return a.failCode("credential_error", "Check that the temporary directory is writable.", "could not create an isolated login directory")
	}
	defer os.RemoveAll(home)
	tokenPath := filepath.Join(home, ".gemini", "antigravity-cli", "antigravity-oauth-token")
	if err := os.MkdirAll(filepath.Dir(tokenPath), 0o700); err != nil {
		return a.failCode("credential_error", "Check that the temporary directory is writable.", "could not prepare the isolated login directory")
	}

	command := exec.Command(binary)
	command.Dir = home
	command.Env = isolatedAgyEnv(home)
	command.Stdin = a.StdinFile
	command.Stdout = a.StdoutFile
	command.Stderr = a.StderrFile
	fmt.Fprintln(a.promptWriter(), "Starting agy in an isolated login directory. Your current account will not be changed unless sign-in succeeds.")
	runErr := command.Run()

	raw, err := os.ReadFile(tokenPath)
	if errors.Is(err, os.ErrNotExist) {
		return a.failCode("credential_not_found", "Your previous active account was not changed. Run `agy-auth list` to verify it.", "agy did not leave a credential in the isolated login directory")
	}
	if err != nil {
		return a.failCode("credential_error", "Your previous active account was not changed. Check temporary directory permissions.", "could not read the isolated agy credential")
	}

	isolatedSettingsPath := filepath.Join(home, ".gemini", "antigravity-cli", "settings.json")
	if isolatedData, sErr := os.ReadFile(isolatedSettingsPath); sErr == nil {
		var s struct {
			GCP struct {
				Project  string `json:"project"`
				Location string `json:"location"`
			} `json:"gcp"`
		}
		if json.Unmarshal(isolatedData, &s) == nil && s.GCP.Project != "" {
			isGCP = true
			if projectID == "" {
				projectID = s.GCP.Project
			}
			if region == "" {
				region = s.GCP.Location
			}
		}
	}

	tokenFile, err := agy.ParseTokenFile(raw)
	if err != nil {
		return a.failCode("credential_error", "Your previous active account was not changed. Sign in again with agy.", "the isolated agy credential is invalid")
	}
	if strings.EqualFold(tokenFile.AuthMethod, agy.AuthMethodGCP) || tokenFile.ProjectID != "" {
		isGCP = true
		if projectID == "" {
			projectID = tokenFile.ProjectID
		}
		if region == "" {
			region = tokenFile.Region
		}
	}
	if isGCP || projectID != "" || region != "" {
		effectiveProject := projectID
		if effectiveProject == "" {
			effectiveProject = tokenFile.ProjectID
		}
		effectiveRegion := region
		if effectiveRegion == "" {
			effectiveRegion = tokenFile.Region
		}
		if effectiveRegion == "" && effectiveProject != "" {
			effectiveRegion = agy.RegionGlobal
		}
		updatedRaw, err := agy.WithGCPOptions(raw, agy.AuthMethodGCP, effectiveProject, effectiveRegion)
		if err != nil {
			return a.failCode("credential_error", "Check the --project and --region flags.", "%s", err)
		}
		raw = updatedRaw
		tokenFile, _ = agy.ParseTokenFile(raw)
	}

	result, err := a.Store.Upsert(raw, alias, true)
	if err != nil {
		return a.registryFailure(err, "could not save the new account")
	}
	report := a.Storage.Write(raw)
	if !report.OK() {
		if errors.Is(report.Error(), agy.ErrBackup) {
			return a.failCode("backup_error", fmt.Sprintf("The account is saved; fix backup permissions and run `agy-auth switch %s`.", result.Account.ID), "could not back up the current agy credential")
		}
		return a.errorDoc(1, ErrorDoc{
			Code:    "state_uncertain",
			Message: "the account was saved, but the agy credential could not be written",
			Hint:    fmt.Sprintf("Run `agy-auth switch %s` after fixing credential storage.", result.Account.ID),
		})
	}
	var warnings []string
	effectiveAuth := tokenFile.AuthMethod
	if isGCP || projectID != "" {
		effectiveAuth = agy.AuthMethodGCP
	}
	if err := a.Storage.SyncSettings(effectiveAuth, tokenFile.ProjectID, tokenFile.Region); err != nil {
		warnings = append(warnings, fmt.Sprintf("settings: %v", err))
	}
	if err := report.Error(); err != nil {
		warnings = append(warnings, err.Error())
	}
	if runErr != nil {
		warnings = append(warnings, "agy exited with an error; imported the credential it left behind")
	}
	if !tokenFile.HasRefreshToken() {
		warnings = append(warnings, "the new snapshot has no refresh token; it will stop working when the access token expires")
	}
	if result.Account.Email == "" {
		warnings = append(warnings, "the ID token did not include an email address")
	}
	return a.finishLogin(result, report, warnings)
}

func isolatedAgyEnv(home string) []string {
	result := make([]string, 0, len(os.Environ())+6)
	for _, entry := range os.Environ() {
		switch {
		case strings.HasPrefix(entry, "HOME="),
			strings.HasPrefix(entry, "USERPROFILE="),
			strings.HasPrefix(entry, "DBUS_SESSION_BUS_ADDRESS="),
			strings.HasPrefix(entry, "XDG_RUNTIME_DIR="),
			strings.HasPrefix(entry, "XDG_CONFIG_HOME="),
			strings.HasPrefix(entry, "XDG_CACHE_HOME="):
			continue
		default:
			result = append(result, entry)
		}
	}
	return append(result,
		"HOME="+home,
		"USERPROFILE="+home,
		"DBUS_SESSION_BUS_ADDRESS=",
		"XDG_RUNTIME_DIR=",
		"XDG_CONFIG_HOME="+filepath.Join(home, ".config"),
		"XDG_CACHE_HOME="+filepath.Join(home, ".cache"),
		"AGY_CLI_DISABLE_AUTO_UPDATE=1",
	)
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
		for _, warning := range warnings {
			a.warn("%s", warning)
		}
		if err := a.emitJSON(map[string]any{
			"created": result.Created,
			"account": result.Account,
			"written": report.Written,
		}); err != nil {
			return a.failCode("output_error", "", "could not write JSON output")
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
