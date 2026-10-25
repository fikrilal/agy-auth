package desktop

import (
	"bytes"
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
	"syscall"
	"time"
)

var (
	ErrNoProfile      = errors.New("no Antigravity desktop profile found")
	ErrRunningProcess = errors.New("Antigravity desktop is currently running")
	ErrBinaryNotFound = errors.New("Antigravity desktop binary not found")
)

const DesktopRetention = 5

// AuthFiles are the specific authentication and identity state files in
// the Electron user data directory that must be snapshotted and restored.
var AuthFiles = []string{
	"Cookies",
	"Cookies-journal",
	"app_storage.json",
	"Preferences",
}

// AuthDirs are the subdirectories in user data containing session state.
var AuthDirs = []string{
	"Local Storage",
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
	BinPath   string
	BackupDir string
	Ops       *ProcessOps
}

// NewManager creates a desktop manager for the given configuration directory.
func NewManager(configDir string, binPath string, backupDir string) *Manager {
	if configDir == "" {
		configDir = DefaultConfigDir()
	}
	if binPath == "" {
		binPath = DefaultBinaryPath()
	}
	return &Manager{
		ConfigDir: configDir,
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

// IsRunning reports whether Antigravity Desktop is currently running and returns its main PID.
func (m *Manager) IsRunning() (bool, int, error) {
	if m.Ops != nil && m.Ops.FindProcesses != nil {
		pids, err := m.Ops.FindProcesses()
		if err != nil {
			return false, 0, err
		}
		if len(pids) > 0 {
			return true, pids[0], nil
		}
		return false, 0, nil
	}
	return findDefaultProcesses()
}

// Terminate sends a graceful termination signal and awaits exit, falling back to force kill.
func (m *Manager) Terminate(pid int, timeout time.Duration) error {
	if m.Ops != nil && m.Ops.KillProcess != nil {
		return m.Ops.KillProcess(pid)
	}
	if pid <= 0 {
		return nil
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return nil
	}

	_ = proc.Signal(syscall.SIGTERM)

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !isProcessAlive(pid) {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}

	_ = proc.Signal(syscall.SIGKILL)
	time.Sleep(100 * time.Millisecond)
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
	cmd := exec.Command(bin)
	cmd.SysProcAttr = detachSysProcAttr()
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	return cmd.Start()
}

func findDefaultProcesses() (bool, int, error) {
	if runtime.GOOS == "linux" {
		entries, err := os.ReadDir("/proc")
		if err == nil {
			var mainPID int
			found := false
			for _, e := range entries {
				if !e.IsDir() {
					continue
				}
				pid, err := strconv.Atoi(e.Name())
				if err != nil {
					continue
				}
				commPath := filepath.Join("/proc", e.Name(), "comm")
				commBytes, err := os.ReadFile(commPath)
				if err != nil {
					continue
				}
				comm := strings.TrimSpace(string(commBytes))
				if strings.EqualFold(comm, "antigravity") {
					found = true
					cmdPath := filepath.Join("/proc", e.Name(), "cmdline")
					cmdBytes, err := os.ReadFile(cmdPath)
					if err == nil && !bytes.Contains(cmdBytes, []byte("--type=")) {
						mainPID = pid
						break
					}
					if mainPID == 0 {
						mainPID = pid
					}
				}
			}
			if found {
				return true, mainPID, nil
			}
			return false, 0, nil
		}
	}

	out, err := exec.Command("pgrep", "-f", "antigravity").Output()
	if err == nil && len(strings.TrimSpace(string(out))) > 0 {
		lines := strings.Fields(string(out))
		if len(lines) > 0 {
			if pid, err := strconv.Atoi(lines[0]); err == nil {
				return true, pid, nil
			}
		}
		return true, 0, nil
	}
	return false, 0, nil
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
