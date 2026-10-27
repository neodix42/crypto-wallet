#include "native.h"
#include <stdlib.h>
#include <string.h>
/* This intentionally small GTK3 ABI binding needs runtime libraries, not GTK
 * development headers. Strings are always plain text: no HTML or GTK markup. */
extern int gtk_init_check(int*,char***);
extern void *gtk_window_new(int);
extern void gtk_window_set_title(void*,const char*);
extern void gtk_window_set_default_size(void*,int,int);
extern void gtk_window_set_position(void*,int);
extern void *gtk_window_get_focus(void*);
extern void gtk_container_add(void*,void*);
extern void gtk_container_set_border_width(void*,unsigned);
extern void *gtk_box_new(int,int);
extern void gtk_box_pack_start(void*,void*,int,int,unsigned);
extern void *gtk_notebook_new(void);
extern void gtk_notebook_set_scrollable(void*,int);
extern int gtk_notebook_append_page(void*,void*,void*);
extern int gtk_notebook_get_n_pages(void*);
extern int gtk_notebook_get_current_page(void*);
extern void *gtk_notebook_get_nth_page(void*,int);
extern void *gtk_notebook_get_tab_label(void*,void*);
extern void gtk_notebook_remove_page(void*,int);
extern void gtk_notebook_set_current_page(void*,int);
extern void *gtk_scrolled_window_new(void*,void*);
extern void gtk_scrolled_window_set_policy(void*,int,int);
extern void gtk_scrolled_window_set_min_content_height(void*,int);
extern void gtk_scrolled_window_set_max_content_height(void*,int);
extern void *gtk_label_new(const char*);
extern void gtk_label_set_xalign(void*,float);
extern void gtk_label_set_line_wrap(void*,int);
extern void gtk_label_set_selectable(void*,int);
extern void gtk_label_set_text(void*,const char*);
extern void *gtk_separator_new(int);
extern void *gtk_button_new_with_label(const char*);
extern void *gtk_entry_new(void);
extern void gtk_entry_set_text(void*,const char*);
extern const char *gtk_entry_get_text(void*);
extern void gtk_entry_set_visibility(void*,int);
extern void gtk_entry_set_input_purpose(void*,int);
extern void gtk_entry_set_input_hints(void*,int);
extern void gtk_entry_set_max_length(void*,int);
extern void gtk_entry_set_width_chars(void*,int);
extern void gtk_editable_set_editable(void*,int);
extern int gtk_editable_get_position(void*);
extern void gtk_editable_set_position(void*,int);
extern int gtk_editable_get_selection_bounds(void*,int*,int*);
extern void gtk_editable_select_region(void*,int,int);
extern void *gtk_combo_box_text_new(void);
extern void gtk_combo_box_text_append(void*,const char*,const char*);
extern int gtk_combo_box_set_active_id(void*,const char*);
extern const char *gtk_combo_box_get_active_id(void*);
extern void *gtk_check_button_new_with_label(const char*);
extern void gtk_toggle_button_set_active(void*,int);
extern int gtk_toggle_button_get_active(void*);
extern void *gtk_event_box_new(void);
extern void gtk_event_box_set_visible_window(void*,int);
extern void gtk_widget_add_events(void*,int);
extern int gdk_event_get_button(void*,unsigned*);
extern void gtk_widget_set_sensitive(void*,int);
extern void gtk_widget_set_tooltip_text(void*,const char*);
extern void *gtk_widget_get_parent(void*);
extern int gtk_widget_get_sensitive(void*);
extern void gtk_widget_grab_focus(void*);
extern unsigned long gtk_container_get_type(void);
typedef struct wallet_list{void*data;struct wallet_list*next,*prev;}wallet_list;
extern wallet_list *gtk_container_get_children(void*);
extern void g_list_free(void*);
extern void *gtk_get_event_widget(void*);
extern unsigned long gtk_button_get_type(void);
extern unsigned long gtk_entry_get_type(void);
extern unsigned long gtk_combo_box_get_type(void);
extern int g_type_check_instance_is_a(void*,unsigned long);
extern void gtk_widget_set_size_request(void*,int,int);
extern void gtk_widget_set_margin_top(void*,int);
extern void gtk_widget_set_margin_bottom(void*,int);
extern void gtk_widget_show_all(void*);
extern void gtk_widget_destroy(void*);
extern void gtk_main(void);
extern void gtk_main_quit(void);
extern unsigned long g_signal_connect_data(void*,const char*,void(*)(void),void*,void(*)(void*,void*),int);
extern void g_object_set_data_full(void*,const char*,void*,void(*)(void*));
extern void *g_object_get_data(void*,const char*);
extern unsigned g_timeout_add(unsigned,int(*)(void*),void*);
extern void *g_timeout_source_new(unsigned);
extern void g_source_set_callback(void*,int(*)(void*),void*,void(*)(void*));
extern void g_source_set_can_recurse(void*,int);
extern unsigned g_source_attach(void*,void*);
extern void g_source_unref(void*);
extern void *gtk_dialog_new_with_buttons(const char*,void*,int,const char*,...);
extern void *gtk_dialog_get_content_area(void*);
extern int gtk_dialog_run(void*);
extern void gtk_dialog_response(void*,int);
extern void *gtk_file_chooser_dialog_new(const char*,void*,int,const char*,...);
extern void gtk_file_chooser_set_do_overwrite_confirmation(void*,int);
extern char *gtk_file_chooser_get_filename(void*);
extern void *gdk_pixbuf_loader_new(void);
extern int gdk_pixbuf_loader_write(void*,const unsigned char*,size_t,void*);
extern int gdk_pixbuf_loader_close(void*,void*);
extern void *gdk_pixbuf_loader_get_pixbuf(void*);
extern void *gdk_pixbuf_scale_simple(void*,int,int,int);
extern void *gtk_image_new_from_pixbuf(void*);
extern void g_object_unref(void*);
extern void *gdk_atom_intern_static_string(const char*);
extern void *gtk_clipboard_get(void*);
extern void gtk_clipboard_set_text(void*,const char*,int);
extern void gtk_clipboard_store(void*);
extern void g_free(void*);
extern void gdk_event_handler_set(void(*)(void*,void*),void*,void(*)(void*));
extern int gdk_event_get_event_type(void*);
extern void gtk_main_do_event(void*);
extern void nativeUIEvent(uintptr_t,char*,char*);
extern void nativeUITick(uintptr_t);
extern void nativeUIClosed(uintptr_t);
extern void nativeUIActivity(uintptr_t);

