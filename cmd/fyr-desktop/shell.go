//go:build linux && cgo

package main

// The GTK side of the Linux app (decision 0038): a GtkApplication (one
// instance; a second start shows the first one's window), an Ayatana
// AppIndicator (StatusNotifierItem) tray item, and
// org.freedesktop.Notifications over GDBus. The window is Chromium, which
// the Go side starts and drives (V0.desktop-chromium, internal/desktop/chrome.go). Everything here runs on the
// GTK main thread; Go reaches it through the shell_* calls, which queue an
// idle callback, and GTK reaches Go through the go* exports in main.go.
// What it decides lives in internal/desktop.

/*
#cgo pkg-config: gtk+-3.0 ayatana-appindicator3-0.1
#include <stdlib.h>
#include <string.h>
#include <gtk/gtk.h>
#include <libayatana-appindicator/app-indicator.h>

extern void goStartup(void);
extern void goActivate(void);
extern void goMenu(int item);
extern void goNotified(int token, guint32 id);
extern void goNotifyAction(guint32 id, char *action);
extern void goNotifyClosed(guint32 id);
extern void goActivationToken(char *token);

enum { MENU_OPEN, MENU_BROWSER, MENU_PAUSE, MENU_LOG, MENU_QUIT };

static GtkApplication *app;
static AppIndicator *ind;
static GtkWidget *status_item, *pause_item;
static GDBusConnection *bus;
static char *icon_path;
static int start_hidden;

static void quit_app(void) { g_application_quit(G_APPLICATION(app)); }

static void open_external(const char *uri) {
	GError *err = NULL;
	if (!g_app_info_launch_default_for_uri(uri, NULL, &err)) {
		g_warning("fyr-desktop: open %s: %s", uri, err ? err->message : "?");
		g_clear_error(&err);
	}
}

static void on_menu(GtkMenuItem *i, gpointer item) { goMenu(GPOINTER_TO_INT(item)); }

static GtkWidget *menu_item(GtkWidget *menu, const char *label, int item) {
	GtkWidget *i = gtk_menu_item_new_with_label(label);
	if (item >= 0) g_signal_connect(i, "activate", G_CALLBACK(on_menu), GINT_TO_POINTER(item));
	gtk_menu_shell_append(GTK_MENU_SHELL(menu), i);
	return i;
}

static void make_tray(void) {
	char *dir = g_path_get_dirname(icon_path);
	char *base = g_path_get_basename(icon_path);
	char *dot = strrchr(base, '.');
	if (dot) *dot = 0;
	ind = app_indicator_new_with_path("io.github.meros.Fyr", base, APP_INDICATOR_CATEGORY_APPLICATION_STATUS, dir);
	g_free(dir);
	g_free(base);
	app_indicator_set_title(ind, "Fyr");
	GtkWidget *m = gtk_menu_new();
	GtkWidget *open = menu_item(m, "Open Fyr", MENU_OPEN);
	menu_item(m, "Open in Browser", MENU_BROWSER);
	gtk_menu_shell_append(GTK_MENU_SHELL(m), gtk_separator_menu_item_new());
	status_item = menu_item(m, "Fyr", -1);
	gtk_widget_set_sensitive(status_item, FALSE);
	pause_item = menu_item(m, "Pause", MENU_PAUSE);
	menu_item(m, "Show Log", MENU_LOG);
	gtk_menu_shell_append(GTK_MENU_SHELL(m), gtk_separator_menu_item_new());
	menu_item(m, "Quit", MENU_QUIT);
	gtk_widget_show_all(m);
	app_indicator_set_menu(ind, GTK_MENU(m));
	app_indicator_set_secondary_activate_target(ind, open); // a middle click opens the window
	app_indicator_set_status(ind, APP_INDICATOR_STATUS_ACTIVE);
}

static void on_notify_signal(GDBusConnection *c, const char *sender, const char *path, const char *iface,
                             const char *signal, GVariant *params, gpointer d) {
	guint32 id;
	if (g_strcmp0(signal, "ActionInvoked") == 0) {
		const char *action;
		g_variant_get(params, "(u&s)", &id, &action);
		goNotifyAction(id, (char *)action);
	} else if (g_strcmp0(signal, "ActivationToken") == 0) {
		// Wayland gives focus only with the server's token (spec 1.2).
		const char *token;
		g_variant_get(params, "(u&s)", &id, &token);
		goActivationToken((char *)token);
	} else if (g_strcmp0(signal, "NotificationClosed") == 0) {
		guint32 reason;
		g_variant_get(params, "(uu)", &id, &reason);
		goNotifyClosed(id);
	}
}

static void on_startup(GApplication *a, gpointer d) {
	g_application_hold(a); // the app lives in the tray when the window is closed
	GError *err = NULL;
	bus = g_bus_get_sync(G_BUS_TYPE_SESSION, NULL, &err);
	if (bus) {
		g_dbus_connection_signal_subscribe(bus, NULL, "org.freedesktop.Notifications", NULL,
			"/org/freedesktop/Notifications", NULL, G_DBUS_SIGNAL_FLAGS_NONE, on_notify_signal, NULL, NULL);
	} else {
		g_warning("fyr-desktop: no session bus: %s; no notifications", err ? err->message : "?");
		g_clear_error(&err);
	}

	make_tray();
	goStartup();
}

// The first activation of an app started with -background (autostart)
// leaves the window hidden; every later one (a second start) shows it.
static void on_activate(GApplication *a, gpointer d) {
	if (start_hidden) {
		start_hidden = 0;
		return;
	}
	goActivate();
}

static int shell_run(char *icon, int hidden) {
	icon_path = icon;
	start_hidden = hidden;
	g_set_application_name("Fyr");
	g_set_prgname("io.github.meros.Fyr"); // the .desktop file's name, for the window's icon in a dock
	#if GLIB_CHECK_VERSION(2, 74, 0)
	app = gtk_application_new("io.github.meros.Fyr", G_APPLICATION_DEFAULT_FLAGS);
#else
	app = gtk_application_new("io.github.meros.Fyr", G_APPLICATION_FLAGS_NONE); // Ubuntu 22.04's GLib 2.72
#endif
	g_signal_connect(app, "startup", G_CALLBACK(on_startup), NULL);
	g_signal_connect(app, "activate", G_CALLBACK(on_activate), NULL);
	char *argv[] = {"fyr-desktop", NULL};
	int status = g_application_run(G_APPLICATION(app), 1, argv);
	g_object_unref(app);
	return status;
}

// ---- calls from Go, run on the main thread ----

enum { OP_STATUS, OP_OPEN_URI, OP_QUIT, OP_NOTIFY, OP_CLOSE_NOTE };

typedef struct {
	int op;
	char *a, *b;
	int i, j, k;
	guint32 u;
} shell_op;

static void close_note(guint32 id) {
	if (!bus) return;
	g_dbus_connection_call(bus, "org.freedesktop.Notifications", "/org/freedesktop/Notifications",
		"org.freedesktop.Notifications", "CloseNotification", g_variant_new("(u)", id),
		NULL, G_DBUS_CALL_FLAGS_NONE, -1, NULL, NULL, NULL);
}

static void notify_done(GObject *src, GAsyncResult *res, gpointer token) {
	GError *err = NULL;
	GVariant *v = g_dbus_connection_call_finish(G_DBUS_CONNECTION(src), res, &err);
	if (!v) {
		g_warning("fyr-desktop: notify: %s", err ? err->message : "?");
		g_clear_error(&err);
		return;
	}
	guint32 id;
	g_variant_get(v, "(u)", &id);
	g_variant_unref(v);
	goNotified(GPOINTER_TO_INT(token), id);
}

static void notify(shell_op *o) {
	if (!bus) return;
	GVariantBuilder actions, hints;
	g_variant_builder_init(&actions, G_VARIANT_TYPE("as"));
	g_variant_builder_add(&actions, "s", "default");
	g_variant_builder_add(&actions, "s", "Open");
	g_variant_builder_init(&hints, G_VARIANT_TYPE("a{sv}"));
	g_variant_builder_add(&hints, "{sv}", "desktop-entry", g_variant_new_string("io.github.meros.Fyr"));
	g_variant_builder_add(&hints, "{sv}", "urgency", g_variant_new_byte((guchar)o->j));
	g_dbus_connection_call(bus, "org.freedesktop.Notifications", "/org/freedesktop/Notifications",
		"org.freedesktop.Notifications", "Notify",
		g_variant_new("(susssasa{sv}i)", "Fyr", o->u, icon_path ? icon_path : "", o->a, o->b, &actions, &hints, -1),
		G_VARIANT_TYPE("(u)"), G_DBUS_CALL_FLAGS_NONE, -1, NULL, notify_done, GINT_TO_POINTER(o->i));
}

static gboolean run_op(gpointer p) {
	shell_op *o = p;
	switch (o->op) {
	case OP_STATUS:
		gtk_menu_item_set_label(GTK_MENU_ITEM(status_item), o->a);
		gtk_menu_item_set_label(GTK_MENU_ITEM(pause_item), o->b);
		gtk_widget_set_sensitive(pause_item, o->i);
		break;
	case OP_OPEN_URI: open_external(o->a); break;
	case OP_QUIT: quit_app(); break;
	case OP_NOTIFY: notify(o); break;
	case OP_CLOSE_NOTE: close_note(o->u); break;
	}
	free(o->a);
	free(o->b);
	free(o);
	return G_SOURCE_REMOVE;
}

static void shell_op_post(int op, char *a, char *b, int i, int j, guint32 u) {
	shell_op *o = calloc(1, sizeof *o);
	o->op = op; o->a = a; o->b = b; o->i = i; o->j = j; o->u = u;
	g_idle_add(run_op, o);
}
*/
import "C"

