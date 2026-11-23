package desktop

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

var (
	ErrNoProfile      = errors.New("no Antigravity desktop profile found")
	ErrRunningProcess = errors.New("Antigravity desktop is currently running")
	ErrBinaryNotFound = errors.New("Antigravity desktop binary not found")
)

const DesktopRetention = 5

const (
	// ScopeFileName is written by Antigravity's authstore to bind credentials
	// to the signed-in user scope.
	ScopeFileName = "credential-user-scope"

	// UpdaterIDFile is the Electron updater UUID in the Desktop user-data directory.
	// It is not an auth file: HasProfile ignores it, and a snapshot that lacks it
	// must not delete the live file.
	UpdaterIDFile = ".updaterId"

	// InstallationIDFile is the UUID used by Desktop's embedded language_server
	// under ~/.gemini/antigravity/installation_id.
	InstallationIDFile = "installation_id"

	appStorageFileName         = "app_storage.json"
	appStorageLastLoginUserKey = "agy.onboarding.lastLoginUsername"
	appStorageLastLoginGCPKey  = "agy.onboarding.lastLoginIsGcpTos"
)

// AuthFiles are the specific authentication and identity state files in
// the Electron user data directory that must be snapshotted and restored.
var AuthFiles = []string{
	"Cookies",
	"Cookies-journal",
	appStorageFileName,
	"Preferences",
	ScopeFileName,
}

// AuthDirs are the subdirectories in user data containing session state.
var AuthDirs = []string{
	"Local Storage",
}

// UpdaterIDPath is the live updater id inside a Desktop user-data directory.
func UpdaterIDPath(configDir string) string {
	return filepath.Join(configDir, UpdaterIDFile)
}

// InstallationIDPath is the live language_server installation id inside Desktop's data directory.
func InstallationIDPath(dataDir string) string {
	return filepath.Join(dataDir, InstallationIDFile)
}

// ProcessOps abstracts process detection and management for testability.
type ProcessOps struct {
	FindProcesses func() ([]int, error)
	KillProcess   func(pid int) error
	StartProcess  func(bin string) error
}

// Manager coordinates profile snapshots, restoration, and process lifecycle
// for the Antigravity Desktop application.
type Manager struct {
	ConfigDir string
	DataDir   string
	ScopeFile string
	BinPath   string
	BackupDir string
	Ops       *ProcessOps
}

// NewManager creates a desktop manager for the given configuration directory.
// When configDir is empty, default host paths (ConfigDir, DataDir, and
// platform-specific ScopeFile) are resolved. Passing an explicit configDir
// leaves DataDir and ScopeFile unset so isolated tests never touch host paths.
func NewManager(configDir string, binPath string, backupDir string) *Manager {
	useDefaultConfig := configDir == ""
	if useDefaultConfig {
		configDir = DefaultConfigDir()
	}
	if binPath == "" {
		binPath = DefaultBinaryPath()
	}
	var dataDir string
	var scopeFile string
	if useDefaultConfig {
		dataDir = DefaultDataDir()
		scopeFile = DefaultScopeFile(configDir)
	}
	return &Manager{
		ConfigDir: configDir,
		DataDir:   dataDir,
		ScopeFile: scopeFile,
		BinPath:   binPath,
		BackupDir: backupDir,
	}
}

// DefaultConfigDir resolves the Antigravity Electron userData directory.
func DefaultConfigDir() string {
	if env := os.Getenv("AGY_DESKTOP_HOME"); env != "" {
		return env
	}
	if env := os.Getenv("AGY_DESKTOP_CONFIG_DIR"); env != "" {
		return env
	}
	base, err := os.UserConfigDir()
	if err != nil {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, ".config", "Antigravity")
	}
	return filepath.Join(base, "Antigravity")
}

