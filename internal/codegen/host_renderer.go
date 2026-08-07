package codegen

import (
	"fmt"
	"io"
	"strings"

	"github.com/paulsmith/twee/internal/engine"
	"github.com/paulsmith/twee/internal/vt"
)

// hostRenderer owns a nested alternate screen. It never forwards child escape
// sequences to the host: child output is represented only by the VT model.
type hostRenderer struct {
	w                 io.Writer
	active            bool
	hostRows          int
	status            bool
	modes             vt.InputModes
	preserve          bool
	frame             vt.Snapshot
	haveFrame         bool
	frameHostRows     int
	frameStatus       bool
	lastStatusLine    string
	lastStatusVisible bool
	cursorStyle       vt.CursorStyle
	haveCursorStyle   bool
}

const hostPrivateInputModes = "1;9;25;66;1000;1002;1003;1004;1005;1006;1015;1016;2004"

func (r *hostRenderer) enter() {
	if !r.active {
		if r.preserve {
			fmt.Fprintf(r.w, "\x1b[?%ss", hostPrivateInputModes)
		}
		fmt.Fprint(r.w, "\x1b[?1049h\x1b[0m\x1b[r\x1b[?6l\x1b[?25h")
		r.writeInputModeReset()
		r.active = true
	}
}
func (r *hostRenderer) render(s vt.Snapshot, line string, presentation vt.Presentation) {
	if !r.active {
		return
	}
	s.AltScreen = false // the wrapper owns host alt-screen state.
	full := !r.haveFrame || r.frame.Size != s.Size || r.frameHostRows != r.hostRows || r.frameStatus != r.status

	// Hide intermediate cursor movement and ask supporting terminals to publish
	// the update atomically. Unknown DEC private modes are safely ignored.
	fmt.Fprint(r.w, "\x1b[?2026h\x1b[?25l")
	if full {
		fmt.Fprint(r.w, "\x1b[0m\x1b[r\x1b[?6l")
		// 1049 preserves the primary-screen cursor style in xterm-compatible
		// terminals, so child style can be represented inside the wrapper screen.
		_, _ = r.w.Write(engine.TraceSeedOutput(s))
	} else {
		_, _ = r.w.Write(engine.SnapshotDiffOutput(r.frame, s))
	}

	statusLine := truncateStatus(line, s.Size.Cols)
	if r.status && r.hostRows >= 2 && (full || !r.lastStatusVisible || statusLine != r.lastStatusLine) {
		r.paintStatus(statusLine, s.Size.Cols)
	}

	if !r.haveCursorStyle || r.cursorStyle != presentation.Cursor {
		fmt.Fprint(r.w, hostCursorStyleSequence(presentation.Cursor))
		r.cursorStyle = presentation.Cursor
		r.haveCursorStyle = true
	}
	r.mirrorInputModes(presentation.Input)
	// Restore the child cursor presentation last, after child and status paint.
	fmt.Fprintf(r.w, "\x1b[%d;%dH", s.Cursor.Row+1, s.Cursor.Col+1)
	if s.Cursor.Visible {
		fmt.Fprint(r.w, "\x1b[?25h")
	} else {
		fmt.Fprint(r.w, "\x1b[?25l")
	}
	fmt.Fprint(r.w, "\x1b[?2026l")

	r.frame = s
	r.haveFrame = true
	r.frameHostRows = r.hostRows
	r.frameStatus = r.status
	r.lastStatusLine = statusLine
	r.lastStatusVisible = r.status && r.hostRows >= 2
}
func (r *hostRenderer) close() {
	if r.active {
		r.mirrorInputModes(vt.InputModes{})
		fmt.Fprint(r.w, "\x1b[?2026l\x1b[0m\x1b[r\x1b[?6l\x1b[?25h\x1b[?1049l")
		if r.preserve {
			fmt.Fprintf(r.w, "\x1b[?%sr", hostPrivateInputModes)
		}
		r.active = false
		r.haveFrame = false
		r.lastStatusVisible = false
		r.haveCursorStyle = false
	}
}

func (r *hostRenderer) paintStatus(line string, cols int) {
	padding := cols - statusWidth(line)
	if padding < 0 {
		padding = 0
	}
	fmt.Fprintf(r.w, "\x1b[%d;1H\x1b[0m\x1b[2K\x1b[7m%s%s\x1b[0m", r.hostRows, line, strings.Repeat(" ", padding))
}

func hostCursorStyleSequence(style vt.CursorStyle) string {
	switch style {
	case vt.CursorStyleBlock, vt.CursorStyleHollow:
		return "\x1b[2 q"
	case vt.CursorStyleUnderline:
		return "\x1b[4 q"
	case vt.CursorStyleBar:
		return "\x1b[6 q"
	default:
		return "\x1b[0 q"
	}
}

// Only input-producing modes are mirrored. Child palette, OSC, margins, and
// screen modes are reconstructed inside the wrapper's alternate screen.
func (r *hostRenderer) mirrorInputModes(next vt.InputModes) {
	for _, mode := range []struct {
		code int
		old  bool
		new  bool
	}{
		{1, r.modes.ApplicationCursor, next.ApplicationCursor},
		{66, r.modes.ApplicationKeypad, next.ApplicationKeypad},
		{2004, r.modes.BracketedPaste, next.BracketedPaste},
		{1004, r.modes.FocusEvents, next.FocusEvents},
		{9, r.modes.MouseX10, next.MouseX10},
		{1000, r.modes.MouseNormal, next.MouseNormal},
		{1002, r.modes.MouseButton, next.MouseButton},
		{1003, r.modes.MouseAny, next.MouseAny},
		{1005, r.modes.MouseUTF8, next.MouseUTF8},
		{1006, r.modes.MouseSGR, next.MouseSGR},
		{1015, r.modes.MouseURxvt, next.MouseURxvt},
	} {
		if mode.old == mode.new {
			continue
		}
		set := 'l'
		if mode.new {
			set = 'h'
		}
		fmt.Fprintf(r.w, "\x1b[?%d%c", mode.code, set)
	}
	r.modes = next
}

func (r *hostRenderer) writeInputModeReset() {
	for _, code := range []int{1, 9, 66, 1000, 1002, 1003, 1004, 1005, 1006, 1015, 1016, 2004} {
		fmt.Fprintf(r.w, "\x1b[?%dl", code)
	}
	r.modes = vt.InputModes{}
}

func nativeHostStateSaveCapable(term string) bool {
	term = strings.ToLower(term)
	return strings.Contains(term, "xterm") || strings.Contains(term, "screen") || strings.Contains(term, "tmux") || strings.Contains(term, "rxvt") || strings.Contains(term, "kitty") || strings.Contains(term, "wezterm") || strings.Contains(term, "ghostty")
}
