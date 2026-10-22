package cli

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/fikrilal/agy-auth/internal/agy"
	"golang.org/x/term"
)

const liveDefaultInterval = 60 * time.Second

type liveMode int

const (
	liveModeList liveMode = iota
	liveModeSwitch
	liveModeRemove
)

func (m liveMode) command() string {
	switch m {
	case liveModeSwitch:
		return "switch"
	case liveModeRemove:
		return "remove"
	default:
		return "list"
	}
}

func (m liveMode) title() string {
	switch m {
	case liveModeSwitch:
		return "Select an account to activate"
	case liveModeRemove:
		return "Select accounts to remove"
	default:
		return "Accounts (live)"
	}
}

func (m liveMode) help() string {
	switch m {
	case liveModeSwitch:
		return "up/down move · pgup/pgdn page · enter switch · q quit"
	case liveModeRemove:
		return "up/down move · space toggle · enter remove · q quit"
	default:
		return "up/down move · pgup/pgdn page · q quit"
	}
}

type liveState struct {
	rows        []accountRow
	selectedID  string
	checked     map[string]bool
	offset      int
	action      string
	source      string
	refreshErr  string
	refreshing  bool
	lastRefresh time.Time
	nextRefresh time.Time
	interval    time.Duration
	gen         int
}

type liveRefreshResult struct {
	gen    int
	rows   []accountRow
	source string
	err    error
}

func liveVisibleRows(height int) int {
	visible := height - 6
	if visible < 3 {
		visible = 3
	}
	return visible
}

func (s *liveState) selectedIndex() int {
	if len(s.rows) == 0 {
		return 0
	}
	for i := range s.rows {
		if s.rows[i].ID == s.selectedID {
			return i
		}
	}
	return 0
}

func (s *liveState) selectedRow() *accountRow {
	if len(s.rows) == 0 {
		return nil
	}
	index := s.selectedIndex()
	return &s.rows[index]
}

func (s *liveState) move(delta int) {
	if len(s.rows) == 0 {
		return
	}
	index := s.selectedIndex() + delta
	if index < 0 {
		index = 0
	}
	if index >= len(s.rows) {
		index = len(s.rows) - 1
	}
	s.selectedID = s.rows[index].ID
}

func (s *liveState) moveTo(index int) {
	if len(s.rows) == 0 {
		return
	}
	if index < 0 {
		index = 0
	}
	if index >= len(s.rows) {
		index = len(s.rows) - 1
	}
	s.selectedID = s.rows[index].ID
}

func (s *liveState) page(delta, pageSize int) {
	if pageSize < 1 {
		pageSize = 1
	}
	s.move(delta * pageSize)
}

func (s *liveState) toggleChecked() {
	row := s.selectedRow()
	if row == nil || row.ID == "" {
		return
	}
	if s.checked == nil {
		s.checked = map[string]bool{}
	}
	if s.checked[row.ID] {
		delete(s.checked, row.ID)
	} else {
		s.checked[row.ID] = true
	}
}

func (s *liveState) checkedIDs() []string {
	if len(s.checked) == 0 {
		return nil
	}
	result := make([]string, 0, len(s.checked))
	for _, row := range s.rows {
		if s.checked[row.ID] {
			result = append(result, row.ID)
		}
	}
	return result
}

func (s *liveState) viewport(height int) (int, int) {
	if len(s.rows) == 0 {
		return 0, 0
	}
	visible := liveVisibleRows(height)
	index := s.selectedIndex()
	if index < s.offset {
		s.offset = index
	}
	if index >= s.offset+visible {
		s.offset = index - visible + 1
	}
	maxOffset := len(s.rows) - visible
	if maxOffset < 0 {
		maxOffset = 0
	}
	if s.offset > maxOffset {
		s.offset = maxOffset
	}
	if s.offset < 0 {
		s.offset = 0
	}
	end := s.offset + visible
	if end > len(s.rows) {
		end = len(s.rows)
	}
	return s.offset, end
}

func (s *liveState) apply(rows []accountRow, source string, err error, now time.Time, interval time.Duration) {
	s.refreshing = false
	s.lastRefresh = now
	s.nextRefresh = now.Add(interval)
	if err != nil {
		s.refreshErr = "refresh failed"
		return
	}
	s.refreshErr = ""
	s.source = source
	s.rows = rows
	if s.checked == nil {
		s.checked = map[string]bool{}
	}
	valid := make(map[string]bool, len(rows))
	for _, row := range rows {
		if row.ID != "" {
			valid[row.ID] = true
		}
	}
	for id := range s.checked {
		if !valid[id] {
			delete(s.checked, id)
		}
	}
	if s.selectedID == "" || !valid[s.selectedID] {
		if len(rows) > 0 {
			s.selectedID = rows[0].ID
		} else {
			s.selectedID = ""
		}
	}
}

