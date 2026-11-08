//go:build !windows

package cli

import (
	"os"
	"time"

	"golang.org/x/sys/unix"
)

func waitForReadable(file *os.File, timeout time.Duration) (bool, error) {
	if file == nil {
		return false, nil
	}
	fd := int32(file.Fd())
	ms := int(timeout.Milliseconds())
	if ms < 0 {
		ms = 0
	}
	pfd := []unix.PollFd{{Fd: fd, Events: unix.POLLIN}}
	for {
		n, err := unix.Poll(pfd, ms)
		if err != nil {
			if err == unix.EINTR {
				continue
			}
			return false, err
		}
		if n == 0 {
			return false, nil
		}
		return pfd[0].Revents&(unix.POLLIN|unix.POLLHUP|unix.POLLERR) != 0, nil
	}
}
