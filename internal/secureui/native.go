// Package secureui is the only credential input surface. It uses the installed
// GTK3 C ABI directly to keep the secret-bearing GUI binding small and auditable.
package secureui

/*
#cgo LDFLAGS: -l:libgtk-3.so.0 -l:libgobject-2.0.so.0 -l:libglib-2.0.so.0 -l:libgdk_pixbuf-2.0.so.0 -l:libgdk-3.so.0
#include <stdlib.h>
#include <string.h>
typedef void GtkWidget;
extern int gtk_init_check(int*,char***);
extern GtkWidget *gtk_dialog_new_with_buttons(const char*,void*,int,const char*,...);
extern void *gtk_dialog_get_content_area(void*);
extern GtkWidget *gtk_label_new(const char*);
extern void gtk_label_set_line_wrap(void*,int);
extern void gtk_label_set_selectable(void*,int);
extern void gtk_label_set_max_width_chars(void*,int);
extern void gtk_label_set_xalign(void*,float);
extern void gtk_label_set_text(void*,const char*);
extern void*gtk_scrolled_window_new(void*,void*);
extern void gtk_scrolled_window_set_policy(void*,int,int);
extern void gtk_container_add(void*,void*);
extern void gtk_widget_set_size_request(void*,int,int);
extern void gtk_box_pack_start(void*,void*,int,int,unsigned);
extern GtkWidget *gtk_box_new(int,int);
extern GtkWidget *gtk_entry_new(void);
extern void gtk_entry_set_visibility(void*,int);
extern void gtk_entry_set_max_length(void*,int);
extern void gtk_entry_set_text(void*,const char*);
extern const char *gtk_entry_get_text(void*);
extern void gtk_entry_set_input_purpose(void*,int);
extern void gtk_entry_set_input_hints(void*,int);
extern void gtk_window_set_default_size(void*,int,int);
extern void gtk_window_set_keep_above(void*,int);
extern void gtk_widget_show_all(void*);
extern int gtk_dialog_run(void*);
extern void gtk_widget_destroy(void*);
extern int gtk_events_pending(void);
extern int gtk_main_iteration(void);
extern int gtk_main_iteration_do(int);
extern GtkWidget *gtk_file_chooser_dialog_new(const char*,void*,int,const char*,...);
extern char *gtk_file_chooser_get_filename(void*);
extern void gtk_file_chooser_set_do_overwrite_confirmation(void*,int);
extern void g_free(void*);
extern void *gdk_pixbuf_loader_new(void);
extern int gdk_pixbuf_loader_write(void*,const unsigned char*,size_t,void*);
extern int gdk_pixbuf_loader_close(void*,void*);
extern void *gdk_pixbuf_loader_get_pixbuf(void*);
extern GtkWidget *gtk_image_new_from_pixbuf(void*);
extern void *gdk_pixbuf_scale_simple(void*,int,int,int);
extern void g_object_unref(void*);
extern void *gdk_atom_intern_static_string(const char*);
extern void *gtk_clipboard_get(void*);
extern void gtk_clipboard_set_text(void*,const char*,int);
extern void gtk_clipboard_store(void*);
extern void gdk_event_handler_set(void(*)(void*,void*),void*,void(*)(void*));
extern int gdk_event_get_event_type(void*);
extern void gtk_main_do_event(void*);
extern void walletNativeActivity(void);
// GDK_BUTTON_PRESS=4 and GDK_KEY_PRESS=8. Do not inspect the event's
// key value, text, coordinates, or any other input payload. GTK's normal
// dispatcher still receives every event unchanged, including on Wayland.
static void activity_dispatch(void*event,void*data){int t=gdk_event_get_event_type(event);if(t==4||t==8)walletNativeActivity();gtk_main_do_event(event);}
static void install_activity(void){gdk_event_handler_set(activity_dispatch,0,0);}
static void public_copy(const char*s){void*c=gtk_clipboard_get(gdk_atom_intern_static_string("CLIPBOARD"));gtk_clipboard_set_text(c,s,-1);gtk_clipboard_store(c);}
static void add_label(void*box,const char*s){void*l=gtk_label_new(s);gtk_label_set_line_wrap(l,1);gtk_label_set_max_width_chars(l,80);gtk_label_set_xalign(l,0);gtk_label_set_selectable(l,1);gtk_box_pack_start(box,l,0,0,10);}
static void add_scroll(void*b,const char*s){void*w=gtk_scrolled_window_new(0,0);gtk_scrolled_window_set_policy(w,1,1);gtk_widget_set_size_request(w,600,240);void*l=gtk_label_new(s);gtk_label_set_line_wrap(l,1);gtk_label_set_max_width_chars(l,80);gtk_label_set_selectable(l,1);gtk_container_add(w,l);gtk_box_pack_start(b,w,1,1,6);}
static void *new_dialog(const char*t){void*d=gtk_dialog_new_with_buttons(t,0,1,"Cancel",-6,"Continue",-3,(char*)0);gtk_window_set_default_size(d,650,240);gtk_window_set_keep_above(d,1);return d;}
static void *new_notice(const char*t){void*d=gtk_dialog_new_with_buttons(t,0,1,"Close",-7,(char*)0);gtk_window_set_default_size(d,650,180);gtk_window_set_keep_above(d,1);return d;}
// Recovery secrets are deliberately confined to this native window. Unlike
// public address labels, the phrase label is never selectable or copyable.
static void *new_recovery(const char*t){void*d=gtk_dialog_new_with_buttons(t,0,1,"Cancel",-6,"I wrote it down",-3,(char*)0);gtk_window_set_default_size(d,650,600);gtk_window_set_keep_above(d,1);return d;}
static void *recovery_phrase(void*b,const char*s){void*w=gtk_scrolled_window_new(0,0);gtk_scrolled_window_set_policy(w,1,1);gtk_widget_set_size_request(w,600,340);void*l=gtk_label_new(s);gtk_label_set_selectable(l,0);gtk_label_set_xalign(l,0);gtk_container_add(w,l);gtk_box_pack_start(b,w,1,1,6);return l;}
static void *new_recovery_check(void){void*d=gtk_dialog_new_with_buttons("Confirm recovery phrase — Go signer",0,1,"Cancel",-6,"Confirm recorded words",-3,(char*)0);gtk_window_set_default_size(d,650,480);gtk_window_set_keep_above(d,1);return d;}
static void secret_wipe(void*p,size_t n){volatile unsigned char*v=p;while(n--)*v++=0;}
static void secret_free(void*p,size_t n){if(p){secret_wipe(p,n);free(p);}}
// Positive response 1 performs an explicit public-address copy without closing
// the verified receive window. Done retains the existing accepted-view result.
static void *new_receive(const char*t){void*d=gtk_dialog_new_with_buttons(t,0,1,"Cancel",-6,"Copy address",1,"Done",-3,(char*)0);gtk_window_set_default_size(d,650,680);gtk_window_set_keep_above(d,1);return d;}
// Keep the action buttons outside this bounded viewport, including when
// network-specific funding guidance needs additional vertical space.
static void *receive_body(void*d){void*w=gtk_scrolled_window_new(0,0);gtk_scrolled_window_set_policy(w,1,1);gtk_widget_set_size_request(w,600,460);void*b=gtk_box_new(1,0);gtk_container_add(w,b);gtk_box_pack_start(gtk_dialog_get_content_area(d),w,1,1,0);return b;}
static void *add_entry(void*b,const char*l){add_label(b,l);void*e=gtk_entry_new();gtk_entry_set_visibility(e,0);gtk_entry_set_input_purpose(e,8);gtk_entry_set_input_hints(e,1026);gtk_entry_set_max_length(e,4096);gtk_box_pack_start(b,e,0,0,8);return e;}
static void finish(void*d){gtk_widget_destroy(d);while(gtk_events_pending())gtk_main_iteration();}
// Backups never overwrite files. Let the signer report BACKUP_EXISTS instead
// of presenting GTK's misleading "Replace" confirmation.
static void *file_dialog(const char*t,int save){void*d=gtk_file_chooser_dialog_new(t,0,save?1:0,"Cancel",-6,save?"Save":"Open",-3,(char*)0);gtk_file_chooser_set_do_overwrite_confirmation(d,0);return d;}
static int add_png(void*b,const unsigned char*p,size_t n){void*l=gdk_pixbuf_loader_new();if(!gdk_pixbuf_loader_write(l,p,n,0)||!gdk_pixbuf_loader_close(l,0)){g_object_unref(l);return 0;}void*i=gtk_image_new_from_pixbuf(gdk_pixbuf_loader_get_pixbuf(l));gtk_box_pack_start(b,i,0,0,6);g_object_unref(l);return 1;}
// Nearest-neighbor rendering preserves crisp QR modules. Only the displayed
// pixbuf changes; the original PNG remains untouched for saving/verification.
static int add_receive_png(void*b,const unsigned char*p,size_t n){void*l=gdk_pixbuf_loader_new();if(!gdk_pixbuf_loader_write(l,p,n,0)||!gdk_pixbuf_loader_close(l,0)){g_object_unref(l);return 0;}void*scaled=gdk_pixbuf_scale_simple(gdk_pixbuf_loader_get_pixbuf(l),320,320,0);if(!scaled){g_object_unref(l);return 0;}void*i=gtk_image_new_from_pixbuf(scaled);gtk_box_pack_start(b,i,0,0,6);g_object_unref(scaled);g_object_unref(l);return 1;}
*/
import "C"
import (
	"bytes"
	"crypto/subtle"
	"errors"
	"runtime"
	"strconv"
	"strings"
	"unsafe"
)

