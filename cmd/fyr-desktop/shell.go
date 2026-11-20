//go:build linux && cgo

package main

// The GTK side of the Linux app (decision 0038): a GtkApplication (one
// instance; a second start shows the first one's window), a WebKitGTK
// window, an Ayatana AppIndicator (StatusNotifierItem) tray item, and
// org.freedesktop.Notifications over GDBus. Everything here runs on the
// GTK main thread; Go reaches it through the shell_* calls, which queue an
// idle callback, and GTK reaches Go through the go* exports in main.go.
// What it decides lives in internal/desktop.

/*
#cgo pkg-config: gtk+-3.0 webkit2gtk-4.1 ayatana-appindicator3-0.1
#include <stdlib.h>
#include <string.h>
#include <gtk/gtk.h>
#include <webkit2/webkit2.h>
#include <libayatana-appindicator/app-indicator.h>

extern void goStartup(void);
extern int goSignedOut(void);
extern void goLoadFailed(void);
extern void goWindowSize(int w, int h, int maximized);
extern void goMenu(int item);
extern void goNotified(int token, guint32 id);
extern void goNotifyAction(guint32 id, char *action);
extern void goNotifyClosed(guint32 id);
extern int goExternal(char *uri);
extern int goIsFyr(char *uri);
extern void goBridge(char *msg, void *reply);

enum { MENU_OPEN, MENU_BROWSER, MENU_PAUSE, MENU_LOG, MENU_QUIT };

static GtkApplication *app;
static GtkWidget *win;
static WebKitWebView *view;
static WebKitWebContext *ctx;
static AppIndicator *ind;
static GtkWidget *status_item, *pause_item;
static GDBusConnection *bus;
static char *icon_path;
static int init_w, init_h, init_max, start_hidden;
static char *data_dir, *cache_dir;

static void save_size(void) {
	if (!win) return;
	int w, h;
	gtk_window_get_size(GTK_WINDOW(win), &w, &h);
	goWindowSize(w, h, gtk_window_is_maximized(GTK_WINDOW(win)));
}

static void present(void) {
	if (!win) return;
	gtk_widget_show_all(win);
	gtk_window_present(GTK_WINDOW(win));
}

// Closing the window hides it: the tray item and the notifications stay
// (the Mac shell's window does the same).
static gboolean on_delete(GtkWidget *w, GdkEvent *e, gpointer d) {
	save_size();
	gtk_widget_hide(w);
	return TRUE;
}

static void quit_app(void) {
	save_size();
	g_application_quit(G_APPLICATION(app));
}

// Ctrl+W hides, Ctrl+Q quits, Ctrl+R reloads (the Mac shell's ⌘W, ⌘Q).
static gboolean on_key(GtkWidget *w, GdkEventKey *e, gpointer d) {
	if (!(e->state & GDK_CONTROL_MASK)) return FALSE;
	switch (gdk_keyval_to_lower(e->keyval)) {
	case GDK_KEY_w: on_delete(win, NULL, NULL); return TRUE;
	case GDK_KEY_q: quit_app(); return TRUE;
	case GDK_KEY_r: webkit_web_view_reload(view); return TRUE;
	}
	return FALSE;
}

static void open_external(const char *uri) {
	GError *err = NULL;
	if (!g_app_info_launch_default_for_uri(uri, NULL, &err)) {
		g_warning("fyr-desktop: open %s: %s", uri, err ? err->message : "?");
		g_clear_error(&err);
	}
}

static gboolean on_policy(WebKitWebView *v, WebKitPolicyDecision *dec, WebKitPolicyDecisionType type, gpointer d) {
	if (type == WEBKIT_POLICY_DECISION_TYPE_RESPONSE) {
		WebKitResponsePolicyDecision *r = WEBKIT_RESPONSE_POLICY_DECISION(dec);
		// A signed-out window asks the owner socket for a new link, and the
		// signed-out page is not shown (it would ask the owner to approve this
		// window). Within 30 s of the last link it is shown instead.
		if (webkit_response_policy_decision_is_main_frame_main_resource(r) &&
		    webkit_uri_response_get_status_code(webkit_response_policy_decision_get_response(r)) == 401 &&
		    goSignedOut()) {
			webkit_policy_decision_ignore(dec);
			return TRUE;
		}
		return FALSE;
	}
	WebKitNavigationAction *a = webkit_navigation_policy_decision_get_navigation_action(WEBKIT_NAVIGATION_POLICY_DECISION(dec));
	const char *uri = webkit_uri_request_get_uri(webkit_navigation_action_get_request(a));
	if (!uri) return FALSE;
	// A link off Fyr goes to the default browser: a new window always, a
	// navigation when the owner clicked it (a frame's own load stays).
	if ((type == WEBKIT_POLICY_DECISION_TYPE_NEW_WINDOW_ACTION || webkit_navigation_action_is_user_gesture(a)) &&
	    goExternal((char *)uri)) {
		open_external(uri);
		webkit_policy_decision_ignore(dec);
		return TRUE;
	}
	return FALSE;
}

// A Fyr page opened in a new window (a preview) gets its own window with
// the same session.
static void on_ready(WebKitWebView *v, gpointer w) { gtk_widget_show_all(GTK_WIDGET(w)); }
static void on_child_close(WebKitWebView *v, gpointer w) { gtk_widget_destroy(GTK_WIDGET(w)); }
static GtkWidget *on_create(WebKitWebView *v, WebKitNavigationAction *a, gpointer d) {
	GtkWidget *child = webkit_web_view_new_with_related_view(v);
	GtkWidget *w = gtk_window_new(GTK_WINDOW_TOPLEVEL);
	gtk_window_set_default_size(GTK_WINDOW(w), 1000, 760);
	gtk_window_set_transient_for(GTK_WINDOW(w), GTK_WINDOW(win));
	gtk_container_add(GTK_CONTAINER(w), child);
	g_signal_connect(child, "ready-to-show", G_CALLBACK(on_ready), w);
	g_signal_connect(child, "close", G_CALLBACK(on_child_close), w);
	return child;
}

static gboolean on_load_failed(WebKitWebView *v, WebKitLoadEvent ev, char *uri, GError *err, gpointer d) {
	if (g_error_matches(err, WEBKIT_NETWORK_ERROR, WEBKIT_NETWORK_ERROR_CANCELLED) ||
	    err->domain == WEBKIT_POLICY_ERROR)
		return FALSE;
	goLoadFailed();
	return FALSE;
}

// The web client asks for no notification permission here (it reads the
// user agent), and the window needs none other; everything is refused.
static gboolean on_permission(WebKitWebView *v, WebKitPermissionRequest *r, gpointer d) {
	webkit_permission_request_deny(r);
	return TRUE;
}

typedef struct {
	WebKitScriptMessageReply *reply;
	JSCContext *ctx;
} bridge_reply;

// fyrPush: the page's notification settings for this device (web/src/pwa.ts,
// V0.mac-notify), answered by Go with { on, endpoint?, error? }. Only
// Fyr's own page may ask.
static gboolean on_bridge(WebKitUserContentManager *m, JSCValue *v, WebKitScriptMessageReply *reply, gpointer d) {
	const char *uri = webkit_web_view_get_uri(view);
	if (!uri || !goIsFyr((char *)uri)) {
		webkit_script_message_reply_return_error_message(reply, "not available");
		return TRUE;
	}
	char *json = jsc_value_to_json(v, 0);
	// The answer is made in the message's own context: a value made in
	// another one reaches the page as an empty object.
	bridge_reply *b = g_new0(bridge_reply, 1);
	b->reply = webkit_script_message_reply_ref(reply); // until Go answers
	b->ctx = g_object_ref(jsc_value_get_context(v));
	goBridge(json ? json : "{}", b);
	g_free(json);
	return TRUE;
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
		if (win) gtk_window_set_startup_id(GTK_WINDOW(win), token);
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

	WebKitWebsiteDataManager *dm = webkit_website_data_manager_new(
		"base-data-directory", data_dir, "base-cache-directory", cache_dir, NULL);
	ctx = webkit_web_context_new_with_website_data_manager(dm);
	char *cookies = g_build_filename(data_dir, "cookies.sqlite", NULL);
	webkit_cookie_manager_set_persistent_storage(webkit_website_data_manager_get_cookie_manager(dm),
		cookies, WEBKIT_COOKIE_PERSISTENT_STORAGE_SQLITE);
	g_free(cookies);

	view = WEBKIT_WEB_VIEW(webkit_web_view_new_with_context(ctx));
	WebKitSettings *s = webkit_web_view_get_settings(view);
	// The web client reads FyrDesktop/<os> and turns off its service worker
	// and Web Push (web/src/push.ts isDesktopApp).
	webkit_settings_set_user_agent_with_application_details(s, "FyrDesktop", "linux");
	webkit_settings_set_javascript_can_access_clipboard(s, TRUE);
	webkit_settings_set_enable_developer_extras(s, g_getenv("FYR_DESKTOP_DEBUG") != NULL);
	g_signal_connect(view, "decide-policy", G_CALLBACK(on_policy), NULL);
	g_signal_connect(view, "create", G_CALLBACK(on_create), NULL);
	g_signal_connect(view, "load-failed", G_CALLBACK(on_load_failed), NULL);
	g_signal_connect(view, "permission-request", G_CALLBACK(on_permission), NULL);
	WebKitUserContentManager *ucm = webkit_web_view_get_user_content_manager(view);
	g_signal_connect(ucm, "script-message-with-reply-received::fyrPush", G_CALLBACK(on_bridge), NULL);
	webkit_user_content_manager_register_script_message_handler_with_reply(ucm, "fyrPush", NULL);

	win = gtk_application_window_new(app);
	gtk_window_set_title(GTK_WINDOW(win), "Fyr");
	gtk_window_set_default_size(GTK_WINDOW(win), init_w, init_h);
	if (init_max) gtk_window_maximize(GTK_WINDOW(win));
	if (icon_path) gtk_window_set_icon_from_file(GTK_WINDOW(win), icon_path, NULL);
	gtk_container_add(GTK_CONTAINER(win), GTK_WIDGET(view));
	g_signal_connect(win, "delete-event", G_CALLBACK(on_delete), NULL);
	g_signal_connect(win, "key-press-event", G_CALLBACK(on_key), NULL);

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
	present();
}

static int shell_run(char *ddir, char *cdir, char *icon, int w, int h, int max, int hidden) {
	data_dir = ddir;
	cache_dir = cdir;
	icon_path = icon;
	init_w = w;
	init_h = h;
	init_max = max;
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

enum { OP_LOAD, OP_EVAL, OP_PRESENT, OP_STATUS, OP_OPEN_URI, OP_QUIT, OP_NOTIFY, OP_RELOAD, OP_REPLY, OP_REPLY_ERROR, OP_CLOSE_NOTE };

typedef struct {
	int op;
	char *a, *b;
	int i, j, k;
	guint32 u;
	void *p;
} shell_op;

static void reply(shell_op *o) {
	bridge_reply *b = o->p;
	if (o->op == OP_REPLY) {
		JSCValue *v = jsc_value_new_from_json(b->ctx, o->a);
		webkit_script_message_reply_return_value(b->reply, v);
		g_object_unref(v);
	} else {
		webkit_script_message_reply_return_error_message(b->reply, o->a);
	}
	webkit_script_message_reply_unref(b->reply);
	g_object_unref(b->ctx);
	g_free(b);
}

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
	case OP_LOAD: webkit_web_view_load_uri(view, o->a); break;
	case OP_RELOAD: webkit_web_view_reload(view); break;
	case OP_EVAL: webkit_web_view_evaluate_javascript(view, o->a, -1, NULL, NULL, NULL, NULL, NULL); break;
	case OP_PRESENT: present(); break;
	case OP_STATUS:
		gtk_menu_item_set_label(GTK_MENU_ITEM(status_item), o->a);
		gtk_menu_item_set_label(GTK_MENU_ITEM(pause_item), o->b);
		gtk_widget_set_sensitive(pause_item, o->i);
		break;
	case OP_OPEN_URI: open_external(o->a); break;
	case OP_QUIT: quit_app(); break;
	case OP_NOTIFY: notify(o); break;
	case OP_REPLY:
	case OP_REPLY_ERROR: reply(o); break;
	case OP_CLOSE_NOTE: close_note(o->u); break;
	}
	free(o->a);
	free(o->b);
	free(o);
	return G_SOURCE_REMOVE;
}

static void shell_op_post_p(int op, char *a, char *b, int i, int j, guint32 u, void *p) {
	shell_op *o = calloc(1, sizeof *o);
	o->op = op; o->a = a; o->b = b; o->i = i; o->j = j; o->u = u; o->p = p;
	g_idle_add(run_op, o);
}

static void shell_op_post(int op, char *a, char *b, int i, int j, guint32 u) {
	shell_op_post_p(op, a, b, i, j, u, NULL);
}
*/
import "C"

