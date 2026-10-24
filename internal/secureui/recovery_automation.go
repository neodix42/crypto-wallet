//go:build automation

package secureui

/*
#include <stdlib.h>
#include <string.h>
typedef struct L{void*data;struct L*next;struct L*prev;}L;
extern L *gtk_window_list_toplevels(void);
extern L *gtk_container_get_children(void*);
extern unsigned long gtk_entry_get_type(void);
extern unsigned long gtk_label_get_type(void);
extern unsigned long gtk_container_get_type(void);
extern unsigned long gtk_dialog_get_type(void);
extern int g_type_check_instance_is_a(void*,unsigned long);
extern void gtk_entry_set_text(void*,const char*);
extern const char *gtk_entry_get_text(void*);
extern int gtk_entry_get_visibility(void*);
extern int gtk_entry_get_max_length(void*);
extern const char *gtk_label_get_text(void*);
extern int gtk_label_get_selectable(void*);
extern void gtk_dialog_response(void*,int);
extern void *gtk_dialog_get_widget_for_response(void*,int);
extern void gtk_window_get_size(void*,int*,int*);
extern int gtk_widget_get_mapped(void*);
extern int gtk_widget_get_allocated_width(void*);
extern int gtk_widget_get_allocated_height(void*);
extern int gtk_widget_translate_coordinates(void*,void*,int,int,int*,int*);
extern void *gtk_widget_get_screen(void*);
extern int gdk_screen_get_width(void*);
extern int gdk_screen_get_height(void*);
extern void g_list_free(void*);
extern unsigned long g_signal_connect_data(void*,const char*,void(*)(void*,void*),void*,void*,int);
extern unsigned int g_timeout_add(unsigned int,int(*)(void*),void*);
extern int g_source_remove(unsigned int);
// Only synthetic known BIP39 fixtures are injected or inspected here. These
// helpers and every inspection/response hook are absent from production builds.
static int recovery_mode,recovery_phase,recovery_count,recovery_words;
static unsigned int recovery_timer;
static int recovery_results[9];
static void recovery_destroy(void*w,void*kind){const char*s=(kind==(void*)1)?gtk_label_get_text(w):gtk_entry_get_text(w);if(*s)recovery_results[0]=0;else if(kind==(void*)1)recovery_results[1]++;else recovery_results[2]++;}
static void recovery_inspect(void*w){
 if(g_type_check_instance_is_a(w,gtk_label_get_type())){const char*s=gtk_label_get_text(w);if(strncmp(s,"1. ",3)==0){recovery_results[3]++;if(gtk_label_get_selectable(w))recovery_results[0]=0;int count=0;for(const char*p=s;*p;p++)if(*p=='\n')count++;if(count!=recovery_words)recovery_results[0]=0;g_signal_connect_data(w,"destroy",recovery_destroy,(void*)1,0,0);}}
 if(g_type_check_instance_is_a(w,gtk_entry_get_type())){if(gtk_entry_get_visibility(w)||gtk_entry_get_max_length(w)!=4096)recovery_results[0]=0;const char*value=recovery_count==2?(recovery_words==24?"art":"about"):"abandon";if(recovery_mode==3)value="wrong";if(recovery_mode==4)value="xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx";gtk_entry_set_text(w,value);recovery_count++;g_signal_connect_data(w,"destroy",recovery_destroy,(void*)2,0,0);return;}
 if(g_type_check_instance_is_a(w,gtk_container_get_type())){L*l=gtk_container_get_children(w);for(L*p=l;p;p=p->next)recovery_inspect(p->data);g_list_free(l);}
}
static void recovery_measure(void*d){int w,h;gtk_window_get_size(d,&w,&h);void*s=gtk_widget_get_screen(d);if(w>gdk_screen_get_width(s)||h>gdk_screen_get_height(s))recovery_results[0]=0;int responses[2]={-6,-3};for(int i=0;i<2;i++){void*b=gtk_dialog_get_widget_for_response(d,responses[i]);int x,y;if(!b||!gtk_widget_get_mapped(b)||!gtk_widget_translate_coordinates(b,d,0,0,&x,&y)||x<0||y<0||x+gtk_widget_get_allocated_width(b)>w||y+gtk_widget_get_allocated_height(b)>h)recovery_results[0]=0;}if(recovery_phase==0){recovery_results[4]=w;recovery_results[5]=h;}else{recovery_results[6]=w;recovery_results[7]=h;}}
static int recovery_event(void*unused){L*l=gtk_window_list_toplevels();int found=0;for(L*p=l;p;p=p->next){void*d=p->data;if(!g_type_check_instance_is_a(d,gtk_dialog_get_type())||!gtk_widget_get_mapped(d))continue;found=1;if(recovery_phase<2){recovery_count=0;recovery_inspect(d);recovery_measure(d);int cancel=(recovery_phase==0&&recovery_mode==1)||(recovery_phase==1&&recovery_mode==2);gtk_dialog_response(d,cancel?-6:-3);}else gtk_dialog_response(d,-7);break;}g_list_free(l);if(!found)return 1;recovery_results[8]++;recovery_phase++;if((recovery_mode==1&&recovery_phase==1)||(recovery_mode<3&&recovery_phase==2)||recovery_phase==3){recovery_timer=0;return 0;}return 1;}
static void recovery_cancel(void){if(recovery_timer){g_source_remove(recovery_timer);recovery_timer=0;}}
static void recovery_schedule(int mode,int words){recovery_cancel();memset(recovery_results,0,sizeof(recovery_results));recovery_results[0]=1;recovery_mode=mode;recovery_phase=0;recovery_words=words;recovery_timer=g_timeout_add(100,recovery_event,0);}
static int recovery_result(int index){return recovery_results[index];}
*/
import "C"

func autoScheduleRecovery(mode, words int) { C.recovery_schedule(C.int(mode), C.int(words)) }
func autoCancelRecovery()                  { C.recovery_cancel() }
func autoRecoveryResults() (ok bool, clearedLabels, clearedEntries, phraseLabels, width, height, checkWidth, checkHeight, dialogs int) {
	return C.recovery_result(0) != 0, int(C.recovery_result(1)), int(C.recovery_result(2)), int(C.recovery_result(3)), int(C.recovery_result(4)), int(C.recovery_result(5)), int(C.recovery_result(6)), int(C.recovery_result(7)), int(C.recovery_result(8))
}
