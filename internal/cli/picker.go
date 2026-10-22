package cli

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/fikrilal/agy-auth/internal/agy"
	"github.com/fikrilal/agy-auth/internal/registry"
	"golang.org/x/term"
)

var errCancelled = errors.New("cancelled")

func (a *App) chooseAccount(accounts []registry.Account, activeID, title string, numbers map[string]int) (*registry.Account, error) {
	if len(accounts) == 0 {
		return nil, errors.New("no accounts available")
	}
	if !a.TTY || a.StdinFile == nil {
		return a.chooseAccountNumbered(accounts, activeID, title, numbers)
	}
	index, err := a.chooseAccountInteractive(accounts, activeID, title, numbers)
	if err != nil {
		return nil, err
	}
	account := accounts[index]
	return &account, nil
}

func accountNumbers(reg *registry.Registry) map[string]int {
	numbers := make(map[string]int, len(reg.Accounts))
	for i, account := range reg.Accounts {
		numbers[account.ID] = i + 1
	}
	return numbers
}

func (a *App) pickerRows(accounts []registry.Account, activeID string, numbers map[string]int) []accountRow {
	now := a.now()
	rows := make([]accountRow, len(accounts))
	for i, account := range accounts {
		number := i + 1
		if numbers != nil {
			if value, ok := numbers[account.ID]; ok {
				number = value
			}
		}
		row := accountRow{
			Number:     number,
			ID:         account.ID,
			Active:     account.ID == activeID,
			Stored:     true,
			Email:      account.Email,
			Alias:      account.Alias,
			AuthMethod: account.AuthMethod,
			LastUsedAt: timePtr(account.LastUsedAt),
		}
		if raw, err := a.Store.LoadToken(account.ID); err == nil {
			if tf, err := agy.ParseTokenFile(raw); err == nil {
				if expiry, ok := tf.ExpiryTime(); ok {
					value := expiry
					row.ExpiresAt = &value
					row.Expired = !expiry.After(now)
				}
				if row.Email == "" {
					row.Email = tf.Email()
				}
				if row.AuthMethod == "" {
					row.AuthMethod = tf.AuthMethod
				}
			}
		}
		rows[i] = row
	}
	return rows
}

func (a *App) chooseAccountNumbered(accounts []registry.Account, activeID, title string, numbers map[string]int) (*registry.Account, error) {
	rows := a.pickerRows(accounts, activeID, numbers)
	fmt.Fprintln(a.Out, title)
	a.renderAccountRows(rows, a.now())
	answer, err := a.readLine("Select account number: ")
	if err != nil {
		return nil, err
	}
	if answer == "" && len(accounts) == 1 {
		answer = strconv.Itoa(rows[0].Number)
	}
	number, err := strconv.Atoi(answer)
	if err == nil {
		for i, row := range rows {
			if row.Number == number {
				account := accounts[i]
				return &account, nil
			}
		}
	}
	return nil, fmt.Errorf("invalid selection %q", answer)
}

func (a *App) chooseAccountInteractive(accounts []registry.Account, activeID, title string, numbers map[string]int) (int, error) {
	fd := int(a.StdinFile.Fd())
	state, err := term.MakeRaw(fd)
	if err != nil {
		return a.chooseAccountInteractiveFallback(accounts, activeID, title, numbers, err)
	}
	defer term.Restore(fd, state)

	fmt.Fprint(a.Out, "\x1b[?1049h\x1b[?25l")
	defer fmt.Fprint(a.Out, "\x1b[?25h\x1b[?1049l")

	index := 0
	for i, account := range accounts {
		if account.ID == activeID {
			index = i
			break
		}
	}
	rows := a.pickerRows(accounts, activeID, numbers)
	now := a.now()

	render := func() {
		fmt.Fprint(a.Out, "\x1b[H\x1b[2J")
		fmt.Fprintf(a.Out, "%s\r\n", a.bold(title))
		fmt.Fprintf(a.Out, "%s\r\n", a.dim("up/down or j/k move · enter select · q cancel"))
		fmt.Fprint(a.Out, "\r\n")
		a.renderAccountTable(rows, now, index, "\r\n")
	}

	render()
	buf := make([]byte, 16)
	for {
		n, err := a.StdinFile.Read(buf)
		if err != nil {
			return -1, err
		}
		if n == 0 {
			continue
		}
		key := buf[:n]
		switch {
		case key[0] == 3 || key[0] == 'q':
			return -1, errCancelled
		case key[0] == '\r' || key[0] == '\n':
			return index, nil
		case key[0] == 27:
			if n == 1 {
				return -1, errCancelled
			}
			if n >= 3 && (key[1] == '[' || key[1] == 'O') {
				switch key[2] {
				case 'A':
					if index > 0 {
						index--
					}
				case 'B':
					if index < len(accounts)-1 {
						index++
					}
				case 'H':
					index = 0
				case 'F':
					index = len(accounts) - 1
				}
			}
		case key[0] == 'k':
			if index > 0 {
				index--
			}
		case key[0] == 'j':
			if index < len(accounts)-1 {
				index++
			}
		case key[0] == 'g':
			index = 0
		case key[0] == 'G':
			index = len(accounts) - 1
		default:
			continue
		}
		render()
	}
}

func (a *App) chooseAccountInteractiveFallback(accounts []registry.Account, activeID, title string, numbers map[string]int, cause error) (int, error) {
	fmt.Fprintf(a.Err, "warning: interactive terminal unavailable (%v); using numbered selection\n", cause)
	account, err := a.chooseAccountNumbered(accounts, activeID, title, numbers)
	if err != nil {
		return -1, err
	}
	for i := range accounts {
		if accounts[i].ID == account.ID {
			return i, nil
		}
	}
	return -1, errors.New("selected account disappeared")
}

func selectionSummary(accounts []registry.Account) string {
	names := make([]string, 0, len(accounts))
	for _, account := range accounts {
		names = append(names, account.DisplayName())
	}
	return strings.Join(names, ", ")
}
