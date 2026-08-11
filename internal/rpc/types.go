// Package rpc defines the wire-format types and codec for twee's
// daemon protocol. Requests and responses are length-prefixed JSON:
//
//	<u32 big-endian length><JSON bytes>
//
// One request, one response, per connection.
package rpc

import (
	"encoding/json"
	"time"
)

// Request is one client → daemon message.
type Request struct {
	ID   string          `json:"id"`
	Op   string          `json:"op"`
	Args json.RawMessage `json:"args,omitempty"`
}

// Response is one daemon → client message.
type Response struct {
	ID    string          `json:"id"`
	OK    bool            `json:"ok"`
	Data  json.RawMessage `json:"data,omitempty"`
	Error *Error          `json:"error,omitempty"`
}

// Error is the structured error body.
type Error struct {
	Code    string          `json:"code"`
	Message string          `json:"message"`
	Details json.RawMessage `json:"details,omitempty"`
}

// StatusData is the response shape for the status op (and per-entry in `twee ls`).
type StatusData struct {
	Cmd       []string  `json:"cmd"`
	Cols      int       `json:"cols"`
	Rows      int       `json:"rows"`
	StartedAt time.Time `json:"started_at"`
	Running   bool      `json:"running"`
	ExitCode  *int      `json:"exit_code,omitempty"`
}

// Lifecycle args.

// StopArgs carries stop's optional grace override: a Go duration string
// (e.g. "500ms"), same convention as the wait ops' Timeout/Quiet fields.
// Empty means "use the daemon's default grace period"; "0" or "0s" means
// SIGKILL immediately, skipping the SIGTERM wait.
type StopArgs struct {
	Grace             string `json:"grace,omitempty"`
	SuppressTombstone bool   `json:"suppress_tombstone,omitempty"`
	// Token scopes a stop request to the daemon generation that returned it
	// from start. Nil retains the interactive, name-only stop behavior; a
	// present empty token is invalid and must never fall back to name-only.
	Token *string `json:"token,omitempty"`
}

// Input args.

type TypeArgs struct {
	Text string `json:"text"`
}

type KeyArgs struct {
	Key string `json:"key"`
}

type PasteArgs struct {
	Text  string `json:"text"`
	Force bool   `json:"force,omitempty"`
}

// Mouse coordinates and scroll ticks are pointers so decoding preserves the
// distinction between an omitted required field and an explicit zero.
type ClickArgs struct {
	X         *int     `json:"x"`
	Y         *int     `json:"y"`
	Button    string   `json:"button,omitempty"`
	Modifiers []string `json:"modifiers,omitempty"`
}

type FindClickArgs struct {
	Pattern   string   `json:"pattern"`
	Regex     bool     `json:"regex,omitempty"`
	Require   *string  `json:"require,omitempty"`
	Select    *string  `json:"select,omitempty"`
	Button    string   `json:"button,omitempty"`
	Modifiers []string `json:"modifiers,omitempty"`
}

type HoverArgs struct {
	X         *int     `json:"x"`
	Y         *int     `json:"y"`
	Modifiers []string `json:"modifiers,omitempty"`
}

type ScrollArgs struct {
	X         *int     `json:"x"`
	Y         *int     `json:"y"`
	Direction string   `json:"direction"`
	Ticks     *int     `json:"ticks,omitempty"`
	Modifiers []string `json:"modifiers,omitempty"`
}

type DragArgs struct {
	FromX     *int     `json:"from_x"`
	FromY     *int     `json:"from_y"`
	ToX       *int     `json:"to_x"`
	ToY       *int     `json:"to_y"`
	Button    string   `json:"button,omitempty"`
	Modifiers []string `json:"modifiers,omitempty"`
}

type SignalArgs struct {
	Name string `json:"name"`
}

// Query args.

type CellArgs struct {
	X int `json:"x"`
	Y int `json:"y"`
}

type RegionArgs struct {
	X int `json:"x"`
	Y int `json:"y"`
	W int `json:"w"`
	H int `json:"h"`
}

