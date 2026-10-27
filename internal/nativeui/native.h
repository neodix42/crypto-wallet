#ifndef WALLET_NATIVE_UI_H
#define WALLET_NATIVE_UI_H
#include <stdint.h>
#include <stddef.h>
int wallet_ui_init(void);
void *wallet_ui_window(uintptr_t owner);
void wallet_ui_run(void);
void wallet_ui_close(void *window);
void wallet_ui_title(void *window, const char *title);
void *wallet_ui_book(void *window);
void wallet_ui_clear(void *book);
void *wallet_ui_page(void *book, uintptr_t owner, const char *id, const char *title);
void wallet_ui_activate(void *book, const char *id);
const char *wallet_ui_active(void *book);
char *wallet_ui_focus(void *window, int *position, int *start, int *end, int *entry);
void wallet_ui_restore_focus(void *book, const char *page, const char *id, int position, int start, int end, int entry);
void wallet_ui_status(void *window, const char *text);
void *wallet_ui_box(void *parent, int horizontal);
void *wallet_ui_row(void *parent, uintptr_t owner, const char *id);
void *wallet_ui_label(void *parent, const char *text, int heading);
void *wallet_ui_address(void *parent, const char *text);
void wallet_ui_tooltip(void *widget, const char *text);
void wallet_ui_separator(void *parent);
void *wallet_ui_button(void *parent, uintptr_t owner, const char *id, const char *text);
void *wallet_ui_entry(void *parent, uintptr_t owner, const char *id, const char *text);
void *wallet_ui_combo(void *parent, uintptr_t owner, const char *id);
void wallet_ui_combo_option(void *combo, const char *id, const char *text);
void wallet_ui_combo_value(void *combo, const char *id);
void *wallet_ui_check(void *parent, uintptr_t owner, const char *id, const char *text, int active);
void wallet_ui_sensitive(void *widget, int sensitive);
void wallet_ui_show(void *window);
void *wallet_ui_dialog(void *parent, const char *title, int kind);
void *wallet_ui_body(void *dialog);
void wallet_ui_input_height(void *dialog, int count);
void *wallet_ui_secret(void *parent, const char *label);
const char *wallet_ui_secret_text(void *entry);
void wallet_ui_secret_clear(void *entry);
void *wallet_ui_phrase(void *parent, const char *text);
void wallet_ui_phrase_clear(void *label);
int wallet_ui_png(void *parent, const unsigned char *png, size_t length);
int wallet_ui_response(void *dialog);
void wallet_ui_destroy(void *dialog);
void wallet_ui_cancel(void *dialog);
void wallet_ui_clipboard(const char *text, int length);
void *wallet_ui_file(void *parent, const char *title, int save);
char *wallet_ui_filename(void *dialog);
void wallet_ui_free(void *value);
void wallet_ui_secret_free(void *value, size_t length);
#endif
