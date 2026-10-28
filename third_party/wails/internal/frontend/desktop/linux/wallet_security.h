/* CryptoWallet additions to pinned Wails v2.17.0. See WALLET-HARDENING.md.
 * A private, ephemeral WebKit context with a default-deny browser surface.
 * This is application policy, not an operating-system sandbox. */
#ifndef CRYPTOWALLET_WEBKIT_SECURITY_H
#define CRYPTOWALLET_WEBKIT_SECURITY_H

static gboolean wallet_uri_allowed(const char *value, gboolean document)
{
    if (!value || strlen(value) > 4096 || strchr(value, '%') || strstr(value, "..")) return FALSE;
    for (const unsigned char *p = (const unsigned char *)value; *p; p++) if (*p <= 32 || *p >= 127) return FALSE;
    GError *error = NULL;
    GUri *uri = g_uri_parse(value, G_URI_FLAGS_NONE, &error);
    if (!uri) { g_clear_error(&error); return FALSE; }
    const char *scheme = g_uri_get_scheme(uri), *host = g_uri_get_host(uri), *path = g_uri_get_path(uri);
    gboolean allowed = scheme && host && path && !strcmp(scheme, "wails") && !strcmp(host, "wails")
        && !g_uri_get_userinfo(uri) && g_uri_get_port(uri) == -1
        && !g_uri_get_query(uri) && !g_uri_get_fragment(uri)
        && path[0] == '/' && !strstr(path, "..") && !strchr(path, '\\');
    if (document) allowed = allowed && (!strcmp(path, "/") || !strcmp(path, "/index.html"));
    g_uri_unref(uri);
    return allowed;
}

