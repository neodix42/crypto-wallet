package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"testing/iotest"
)

const atomDiagnosticPrefix = "[163420:1025/143139.802842:ERROR:ui/gfx/x/atom_cache.cc:236] Add "

func TestStderrRelayOmitsOnlyCompleteKnownAtomDiagnostics(t *testing.T) {
	for _, atom := range []string{"_NET_WM_STATE_MODAL", "application/vnd.portal.filetransfer", "application/vnd.portal.files"} {
		for _, prefix := range []string{atomDiagnosticPrefix, "[163420:163421:1025/143139.802842:ERROR:ui/gfx/x/atom_cache.cc:1] Add "} {
			t.Run(atom+prefix, func(t *testing.T) {
				var output bytes.Buffer
				if err := relayStderr(strings.NewReader(prefix+atom+" to kAtomsToCache\n"), &output); err != nil || output.Len() != 0 {
					t.Fatalf("known diagnostic retained or failed: %q %v", output.Bytes(), err)
				}
			})
		}
	}
}

func TestStderrRelayPreservesLookalikesAndEveryOtherByte(t *testing.T) {
	known := atomDiagnosticPrefix + "_NET_WM_STATE_MODAL to kAtomsToCache\n"
	inputs := [][]byte{
		nil,
		[]byte("\n\nordinary output\n"),
		[]byte("Error: wallet request failed\n"),
		[]byte(strings.Replace(known, ":ERROR:", ":FATAL:", 1)),
		[]byte(strings.Replace(known, ":ERROR:", ":WARNING:", 1)),
		[]byte(strings.Replace(known, "_NET_WM_STATE_MODAL", "_NET_WM_OTHER", 1)),
		[]byte(strings.Replace(known, "atom_cache.cc", "other_atom_cache.cc", 1)),
		[]byte(strings.Replace(known, "atom_cache.cc", "atom_cacheXcc", 1)),
		[]byte(strings.Replace(known, "ui/gfx/x/", "gfx/x/", 1)),
		[]byte(strings.Replace(known, "143139.802842", "143139X802842", 1)),
		[]byte(strings.Replace(known, "143139.802842", "143139.80284", 1)),
		[]byte(strings.Replace(known, "1025/", "125/", 1)),
		[]byte(strings.Replace(known, "[163420:", "[pid:", 1)),
		[]byte(strings.Replace(known, ":236]", ":line]", 1)),
		[]byte(strings.Replace(known, "[163420:", "[163420:163421:163422:", 1)),
		[]byte(strings.Replace(known, "Add ", "Add  ", 1)),
		[]byte(strings.Replace(known, "Add _NET", "Add _net", 1)),
		[]byte(strings.Replace(known, "\n", "\r\n", 1)),
		[]byte(" " + known),
		[]byte("prefix " + known),
		[]byte(strings.TrimSuffix(known, "\n") + " trailing text\n"),
		[]byte(strings.TrimSuffix(known, "\n") + " \n"),
		[]byte(strings.TrimSuffix(known, "\n")),
		[]byte("[163420:1025/143139.802842:ERROR:ui/gfx/x/atom_cache.cc:236] Another error\n"),
		[]byte(atomDiagnosticPrefix + "application/vndXportalXfiles to kAtomsToCache\n"),
		[]byte(atomDiagnosticPrefix + "application/vnd.portal.files-extra to kAtomsToCache\n"),
		append([]byte{0, 0xff, 0xfe}, []byte(known)...),
		append([]byte(strings.TrimSuffix(known, "\n")), 0, '\n'),
		[]byte("Add _NET_WM_STATE_MODAL to kAtomsToCache\n"),
	}
	for index, input := range inputs {
		t.Run(fmt.Sprint(index), func(t *testing.T) {
			var output bytes.Buffer
			if err := relayStderr(bytes.NewReader(input), &output); err != nil || !bytes.Equal(output.Bytes(), input) {
				t.Fatalf("diagnostic altered: got %q; want %q; error %v", output.Bytes(), input, err)
			}
		})
	}
}

func TestStderrRelayHandlesFragmentedMixedAndBinaryOutput(t *testing.T) {
	var input, expected bytes.Buffer
	for _, atom := range []string{"_NET_WM_STATE_MODAL", "application/vnd.portal.filetransfer", "application/vnd.portal.files"} {
		other := []byte{'p', 0, 0xff, '\n', '\r', '\n'}
		input.Write(other)
		expected.Write(other)
		input.WriteString(atomDiagnosticPrefix + atom + " to kAtomsToCache\n")
		input.WriteString("wallet failure\n")
		expected.WriteString("wallet failure\n")
	}
	input.WriteString("unterminated tail")
	expected.WriteString("unterminated tail")
	for name, wrap := range map[string]func(io.Reader) io.Reader{"one-byte": iotest.OneByteReader, "half": iotest.HalfReader, "data-before-eof": iotest.DataErrReader} {
		t.Run(name, func(t *testing.T) {
			var output bytes.Buffer
			if err := relayStderr(wrap(bytes.NewReader(input.Bytes())), &output); err != nil || !bytes.Equal(output.Bytes(), expected.Bytes()) {
				t.Fatalf("fragmented output altered: %q %v", output.Bytes(), err)
			}
		})
	}
}

type stderrReaderFunc func([]byte) (int, error)

