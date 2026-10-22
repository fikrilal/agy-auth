package cli

import (
	"bytes"
	"os"
	"testing"
	"time"
)

func TestDecodeKey(t *testing.T) {
	cases := []struct {
		name     string
		input    string
		want     keyKind
		r        rune
		consumed int
		needMore bool
	}{
		{"letter", "j", keyRune, 'j', 1, false},
		{"digit", "7", keyRune, '7', 1, false},
		{"ctrl-c", "\x03", keyCtrlC, 0, 1, false},
		{"enter", "\r", keyEnter, 0, 1, false},
		{"space", " ", keySpace, 0, 1, false},
		{"backspace", "\x7f", keyBackspace, 0, 1, false},
		{"up", "\x1b[A", keyUp, 0, 3, false},
		{"modified down", "\x1b[1;2B", keyDown, 0, 6, false},
		{"ss3 left", "\x1bOD", keyLeft, 0, 3, false},
		{"page up", "\x1b[5~", keyPageUp, 0, 4, false},
		{"page down", "\x1b[6~", keyPageDown, 0, 4, false},
		{"home", "\x1b[1~", keyHome, 0, 4, false},
		{"end", "\x1b[4~", keyEnd, 0, 4, false},
		{"csi-u up", "\x1b[57352u", keyUp, 0, 8, false},
		{"csi-u esc", "\x1b[27u", keyEsc, 0, 5, false},
		{"mouse ignored", "\x1b[<0;1;1M", keyUnknown, 0, 9, false},
		{"lone esc waits", "\x1b", 0, 0, 0, true},
		{"incomplete csi waits", "\x1b[", 0, 0, 0, true},
		{"multibyte", "é", keyRune, 'é', 2, false},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			event, consumed, needMore := decodeKey([]byte(test.input))
			if needMore != test.needMore || consumed != test.consumed {
				t.Fatalf("needMore=%v consumed=%d", needMore, consumed)
			}
			if test.needMore {
				return
			}
			if event.kind != test.want || event.r != test.r {
				t.Errorf("event = %+v, want kind=%v rune=%q", event, test.want, test.r)
			}
		})
	}
}

func TestReadKeyEvents(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	keys := make(chan keyEvent, 8)
	go readKeyEvents(reader, keys)
	if _, err := writer.Write([]byte("\x1b[A\x1b[57353u7")); err != nil {
		t.Fatal(err)
	}
	writer.Close()

	var events []keyEvent
	timeout := time.After(2 * time.Second)
	for len(events) < 3 {
		select {
		case event := <-keys:
			events = append(events, event)
		case <-timeout:
			t.Fatalf("timed out with %d events", len(events))
		}
	}
	if events[0].kind != keyUp || events[1].kind != keyDown {
		t.Errorf("arrows = %+v", events[:2])
	}
	if events[2].kind != keyRune || events[2].r != '7' {
		t.Errorf("digit = %+v", events[2])
	}
}

func TestPaintLiveFrameIncremental(t *testing.T) {
	app := &App{Out: &bytes.Buffer{}}
	state := &liveState{}
	app.paintLiveFrame(state, []string{"one", "two"})
	first := app.Out.(*bytes.Buffer).String()
	if !bytes.Contains([]byte(first), []byte("\x1b[2J")) {
		t.Errorf("first frame did not clear: %q", first)
	}
	app.Out = &bytes.Buffer{}
	app.paintLiveFrame(state, []string{"one", "changed"})
	second := app.Out.(*bytes.Buffer).String()
	if bytes.Contains([]byte(second), []byte("\x1b[2J")) {
		t.Errorf("incremental frame cleared the screen: %q", second)
	}
	if bytes.Contains([]byte(second), []byte("one")) {
		t.Errorf("unchanged line was repainted: %q", second)
	}
	if !bytes.Contains([]byte(second), []byte("changed")) {
		t.Errorf("changed line was not painted: %q", second)
	}
}
