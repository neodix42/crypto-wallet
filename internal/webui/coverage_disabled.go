//go:build !walletcoverage

package webui

// Normal builds have no coverage directory, runtime/coverage dependency or
// private-child diagnostic environment capability.
func coveragePromptEnvironment(env []string) []string { return env }
func flushPromptCoverage()                            {}
