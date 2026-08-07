package codegen

func compositorCapable(noStatus bool, term string, isTTY bool) bool {
	return !noStatus && term != "dumb" && isTTY
}