var ErrCancelled = errors.New("native request cancelled or unavailable")
var ErrInputTooLong = errors.New("native input exceeds UTF-8 byte limit")

func Init() error {
	runtime.LockOSThread()
	if C.gtk_init_check(nil, nil) == 0 {
		return ErrCancelled
	}
	C.install_activity()
	return nil
}
func cstr(s string) *C.char { return C.CString(s) }

// Pump services native public clipboard ownership while the signer is idle.
// Call only on the GTK thread. Timer-driven dispatch never signals activity;
// the event dispatcher reports key/button presses only during active requests.
func Pump() {
	// Bound each idle iteration even if a noisy display keeps producing events.
	for i := 0; i < 64 && C.gtk_events_pending() != 0; i++ {
		C.gtk_main_iteration_do(0)
	}
}

func Input(title string, labels ...string) ([][]byte, error) {
	t := cstr(title)
	defer C.free(unsafe.Pointer(t))
	d := C.new_dialog(t)
	box := C.gtk_dialog_get_content_area(d)
	entries := make([]unsafe.Pointer, len(labels))
	for i, s := range labels {
		l := cstr(s)
		entries[i] = C.add_entry(box, l)
		C.free(unsafe.Pointer(l))
	}
	C.gtk_widget_show_all(d)
	response := C.gtk_dialog_run(d)
	tooLong := false
	var result [][]byte
	if response == -3 {
		for _, e := range entries {
			p := C.gtk_entry_get_text(e)
			n := C.strlen(p)
			if n > 4096 {
				response = -6
				tooLong = true
				break
			}
			result = append(result, C.GoBytes(unsafe.Pointer(p), C.int(n)))
		}
	}
	empty := cstr("")
	for _, e := range entries {
		C.gtk_entry_set_text(e, empty)
	}
	C.free(unsafe.Pointer(empty))
	C.finish(d)
	if response != -3 {
		for _, b := range result {
			clear(b)
		}
		if tooLong {
			return nil, ErrInputTooLong
		}
		return nil, ErrCancelled
	}
	return result, nil
}