// DefaultDataDir resolves Antigravity Desktop's language_server data directory
// (~/.gemini/antigravity).
func DefaultDataDir() string {
	if env := os.Getenv("AGY_DESKTOP_DATA_DIR"); env != "" {
		return env
	}
	if os.Getenv("AGY_DESKTOP_HOME") != "" || os.Getenv("AGY_DESKTOP_CONFIG_DIR") != "" {
		return ""
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".gemini", "antigravity")
	}
	return filepath.Join(home, ".gemini", "antigravity")
}

// DefaultScopeFile resolves the credential-user-scope file used by Desktop's
// embedded language_server. On case-sensitive Linux filesystems, language_server
// stores it under <config>/antigravity/credential-user-scope rather than
// <config>/Antigravity/credential-user-scope.
func DefaultScopeFile(configDir string) string {
	if configDir == "" {
		return ""
	}
	if runtime.GOOS == "linux" && filepath.Base(configDir) == "Antigravity" {
		return filepath.Join(filepath.Dir(configDir), "antigravity", ScopeFileName)
	}
	return filepath.Join(configDir, ScopeFileName)
}

// DefaultBinaryPath resolves the installed Antigravity desktop executable.
func DefaultBinaryPath() string {
	if env := os.Getenv("AGY_DESKTOP_BIN"); env != "" {
		return env
	}
	switch runtime.GOOS {
	case "darwin":
		return "/Applications/Antigravity.app/Contents/MacOS/Antigravity"
	case "windows":
		local := os.Getenv("LOCALAPPDATA")
		if local == "" {
			local = `C:\Users\Default\AppData\Local`
		}
		return filepath.Join(local, "Programs", "Antigravity", "Antigravity.exe")
	default:
		candidates := []string{
			"/opt/antigravity/Antigravity-x64/antigravity",
			"/opt/antigravity/antigravity",
			"/usr/bin/antigravity",
			"/usr/local/bin/antigravity",
		}
		for _, c := range candidates {
			if _, err := os.Stat(c); err == nil {
				return c
			}
		}
		if p, err := exec.LookPath("antigravity"); err == nil {
			return p
		}
		return candidates[0]
	}
}

// HasProfile checks if the specified directory contains an active session.
func HasProfile(dir string) bool {
	if _, err := os.Stat(dir); err != nil {
		return false
	}
	for _, f := range AuthFiles {
		if _, err := os.Stat(filepath.Join(dir, f)); err == nil {
			return true
		}
	}
	for _, d := range AuthDirs {
		if _, err := os.Stat(filepath.Join(dir, d)); err == nil {
			return true
		}
	}
	return false
}

// LastLoginEmail returns the last signed-in email stored in app_storage.json,
// or empty string when unavailable.
func LastLoginEmail(configDir string) string {
	if configDir == "" {
		return ""
	}
	raw, err := os.ReadFile(filepath.Join(configDir, appStorageFileName))
	if err != nil {
		return ""
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return ""
	}
	value, _ := doc[appStorageLastLoginUserKey].(string)
	return strings.TrimSpace(value)
}

// SyncAppStorage updates onboarding last-login metadata inside an existing
// app_storage.json without disturbing other Desktop preferences or layouts.
// If app_storage.json does not exist yet, it is a no-op.
func (m *Manager) SyncAppStorage(email string, isGCP bool) error {
	if m == nil || m.ConfigDir == "" {
		return nil
	}
	path := filepath.Join(m.ConfigDir, appStorageFileName)
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read %s: %w", appStorageFileName, err)
	}
	var doc map[string]any
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		doc = make(map[string]any)
	} else if err := json.Unmarshal(trimmed, &doc); err != nil {
		return fmt.Errorf("parse %s: %w", appStorageFileName, err)
	}
	if doc == nil {
		doc = make(map[string]any)
	}
	if trimmedEmail := strings.TrimSpace(email); trimmedEmail != "" {
		doc[appStorageLastLoginUserKey] = trimmedEmail
	}
	if isGCP {
		doc[appStorageLastLoginGCPKey] = "true"
	} else {
		doc[appStorageLastLoginGCPKey] = "false"
	}
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return fmt.Errorf("encode %s: %w", appStorageFileName, err)
	}
	out = append(out, '\n')
	if err := writeFileAtomic(path, out, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", appStorageFileName, err)
	}
	return nil
}