type FindArgs struct {
	Text  string `json:"text"`
	Regex bool   `json:"regex,omitempty"`
}

// State args.

type ResizeArgs struct {
	Cols int `json:"cols"`
	Rows int `json:"rows"`
}

type ScreenshotArgs struct {
	Out         string `json:"out,omitempty"`
	PixelWidth  int    `json:"pixel_width,omitempty"`
	PixelHeight int    `json:"pixel_height,omitempty"`
}

type TraceStartArgs struct {
	Out string `json:"out,omitempty"`
}

type DiffArgs struct {
	Against string `json:"against"`
}

// Wait args.

type WaitTextArgs struct {
	Text    string `json:"text"`
	Regex   bool   `json:"regex,omitempty"`
	Timeout string `json:"timeout,omitempty"`
}

type WaitNoTextArgs struct {
	Text    string `json:"text"`
	Timeout string `json:"timeout,omitempty"`
}

type WaitStableArgs struct {
	Quiet   string `json:"quiet,omitempty"`
	Timeout string `json:"timeout,omitempty"`
	Exclude []Rect `json:"exclude,omitempty"`
}

// Rect is a rectangle of terminal cells, with its origin at the top left of
// the viewport. It is used by wait stable to omit busy regions from its
// stability comparison.
type Rect struct {
	X int `json:"x"`
	Y int `json:"y"`
	W int `json:"w"`
	H int `json:"h"`
}

type WaitCellArgs struct {
	X         *int          `json:"x"`
	Y         *int          `json:"y"`
	Predicate CellPredicate `json:"predicate"`
	Timeout   string        `json:"timeout,omitempty"`
}

type WaitCursorArgs struct {
	X       int    `json:"x"`
	Y       int    `json:"y"`
	Timeout string `json:"timeout,omitempty"`
}

type WaitExitArgs struct {
	Timeout string `json:"timeout,omitempty"`
}

// Misc.

type SleepArgs struct {
	Duration string `json:"duration"`
}

// Data shapes returned by ops with non-trivial responses.

type TextData struct {
	Text string `json:"text"`
}

type LinesData struct {
	Lines []string `json:"lines"`
}

type CursorData struct {
	X       int    `json:"x"`
	Y       int    `json:"y"`
	Visible bool   `json:"visible"`
	Shape   string `json:"shape"`
}

// Color kind values for ColorData.Kind.
const (
	ColorKindDefault = "default"
	ColorKindPalette = "palette"
	ColorKindRGB     = "rgb"
)

// ColorData is the wire shape of a cell's foreground/background color.
// Kind selects which of the other fields are meaningful:
//
//   - "default": no other field set.
//   - "palette": Index set (as a pointer, always present — palette index 0
//     is a valid color, so a bare zero value must not read as "absent").
//   - "rgb": R, G, B set.
type ColorData struct {
	Kind  string `json:"kind"`
	Index *uint8 `json:"index,omitempty"`
	R     uint8  `json:"r,omitempty"`
	G     uint8  `json:"g,omitempty"`
	B     uint8  `json:"b,omitempty"`
}

// ColorPredicate is the strict wire representation accepted by cell
// predicates. Pointer channels preserve explicit zero values and let handlers
// reject incomplete or irrelevant fields.
type ColorPredicate struct {
	Kind  string `json:"kind"`
	Index *uint8 `json:"index,omitempty"`
	R     *uint8 `json:"r,omitempty"`
	G     *uint8 `json:"g,omitempty"`
	B     *uint8 `json:"b,omitempty"`
}

// CellPredicate constrains one physical terminal cell. Every populated field
// must match; pointer booleans distinguish omitted from explicitly false.
type CellPredicate struct {
	Text          *string         `json:"text,omitempty"`
	Width         *int            `json:"width,omitempty"`
	Fg            *ColorPredicate `json:"fg,omitempty"`
	Bg            *ColorPredicate `json:"bg,omitempty"`
	Bold          *bool           `json:"bold,omitempty"`
	Dim           *bool           `json:"dim,omitempty"`
	Italic        *bool           `json:"italic,omitempty"`
	Underline     *bool           `json:"underline,omitempty"`
	Inverse       *bool           `json:"inverse,omitempty"`
	Strikethrough *bool           `json:"strikethrough,omitempty"`
}

