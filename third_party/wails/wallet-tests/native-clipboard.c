/* Public selection markers on the test-owned display only. */
#include <gtk/gtk.h>
#include <string.h>
#include <stdio.h>
#include "../internal/frontend/desktop/linux/wallet_clipboard.h"
int main(int argc,char **argv){
 gtk_init(&argc,&argv);
 GtkClipboard *primary=gtk_clipboard_get(GDK_SELECTION_PRIMARY);
 GtkClipboard *clipboard=gtk_clipboard_get(GDK_SELECTION_CLIPBOARD);
 gtk_clipboard_set_text(primary,"public-primary-baseline",-1);
 if(!WalletSetClipboardText("public-explicit-copy"))return 2;
 gchar *copied=gtk_clipboard_wait_for_text(clipboard),*selected=gtk_clipboard_wait_for_text(primary);
 gboolean passed=copied&&selected&&!strcmp(copied,"public-explicit-copy")&&!strcmp(selected,"public-primary-baseline");
 if(copied)WalletClearClipboardText(copied,strlen(copied));
 if(selected)WalletClearClipboardText(selected,strlen(selected));
 if(!passed)return 3;
 puts("PASS native clipboard: CLIPBOARD changed, PRIMARY preserved");return 0;
}