// ClearUserScope deletes live credential-user-scope files so Desktop's embedded
// language_server recomputes the user scope for the newly activated account.
func (m *Manager) ClearUserScope() error {
	if m == nil {
		return nil
	}
	paths := m.scopeFilePaths()
	for _, p := range paths {
		if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove %s: %w", ScopeFileName, err)
		}
	}
	return nil
}

func (m *Manager) scopeFilePaths() []string {
	var paths []string
	if m.ConfigDir != "" {
		paths = append(paths, filepath.Join(m.ConfigDir, ScopeFileName))
	}
	if m.ScopeFile != "" {
		cleanScope := filepath.Clean(m.ScopeFile)
		dup := false
		for _, p := range paths {
			if filepath.Clean(p) == cleanScope {
				dup = true
				break
			}
		}
		if !dup {
			paths = append(paths, m.ScopeFile)
		}
	}
	return paths
}

// Snapshot copies the whitelisted session files from ConfigDir into destDir.
func (m *Manager) Snapshot(destDir string) error {
	if !HasProfile(m.ConfigDir) {
		return ErrNoProfile
	}
	if err := os.MkdirAll(destDir, 0o700); err != nil {
		return fmt.Errorf("create snapshot directory: %w", err)
	}
	if err := os.Chmod(destDir, 0o700); err != nil {
		return err
	}

	copiedScope := false
	for _, file := range AuthFiles {
		srcPath := filepath.Join(m.ConfigDir, file)
		if _, err := os.Stat(srcPath); err != nil {
			continue
		}
		data, err := os.ReadFile(srcPath)
		if err != nil {
			return fmt.Errorf("read %s: %w", file, err)
		}
		destPath := filepath.Join(destDir, file)
		if err := writeFileAtomic(destPath, data, 0o600); err != nil {
			return fmt.Errorf("write %s snapshot: %w", file, err)
		}
		if file == ScopeFileName {
			copiedScope = true
		}
	}
	if !copiedScope && m.ScopeFile != "" {
		if data, err := os.ReadFile(m.ScopeFile); err == nil {
			if err := writeFileAtomic(filepath.Join(destDir, ScopeFileName), data, 0o600); err != nil {
				return fmt.Errorf("write %s snapshot: %w", ScopeFileName, err)
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("read %s: %w", ScopeFileName, err)
		}
	}

	for _, dir := range AuthDirs {
		srcSub := filepath.Join(m.ConfigDir, dir)
		if _, err := os.Stat(srcSub); err != nil {
			continue
		}
		destSub := filepath.Join(destDir, dir)
		if err := copyDir(srcSub, destSub); err != nil {
			return fmt.Errorf("copy %s: %w", dir, err)
		}
	}
	if err := copyUpdaterID(m.ConfigDir, destDir); err != nil {
		return err
	}
	return nil
}

// Restore restores the session profile from srcDir into ConfigDir, backing up the current profile first.
func (m *Manager) Restore(srcDir string) error {
	if !HasProfile(srcDir) {
		return fmt.Errorf("source %s: %w", srcDir, ErrNoProfile)
	}

	if m.BackupDir != "" && HasProfile(m.ConfigDir) {
		if err := m.backupLive(); err != nil {
			return fmt.Errorf("desktop profile backup: %w", err)
		}
	}

	if err := os.MkdirAll(m.ConfigDir, 0o700); err != nil {
		return fmt.Errorf("ensure config dir: %w", err)
	}
	if err := os.Chmod(m.ConfigDir, 0o700); err != nil {
		return err
	}

	for _, file := range AuthFiles {
		srcPath := filepath.Join(srcDir, file)
		destPath := filepath.Join(m.ConfigDir, file)
		if _, err := os.Stat(srcPath); errors.Is(err, os.ErrNotExist) {
			_ = os.Remove(destPath)
			continue
		}
		data, err := os.ReadFile(srcPath)
		if err != nil {
			return fmt.Errorf("read %s: %w", file, err)
		}
		if err := writeFileAtomic(destPath, data, 0o600); err != nil {
			return fmt.Errorf("restore %s: %w", file, err)
		}
	}
	if m.ScopeFile != "" && filepath.Clean(m.ScopeFile) != filepath.Clean(filepath.Join(m.ConfigDir, ScopeFileName)) {
		srcScope := filepath.Join(srcDir, ScopeFileName)
		if data, err := os.ReadFile(srcScope); err == nil {
			if err := writeFileAtomic(m.ScopeFile, data, 0o600); err != nil {
				return fmt.Errorf("restore %s: %w", ScopeFileName, err)
			}
		} else if errors.Is(err, os.ErrNotExist) {
			_ = os.Remove(m.ScopeFile)
		} else {
			return fmt.Errorf("read %s: %w", ScopeFileName, err)
		}
	}

	for _, dir := range AuthDirs {
		srcSub := filepath.Join(srcDir, dir)
		destSub := filepath.Join(m.ConfigDir, dir)
		if _, err := os.Stat(srcSub); errors.Is(err, os.ErrNotExist) {
			_ = os.RemoveAll(destSub)
			continue
		}
		_ = os.RemoveAll(destSub)
		if err := copyDir(srcSub, destSub); err != nil {
			return fmt.Errorf("restore %s: %w", dir, err)
		}
	}
	if err := copyUpdaterID(srcDir, m.ConfigDir); err != nil {
		return err
	}
	return nil
}

// copyUpdaterID copies .updaterId when the source has one.
// A missing source leaves the destination file in place.
func copyUpdaterID(srcDir, destDir string) error {
	data, err := os.ReadFile(filepath.Join(srcDir, UpdaterIDFile))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read %s: %w", UpdaterIDFile, err)
	}
	if err := writeFileAtomic(filepath.Join(destDir, UpdaterIDFile), data, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", UpdaterIDFile, err)
	}
	return nil
}

