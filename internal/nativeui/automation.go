//go:build automation

// Synthetic private-display input helpers. These are absent from shipped builds.
package nativeui

/*
#include <stdlib.h>
#include <string.h>
#include "native.h"
typedef struct L{void *data;struct L *next,*prev;}L;
extern L *gtk_window_list_toplevels(void);
extern L *gtk_container_get_children(void*);
extern unsigned long gtk_entry_get_type(void);
extern unsigned long gtk_container_get_type(void);
extern unsigned long gtk_dialog_get_type(void);
extern int g_type_check_instance_is_a(void*,unsigned long);
extern int gtk_entry_get_visibility(void*);
extern int gtk_entry_get_max_length(void*);
extern void gtk_widget_grab_focus(void*);
extern void gtk_editable_set_position(void*,int);
extern void gtk_editable_select_region(void*,int,int);
extern void *g_object_get_data(void*,const char*);
extern void *gtk_notebook_get_nth_page(void*,int);
extern void *gtk_notebook_get_tab_label(void*,void*);
extern int gtk_widget_get_mapped(void*);
extern int gtk_widget_get_visible(void*);
extern void *gtk_bin_get_child(void*);
extern const char *gtk_label_get_text(void*);
extern unsigned long gtk_label_get_type(void);
extern void gtk_entry_set_text(void*,const char*);
extern void gtk_dialog_response(void*,int);
extern unsigned long gtk_file_chooser_get_type(void);
extern int gtk_file_chooser_set_current_folder(void*,const char*);
extern void gtk_file_chooser_set_current_name(void*,const char*);
extern void g_list_free(void*);
extern unsigned g_timeout_add(unsigned,int(*)(void*),void*);
extern int g_source_remove(unsigned);
extern void *gdk_atom_intern_static_string(const char*);
extern void *gtk_clipboard_get(void*);
extern char *gtk_clipboard_wait_for_text(void*);
extern void g_free(void*);
static unsigned test_timer;
static int test_masked,test_response,test_after;
static char *test_value;
static char *test_folder,*test_name;static int test_file_staged;
static void test_fill(void*w){if(g_type_check_instance_is_a(w,gtk_entry_get_type())){if(gtk_entry_get_visibility(w)||gtk_entry_get_max_length(w)!=4096)test_masked=0;gtk_entry_set_text(w,test_value);return;}if(g_type_check_instance_is_a(w,gtk_container_get_type())){L*l=gtk_container_get_children(w);for(L*p=l;p;p=p->next)test_fill(p->data);g_list_free(l);}}
static int test_respond(void*x){L*l=gtk_window_list_toplevels();int again=0;for(L*p=l;p;p=p->next)if(g_type_check_instance_is_a(p->data,gtk_dialog_get_type())){if(test_folder&&!test_file_staged&&g_type_check_instance_is_a(p->data,gtk_file_chooser_get_type())){gtk_file_chooser_set_current_folder(p->data,test_folder);gtk_file_chooser_set_current_name(p->data,test_name);test_file_staged=1;again=1;}else{if(test_value[0])test_fill(p->data);gtk_dialog_response(p->data,test_response);}}g_list_free(l);if(again)return 1;if(test_after){test_response=test_after;test_after=0;return 1;}test_timer=0;memset(test_value,0,strlen(test_value));free(test_value);test_value=0;if(test_folder){free(test_folder);free(test_name);test_folder=test_name=0;}return 0;}
static void test_schedule(const char*v,int response,int after){if(test_timer)g_source_remove(test_timer);if(test_value){memset(test_value,0,strlen(test_value));free(test_value);}test_value=strdup(v);test_masked=1;test_response=response;test_after=after;test_timer=g_timeout_add(80,test_respond,0);}
static int test_is_masked(void){return test_masked;}
static void test_choose_file(const char*folder,const char*name){test_folder=strdup(folder);test_name=strdup(name);test_file_staged=0;}
static char *test_clipboard(void){return gtk_clipboard_wait_for_text(gtk_clipboard_get(gdk_atom_intern_static_string("CLIPBOARD")));}
static void *test_find(void*w,const char*id){const char*s=g_object_get_data(w,"wallet-input-id");if(s&&!strcmp(s,id))return w;if(g_type_check_instance_is_a(w,gtk_container_get_type())){L*l=gtk_container_get_children(w);void*found=0;for(L*p=l;p&&!found;p=p->next)found=test_find(p->data,id);g_list_free(l);return found;}return 0;}
static int test_focus_entry(void*w,const char*id,int position,int start,int end){void*e=test_find(w,id);if(!e)return 0;gtk_widget_grab_focus(e);gtk_editable_set_position(e,position);if(start>=0)gtk_editable_select_region(e,start,end);return 1;}
static int test_visible_tab(void*n,int index){void*p=gtk_notebook_get_nth_page(n,index);if(!p)return 0;void*t=gtk_notebook_get_tab_label(n,p);if(!t)return 1;if(!gtk_widget_get_visible(t))return 2;void*l=gtk_bin_get_child(t);if(!l)return 3;if(!gtk_widget_get_visible(l))return 4;const char*s=gtk_label_get_text(l);if(!s||!s[0])return 5;return 6;}
static int test_plain_label(void*w,const char*text){if(g_type_check_instance_is_a(w,gtk_label_get_type())){const char*s=gtk_label_get_text(w);return s&&!strcmp(s,text)&&gtk_widget_get_visible(w);}if(g_type_check_instance_is_a(w,gtk_container_get_type())){L*l=gtk_container_get_children(w);int found=0;for(L*p=l;p&&!found;p=p->next)found=test_plain_label(p->data,text);g_list_free(l);return found;}return 0;}
*/
import "C"

