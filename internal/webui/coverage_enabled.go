//go:build walletcoverage

package webui

import (
	"os"
	"path/filepath"
	"runtime/coverage"
	"syscall"
)

// Diagnostic builds alone can inherit an owned private counter directory.
// These files contain Go statement metadata/counters, never prompt values.
// The regular private-pipe/executable-identity checks remain mandatory.
func privateCoverageDirectory() string {
	directory := os.Getenv("GOCOVERDIR")
	if directory == "" || !filepath.IsAbs(directory) || filepath.Clean(directory) != directory {
		return ""
	}
	info, err := os.Lstat(directory)
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0700 {
		return ""
	}
	owner, ok := info.Sys().(*syscall.Stat_t)
	if !ok || owner.Uid != uint32(os.Getuid()) {
		return ""
	}
	return directory
}

func coveragePromptEnvironment(env []string) []string {
	if directory := privateCoverageDirectory(); directory != "" {
		return append(env, "GOCOVERDIR="+directory)
	}
	return env
}

func flushPromptCoverage() {
	if directory := privateCoverageDirectory(); directory != "" {
		// A missing/failed write cannot create passing evidence: the collector
		// requires counters from each real private role before converting them.
		_ = coverage.WriteMetaDir(directory)
		_ = coverage.WriteCountersDir(directory)
	}
}