func (m *Manager) backupLive() error {
	backupBase := filepath.Join(m.BackupDir, "desktop")
	if err := os.MkdirAll(backupBase, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(backupBase, 0o700); err != nil {
		return err
	}

	stamp := time.Now().UTC().Format("20060102T150405.000000000Z")
	target := filepath.Join(backupBase, fmt.Sprintf("desktop-%s", stamp))
	for i := 1; ; i++ {
		if _, err := os.Stat(target); errors.Is(err, os.ErrNotExist) {
			break
		}
		target = filepath.Join(backupBase, fmt.Sprintf("desktop-%s-%d", stamp, i))
	}

	if err := m.Snapshot(target); err != nil {
		return err
	}
	m.pruneBackups(backupBase, DesktopRetention)
	return nil
}

func (m *Manager) pruneBackups(dir string, keep int) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	var backups []string
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), "desktop-") {
			backups = append(backups, filepath.Join(dir, e.Name()))
		}
	}
	if len(backups) <= keep {
		return
	}
	sort.Strings(backups)
	for i := 0; i < len(backups)-keep; i++ {
		_ = os.RemoveAll(backups[i])
	}
}

// RunningPIDs returns running Antigravity Desktop PIDs with the main Electron
// process ordered first, followed by helper and embedded language_server PIDs.
func (m *Manager) RunningPIDs() ([]int, error) {
	if m.Ops != nil && m.Ops.FindProcesses != nil {
		return m.Ops.FindProcesses()
	}
	return findDefaultProcesses(m.BinPath)
}