// Notice shows fixed public guidance. No input values or filesystem paths are
// passed to this function by the signer request error handler.
func Notice(title, message string) {
	t, s := cstr(title), cstr(message)
	defer C.free(unsafe.Pointer(t))
	defer C.free(unsafe.Pointer(s))
	d := C.new_notice(t)
	C.add_label(C.gtk_dialog_get_content_area(d), s)
	C.gtk_widget_show_all(d)
	C.gtk_dialog_run(d)
	C.finish(d)
}
func Confirm(title, summary string) bool             { return display(title, summary, nil) }
func Receive(title, summary string, png []byte) bool { return display(title, summary, png) }

// Recovery displays an engine-owned canonical English mnemonic only in GTK,
// then asks for three recorded words in separate masked entries. It never
// copies the phrase, creates an immutable Go string from it or returns words.
// The caller retains ownership of phrase and must clear it after this call.
func Recovery(title string, phrase []byte) bool {
	words := bytes.Fields(phrase)
	validLength := len(words) == 12 || len(words) == 15 || len(words) == 18 || len(words) == 21 || len(words) == 24
	if !validLength || len(phrase) > 256 {
		return false
	}
	n := len(words) - 1
	for _, word := range words {
		n += len(word)
		for _, c := range word {
			if c < 'a' || c > 'z' {
				return false
			}
		}
	}
	if n != len(phrase) { // Engine phrases use single spaces, never repaired input.
		return false
	}
	numbered := make([]byte, 0, len(phrase)+len(words)*5)
	for i, word := range words {
		numbered = strconv.AppendInt(numbered, int64(i+1), 10)
		numbered = append(numbered, '.', ' ')
		numbered = append(numbered, word...)
		numbered = append(numbered, '\n')
	}
	defer clear(numbered)
	secret := C.malloc(C.size_t(len(numbered) + 1))
	if secret == nil {
		return false
	}
	copy(unsafe.Slice((*byte)(secret), len(numbered)+1), numbered)
	unsafe.Slice((*byte)(secret), len(numbered)+1)[len(numbered)] = 0
	defer C.secret_free(secret, C.size_t(len(numbered)+1))
	t := cstr(title)
	defer C.free(unsafe.Pointer(t))
	d := C.new_recovery(t)
	b := C.gtk_dialog_get_content_area(d)
	guidance := cstr("Write these words in order and store them somewhere private and offline. Anyone with this phrase can spend your funds. Do not photograph or share them, copy them to the clipboard, or enter them on a website. Your app password encrypts this device; it does not change these wallets. Also keep any original optional BIP39 passphrase separately: these words and the app password cannot replace it.")
	C.add_label(b, guidance)
	C.free(unsafe.Pointer(guidance))
	label := C.recovery_phrase(b, (*C.char)(secret))
	C.gtk_widget_show_all(d)
	response := C.gtk_dialog_run(d)
	empty := cstr("")
	C.gtk_label_set_text(label, empty)
	C.free(unsafe.Pointer(empty))
	// Clear our additional phrase copies before destroying the native label.
	// Only caller-owned word slices remain for the subsequent masked check.
	clear(numbered)
	C.secret_wipe(secret, C.size_t(len(numbered)+1))
	C.finish(d)
	if response != -3 {
		return false
	}
	positions := []int{1, 5, len(words) - 1}
	labels := make([]string, len(positions))
	for i, position := range positions {
		labels[i] = "Recorded word " + strconv.Itoa(position+1)
	}
	return recoveryCheck(words, positions, labels)
}