static gboolean wallet_decide_policy(WebKitWebView *view, WebKitPolicyDecision *decision, WebKitPolicyDecisionType type, gpointer data)
{
    const char *uri = NULL;
    if (type == WEBKIT_POLICY_DECISION_TYPE_NAVIGATION_ACTION) {
        WebKitNavigationAction *action = webkit_navigation_policy_decision_get_navigation_action(WEBKIT_NAVIGATION_POLICY_DECISION(decision));
        uri = webkit_uri_request_get_uri(webkit_navigation_action_get_request(action));
        if (wallet_uri_allowed(uri, TRUE)) { webkit_policy_decision_use(decision); return TRUE; }
    } else if (type == WEBKIT_POLICY_DECISION_TYPE_RESPONSE) {
        WebKitResponsePolicyDecision *response = WEBKIT_RESPONSE_POLICY_DECISION(decision);
        uri = webkit_uri_request_get_uri(webkit_response_policy_decision_get_request(response));
        if (wallet_uri_allowed(uri, FALSE) && webkit_response_policy_decision_is_mime_type_supported(response)) {
            webkit_policy_decision_use(decision); return TRUE;
        }
    }
    webkit_policy_decision_ignore(decision);
    return TRUE;
}
static GtkWidget *wallet_create(WebKitWebView *view, WebKitNavigationAction *action, gpointer data) { return NULL; }
static gboolean wallet_permission(WebKitWebView *view, WebKitPermissionRequest *request, gpointer data)
{ webkit_permission_request_deny(request); return TRUE; }
static gboolean wallet_file_chooser(WebKitWebView *view, WebKitFileChooserRequest *request, gpointer data)
{ webkit_file_chooser_request_cancel(request); return TRUE; }
static gboolean wallet_authenticate(WebKitWebView *view, WebKitAuthenticationRequest *request, gpointer data)
{ webkit_authentication_request_cancel(request); return TRUE; }
static gboolean wallet_script_dialog(WebKitWebView *view, WebKitScriptDialog *dialog, gpointer data)
{ webkit_script_dialog_close(dialog); return TRUE; }
static gboolean wallet_print(WebKitWebView *view, WebKitPrintOperation *operation, gpointer data) { return TRUE; }
static gboolean wallet_fullscreen(WebKitWebView *view, gpointer data) { return TRUE; }
static gboolean wallet_notification(WebKitWebView *view, WebKitNotification *notification, gpointer data) { return TRUE; }
static void wallet_download(WebKitWebContext *context, WebKitDownload *download, gpointer data) { webkit_download_cancel(download); }
static gboolean wallet_key_press(GtkWidget *widget, GdkEventKey *event, gpointer data)
{
    /* Clipboard writes are explicit, validated Go actions; native browser
       shortcuts cannot copy a secret around that authorization. Paste works. */
    guint key = gdk_keyval_to_lower(event->keyval);
    if ((event->state & GDK_CONTROL_MASK) && (key == GDK_KEY_c || key == GDK_KEY_x || key == GDK_KEY_Insert)) return TRUE;
    if ((event->state & GDK_SHIFT_MASK) && key == GDK_KEY_Delete) return TRUE;
    return FALSE;
}
static void wallet_scheme_request(WebKitURISchemeRequest *request, gpointer data)
{
    if (!wallet_uri_allowed(webkit_uri_scheme_request_get_uri(request), FALSE)) {
        GError *error = g_error_new_literal(G_IO_ERROR, G_IO_ERROR_PERMISSION_DENIED, "Embedded asset unavailable");
        webkit_uri_scheme_request_finish_error(request, error);
        g_error_free(error);
        return;
    }
    processURLRequest(request);
}
static void wallet_configure_webview(GtkWidget *widget, WebKitWebContext *context)
{
    WebKitWebView *view = WEBKIT_WEB_VIEW(widget);
    WebKitSettings *settings = webkit_web_view_get_settings(view);
    webkit_settings_set_enable_developer_extras(settings, FALSE);
    webkit_settings_set_enable_html5_local_storage(settings, FALSE);
    webkit_settings_set_enable_html5_database(settings, FALSE);
    webkit_settings_set_enable_page_cache(settings, FALSE);
    webkit_settings_set_javascript_can_open_windows_automatically(settings, FALSE);
    webkit_settings_set_javascript_can_access_clipboard(settings, FALSE);
    webkit_settings_set_allow_file_access_from_file_urls(settings, FALSE);
    webkit_settings_set_allow_universal_access_from_file_urls(settings, FALSE);
    webkit_settings_set_allow_top_navigation_to_data_urls(settings, FALSE);
    webkit_settings_set_enable_write_console_messages_to_stdout(settings, FALSE);
    webkit_settings_set_enable_webgl(settings, FALSE);
    webkit_settings_set_enable_webaudio(settings, FALSE);
    webkit_settings_set_enable_media_stream(settings, FALSE);
    webkit_settings_set_enable_media(settings, FALSE);
    webkit_settings_set_enable_webrtc(settings, FALSE);
    webkit_settings_set_enable_fullscreen(settings, FALSE);
    webkit_settings_set_allow_modal_dialogs(settings, FALSE);
    webkit_settings_set_disable_web_security(settings, FALSE);
    g_signal_connect(view, "decide-policy", G_CALLBACK(wallet_decide_policy), NULL);
    g_signal_connect(view, "create", G_CALLBACK(wallet_create), NULL);
    g_signal_connect(view, "permission-request", G_CALLBACK(wallet_permission), NULL);
    g_signal_connect(view, "run-file-chooser", G_CALLBACK(wallet_file_chooser), NULL);
    g_signal_connect(view, "authenticate", G_CALLBACK(wallet_authenticate), NULL);
    g_signal_connect(view, "script-dialog", G_CALLBACK(wallet_script_dialog), NULL);
    g_signal_connect(view, "print", G_CALLBACK(wallet_print), NULL);
    g_signal_connect(view, "enter-fullscreen", G_CALLBACK(wallet_fullscreen), NULL);
    g_signal_connect(view, "show-notification", G_CALLBACK(wallet_notification), NULL);
    g_signal_connect(view, "key-press-event", G_CALLBACK(wallet_key_press), NULL);
    g_signal_connect(context, "download-started", G_CALLBACK(wallet_download), NULL);
}
#endif