type AssertCellArgs struct {
	X         *int          `json:"x"`
	Y         *int          `json:"y"`
	Predicate CellPredicate `json:"predicate"`
}

type AssertRegionArgs struct {
	X         *int          `json:"x,omitempty"`
	Y         *int          `json:"y,omitempty"`
	W         *int          `json:"w,omitempty"`
	H         *int          `json:"h,omitempty"`
	Match     string        `json:"match,omitempty"`
	Predicate CellPredicate `json:"predicate"`
}

// CellData is the wire shape of one terminal cell: the "cell" op's data,
// and each element of a "region" row. Style booleans are always present
// (no omitempty) so a cell's full attribute set is explicit on the wire.
type CellData struct {
	Text          string    `json:"text"`
	Width         int       `json:"width"`
	Fg            ColorData `json:"fg"`
	Bg            ColorData `json:"bg"`
	Bold          bool      `json:"bold"`
	Dim           bool      `json:"dim"`
	Italic        bool      `json:"italic"`
	Underline     bool      `json:"underline"`
	Inverse       bool      `json:"inverse"`
	Strikethrough bool      `json:"strikethrough"`
}

type SizeData struct {
	Cols int `json:"cols"`
	Rows int `json:"rows"`
}

type TitleData struct {
	Title string `json:"title"`
}

type ModeData struct {
	DECCKM             bool  `json:"decckm"`
	ApplicationKeypad  bool  `json:"application_keypad"`
	BracketedPaste     bool  `json:"bracketed_paste"`
	FocusEvents        bool  `json:"focus_events"`
	KittyKeyboardKnown bool  `json:"kitty_keyboard_known"`
	KittyKeyboardFlags uint8 `json:"kitty_keyboard_flags"`
	AltScreen          bool  `json:"alt_screen"`
	Mouse              bool  `json:"mouse"`
	MouseKnown         bool  `json:"mouse_known"`
	MouseRaw           bool  `json:"mouse_raw"`

	MouseTracking string `json:"mouse_tracking,omitempty"`
	MouseFormat   string `json:"mouse_format,omitempty"`

	MouseTrackingX10    bool `json:"mouse_tracking_x10"`
	MouseTrackingNormal bool `json:"mouse_tracking_normal"`
	MouseTrackingButton bool `json:"mouse_tracking_button"`
	MouseTrackingAny    bool `json:"mouse_tracking_any"`

	MouseFormatUTF8      bool `json:"mouse_format_utf8"`
	MouseFormatSGR       bool `json:"mouse_format_sgr"`
	MouseFormatURxvt     bool `json:"mouse_format_urxvt"`
	MouseFormatSGRPixels bool `json:"mouse_format_sgr_pixels"`
}

type FindMatch struct {
	X    int    `json:"x"`
	Y    int    `json:"y"`
	W    int    `json:"w"`
	H    int    `json:"h"`
	Line int    `json:"line"`
	Text string `json:"text"`
}

type PointData struct {
	X int `json:"x"`
	Y int `json:"y"`
}

type FindClickData struct {
	Match     FindMatch `json:"match"`
	Target    PointData `json:"target"`
	Selection string    `json:"selection"`
}

type DiffData struct {
	Equal    bool   `json:"equal"`
	Unified  string `json:"unified"`
	Current  string `json:"current"`
	Expected string `json:"expected"`
}

type WaitExitData struct {
	ExitCode int `json:"exit_code"`
	// TracePath is the bundle written when the session's artifacts were
	// finalized at child exit; empty if no trace was active.
	TracePath string `json:"trace_path,omitempty"`
}

type ScreenshotData struct {
	Out       string `json:"out,omitempty"`
	PNGBase64 string `json:"png_base64,omitempty"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
}