func (s *liveState) statusLine(now time.Time) string {
	source := s.source
	if source == "" {
		source = "none"
	}
	refresh := "Refresh due"
	if s.refreshing {
		refresh = "Refresh running"
	} else if !s.nextRefresh.IsZero() {
		remaining := int(s.nextRefresh.Sub(now).Seconds())
		if remaining < 0 {
			remaining = 0
		}
		refresh = fmt.Sprintf("Refresh in %ds", remaining)
	}
	line := fmt.Sprintf("Live refresh: %s | %s", source, refresh)
	if s.refreshErr != "" {
		line += " | Error: " + s.refreshErr
	}
	return line
}

func (a *App) loadLiveRows(mode liveMode) ([]accountRow, string, error) {
	reg, err := a.Store.Load()
	if err != nil {
		return nil, "", err
	}
	source := ""
	read, readErr := a.Storage.Read()
	if readErr == nil {
		source = string(read.Source)
	} else if !errors.Is(readErr, agy.ErrNoCredentials) {
		source = "unavailable"
	}

	activeID := ""
	if readErr == nil {
		if matched, _ := reg.Match(read.Raw); matched != nil {
			activeID = matched.ID
		}
	}
	rows := a.pickerRows(reg.Accounts, activeID, accountNumbers(reg))
	if mode == liveModeList && readErr == nil && activeID == "" {
		if tf, err := agy.ParseTokenFile(read.Raw); err == nil {
			row := accountRow{
				Active:     true,
				Stored:     false,
				Email:      tf.Email(),
				AuthMethod: tf.AuthMethod,
				Source:     source,
			}
			if expiry, ok := tf.ExpiryTime(); ok {
				value := expiry
				row.ExpiresAt = &value
				row.Expired = !expiry.After(a.now())
			}
			rows = append([]accountRow{row}, rows...)
		}
	}
	return rows, source, nil
}

func (a *App) runLive(mode liveMode, logout bool) int {
	command := mode.command()
	if a.JSON {
		return a.usageFor(command, "--live cannot be used with --json")
	}
	if !a.TTY || !a.OutTTY || a.StdinFile == nil || a.StdoutFile == nil {
		return a.usageFor(command, "--live requires an interactive terminal")
	}

	fd := int(a.StdinFile.Fd())
	rawState, err := term.MakeRaw(fd)
	if err != nil {
		return a.usageFor(command, "interactive terminal unavailable for --live")
	}
	defer term.Restore(fd, rawState)
	fmt.Fprint(a.Out, "\x1b[?1049h\x1b[?25l")
	defer fmt.Fprint(a.Out, "\x1b[?25h\x1b[?1049l")

	state := &liveState{checked: map[string]bool{}, interval: liveDefaultInterval}
	rows, source, loadErr := a.loadLiveRows(mode)
	state.apply(rows, source, loadErr, a.now(), state.interval)
	state.gen = 1

	keys := make(chan []byte, 8)
	go readLiveKeys(a.StdinFile, keys)
	refreshCh := make(chan liveRefreshResult, 4)
	startRefresh := func() {
		if state.refreshing {
			return
		}
		state.refreshing = true
		gen := state.gen
		go func() {
			rows, source, err := a.loadLiveRows(mode)
			refreshCh <- liveRefreshResult{gen: gen, rows: rows, source: source, err: err}
		}()
	}

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	a.renderLiveFrame(state, mode)

	for {
		select {
		case key, ok := <-keys:
			if !ok {
				return 0
			}
			quit, refresh := a.handleLiveKey(state, key, mode, logout)
			if quit {
				return 0
			}
			if refresh {
				state.gen++
				state.refreshing = false
				startRefresh()
			}
			a.renderLiveFrame(state, mode)
		case result := <-refreshCh:
			if result.gen == state.gen {
				state.apply(result.rows, result.source, result.err, a.now(), state.interval)
			}
			a.renderLiveFrame(state, mode)
		case now := <-ticker.C:
			if !state.refreshing && (state.nextRefresh.IsZero() || !now.Before(state.nextRefresh)) {
				state.gen++
				startRefresh()
			}
			a.renderLiveFrame(state, mode)
		}
	}
}