typedef struct { uintptr_t owner; char *id; int kind; } binding;
static binding *bind_new(uintptr_t owner,const char *id,int kind){binding*b=calloc(1,sizeof(*b));if(!b)return 0;b->owner=owner;b->id=strdup(id);b->kind=kind;return b;}
static void bind_free(void *data,void *closure){binding*b=data;if(b){free(b->id);free(b);}}
static void changed(void *widget,void *data){binding*b=data;const char*v="";if(b->kind==1)v=gtk_entry_get_text(widget);if(b->kind==2){v=gtk_combo_box_get_active_id(widget);if(!v)v="";}if(b->kind==3)v=gtk_toggle_button_get_active(widget)?"true":"false";nativeUIEvent(b->owner,b->id,(char*)v);}
static void connect_event(void *widget,const char *signal,uintptr_t owner,const char *id,int kind){binding*b=bind_new(owner,id,kind);if(b)g_signal_connect_data(widget,signal,(void(*)(void))changed,b,bind_free,0);if(kind)g_object_set_data_full(widget,"wallet-input-id",strdup(id),free);}
static void closed(void *widget,void *owner){nativeUIClosed((uintptr_t)owner);gtk_main_quit();}
static int tick(void *owner){nativeUITick((uintptr_t)owner);return 1;}
static void switch_page(void *book,void *page,unsigned number,void *owner){const char*id=g_object_get_data(page,"wallet-page-id");if(id)nativeUIEvent((uintptr_t)owner,"tab",(char*)id);}
static int tab_button(void *widget,void *event,void *data){unsigned button=0;binding*b=data;if(gdk_event_get_button(event,&button)&&button==2){nativeUIEvent(b->owner,"closeTab",b->id);return 1;}return 0;}
static int row_button(void *widget,void *event,void *data){unsigned button=0;binding*b=data;for(void*target=gtk_get_event_widget(event);target&&target!=widget;target=gtk_widget_get_parent(target))if(g_type_check_instance_is_a(target,gtk_button_get_type())||g_type_check_instance_is_a(target,gtk_entry_get_type())||g_type_check_instance_is_a(target,gtk_combo_box_get_type()))return 0;if(gdk_event_get_button(event,&button)&&button==1){nativeUIEvent(b->owner,b->id,"");return 1;}return 0;}
static void activity_dispatch(void *event,void *owner){int type=gdk_event_get_event_type(event);if(type==4||type==8)nativeUIActivity((uintptr_t)owner);gtk_main_do_event(event);}
static void pack(void*parent,void*child,int expand){gtk_box_pack_start(parent,child,expand,expand,4);}
int wallet_ui_init(void){return gtk_init_check(0,0);}
void *wallet_ui_window(uintptr_t owner){void*w=gtk_window_new(0);gtk_window_set_default_size(w,1440,960);gtk_window_set_title(w,"CryptoWallet");void*b=gtk_box_new(1,8);gtk_container_set_border_width(b,12);gtk_container_add(w,b);void*n=gtk_notebook_new();gtk_notebook_set_scrollable(n,1);pack(b,n,1);void*s=gtk_scrolled_window_new(0,0);gtk_scrolled_window_set_policy(s,1,2);gtk_widget_set_size_request(s,-1,52);gtk_scrolled_window_set_min_content_height(s,52);gtk_scrolled_window_set_max_content_height(s,52);void*l=gtk_label_new("");gtk_label_set_xalign(l,0);gtk_container_add(s,l);pack(b,s,0);g_object_set_data_full(w,"wallet-book",n,0);g_object_set_data_full(w,"wallet-status",l,0);g_signal_connect_data(w,"destroy",(void(*)(void))closed,(void*)owner,0,0);g_signal_connect_data(n,"switch-page",(void(*)(void))switch_page,(void*)owner,0,0);void*source=g_timeout_source_new(20);g_source_set_callback(source,tick,(void*)owner,0);g_source_set_can_recurse(source,1);g_source_attach(source,0);g_source_unref(source);gdk_event_handler_set(activity_dispatch,(void*)owner,0);return w;}
void wallet_ui_run(void){gtk_main();}
void wallet_ui_close(void*w){gtk_widget_destroy(w);}
void wallet_ui_title(void*w,const char*t){gtk_window_set_title(w,t);}
void *wallet_ui_book(void*w){return g_object_get_data(w,"wallet-book");}
void wallet_ui_clear(void*n){while(gtk_notebook_get_n_pages(n)>0)gtk_notebook_remove_page(n,0);}
void *wallet_ui_page(void*n,uintptr_t owner,const char*id,const char*title){void*s=gtk_scrolled_window_new(0,0);gtk_scrolled_window_set_policy(s,1,1);void*b=gtk_box_new(1,6);gtk_container_set_border_width(b,12);gtk_container_add(s,b);void*l=gtk_label_new(title);void*e=gtk_event_box_new();gtk_event_box_set_visible_window(e,0);gtk_widget_add_events(e,1<<8);gtk_container_add(e,l);binding*binding=bind_new(owner,id,0);if(binding)g_signal_connect_data(e,"button-press-event",(void(*)(void))tab_button,binding,bind_free,0);g_object_set_data_full(s,"wallet-page-id",strdup(id),free);gtk_widget_show_all(e);gtk_notebook_append_page(n,s,e);return b;}
void wallet_ui_activate(void*n,const char*id){int count=gtk_notebook_get_n_pages(n);for(int i=0;i<count;i++){const char*p=g_object_get_data(gtk_notebook_get_nth_page(n,i),"wallet-page-id");if(p&&!strcmp(p,id)){gtk_notebook_set_current_page(n,i);break;}}}
const char *wallet_ui_active(void*n){void*p=gtk_notebook_get_nth_page(n,gtk_notebook_get_current_page(n));return p?g_object_get_data(p,"wallet-page-id"):0;}
char *wallet_ui_focus(void*w,int*position,int*start,int*end,int*entry){void*f=gtk_window_get_focus(w);*position=0;*start=-1;*end=-1;*entry=0;for(void*p=f;p;p=gtk_widget_get_parent(p)){const char*id=g_object_get_data(p,"wallet-input-id");if(id){if(g_type_check_instance_is_a(p,gtk_entry_get_type())){*entry=1;*position=gtk_editable_get_position(p);if(!gtk_editable_get_selection_bounds(p,start,end)){*start=-1;*end=-1;}}return strdup(id);}}return 0;}
static void *find_input(void*w,const char*id){const char*actual=g_object_get_data(w,"wallet-input-id");if(actual&&!strcmp(actual,id))return w;if(g_type_check_instance_is_a(w,gtk_container_get_type())){wallet_list*l=gtk_container_get_children(w);void*found=0;for(wallet_list*p=l;p&&!found;p=p->next)found=find_input(p->data,id);g_list_free(l);return found;}return 0;}
void wallet_ui_restore_focus(void*n,const char*page,const char*id,int position,int start,int end,int entry){const char*active=wallet_ui_active(n);if(!active||strcmp(active,page))return;void*p=gtk_notebook_get_nth_page(n,gtk_notebook_get_current_page(n));void*w=find_input(p,id);if(!w||!gtk_widget_get_sensitive(w))return;gtk_widget_grab_focus(w);if(entry&&g_type_check_instance_is_a(w,gtk_entry_get_type())){gtk_editable_set_position(w,position);if(start>=0&&end>=0)gtk_editable_select_region(w,start,end);}}
void wallet_ui_status(void*w,const char*t){gtk_label_set_text(g_object_get_data(w,"wallet-status"),t);}
void *wallet_ui_box(void*p,int horizontal){void*b=gtk_box_new(horizontal?0:1,8);pack(p,b,0);return b;}
void *wallet_ui_row(void*p,uintptr_t owner,const char*id){void*e=gtk_event_box_new();gtk_event_box_set_visible_window(e,0);gtk_widget_add_events(e,1<<8);void*b=gtk_box_new(0,8);gtk_container_add(e,b);binding*binding=bind_new(owner,id,0);if(binding)g_signal_connect_data(e,"button-press-event",(void(*)(void))row_button,binding,bind_free,0);pack(p,e,0);return b;}
void *wallet_ui_label(void*p,const char*t,int heading){void*l=gtk_label_new(t);gtk_label_set_xalign(l,0);gtk_label_set_line_wrap(l,1);if(heading){gtk_widget_set_margin_top(l,10);gtk_widget_set_margin_bottom(l,8);}pack(p,l,0);return l;}
void *wallet_ui_address(void*p,const char*t){void*e=gtk_entry_new();gtk_entry_set_text(e,t);gtk_editable_set_editable(e,0);size_t n=strlen(t);gtk_entry_set_width_chars(e,n>128?128:(int)n);pack(p,e,1);return e;}
void wallet_ui_tooltip(void*w,const char*t){gtk_widget_set_tooltip_text(w,t);}
void wallet_ui_separator(void*p){pack(p,gtk_separator_new(0),0);}
void *wallet_ui_button(void*p,uintptr_t owner,const char*id,const char*t){void*w=gtk_button_new_with_label(t);connect_event(w,"clicked",owner,id,0);pack(p,w,0);return w;}
void *wallet_ui_entry(void*p,uintptr_t owner,const char*id,const char*t){void*w=gtk_entry_new();gtk_entry_set_max_length(w,4096);gtk_entry_set_text(w,t);connect_event(w,"changed",owner,id,1);pack(p,w,0);return w;}
void *wallet_ui_combo(void*p,uintptr_t owner,const char*id){void*w=gtk_combo_box_text_new();connect_event(w,"changed",owner,id,2);pack(p,w,0);return w;}
void wallet_ui_combo_option(void*w,const char*id,const char*t){gtk_combo_box_text_append(w,id,t);}
void wallet_ui_combo_value(void*w,const char*id){gtk_combo_box_set_active_id(w,id);}
void *wallet_ui_check(void*p,uintptr_t owner,const char*id,const char*t,int active){void*w=gtk_check_button_new_with_label(t);gtk_toggle_button_set_active(w,active);connect_event(w,"toggled",owner,id,3);pack(p,w,0);return w;}
void wallet_ui_sensitive(void*w,int sensitive){gtk_widget_set_sensitive(w,sensitive);}
void wallet_ui_show(void*w){gtk_widget_show_all(w);void*n=g_object_get_data(w,"wallet-book");if(n){int count=gtk_notebook_get_n_pages(n);for(int i=0;i<count;i++){void*p=gtk_notebook_get_nth_page(n,i);void*t=gtk_notebook_get_tab_label(n,p);if(t)gtk_widget_show_all(t);}}}
/* kind: 0=input/confirm, 1=notice, 2=receive, 3=recovery, 4=backup. */
void *wallet_ui_dialog(void*p,const char*t,int kind){void*d;if(kind==1)d=gtk_dialog_new_with_buttons(t,p,1,"Close",-7,(char*)0);else if(kind==2)d=gtk_dialog_new_with_buttons(t,p,1,"Copy address",1,"Done",-3,(char*)0);else if(kind==3)d=gtk_dialog_new_with_buttons(t,p,1,"Cancel",-6,"I wrote it down",-3,(char*)0);else if(kind==4)d=gtk_dialog_new_with_buttons(t,p,1,"Copy phrase",1,"Done",-3,(char*)0);else d=gtk_dialog_new_with_buttons(t,p,1,"Cancel",-6,"OK",-3,(char*)0);gtk_window_set_default_size(d,680,kind==0?180:460);return d;}
void *wallet_ui_body(void*d){void*s=gtk_scrolled_window_new(0,0);gtk_scrolled_window_set_policy(s,2,1);gtk_scrolled_window_set_max_content_height(s,600);void*b=gtk_box_new(1,6);gtk_container_set_border_width(b,14);gtk_container_add(s,b);pack(gtk_dialog_get_content_area(d),s,1);return b;}
void wallet_ui_input_height(void*d,int count){gtk_window_set_default_size(d,680,112+68*count);}
void *wallet_ui_secret(void*p,const char*l){wallet_ui_label(p,l,0);void*e=gtk_entry_new();gtk_entry_set_visibility(e,0);gtk_entry_set_input_purpose(e,8);gtk_entry_set_input_hints(e,1026);gtk_entry_set_max_length(e,4096);pack(p,e,0);return e;}
const char *wallet_ui_secret_text(void*e){return gtk_entry_get_text(e);}
void wallet_ui_secret_clear(void*e){gtk_entry_set_text(e,"");}
void *wallet_ui_phrase(void*p,const char*t){void*l=gtk_label_new(t);gtk_label_set_xalign(l,0);gtk_label_set_selectable(l,0);pack(p,l,0);return l;}
void wallet_ui_phrase_clear(void*l){gtk_label_set_text(l,"");}
int wallet_ui_png(void*p,const unsigned char*b,size_t n){void*l=gdk_pixbuf_loader_new();if(!gdk_pixbuf_loader_write(l,b,n,0)||!gdk_pixbuf_loader_close(l,0)){g_object_unref(l);return 0;}void*s=gdk_pixbuf_scale_simple(gdk_pixbuf_loader_get_pixbuf(l),320,320,0);if(!s){g_object_unref(l);return 0;}void*i=gtk_image_new_from_pixbuf(s);pack(p,i,0);g_object_unref(s);g_object_unref(l);return 1;}
int wallet_ui_response(void*d){gtk_widget_show_all(d);return gtk_dialog_run(d);}
void wallet_ui_destroy(void*d){gtk_widget_destroy(d);}
void wallet_ui_cancel(void*d){gtk_dialog_response(d,-6);}
void wallet_ui_clipboard(const char*t,int n){void*c=gtk_clipboard_get(gdk_atom_intern_static_string("CLIPBOARD"));gtk_clipboard_set_text(c,t,n);gtk_clipboard_store(c);}
void *wallet_ui_file(void*p,const char*t,int save){void*d=gtk_file_chooser_dialog_new(t,p,save?1:0,"Cancel",-6,save?"Save":"Open",-3,(char*)0);gtk_file_chooser_set_do_overwrite_confirmation(d,1);return d;}
char *wallet_ui_filename(void*d){return gtk_file_chooser_get_filename(d);}
void wallet_ui_free(void*p){g_free(p);}
void wallet_ui_secret_free(void*p,size_t n){if(p){volatile unsigned char*v=p;while(n--)*v++=0;free(p);}}
