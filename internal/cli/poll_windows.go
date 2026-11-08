//go:build windows

package cli

import (
	"os"
	"time"

	"golang.org/x/sys/windows"
)

func waitForReadable(file *os.File, timeout time.Duration) (bool, error) {
	if file == nil {
		return false, nil
	}
	h := windows.Handle(file.Fd())
	ms := uint32(timeout.Milliseconds())
	event, err := windows.WaitForSingleObject(h, ms)
	if err != nil {
		return false, err
	}
	return event == windows.WAIT_OBJECT_0, nil
}
