package cli

import (
	"fmt"
	"os"
	"sync"
	"time"
)

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// startSpinner displays an animated spinner with msg if stdout is an interactive terminal.
// It returns a stop function that erases the spinner line and restores the cursor.
// If the operation finishes faster than 100ms, the spinner is not displayed.
func (a *App) startSpinner(msg string) func() {
	if !a.OutTTY || a.JSON || a.InTUI || a.NoColor || os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
		return func() {}
	}

	done := make(chan struct{})
	stopped := make(chan struct{})
	var once sync.Once

	go func() {
		defer close(stopped)

		// Brief delay to avoid flashing the spinner on fast / cached responses.
		select {
		case <-done:
			return
		case <-time.After(100 * time.Millisecond):
		}

		// Hide cursor while spinner is active.
		fmt.Fprint(a.Out, "\033[?25l")
		defer func() {
			// Clear line and restore cursor.
			fmt.Fprint(a.Out, "\r\033[K\033[?25h")
		}()

		ticker := time.NewTicker(80 * time.Millisecond)
		defer ticker.Stop()

		frame := 0
		for {
			symbol := a.cyan(spinnerFrames[frame%len(spinnerFrames)])
			text := a.dim(msg)
			fmt.Fprintf(a.Out, "\r%s %s", symbol, text)
			frame++

			select {
			case <-ticker.C:
			case <-done:
				return
			}
		}
	}()

	return func() {
		once.Do(func() {
			close(done)
			<-stopped
		})
	}
}
