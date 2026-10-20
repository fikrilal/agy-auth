package cli

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/fikrilal/agy-auth/internal/registry"
	"golang.org/x/term"
)

var errCancelled = errors.New("cancelled")

func (a *App) chooseAccount(accounts []registry.Account, activeID, title string) (*registry.Account, error) {
	if len(accounts) == 0 {
		return nil, errors.New("no accounts available")
	}
	if !a.TTY || a.StdinFile == nil {
		return a.chooseAccountNumbered(accounts, activeID, title)
	}
	index, err := a.chooseAccountInteractive(accounts, activeID, title)
	if err != nil {
		return nil, err
	}
	account := accounts[index]
	return &account, nil
}

func (a *App) chooseAccountNumbered(accounts []registry.Account, activeID, title string) (*registry.Account, error) {
	fmt.Fprintln(a.Out, title)
	for i, account := range accounts {
		marker := " "
		if account.ID == activeID {
			marker = "*"
		}
		fmt.Fprintf(a.Out, "  %s %02d) %s\n", marker, i+1, account.DisplayName())
	}
	answer, err := a.readLine(fmt.Sprintf("Select account [1-%d]: ", len(accounts)))
	if err != nil {
		return nil, err
	}
	if answer == "" && len(accounts) == 1 {
		answer = "1"
	}
	number, err := strconv.Atoi(answer)
	if err != nil || number < 1 || number > len(accounts) {
		return nil, fmt.Errorf("invalid selection %q", answer)
	}
	account := accounts[number-1]
	return &account, nil
}

func (a *App) chooseAccountInteractive(accounts []registry.Account, activeID, title string) (int, error) {
	fd := int(a.StdinFile.Fd())
	state, err := term.MakeRaw(fd)
	if err != nil {
		return a.chooseAccountInteractiveFallback(accounts, activeID, title, err)
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

	render := func() {
		fmt.Fprint(a.Out, "\x1b[H\x1b[2J")
		fmt.Fprintf(a.Out, "%s\r\n", a.bold(title))
		fmt.Fprintf(a.Out, "%s\r\n", a.dim("up/down or j/k move · enter select · q cancel"))
		fmt.Fprint(a.Out, "\r\n")
		for i, account := range accounts {
			cursor := "  "
			if i == index {
				cursor = a.green("> ")
			}
			marker := " "
			if account.ID == activeID {
				marker = a.green("*")
			}
			line := fmt.Sprintf("%s%s %02d  %s", cursor, marker, i+1, account.DisplayName())
			if i == index {
				line = a.bold(line)
			}
			fmt.Fprintf(a.Out, "%s\r\n", line)
		}
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

func (a *App) chooseAccountInteractiveFallback(accounts []registry.Account, activeID, title string, cause error) (int, error) {
	fmt.Fprintf(a.Err, "warning: interactive terminal unavailable (%v); using numbered selection\n", cause)
	account, err := a.chooseAccountNumbered(accounts, activeID, title)
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
