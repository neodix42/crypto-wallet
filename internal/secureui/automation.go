//go:build automation

// Test-only native event injection. This file is absent from production binaries.
package secureui

/*
#cgo LDFLAGS: -l:libgtk-3.so.0 -l:libgobject-2.0.so.0 -l:libglib-2.0.so.0
#include <stdlib.h>
#include <string.h>
typedef struct L{void*data;struct L*next;struct L*prev;}L;
extern L *gtk_window_list_toplevels(void);
extern L *gtk_container_get_children(void*);
extern unsigned long gtk_entry_get_type(void);
extern int g_type_check_instance_is_a(void*,unsigned long);
extern void gtk_entry_set_text(void*,const char*);
extern int gtk_entry_get_visibility(void*);
extern int gtk_entry_get_max_length(void*);
extern unsigned long gtk_container_get_type(void);
extern unsigned long gtk_dialog_get_type(void);
extern unsigned long gtk_file_chooser_get_type(void);
extern int gtk_file_chooser_set_filename(void*,const char*);
extern void gtk_dialog_response(void*,int);
extern void g_list_free(void*);
extern unsigned int g_timeout_add(unsigned int,int(*)(void*),void*);
extern int g_source_remove(unsigned int);
static int automask=1;static int autoresponse=-3;static char*autovalue;static char*autofile;static int filestage;static unsigned int autoid;
static void autofill(void*w){if(g_type_check_instance_is_a(w,gtk_entry_get_type())){if(gtk_entry_get_visibility(w)||gtk_entry_get_max_length(w)!=4096)automask=0;gtk_entry_set_text(w,autovalue);return;}if(g_type_check_instance_is_a(w,gtk_container_get_type())){L*l=gtk_container_get_children(w);for(L*p=l;p;p=p->next)autofill(p->data);g_list_free(l);}}
static int autoevent(void*x){L*l=gtk_window_list_toplevels();int again=0;for(L*p=l;p;p=p->next){if(g_type_check_instance_is_a(p->data,gtk_dialog_get_type())){if(autofile&&g_type_check_instance_is_a(p->data,gtk_file_chooser_get_type())&&!filestage){gtk_file_chooser_set_filename(p->data,autofile);filestage=1;again=1;}else{if(!autofile)autofill(p->data);gtk_dialog_response(p->data,autoresponse);}}}g_list_free(l);if(again)return 1;autoid=0;memset(autovalue,0,strlen(autovalue));free(autovalue);autovalue=0;if(autofile){free(autofile);autofile=0;}return 0;}
static int get_mask(void){return automask;}
static void auto_cancel(void){if(autoid){g_source_remove(autoid);autoid=0;}if(autovalue){memset(autovalue,0,strlen(autovalue));free(autovalue);autovalue=0;}if(autofile){free(autofile);autofile=0;}}
static void schedule(const char*v,int r){auto_cancel();autovalue=strdup(v);autoresponse=r;automask=1;autoid=g_timeout_add(100,autoevent,0);}
static void auto_delay(unsigned int ms){if(autoid)g_source_remove(autoid);autoid=g_timeout_add(ms,autoevent,0);}
static void schedule_file(const char*path){schedule("",-3);autofile=strdup(path);filestage=0;auto_delay(300);}
*/
import "C"
import "unsafe"

func autoSchedule(value string, response int) {
	s := C.CString(value)
	defer C.free(unsafe.Pointer(s))
	C.schedule(s, C.int(response))
}
func autoMasked() bool              { return C.get_mask() == 1 }
func autoDelay(milliseconds uint32) { C.auto_delay(C.uint(milliseconds)) }
func autoCancel()                   { C.auto_cancel() }
func autoScheduleFile(path string) {
	s := C.CString(path)
	defer C.free(unsafe.Pointer(s))
	C.schedule_file(s)
}
