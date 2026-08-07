package codegen

import (
	"strconv"
	"strings"
)

// terminalPath leaves ordinary paths readable while quoting controls that
// would otherwise alter the terminal when shown in recorder feedback.
func terminalPath(path string) string {
	for _, r := range path {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0) {
			return strconv.Quote(path)
		}
	}
	return path
}

func artifactSummary(script *scriptController, trace *traceController) string {
	var out []string
	if script.state == recorderFinalized {
		s := "script saved: " + terminalPath(script.path)
		if script.partial {
			s += " (partial)"
		}
		out = append(out, s)
	}
	if trace.state == recorderFinalized {
		out = append(out, "trace saved: "+terminalPath(trace.path))
	}
	return strings.Join(out, "; ")
}