// IsRunning reports whether Antigravity Desktop is currently running and returns its main PID.
func (m *Manager) IsRunning() (bool, int, error) {
	pids, err := m.RunningPIDs()
	if err != nil {
		return false, 0, err
	}
	if len(pids) > 0 {
		return true, pids[0], nil
	}
	return false, 0, nil
}

// Terminate sends a graceful termination signal and awaits exit of the main
// process and any remaining Desktop child processes, falling back to force kill.
func (m *Manager) Terminate(pid int, timeout time.Duration) error {
	if m.Ops != nil && m.Ops.KillProcess != nil {
		return m.Ops.KillProcess(pid)
	}
	if pid <= 0 {
		return nil
	}

	_ = signalTerminate(pid)

	deadline := time.Now().Add(timeout)
	signaled := map[int]bool{pid: true}
	var mainExitedAt time.Time

	for time.Now().Before(deadline) {
		mainAlive := isProcessAlive(pid)
		if !mainAlive {
			if m.Ops != nil {
				return nil
			}
			rem, _ := m.RunningPIDs()
			if len(rem) == 0 {
				return nil
			}
			if mainExitedAt.IsZero() {
				mainExitedAt = time.Now()
			} else if time.Since(mainExitedAt) >= 500*time.Millisecond {
				for _, childPID := range rem {
					if !signaled[childPID] {
						_ = signalTerminate(childPID)
						signaled[childPID] = true
					}
				}
			}
		}
		time.Sleep(100 * time.Millisecond)
	}

	if isProcessAlive(pid) {
		_ = signalKill(pid)
	}
	if m.Ops == nil {
		if rem, err := m.RunningPIDs(); err == nil {
			for _, childPID := range rem {
				_ = signalKill(childPID)
			}
		}
	}
	time.Sleep(150 * time.Millisecond)
	return nil
}

// Relaunch launches the Antigravity Desktop binary detached from the current process.
func (m *Manager) Relaunch() error {
	bin := m.BinPath
	if bin == "" {
		bin = DefaultBinaryPath()
	}
	if m.Ops != nil && m.Ops.StartProcess != nil {
		return m.Ops.StartProcess(bin)
	}
	if _, err := os.Stat(bin); err != nil {
		return fmt.Errorf("%w: %s", ErrBinaryNotFound, bin)
	}
	if runtime.GOOS == "darwin" {
		if bundle := macOSAppBundle(bin); bundle != "" {
			cmd := exec.Command("open", "-a", bundle)
			cmd.Stdin = nil
			cmd.Stdout = nil
			cmd.Stderr = nil
			return cmd.Run()
		}
	}
	cmd := exec.Command(bin)
	cmd.SysProcAttr = detachSysProcAttr()
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	return cmd.Start()
}

// macOSAppBundle extracts the enclosing .app bundle path when bin points to an
// executable inside <path>.app/Contents/MacOS/<binary>.
func macOSAppBundle(bin string) string {
	slash := filepath.ToSlash(filepath.Clean(bin))
	lower := strings.ToLower(slash)
	const marker = ".app/contents/macos/"
	idx := strings.Index(lower, marker)
	if idx <= 0 {
		return ""
	}
	return filepath.FromSlash(slash[:idx+len(".app")])
}

func findDefaultProcesses(binPath string) ([]int, error) {
	selfPID := os.Getpid()
	switch runtime.GOOS {
	case "darwin":
		out, err := exec.Command("ps", "-axo", "pid=,args=").Output()
		if err != nil {
			return nil, nil
		}
		return parseDarwinProcesses(string(out), binPath, selfPID), nil
	case "windows":
		image := "Antigravity.exe"
		if binPath != "" {
			if base := filepath.Base(binPath); strings.HasSuffix(strings.ToLower(base), ".exe") {
				image = base
			}
		}
		out, err := exec.Command("tasklist", "/FO", "CSV", "/NH", "/FI", "IMAGENAME eq "+image).Output()
		if err != nil {
			return nil, nil
		}
		return parseWindowsTasklist(string(out), image, selfPID), nil
	default:
		if pids, ok := findLinuxProcProcesses(selfPID); ok {
			return pids, nil
		}
		out, err := exec.Command("ps", "-axo", "pid=,args=").Output()
		if err != nil {
			return nil, nil
		}
		return parseUnixProcesses(string(out), binPath, selfPID), nil
	}
}