import (
	"runtime"
	"unsafe"
)

// GTK runs on the process's first thread.
func init() { runtime.LockOSThread() }

// cstr is a C copy of s; the main-thread op frees it.
func cstr(s string) *C.char { return C.CString(s) }

func shellRun(dataDir, cacheDir, icon string, w, h int, maximized, hidden bool) int {
	m, hd := 0, 0
	if maximized {
		m = 1
	}
	if hidden {
		hd = 1
	}
	// These live as long as the process.
	return int(C.shell_run(cstr(dataDir), cstr(cacheDir), cstr(icon), C.int(w), C.int(h), C.int(m), C.int(hd)))
}

func shellLoad(uri string)    { C.shell_op_post(C.OP_LOAD, cstr(uri), nil, 0, 0, 0) }
func shellReload()            { C.shell_op_post(C.OP_RELOAD, nil, nil, 0, 0, 0) }
func shellEval(js string)     { C.shell_op_post(C.OP_EVAL, cstr(js), nil, 0, 0, 0) }
func shellPresent()           { C.shell_op_post(C.OP_PRESENT, nil, nil, 0, 0, 0) }
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

func shellReply(reply unsafe.Pointer, json string) {
	C.shell_op_post_p(C.OP_REPLY, cstr(json), nil, 0, 0, 0, reply)
}

func shellReplyError(reply unsafe.Pointer, msg string) {
	C.shell_op_post_p(C.OP_REPLY_ERROR, cstr(msg), nil, 0, 0, 0, reply)
}

func shellCloseNotification(id uint32) {
	C.shell_op_post(C.OP_CLOSE_NOTE, nil, nil, 0, 0, C.guint32(id))
}