func readLiveKeys(file *os.File, keys chan<- []byte) {
	defer close(keys)
	buf := make([]byte, 16)
	for {
		n, err := file.Read(buf)
		if err != nil {
			return
		}
		if n == 0 {
			continue
		}
		key := make([]byte, n)
		copy(key, buf[:n])
		keys <- key
	}
}

func (a *App) handleLiveKey(state *liveState, key []byte, mode liveMode, logout bool) (bool, bool) {
	if len(key) == 0 {
		return false, false
	}
	switch {
	case key[0] == 3 || key[0] == 'q':
		return true, false
	case key[0] == 27:
		if len(key) == 1 {
			return true, false
		}
		if len(key) >= 3 && (key[1] == '[' || key[1] == 'O') {
			switch key[2] {
			case 'A':
				state.move(-1)
			case 'B':
				state.move(1)
			case 'H':
				state.moveTo(0)
			case 'F':
				state.moveTo(len(state.rows) - 1)
			case '5', '6':
				if len(key) >= 4 && key[3] == '~' {
					delta := -1
					if key[2] == '6' {
						delta = 1
					}
					state.page(delta, liveVisibleRows(a.tableHeight()))
				}
			case '1', '4':
				if len(key) >= 4 && key[3] == '~' {
					if key[2] == '1' {
						state.moveTo(0)
					} else {
						state.moveTo(len(state.rows) - 1)
					}
				}
			}
		}
	case key[0] == 'k':
		state.move(-1)
	case key[0] == 'j':
		state.move(1)
	case key[0] == 'g':
		state.moveTo(0)
	case key[0] == 'G':
		state.moveTo(len(state.rows) - 1)
	case key[0] == ' ' && mode == liveModeRemove:
		state.toggleChecked()
	case key[0] == '\r' || key[0] == '\n':
		switch mode {
		case liveModeSwitch:
			if row := state.selectedRow(); row != nil {
				state.action = a.liveSwitch(row.ID, row.displayName())
				return false, true
			}
		case liveModeRemove:
			ids := state.checkedIDs()
			if len(ids) == 0 {
				state.action = "No accounts checked."
				return false, false
			}
			state.action = a.liveRemove(ids, logout)
			return false, true
		}
	}
	return false, false
}

func (a *App) liveChild() *App {
	child := *a
	var out, errOut bytes.Buffer
	child.In = strings.NewReader("")
	child.Out = &out
	child.Err = &errOut
	child.JSON = true
	child.TTY = false
	child.OutTTY = false
	child.reader = nil
	return &child
}

func (a *App) liveSwitch(id, label string) string {
	child := a.liveChild()
	if code := child.Run([]string{"switch", id, "--json"}); code != 0 {
		return "Switch failed."
	}
	return fmt.Sprintf("Switched to %s.", label)
}

func (a *App) liveRemove(ids []string, logout bool) string {
	args := []string{"remove"}
	args = append(args, ids...)
	if logout {
		args = append(args, "--logout")
	}
	args = append(args, "--yes", "--json")
	child := a.liveChild()
	if code := child.Run(args); code != 0 {
		return "Remove failed."
	}
	return fmt.Sprintf("Removed %d account(s).", len(ids))
}

func (a *App) renderLiveFrame(state *liveState, mode liveMode) {
	width := a.tableWidth()
	now := a.now()
	fmt.Fprint(a.Out, "\x1b[H\x1b[2J")
	fmt.Fprintf(a.Out, "%s\r\n", a.bold(truncatePlain(mode.title(), width)))
	fmt.Fprint(a.Out, "\r\n")
	if len(state.rows) == 0 {
		fmt.Fprint(a.Out, "No accounts stored.\r\n")
	} else {
		start, end := state.viewport(a.tableHeight())
		selected := state.selectedIndex()
		allRows := make([]tableRow, len(state.rows))
		for i, row := range state.rows {
			var checked *bool
			if mode == liveModeRemove {
				value := state.checked[row.ID]
				checked = &value
			}
			allRows[i] = a.accountTableRow(row, now, checked, false)
		}
		visible := append([]tableRow(nil), allRows[start:end]...)
		for i := range visible {
			visible[i].Highlight = start+i == selected
		}
		a.renderTableRows(a.Out, width, accountTableColumns, allRows, visible, "\r\n")
	}
	fmt.Fprintf(a.Out, "%s\r\n", truncatePlain(state.statusLine(now), width))
	if mode != liveModeList {
		action := state.action
		if action == "" {
			action = " "
		}
		fmt.Fprintf(a.Out, "%s\r\n", truncatePlain(action, width))
	}
	fmt.Fprintf(a.Out, "%s\r\n", truncatePlain(mode.help(), width))
}