func (f stderrReaderFunc) Read(p []byte) (int, error) { return f(p) }

type stderrWriterFunc func([]byte) (int, error)

func (f stderrWriterFunc) Write(p []byte) (int, error) { return f(p) }

func TestStderrRelayStreamsOversizedLinesAndResumesMatching(t *testing.T) {
	// This would match the diagnostic pattern without the line-size bound. Once
	// streaming starts, even its matching suffix must remain in the output.
	oversized := "[" + strings.Repeat("1", stderrLineLimit*3) + strings.TrimPrefix(atomDiagnosticPrefix, "[163420") + "_NET_WM_STATE_MODAL to kAtomsToCache\n"
	known := atomDiagnosticPrefix + "application/vnd.portal.files to kAtomsToCache\n"
	for _, suffix := range []string{"", "unterminated"} {
		t.Run(fmt.Sprintf("tail-%d", len(suffix)), func(t *testing.T) {
			var output bytes.Buffer
			remaining := []byte(oversized + known + suffix)
			reads := 0
			reader := stderrReaderFunc(func(p []byte) (int, error) {
				if reads == 1 && output.Len() != stderrLineLimit {
					t.Fatalf("oversized output buffered beyond bound: %d bytes", output.Len())
				}
				reads++
				if len(remaining) == 0 {
					return 0, io.EOF
				}
				n := copy(p, remaining)
				remaining = remaining[n:]
				return n, nil
			})
			if err := relayStderr(reader, &output); err != nil || output.String() != oversized+suffix {
				t.Fatalf("oversized diagnostic changed: got length %d want %d; error %v", output.Len(), len(oversized+suffix), err)
			}
		})
	}
	for _, size := range []int{stderrLineLimit - 1, stderrLineLimit, stderrLineLimit + 1} {
		for _, terminator := range []string{"", "\n"} {
			input := strings.Repeat("x", size) + terminator
			var output bytes.Buffer
			if err := relayStderr(strings.NewReader(input), &output); err != nil || output.String() != input {
				t.Fatalf("boundary length %d changed: %v", len(input), err)
			}
		}
	}
	// A complete-looking diagnostic can begin exactly at the next chunk's
	// boundary while still belonging to an oversized preceding line. It must
	// remain visible; matching resumes only after that line's newline.
	for _, size := range []int{stderrLineLimit, stderrLineLimit * 2} {
		prefix := strings.Repeat("x", size)
		var output bytes.Buffer
		if err := relayStderr(strings.NewReader(prefix+known+known), &output); err != nil || output.String() != prefix+known {
			t.Fatalf("oversized line's matching suffix omitted at %d: %v", size, err)
		}
	}
}

func TestStderrRelayPreservesFragmentsAndReturnsReadErrors(t *testing.T) {
	readFailure := errors.New("synthetic read failure")
	for _, fragment := range []string{"", "incomplete diagnostic", strings.TrimSuffix(atomDiagnosticPrefix+"_NET_WM_STATE_MODAL to kAtomsToCache\n", "\n"), strings.Repeat("x", stderrLineLimit*2+3)} {
		var output bytes.Buffer
		remaining := fragment
		reader := stderrReaderFunc(func(p []byte) (int, error) {
			n := copy(p, remaining)
			remaining = remaining[n:]
			if remaining == "" {
				return n, readFailure
			}
			return n, nil
		})
		if err := relayStderr(reader, &output); err != readFailure || output.String() != fragment {
			t.Fatalf("read failure lost diagnostic or identity: length %d, error %v", output.Len(), err)
		}
	}
	var output bytes.Buffer
	if err := relayStderr(stderrReaderFunc(func([]byte) (int, error) { return 0, nil }), &output); err != io.ErrNoProgress || output.Len() != 0 {
		t.Fatalf("stalled reader did not stop: %v", err)
	}
	// A terminated known diagnostic does not conceal a subsequent read error,
	// including readers that return the error together with their last bytes.
	remaining := atomDiagnosticPrefix + "_NET_WM_STATE_MODAL to kAtomsToCache\n"
	reader := stderrReaderFunc(func(p []byte) (int, error) {
		n := copy(p, remaining)
		remaining = remaining[n:]
		return n, readFailure
	})
	if err := relayStderr(reader, &output); err != readFailure || output.Len() != 0 {
		t.Fatalf("ignored diagnostic hid read failure: %v", err)
	}
}

func TestStderrRelayReturnsWriteFailuresAndRejectsShortWrites(t *testing.T) {
	writeFailure := errors.New("synthetic write failure")
	for _, test := range []struct {
		name string
		n    int
		err  error
		want error
	}{
		{"partial-error", 2, writeFailure, writeFailure},
		{"zero-error", 0, writeFailure, writeFailure},
		{"complete-error", 8, writeFailure, writeFailure},
		{"partial-no-error", 2, nil, io.ErrShortWrite},
		{"zero-no-error", 0, nil, io.ErrShortWrite},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			writer := stderrWriterFunc(func(p []byte) (int, error) {
				calls++
				if string(p) != "visible\n" {
					t.Fatalf("unexpected forwarded bytes: %q", p)
				}
				return test.n, test.err
			})
			if err := relayStderr(strings.NewReader("visible\nnext line\n"), writer); err != test.want || calls != 1 {
				t.Fatalf("write failure hidden or retried: %v, %d writes", err, calls)
			}
		})
	}
}
