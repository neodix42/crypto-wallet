package main

import (
	"bufio"
	"io"
	"regexp"
)

// Bound the amount retained while deciding whether a complete diagnostic is
// eligible for omission. Longer lines pass through without further matching.
const stderrLineLimit = 4096

// These three complete Chromium X11 diagnostics report an uncached atom, not a
// failed wallet operation. Everything else, including other atom-cache errors,
// malformed prefixes and unfinished lines, must remain visible verbatim.
var ignorableAtomDiagnostic = regexp.MustCompile(`^\[[0-9]+(:[0-9]+)?:[0-9]{4}/[0-9]{6}\.[0-9]{6}:ERROR:ui/gfx/x/atom_cache\.cc:[0-9]+\] Add (_NET_WM_STATE_MODAL|application/vnd\.portal\.filetransfer|application/vnd\.portal\.files) to kAtomsToCache\n$`)

// relayStderr streams diagnostics with bounded buffering. In particular, a
// stalled or unbounded line cannot delay already accumulated oversized output
// or exhaust memory. Read/write failures remain errors rather than being
// mistaken for a diagnostic that can be omitted.
func relayStderr(input io.Reader, output io.Writer) error {
	reader := bufio.NewReaderSize(input, stderrLineLimit)
	passthrough := false
	for {
		line, readErr := reader.ReadSlice('\n')
		if len(line) != 0 && (passthrough || readErr != nil || !ignorableAtomDiagnostic.Match(line)) {
			n, writeErr := output.Write(line)
			if writeErr != nil {
				return writeErr
			}
			if n != len(line) {
				return io.ErrShortWrite
			}
		}
		if readErr == bufio.ErrBufferFull {
			passthrough = true
			continue
		}
		passthrough = false
		if readErr == io.EOF {
			return nil
		}
		if readErr != nil {
			return readErr
		}
	}
}