func recoveryCheck(words [][]byte, positions []int, labels []string) bool {
	d := C.new_recovery_check()
	b := C.gtk_dialog_get_content_area(d)
	guidance := cstr("Enter the requested words from your offline copy. The phrase stays hidden here. Confirm only after checking that your complete phrase is safely recorded.")
	C.add_label(b, guidance)
	C.free(unsafe.Pointer(guidance))
	entries := make([]unsafe.Pointer, len(labels))
	for i, label := range labels {
		l := cstr(label)
		entries[i] = C.add_entry(b, l)
		C.free(unsafe.Pointer(l))
	}
	C.gtk_widget_show_all(d)
	response := C.gtk_dialog_run(d)
	matched := response == -3
	empty := cstr("")
	for i, entry := range entries {
		p := C.gtk_entry_get_text(entry)
		n := C.strlen(p)
		if response == -3 {
			// Fixed mnemonic words are short. Bound byte copying even if GTK's
			// character limit accepted a much longer multibyte submission.
			if n > 32 {
				matched = false
			} else {
				word := C.GoBytes(unsafe.Pointer(p), C.int(n))
				if subtle.ConstantTimeCompare(word, words[positions[i]]) != 1 {
					matched = false
				}
				clear(word)
			}
		}
		C.gtk_entry_set_text(entry, empty)
	}
	C.free(unsafe.Pointer(empty))
	C.finish(d)
	if response == -3 && !matched {
		Notice("Recovery phrase not confirmed", "The recorded words did not match. Record the complete phrase in order, then try the confirmation again. Your wallet remains unavailable for payments until this is complete.")
	}
	return matched
}

