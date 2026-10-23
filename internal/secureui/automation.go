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
extern void gtk_window_get_size(void*,int*,int*);
extern void *gtk_dialog_get_widget_for_response(void*,int);
extern int gtk_widget_get_allocated_width(void*);
extern int gtk_widget_get_allocated_height(void*);
extern int gtk_widget_get_mapped(void*);
extern int gtk_widget_translate_coordinates(void*,void*,int,int,int*,int*);
extern void *gtk_widget_get_screen(void*);
extern int gdk_screen_get_width(void*);
extern int gdk_screen_get_height(void*);
extern void g_list_free(void*);
extern void *gdk_atom_intern_static_string(const char*);
extern void *gtk_clipboard_get(void*);
extern char *gtk_clipboard_wait_for_text(void*);
extern void g_free(void*);
extern unsigned int g_timeout_add(unsigned int,int(*)(void*),void*);
extern int g_source_remove(unsigned int);
static int inspect_receive;static int receive_layout[5];
static void measure_receive(void*d){int w,h;gtk_window_get_size(d,&w,&h);void*s=gtk_widget_get_screen(d);receive_layout[1]=w;receive_layout[2]=h;receive_layout[3]=gdk_screen_get_width(s);receive_layout[4]=gdk_screen_get_height(s);int fits=w<=receive_layout[3]&&h<=receive_layout[4];int responses[2]={1,-3};for(int i=0;i<2;i++){void*b=gtk_dialog_get_widget_for_response(d,responses[i]);int x,y;if(!b||!gtk_widget_get_mapped(b)||!gtk_widget_translate_coordinates(b,d,0,0,&x,&y)||x<0||y<0||x+gtk_widget_get_allocated_width(b)>w||y+gtk_widget_get_allocated_height(b)>h)fits=0;}receive_layout[0]=fits;}
static int automask=1;static int autoresponse=-3;static int autoafter;static char*autovalue;static char*autofile;static int filestage;static unsigned int autoid;
static void autofill(void*w){if(g_type_check_instance_is_a(w,gtk_entry_get_type())){if(gtk_entry_get_visibility(w)||gtk_entry_get_max_length(w)!=4096)automask=0;gtk_entry_set_text(w,autovalue);return;}if(g_type_check_instance_is_a(w,gtk_container_get_type())){L*l=gtk_container_get_children(w);for(L*p=l;p;p=p->next)autofill(p->data);g_list_free(l);}}
static int autoevent(void*x){L*l=gtk_window_list_toplevels();int again=0;for(L*p=l;p;p=p->next){if(g_type_check_instance_is_a(p->data,gtk_dialog_get_type())){if(autofile&&g_type_check_instance_is_a(p->data,gtk_file_chooser_get_type())&&!filestage){gtk_file_chooser_set_filename(p->data,autofile);filestage=1;again=1;}else{if(!autofile)autofill(p->data);if(inspect_receive){measure_receive(p->data);inspect_receive=0;}gtk_dialog_response(p->data,autoresponse);}}}g_list_free(l);if(again)return 1;if(autoafter){autoresponse=autoafter;autoafter=0;return 1;}autoid=0;memset(autovalue,0,strlen(autovalue));free(autovalue);autovalue=0;if(autofile){free(autofile);autofile=0;}return 0;}
static int get_mask(void){return automask;}
static void auto_cancel(void){autoafter=0;if(autoid){g_source_remove(autoid);autoid=0;}if(autovalue){memset(autovalue,0,strlen(autovalue));free(autovalue);autovalue=0;}if(autofile){free(autofile);autofile=0;}}
static void schedule(const char*v,int r){auto_cancel();autovalue=strdup(v);autoresponse=r;automask=1;autoid=g_timeout_add(100,autoevent,0);}
static void auto_delay(unsigned int ms){if(autoid)g_source_remove(autoid);autoid=g_timeout_add(ms,autoevent,0);}
static void schedule_file(const char*path){schedule("",-3);autofile=strdup(path);filestage=0;auto_delay(300);}
static void schedule_receive_copy(int after){schedule("",1);autoafter=after;}
static void schedule_receive_layout(void){schedule("",-3);memset(receive_layout,0,sizeof(receive_layout));inspect_receive=1;}
static int receive_metric(int index){return receive_layout[index];}
static char *public_clipboard_text(void){return gtk_clipboard_wait_for_text(gtk_clipboard_get(gdk_atom_intern_static_string("CLIPBOARD")));}
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

// These helpers read only public synthetic clipboard fixtures on the isolated
// test display. No clipboard-reading surface is linked into production builds.
func autoScheduleReceiveCopy(then int) { C.schedule_receive_copy(C.int(then)) }
func autoPublicClipboard() string {
	p := C.public_clipboard_text()
	if p == nil {
		return ""
	}
	defer C.g_free(unsafe.Pointer(p))
	return C.GoString(p)
}

func autoScheduleReceiveLayout() { C.schedule_receive_layout() }
func autoReceiveLayout() (fits bool, width, height, screenWidth, screenHeight int) {
	return C.receive_metric(0) != 0, int(C.receive_metric(1)), int(C.receive_metric(2)), int(C.receive_metric(3)), int(C.receive_metric(4))
}
