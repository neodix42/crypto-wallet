//go:build linux && cgo

package webui

/*
#include <signal.h>
#include <stdbool.h>

// Exported by JavaScriptCore since WebKitGTK 2.32, but its declaration lives
// in JSBasePrivate.h rather than the installed public headers. Keep it weak:
// older/different runtimes retain their normal initialization behavior.
extern bool JSConfigureSignalForGC(int signal) __attribute__((weak));

// SIGRTMIN is a libc expression, not a portable numeric constant. Starting
// there excludes NPTL's private signals, and sigaction keeps other libraries'
// installed handlers intact. Go leaves libc's first public realtime signal
// available on glibc Linux; higher signals may already belong to Go.
static int cryptowallet_configure_gc_signal(void) {
    if (!JSConfigureSignalForGC)
        return 0;
    struct sigaction current;
    for (int candidate = SIGRTMIN; candidate <= SIGRTMAX; candidate++) {
        if (sigaction(candidate, NULL, &current) == 0 && current.sa_handler == SIG_DFL)
            return JSConfigureSignalForGC(candidate) ? candidate : 0;
    }
    return 0;
}
*/
import "C"

import (
	"os"
	"strings"
)

// WebKit's remote inspector can open a TCP listener from an inherited
// environment variable even when developer extras are disabled. Freeze those
// runtime controls before any JSC/WebKit initialization in either UI role.
// Clearing loader variables protects subsequently spawned engine processes;
// it cannot undo a library injected before this executable started.
func prepareWebKitRuntime() {
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(key, "WEBKIT_") || strings.HasPrefix(key, "JSC_") ||
			key == "LD_PRELOAD" || key == "LD_LIBRARY_PATH" || key == "LD_AUDIT" ||
			key == "GIO_EXTRA_MODULES" || key == "GIO_MODULE_DIR" || key == "GTK_PATH" || key == "GTK_MODULES" {
			_ = os.Unsetenv(key)
		}
	}
}

// configureWebKitSignal gives JavaScriptCore an unused GC suspension signal
// before Wails initializes WebKit. Its SIGUSR1 default otherwise replaces Go's
// handler and prints a notice on every launch. An explicit environment setting
// takes precedence, and an occupied signal is never reset or replaced here.
func configureWebKitSignal() int {
	if _, configured := os.LookupEnv("JSC_SIGNAL_FOR_GC"); configured {
		return 0
	}
	// Calling the SPI also avoids newer JSC versions treating their documented
	// JSC_SIGNAL_FOR_GC environment variable as an invalid VM option. Returning
	// zero leaves default initialization intact if the SPI is unavailable,
	// JSC already initialized, or no public realtime signal is free.
	return int(C.cryptowallet_configure_gc_signal())
}
