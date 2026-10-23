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
extern void*gtk_scrolled_window_new(void*,void*);
extern void gtk_scrolled_window_set_policy(void*,int,int);
extern void gtk_container_add(void*,void*);
extern void gtk_widget_set_size_request(void*,int,int);
extern void gtk_box_pack_start(void*,void*,int,int,unsigned);
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
static void *add_entry(void*b,const char*l){add_label(b,l);void*e=gtk_entry_new();gtk_entry_set_visibility(e,0);gtk_entry_set_input_purpose(e,8);gtk_entry_set_input_hints(e,1026);gtk_entry_set_max_length(e,4096);gtk_box_pack_start(b,e,0,0,8);return e;}
static void finish(void*d){gtk_widget_destroy(d);while(gtk_events_pending())gtk_main_iteration();}
static void *file_dialog(const char*t,int save){void*d=gtk_file_chooser_dialog_new(t,0,save?1:0,"Cancel",-6,save?"Save":"Open",-3,(char*)0);gtk_file_chooser_set_do_overwrite_confirmation(d,1);return d;}
static int add_png(void*b,const unsigned char*p,size_t n){void*l=gdk_pixbuf_loader_new();if(!gdk_pixbuf_loader_write(l,p,n,0)||!gdk_pixbuf_loader_close(l,0)){g_object_unref(l);return 0;}void*i=gtk_image_new_from_pixbuf(gdk_pixbuf_loader_get_pixbuf(l));gtk_box_pack_start(b,i,0,0,6);g_object_unref(l);return 1;}
*/
import "C"
import (
	"errors"
	"runtime"
	"strings"
	"unsafe"
)

var ErrCancelled = errors.New("native request cancelled or unavailable")

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
	var result [][]byte
	if response == -3 {
		for _, e := range entries {
			p := C.gtk_entry_get_text(e)
			n := C.strlen(p)
			if n > 4096 {
				response = -6
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
		return nil, ErrCancelled
	}
	return result, nil
}
func Confirm(title, summary string) bool             { return display(title, summary, nil) }
func Receive(title, summary string, png []byte) bool { return display(title, summary, png) }
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

func (Native) CopyPublic(address string) {
	s := cstr(address)
	defer C.free(unsafe.Pointer(s))
	C.public_copy(s)
}
