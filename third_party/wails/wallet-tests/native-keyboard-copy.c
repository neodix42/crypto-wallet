/* Real WebKit editing commands on an owned display, with public markers only.
 * This exercises the exact native key callback, not renderer clipboard IPC. */
#include <gtk/gtk.h>
#include <webkit2/webkit2.h>
#include <stdio.h>
#include <string.h>
static void processURLRequest(void *raw);
#include "../internal/frontend/desktop/linux/wallet_security.h"

static WebKitWebView *view;
static int failed = 1;
static unsigned int current;
static const char *marker = "public-keyboard-copy-marker";
static const char *baseline = "public-unchanged-clipboard-marker";
static const struct {
    guint key;
    GdkModifierType modifiers;
} cases[] = {
    { GDK_KEY_c, GDK_CONTROL_MASK },
    { GDK_KEY_C, GDK_CONTROL_MASK | GDK_SHIFT_MASK },
    { GDK_KEY_Insert, GDK_CONTROL_MASK },
};

static void finish_failure(const char *reason) {
    fprintf(stderr, "FAIL native keyboard copy: %s (case %u)\n", reason, current);
    gtk_main_quit();
}

static void processURLRequest(void *raw) {
    WebKitURISchemeRequest *request = raw;
    const char *content = "<!doctype html><html><body><p id='public'>public-keyboard-copy-marker</p></body></html>";
    GInputStream *stream = g_memory_input_stream_new_from_data(content, strlen(content), NULL);
    webkit_uri_scheme_request_finish(request, stream, strlen(content), "text/html");
    g_object_unref(stream);
}

static void start_case(void);

static gboolean verify_case(gpointer data) {
    gchar *text = gtk_clipboard_wait_for_text(gtk_clipboard_get(GDK_SELECTION_CLIPBOARD));
    gboolean passed = text && !strcmp(text, marker);
    g_free(text);
    if (!passed) { finish_failure("clipboard did not match native policy"); return G_SOURCE_REMOVE; }
    current++;
    if (current < G_N_ELEMENTS(cases)) start_case();
    else {
        if (!webkit_settings_get_javascript_can_access_clipboard(webkit_web_view_get_settings(view))) {
            finish_failure("trusted UI clipboard access disabled"); return G_SOURCE_REMOVE;
        }
        failed = 0;
        puts("PASS native keyboard copy: all three copy shortcuts work in the trusted UI");
        gtk_main_quit();
    }
    return G_SOURCE_REMOVE;
}

static void selection_finished(GObject *object, GAsyncResult *result, gpointer data) {
    GError *error = NULL;
    WebKitJavascriptResult *js = webkit_web_view_run_javascript_finish(WEBKIT_WEB_VIEW(object), result, &error);
    gchar *selected = js ? jsc_value_to_string(webkit_javascript_result_get_js_value(js)) : NULL;
    gboolean ready = selected && !error && !strcmp(selected, marker);
    g_free(selected);
    if (js) webkit_javascript_result_unref(js);
    g_clear_error(&error);
    if (!ready) { finish_failure("public DOM selection was unavailable"); return; }
    gtk_widget_grab_focus(GTK_WIDGET(view));
    GdkEvent *event = gdk_event_new(GDK_KEY_PRESS);
    event->key.window = g_object_ref(gtk_widget_get_window(GTK_WIDGET(view)));
    event->key.send_event = TRUE;
    event->key.time = GDK_CURRENT_TIME;
    event->key.keyval = cases[current].key;
    event->key.state = cases[current].modifiers;
    gboolean handled = FALSE;
    g_signal_emit_by_name(view, "key-press-event", &event->key, &handled);
    gdk_event_free(event);
    if (!handled) { finish_failure("native shortcut was not handled"); return; }
    /* Editing commands cross WebKit's process boundary; observe their result
       after the event loop has had time to process the native command. */
    g_timeout_add(200, verify_case, NULL);
}

static void start_case(void) {
    gtk_clipboard_set_text(gtk_clipboard_get(GDK_SELECTION_CLIPBOARD), baseline, -1);
    webkit_web_view_run_javascript(view,
        "(()=>{const range=document.createRange();range.selectNodeContents(document.getElementById('public'));"
        "const selection=getSelection();selection.removeAllRanges();selection.addRange(range);return selection.toString()})()",
        NULL, selection_finished, NULL);
}

static void loaded(WebKitWebView *webview, WebKitLoadEvent event, gpointer data) {
    if (event == WEBKIT_LOAD_FINISHED) start_case();
}
static gboolean timeout(gpointer data) {
    finish_failure("probe exceeded its bounded deadline");
    return G_SOURCE_REMOVE;
}

int main(int argc, char **argv) {
    gtk_init(&argc, &argv);
    WebKitWebContext *context = webkit_web_context_new_ephemeral();
    webkit_web_context_set_sandbox_enabled(context, FALSE);
    WebKitUserContentManager *manager = webkit_user_content_manager_new();
    view = WEBKIT_WEB_VIEW(g_object_new(WEBKIT_TYPE_WEB_VIEW, "web-context", context, "user-content-manager", manager, NULL));
    wallet_configure_webview(GTK_WIDGET(view), context);
    if (!webkit_settings_get_javascript_can_access_clipboard(webkit_web_view_get_settings(view))) return 2;
    webkit_web_context_register_uri_scheme(context, "wails", wallet_scheme_request, NULL, NULL);
    webkit_security_manager_register_uri_scheme_as_secure(webkit_web_context_get_security_manager(context), "wails");
    GtkWidget *window = gtk_window_new(GTK_WINDOW_TOPLEVEL);
    gtk_window_set_default_size(GTK_WINDOW(window), 640, 480);
    gtk_container_add(GTK_CONTAINER(window), GTK_WIDGET(view));
    gtk_widget_show_all(window);
    gtk_clipboard_set_text(gtk_clipboard_get(GDK_SELECTION_CLIPBOARD), baseline, -1);
    g_signal_connect(view, "load-changed", G_CALLBACK(loaded), NULL);
    webkit_web_view_load_uri(view, "wails://wails/");
    g_timeout_add_seconds(15, timeout, NULL);
    gtk_main();
    gtk_widget_destroy(window);
    g_object_unref(context);
    g_object_unref(manager);
    return failed;
}