func findLinuxProcProcesses(selfPID int) ([]int, bool) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, false
	}
	var mainPIDs []int
	var childPIDs []int
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid <= 0 || pid == selfPID {
			continue
		}
		commBytes, err := os.ReadFile(filepath.Join("/proc", e.Name(), "comm"))
		if err != nil {
			continue
		}
		comm := strings.TrimSpace(string(commBytes))
		cmdBytes, _ := os.ReadFile(filepath.Join("/proc", e.Name(), "cmdline"))
		switch {
		case strings.EqualFold(comm, "antigravity"):
			if !bytes.Contains(cmdBytes, []byte("--type=")) {
				mainPIDs = append(mainPIDs, pid)
			} else {
				childPIDs = append(childPIDs, pid)
			}
		case strings.EqualFold(comm, "language_server"):
			cmdLower := strings.ToLower(strings.ReplaceAll(string(cmdBytes), "\x00", " "))
			if strings.Contains(cmdLower, "/antigravity/") && strings.Contains(cmdLower, "language_server") {
				childPIDs = append(childPIDs, pid)
			}
		}
	}
	return append(mainPIDs, childPIDs...), true
}

func parseDarwinProcesses(output, binPath string, selfPID int) []int {
	var mainPIDs []int
	var childPIDs []int
	lowerBin := strings.ToLower(strings.TrimSpace(binPath))
	bundlePrefix := ""
	if bundle := macOSAppBundle(binPath); bundle != "" {
		bundlePrefix = strings.ToLower(filepath.ToSlash(bundle)) + "/contents/"
	}

	for _, line := range strings.Split(output, "\n") {
		pid, args, ok := splitPSLine(line)
		if !ok || pid == selfPID {
			continue
		}
		lowerArgs := strings.ToLower(args)
		isHelperFlag := strings.Contains(lowerArgs, "--type=")

		if isDarwinMainProcess(lowerArgs, lowerBin, isHelperFlag) {
			mainPIDs = append(mainPIDs, pid)
			continue
		}
		if isDarwinChildProcess(lowerArgs, bundlePrefix) {
			childPIDs = append(childPIDs, pid)
		}
	}
	return append(mainPIDs, childPIDs...)
}

func isDarwinMainProcess(lowerArgs, lowerBin string, isHelperFlag bool) bool {
	if isHelperFlag {
		return false
	}
	if lowerBin != "" && hasExecutablePrefix(lowerArgs, lowerBin) {
		return true
	}
	const suffix = "/antigravity.app/contents/macos/antigravity"
	idx := strings.Index(lowerArgs, suffix)
	if idx < 0 {
		return false
	}
	// Ensure the matched path is the executable itself, not an argument to another command.
	if !isExecutablePathPrefix(lowerArgs[:idx]) {
		return false
	}
	end := idx + len(suffix)
	return end == len(lowerArgs) || lowerArgs[end] == ' ' || lowerArgs[end] == '\t'
}

func isDarwinChildProcess(lowerArgs, bundlePrefix string) bool {
	if bundlePrefix != "" && strings.HasPrefix(lowerArgs, bundlePrefix) {
		return true
	}
	for _, marker := range []string{
		"/antigravity.app/contents/frameworks/antigravity helper",
		"/antigravity.app/contents/resources/bin/language_server",
	} {
		idx := strings.Index(lowerArgs, marker)
		if idx >= 0 && isExecutablePathPrefix(lowerArgs[:idx]) {
			return true
		}
	}
	return false
}

