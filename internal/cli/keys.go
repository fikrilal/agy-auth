package cli

import (
	"os"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	escapeSequenceTimeout = 50 * time.Millisecond
	typeaheadTimeout      = 800 * time.Millisecond
)

type keyKind int

const (
	keyUnknown keyKind = iota
	keyRune
	keyUp
	keyDown
	keyLeft
	keyRight
	keyPageUp
	keyPageDown
	keyHome
	keyEnd
	keyEnter
	keyEsc
	keyBackspace
	keySpace
	keyTab
	keyCtrlC
)

type keyEvent struct {
	kind keyKind
	r    rune
}

func (k keyEvent) digit() (rune, bool) {
	if k.kind != keyRune || k.r < '0' || k.r > '9' {
		return 0, false
	}
	return k.r, true
}

func readKeyEvents(file *os.File, keys chan<- keyEvent) {
	defer close(keys)
	chunks := make(chan []byte, 4)
	go func() {
		defer close(chunks)
		buf := make([]byte, 64)
		for {
			n, err := file.Read(buf)
			if n > 0 {
				chunk := make([]byte, n)
				copy(chunk, buf[:n])
				chunks <- chunk
			}
			if err != nil {
				return
			}
		}
	}()

	var pending []byte
	var timer *time.Timer
	var timeout <-chan time.Time
	stopTimer := func() {
		if timer != nil {
			timer.Stop()
			timer = nil
		}
		timeout = nil
	}
	setTimer := func() {
		stopTimer()
		timer = time.NewTimer(escapeSequenceTimeout)
		timeout = timer.C
	}
	defer stopTimer()

	for {
		select {
		case chunk, ok := <-chunks:
			if !ok {
				if len(pending) > 0 && pending[0] == 27 {
					keys <- keyEvent{kind: keyEsc}
				}
				return
			}
			pending = append(pending, chunk...)
		case <-timeout:
			timeout = nil
			if len(pending) > 0 && pending[0] == 27 {
				pending = pending[1:]
				keys <- keyEvent{kind: keyEsc}
			}
		}

		for len(pending) > 0 {
			event, consumed, needMore := decodeKey(pending)
			if needMore {
				break
			}
			pending = pending[consumed:]
			keys <- event
		}
		if len(pending) > 0 && pending[0] == 27 {
			setTimer()
		} else {
			stopTimer()
		}
	}
}

func decodeKey(buf []byte) (keyEvent, int, bool) {
	if len(buf) == 0 {
		return keyEvent{}, 0, true
	}
	if buf[0] != 27 {
		return decodePlainKey(buf)
	}
	if len(buf) == 1 {
		return keyEvent{}, 0, true
	}
	if buf[1] != '[' && buf[1] != 'O' {
		return keyEvent{kind: keyEsc}, 1, false
	}
	if len(buf) < 3 {
		return keyEvent{}, 0, true
	}
	if buf[1] == 'O' {
		event := keyEvent{kind: keyUnknown}
		switch buf[2] {
		case 'A':
			event.kind = keyUp
		case 'B':
			event.kind = keyDown
		case 'C':
			event.kind = keyRight
		case 'D':
			event.kind = keyLeft
		case 'H':
			event.kind = keyHome
		case 'F':
			event.kind = keyEnd
		}
		return event, 3, false
	}

	final := -1
	for i := 2; i < len(buf); i++ {
		if buf[i] >= 0x40 && buf[i] <= 0x7e {
			final = i
			break
		}
	}
	if final < 0 {
		if len(buf) > 32 {
			return keyEvent{kind: keyUnknown}, 32, false
		}
		return keyEvent{}, 0, true
	}
	params := string(buf[2:final])
	event := keyEvent{kind: keyUnknown}
	switch buf[final] {
	case 'A':
		event.kind = keyUp
	case 'B':
		event.kind = keyDown
	case 'C':
		event.kind = keyRight
	case 'D':
		event.kind = keyLeft
	case 'H':
		event.kind = keyHome
	case 'F':
		event.kind = keyEnd
	case '~':
		event = decodeTildeKey(params)
	case 'u':
		event = decodeCSIuKey(params)
	}
	return event, final + 1, false
}

func decodePlainKey(buf []byte) (keyEvent, int, bool) {
	switch buf[0] {
	case 3:
		return keyEvent{kind: keyCtrlC}, 1, false
	case 9:
		return keyEvent{kind: keyTab}, 1, false
	case 10, 13:
		return keyEvent{kind: keyEnter}, 1, false
	case 32:
		return keyEvent{kind: keySpace}, 1, false
	case 8, 127:
		return keyEvent{kind: keyBackspace}, 1, false
	}
	if buf[0] < 0x20 {
		return keyEvent{kind: keyUnknown}, 1, false
	}
	if !utf8.FullRune(buf) {
		return keyEvent{}, 0, true
	}
	r, size := utf8.DecodeRune(buf)
	if r == utf8.RuneError && size == 1 {
		return keyEvent{kind: keyUnknown}, 1, false
	}
	return keyEvent{kind: keyRune, r: r}, size, false
}

func decodeTildeKey(params string) keyEvent {
	first := strings.SplitN(params, ";", 2)[0]
	number, err := strconv.Atoi(first)
	if err != nil {
		return keyEvent{kind: keyUnknown}
	}
	switch number {
	case 1, 7:
		return keyEvent{kind: keyHome}
	case 4, 8:
		return keyEvent{kind: keyEnd}
	case 5:
		return keyEvent{kind: keyPageUp}
	case 6:
		return keyEvent{kind: keyPageDown}
	default:
		return keyEvent{kind: keyUnknown}
	}
}

func decodeCSIuKey(params string) keyEvent {
	first := strings.SplitN(params, ";", 2)[0]
	number, err := strconv.Atoi(first)
	if err != nil {
		return keyEvent{kind: keyUnknown}
	}
	switch number {
	case 27:
		return keyEvent{kind: keyEsc}
	case 9:
		return keyEvent{kind: keyTab}
	case 13:
		return keyEvent{kind: keyEnter}
	case 32:
		return keyEvent{kind: keySpace}
	case 127:
		return keyEvent{kind: keyBackspace}
	case 57350:
		return keyEvent{kind: keyLeft}
	case 57351:
		return keyEvent{kind: keyRight}
	case 57352:
		return keyEvent{kind: keyUp}
	case 57353:
		return keyEvent{kind: keyDown}
	case 57354:
		return keyEvent{kind: keyPageUp}
	case 57355:
		return keyEvent{kind: keyPageDown}
	case 57356:
		return keyEvent{kind: keyHome}
	case 57357:
		return keyEvent{kind: keyEnd}
	default:
		return keyEvent{kind: keyUnknown}
	}
}
