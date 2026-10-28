#ifndef CRYPTOWALLET_CLIPBOARD_H
#define CRYPTOWALLET_CLIPBOARD_H
/* GTK copies the explicitly authorized text into CLIPBOARD ownership. Never
 * export it into PRIMARY merely because the copy button was pressed. */
static gboolean WalletSetClipboardText(const gchar *text) {
 GtkClipboard *clipboard=gtk_clipboard_get(GDK_SELECTION_CLIPBOARD);
 if(!clipboard||!text)return FALSE;
 gtk_clipboard_set_text(clipboard,text,-1);
 return TRUE;
}
/* Best effort for the temporary C allocation; immutable Go strings and GTK's
 * requested clipboard ownership have separate lifetimes. */
static void WalletClearClipboardText(gchar *text,gsize length) {
 if(!text)return;
 volatile unsigned char *cursor=(volatile unsigned char *)text;
 while(length--)*cursor++=0;
 g_free(text);
}
#endif