// isExecutablePathPrefix reports whether prefix looks like the start of an
// absolute executable path (allowing spaces in directory names like
// "/Users/John Doe/Applications") rather than another command taking a path
// argument (like "grep /Applications/..." or "/usr/bin/vim -R /Applications/...").
func isExecutablePathPrefix(prefix string) bool {
	if prefix == "" {
		return true
	}
	if !strings.HasPrefix(prefix, "/") {
		return false
	}
	return !strings.Contains(prefix, " /") && !strings.Contains(prefix, "\t/") &&
		!strings.Contains(prefix, " -") && !strings.Contains(prefix, "\t-")
}

func parseUnixProcesses(output, binPath string, selfPID int) []int {
	var mainPIDs []int
	var childPIDs []int
	lowerBin := strings.ToLower(strings.TrimSpace(binPath))
	for _, line := range strings.Split(output, "\n") {
		pid, args, ok := splitPSLine(line)
		if !ok || pid == selfPID {
			continue
		}
		lowerArgs := strings.ToLower(args)
		execPath := firstToken(lowerArgs)
		base := filepath.Base(execPath)
		switch {
		case base == "antigravity" || (lowerBin != "" && hasExecutablePrefix(lowerArgs, lowerBin)):
			if strings.Contains(lowerArgs, "--type=") {
				childPIDs = append(childPIDs, pid)
			} else {
				mainPIDs = append(mainPIDs, pid)
			}
		case base == "language_server" && strings.Contains(execPath, "antigravity"):
			childPIDs = append(childPIDs, pid)
		}
	}
	return append(mainPIDs, childPIDs...)
}

func parseWindowsTasklist(output, image string, selfPID int) []int {
	reader := csv.NewReader(strings.NewReader(output))
	reader.FieldsPerRecord = -1
	var pids []int
	for {
		rec, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil || len(rec) < 2 {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(rec[0]), image) {
			continue
		}
		pid, err := strconv.Atoi(strings.TrimSpace(rec[1]))
		if err != nil || pid <= 0 || pid == selfPID {
			continue
		}
		pids = append(pids, pid)
	}
	return pids
}

func splitPSLine(line string) (int, string, bool) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return 0, "", false
	}
	sep := strings.IndexAny(trimmed, " \t")
	if sep <= 0 {
		return 0, "", false
	}
	pid, err := strconv.Atoi(trimmed[:sep])
	if err != nil || pid <= 0 {
		return 0, "", false
	}
	args := strings.TrimSpace(trimmed[sep+1:])
	if args == "" {
		return 0, "", false
	}
	return pid, args, true
}

func hasExecutablePrefix(lowerArgs, lowerBin string) bool {
	if !strings.HasPrefix(lowerArgs, lowerBin) {
		return false
	}
	if len(lowerArgs) == len(lowerBin) {
		return true
	}
	next := lowerArgs[len(lowerBin)]
	return next == ' ' || next == '\t'
}

func firstToken(s string) string {
	if idx := strings.IndexAny(s, " \t"); idx >= 0 {
		return s[:idx]
	}
	return s
}

func copyDir(src, dest string) error {
	if err := os.MkdirAll(dest, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(dest, 0o700); err != nil {
		return err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		destPath := filepath.Join(dest, entry.Name())
		if entry.IsDir() {
			if err := copyDir(srcPath, destPath); err != nil {
				return err
			}
		} else {
			data, err := os.ReadFile(srcPath)
			if err != nil {
				return err
			}
			if err := writeFileAtomic(destPath, data, 0o600); err != nil {
				return err
			}
		}
	}
	return nil
}

func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return err
	}
	if existing, err := os.ReadFile(path); err == nil && bytes.Equal(existing, data) {
		return os.Chmod(path, mode)
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".desktop-tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() {
		_ = os.Remove(tmpName)
	}()

	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := io.Copy(tmp, bytes.NewReader(data)); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}