import "runtime"

// GTK runs on the process's first thread.
func init() { runtime.LockOSThread() }

// cstr is a C copy of s; the main-thread op frees it.
func cstr(s string) *C.char { return C.CString(s) }

func shellRun(icon string, hidden bool) int {
	hd := 0
	if hidden {
		hd = 1
	}
	// These live as long as the process.
	return int(C.shell_run(cstr(icon), C.int(hd)))
}

func shellOpenURI(uri string) { C.shell_op_post(C.OP_OPEN_URI, cstr(uri), nil, 0, 0, 0) }
func shellQuit()              { C.shell_op_post(C.OP_QUIT, nil, nil, 0, 0, 0) }

func shellStatus(words, pause string, canPause bool) {
	i := 0
	if canPause {
		i = 1
	}
	C.shell_op_post(C.OP_STATUS, cstr(words), cstr(pause), C.int(i), 0, 0)
}

func shellNotify(token int, summary, body string, urgency byte, replaces uint32) {
	C.shell_op_post(C.OP_NOTIFY, cstr(summary), cstr(body), C.int(token), C.int(urgency), C.guint32(replaces))
}

func shellCloseNotification(id uint32) {
	C.shell_op_post(C.OP_CLOSE_NOTE, nil, nil, 0, 0, C.guint32(id))
}