import "unsafe"

func autoSchedule(value string, response, after int) {
	p := C.CString(value)
	defer C.free(unsafe.Pointer(p))
	C.test_schedule(p, C.int(response), C.int(after))
}
func autoMasked() bool { return C.test_is_masked() != 0 }
func autoScheduleFile(folder, name string) {
	autoSchedule("", -3, 0)
	f := C.CString(folder)
	n := C.CString(name)
	defer C.free(unsafe.Pointer(f))
	defer C.free(unsafe.Pointer(n))
	C.test_choose_file(f, n)
}
func autoClipboard() string {
	p := C.test_clipboard()
	if p == nil {
		return ""
	}
	defer C.g_free(unsafe.Pointer(p))
	return C.GoString(p)
}

func (w *Window) autoFocus(id string, position, start, end int) bool {
	p := C.CString(id)
	defer C.free(unsafe.Pointer(p))
	return C.test_focus_entry(w.widget, p, C.int(position), C.int(start), C.int(end)) != 0
}

func (w *Window) autoFocusState() (id string, position, start, end int) {
	var p, s, e, entry C.int
	value := C.wallet_ui_focus(w.widget, &p, &s, &e, &entry)
	if value != nil {
		id = C.GoString(value)
		C.free(unsafe.Pointer(value))
	}
	return id, int(p), int(s), int(e)
}
func (w *Window) autoActive() string {
	p := C.wallet_ui_active(w.book)
	if p == nil {
		return ""
	}
	return C.GoString(p)
}
func (w *Window) autoVisibleTab(index int) int { return int(C.test_visible_tab(w.book, C.int(index))) }
func (w *Window) autoStatus() string {
	key := C.CString("wallet-status")
	defer C.free(unsafe.Pointer(key))
	p := C.g_object_get_data(w.widget, key)
	if p == nil {
		return ""
	}
	return C.GoString(C.gtk_label_get_text(p))
}
func (w *Window) autoPlainLabel(text string) bool {
	p := C.CString(text)
	defer C.free(unsafe.Pointer(p))
	return C.test_plain_label(w.widget, p) != 0
}

func (w *Window) autoSetPublicEntry(id, value string) bool {
	key := C.CString(id)
	text := C.CString(value)
	defer C.free(unsafe.Pointer(key))
	defer C.free(unsafe.Pointer(text))
	entry := C.test_find(w.widget, key)
	if entry == nil {
		return false
	}
	C.gtk_entry_set_text(entry, text)
	return true
}

func (w *Window) autoActivity() { nativeUIActivity(C.uintptr_t(w.id)) }

func (w *Window) autoClosePublic() { C.wallet_ui_close(w.widget) }

func (w *Window) autoLateCallbacks() {
	id := C.uintptr_t(w.id)
	nativeUITick(id)
	nativeUIClosed(id)
	nativeUIActivity(id)
	ctext("expired-public-event", func(p *C.char) { ctext("", func(v *C.char) { nativeUIEvent(id, p, v) }) })
}
