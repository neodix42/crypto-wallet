/* Real WebKit acceptance for CryptoWallet's native policy. Disposable public
 * fixtures only: no wallet process, profile, signing key or mainnet connection. */
#include <gtk/gtk.h>
#include <webkit2/webkit2.h>
#include <stdio.h>
#include <string.h>
#include <stdlib.h>
static void processURLRequest(void *raw);
#include "../internal/frontend/desktop/linux/wallet_security.h"
static int failed=1;
static const char *policy="default-src 'none'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; font-src 'self'; connect-src 'none'; object-src 'none'; frame-src 'none'; worker-src 'none'; base-uri 'none'; form-action 'none'; media-src 'none'; manifest-src 'none'";
static char *script;
static void processURLRequest(void *raw) {
 WebKitURISchemeRequest *request=raw;
 const char *uri=webkit_uri_scheme_request_get_uri(request);
 const char *content=strstr(uri,"/probe.js")?script:"<!doctype html><html><head><script src='/probe.js'></script></head><body>Public security probe</body></html>";
 GInputStream *stream=g_memory_input_stream_new_from_data(content,strlen(content),NULL);
 WebKitURISchemeResponse *response=webkit_uri_scheme_response_new(stream,strlen(content));
 SoupMessageHeaders *headers=soup_message_headers_new(SOUP_MESSAGE_HEADERS_RESPONSE);
 soup_message_headers_append(headers,"Content-Security-Policy",policy);
 soup_message_headers_append(headers,"Cache-Control","no-store");
 webkit_uri_scheme_response_set_http_headers(response,headers);
 webkit_uri_scheme_response_set_content_type(response,strstr(uri,"/probe.js")?"text/javascript":"text/html");
 webkit_uri_scheme_request_finish_with_response(request,response);
 /* set_http_headers takes full ownership of headers. */
 g_object_unref(response);g_object_unref(stream);
}
static void receive(WebKitUserContentManager *manager,WebKitJavascriptResult *result,gpointer user) {
 JSCValue *value=webkit_javascript_result_get_js_value(result);
 char *text=jsc_value_to_string(value);
 if(!strcmp(text,"PASS")){failed=0;puts("PASS real WebKit: network/eval/storage/popup/external-navigation denied");}
 else {puts("FAIL real WebKit policy checks");}
 g_free(text);gtk_main_quit();
}
static gboolean timeout(gpointer data){puts("FAIL WebKit probe timed out");gtk_main_quit();return G_SOURCE_REMOVE;}
int main(int argc,char **argv){
 gtk_init(&argc,&argv);
 const char *allowed[]={"wails://wails/","wails://wails/index.html","wails://wails/assets/main-a.js"};
 const char *denied[]={"https://example.com/","http://127.0.0.1/","file:///etc/passwd","data:text/html,hi","wails://evil/","wails://wails.evil/","wails://user@wails/","wails://wails:80/","wails://wails/?secret=canary","wails://wails/#canary","wails://wails/../private","wails://wails/%2e%2e/private"};
 for(size_t i=0;i<G_N_ELEMENTS(allowed);i++)if(!wallet_uri_allowed(allowed[i],FALSE))return 2;
 for(size_t i=0;i<G_N_ELEMENTS(denied);i++)if(wallet_uri_allowed(denied[i],FALSE))return 3;
 if(wallet_uri_allowed("wails://wails/assets/main.js",TRUE))return 4;
 WebKitWebContext *context=webkit_web_context_new_ephemeral();webkit_web_context_set_sandbox_enabled(context,FALSE);
 WebKitUserContentManager *manager=webkit_user_content_manager_new();
 webkit_user_content_manager_register_script_message_handler(manager,"probe");
 g_signal_connect(manager,"script-message-received::probe",G_CALLBACK(receive),NULL);
 GtkWidget *view=GTK_WIDGET(g_object_new(WEBKIT_TYPE_WEB_VIEW,"web-context",context,"user-content-manager",manager,NULL));
 wallet_configure_webview(view,context);
 webkit_web_context_register_uri_scheme(context,"wails",wallet_scheme_request,NULL,NULL);
 webkit_security_manager_register_uri_scheme_as_secure(webkit_web_context_get_security_manager(context),"wails");
 WebKitSettings *s=webkit_web_view_get_settings(WEBKIT_WEB_VIEW(view));
 if(!webkit_web_context_is_ephemeral(context)||webkit_web_context_get_sandbox_enabled(context)||!webkit_web_view_is_ephemeral(WEBKIT_WEB_VIEW(view)))return 5;
 if(webkit_settings_get_enable_html5_local_storage(s)||webkit_settings_get_enable_html5_database(s)||webkit_settings_get_enable_page_cache(s)||!webkit_settings_get_javascript_can_access_clipboard(s)||webkit_settings_get_enable_developer_extras(s)||webkit_settings_get_enable_webrtc(s))return 6;
 const char *endpoint=g_getenv("WALLET_PROBE_ENDPOINT");if(!endpoint||!g_str_has_prefix(endpoint,"http://127.0.0.1:"))return 7;
 script=g_strdup_printf("(async()=>{let storage=false,evaluation=false,network=false;try{localStorage.setItem('public-probe','1')}catch{storage=true}try{eval('1+1')}catch{evaluation=true}try{await fetch('%s/public-probe')}catch{network=true}const popup=window.open('%s/popup')===null;new Image().src='%s/image';location.href='%s/navigate';setTimeout(()=>{window.webkit.messageHandlers.probe.postMessage(storage&&evaluation&&network&&popup&&location.href==='wails://wails/'?'PASS':'FAIL')},500)})()",endpoint,endpoint,endpoint,endpoint);
 GtkWidget *window=gtk_window_new(GTK_WINDOW_TOPLEVEL);gtk_window_set_default_size(GTK_WINDOW(window),640,480);gtk_container_add(GTK_CONTAINER(window),view);gtk_widget_show_all(window);
 webkit_web_view_load_uri(WEBKIT_WEB_VIEW(view),"wails://wails/");g_timeout_add_seconds(15,timeout,NULL);gtk_main();
 gtk_widget_destroy(window);g_object_unref(context);g_object_unref(manager);g_free(script);return failed;
}