// ReceiveAddress displays a signer-validated public address and its QR. The
// address is supplied separately from the public summary, never parsed from it.
// Copying requires the native Copy address button and does not close the view.
func ReceiveAddress(title, summary, address string, png []byte) bool {
	if address == "" || strings.ContainsRune(address, '\x00') || len(png) == 0 {
		return false
	}
	t, s, a := cstr(title), cstr(summary), cstr(address)
	defer C.free(unsafe.Pointer(t))
	defer C.free(unsafe.Pointer(s))
	defer C.free(unsafe.Pointer(a))
	d := C.new_receive(t)
	b := C.receive_body(d)
	C.add_label(b, s)
	if C.add_receive_png(b, (*C.uchar)(unsafe.Pointer(&png[0])), C.size_t(len(png))) == 0 {
		C.finish(d)
		return false
	}
	empty := cstr("")
	status := C.gtk_label_new(empty)
	C.free(unsafe.Pointer(empty))
	C.gtk_box_pack_start(C.gtk_dialog_get_content_area(d), status, 0, 0, 6)
	C.gtk_widget_show_all(d)
	r := C.gtk_dialog_run(d)
	for r == 1 {
		C.public_copy(a)
		copied := cstr("Address copied.")
		C.gtk_label_set_text(status, copied)
		C.free(unsafe.Pointer(copied))
		r = C.gtk_dialog_run(d)
	}
	C.finish(d)
	return r == -3
}

func display(title, summary string, png []byte) bool {
	t := cstr(title)
	defer C.free(unsafe.Pointer(t))
	d := C.new_dialog(t)
	b := C.gtk_dialog_get_content_area(d)
	parts := strings.SplitN(summary, "Exact chain effects:\n", 2)
	s := cstr(parts[0])
	C.add_label(b, s)
	C.free(unsafe.Pointer(s))
	if len(parts) == 2 {
		s = cstr(parts[1])
		C.add_scroll(b, s)
		C.free(unsafe.Pointer(s))
	}
	if len(png) > 0 {
		if C.add_png(b, (*C.uchar)(unsafe.Pointer(&png[0])), C.size_t(len(png))) == 0 {
			C.finish(d)
			return false
		}
	}
	C.gtk_widget_show_all(d)
	r := C.gtk_dialog_run(d)
	C.finish(d)
	return r == -3
}
func File(title string, save bool) (string, error) {
	t := cstr(title)
	defer C.free(unsafe.Pointer(t))
	var flag C.int
	if save {
		flag = 1
	}
	d := C.file_dialog(t, flag)
	r := C.gtk_dialog_run(d)
	var path string
	if r == -3 {
		p := C.gtk_file_chooser_get_filename(d)
		if p != nil {
			path = C.GoString(p)
			C.g_free(unsafe.Pointer(p))
		}
	}
	C.finish(d)
	if path == "" {
		return "", ErrCancelled
	}
	return path, nil
}

func (Native) CopyPublic(address string) error {
	s := cstr(address)
	defer C.free(unsafe.Pointer(s))
	C.public_copy(s)
	return nil
}
