//go:build linux && cgo

package main

/*
#cgo pkg-config: x11 wayland-client
#include <stdlib.h>
#include <string.h>
#include <poll.h>
#include <X11/Xlib.h>
#include <wayland-client.h>

static const struct wl_interface host_token_interface;
static const struct wl_interface *host_serial_types[] = {NULL, &wl_seat_interface};
static const struct wl_interface *host_surface_types[] = {&wl_surface_interface};
static const struct wl_message host_token_requests[] = {
	{"set_serial", "uo", host_serial_types},
	{"set_app_id", "s", NULL},
	{"set_surface", "o", host_surface_types},
	{"commit", "", NULL},
	{"destroy", "", NULL},
};
static const struct wl_message host_token_events[] = {{"done", "s", NULL}};
static const struct wl_interface host_token_interface = {
	"xdg_activation_token_v1", 1, 5, host_token_requests, 1, host_token_events
};
static const struct wl_interface *host_token_types[] = {&host_token_interface};
static const struct wl_interface *host_activate_types[] = {NULL, &wl_surface_interface};
static const struct wl_message host_activation_requests[] = {
	{"destroy", "", NULL},
	{"get_activation_token", "n", host_token_types},
	{"activate", "so", host_activate_types},
};
static const struct wl_interface host_activation_interface = {
	"xdg_activation_v1", 1, 3, host_activation_requests, 0, NULL
};

struct host_activation {
	struct wl_proxy *manager;
	struct wl_surface *surface;
	int activated;
};

static void host_registry_global(void *data, struct wl_registry *registry,
	uint32_t name, const char *interface, uint32_t version) {
	struct host_activation *activation = data;
	if (strcmp(interface, "xdg_activation_v1") == 0 && !activation->manager) {
		activation->manager = wl_registry_bind(registry, name, &host_activation_interface, 1);
	}
}

static void host_registry_removed(void *data, struct wl_registry *registry, uint32_t name) {}
static const struct wl_registry_listener host_registry_listener = {
	host_registry_global, host_registry_removed
};

static void host_token_done(void *data, struct wl_proxy *token, const char *value) {
	struct host_activation *activation = data;
	wl_proxy_marshal(activation->manager, 2, value, activation->surface);
	activation->activated = 1;
}

static const struct {
	void (*done)(void *, struct wl_proxy *, const char *);
} host_token_listener = {host_token_done};

static int host_wait_token(struct wl_display *display, struct wl_event_queue *queue,
	struct host_activation *activation) {
	for (int attempt = 0; attempt < 4 && !activation->activated; attempt++) {
		while (wl_display_prepare_read_queue(display, queue) != 0) {
			if (wl_display_dispatch_queue_pending(display, queue) < 0) return -1;
		}
		if (activation->activated) {
			wl_display_cancel_read(display);
			break;
		}
		wl_display_flush(display);
		struct pollfd fd = {.fd = wl_display_get_fd(display), .events = POLLIN};
		int ready = poll(&fd, 1, 250);
		if (ready <= 0 || !(fd.revents & POLLIN)) {
			wl_display_cancel_read(display);
			if (ready < 0 || fd.revents) return -1;
			continue;
		}
		if (wl_display_read_events(display) < 0) return -1;
		if (wl_display_dispatch_queue_pending(display, queue) < 0) return -1;
	}
	return activation->activated ? 0 : -1;
}

static int host_activate_wayland(struct wl_display *display, struct wl_surface *surface,
	const char *launch_token, const char *app_id) {
	struct host_activation activation = {.surface = surface};
	struct wl_event_queue *queue = wl_display_create_queue(display);
	if (!queue) return -1;
	struct wl_display *wrapper = wl_proxy_create_wrapper(display);
	if (!wrapper) {
		wl_event_queue_destroy(queue);
		return -1;
	}
	wl_proxy_set_queue((struct wl_proxy *)wrapper, queue);
	struct wl_registry *registry = wl_display_get_registry(wrapper);
	wl_proxy_wrapper_destroy(wrapper);
	wl_registry_add_listener(registry, &host_registry_listener, &activation);
	int result = wl_display_roundtrip_queue(display, queue);
	if (result >= 0 && !activation.manager) result = -1;
	if (result >= 0) {
		if (launch_token[0]) {
			wl_proxy_marshal(activation.manager, 2, launch_token, surface);
		} else {
			struct wl_proxy *token = wl_proxy_marshal_constructor(
				activation.manager, 1, &host_token_interface, NULL);
			if (token) {
				wl_proxy_add_listener(token, (void (**)(void))&host_token_listener, &activation);
				wl_proxy_marshal(token, 1, app_id);
				wl_proxy_marshal(token, 3);
				result = host_wait_token(display, queue, &activation);
				wl_proxy_marshal(token, 4);
				wl_proxy_destroy(token);
			} else {
				result = -1;
			}
		}
	}
	if (activation.manager) {
		wl_proxy_marshal(activation.manager, 0);
		wl_proxy_destroy(activation.manager);
	}
	wl_registry_destroy(registry);
	wl_display_flush(display);
	wl_event_queue_destroy(queue);
	return result;
}

static void host_activate_x11(Display *display, Window window, const char *startup_id) {
	if (startup_id[0]) {
		Atom property = XInternAtom(display, "_NET_STARTUP_ID", False);
		Atom utf8 = XInternAtom(display, "UTF8_STRING", False);
		XChangeProperty(display, window, property, utf8, 8, PropModeReplace,
			(const unsigned char *)startup_id, strlen(startup_id));
	}
	XMapRaised(display, window);
	XEvent request = {0};
	request.xclient.type = ClientMessage;
	request.xclient.window = window;
	request.xclient.message_type = XInternAtom(display, "_NET_ACTIVE_WINDOW", False);
	request.xclient.format = 32;
	request.xclient.data.l[0] = 1;
	request.xclient.data.l[1] = CurrentTime;
	XSendEvent(display, DefaultRootWindow(display), False,
		SubstructureRedirectMask | SubstructureNotifyMask, &request);
	XFlush(display);
}
*/
import "C"

import (
	"fmt"
	"os"
	"unsafe"

	"gioui.org/app"
	"gioui.org/io/event"
)

func foregroundWindow(window *app.Window) func(event.Event) {
	token := os.Getenv("XDG_ACTIVATION_TOKEN")
	startupID := os.Getenv("DESKTOP_STARTUP_ID")
	os.Unsetenv("XDG_ACTIVATION_TOKEN")
	os.Unsetenv("DESKTOP_STARTUP_ID")
	var view app.ViewEvent
	framed, raised := false, false
	return func(ev event.Event) {
		switch ev := ev.(type) {
		case app.ViewEvent:
			view = ev
		case app.FrameEvent:
			framed = true
		default:
			return
		}
		if raised || !framed || view == nil || !view.Valid() {
			return
		}
		raised = true
		window.Run(func() {
			switch view := view.(type) {
			case app.X11ViewEvent:
				id := C.CString(startupID)
				defer C.free(unsafe.Pointer(id))
				C.host_activate_x11((*C.Display)(view.Display), C.Window(view.Window), id)
			case app.WaylandViewEvent:
				launchToken, appID := C.CString(token), C.CString(app.ID)
				defer C.free(unsafe.Pointer(launchToken))
				defer C.free(unsafe.Pointer(appID))
				if C.host_activate_wayland((*C.struct_wl_display)(view.Display),
					(*C.struct_wl_surface)(view.Surface), launchToken, appID) < 0 {
					fmt.Fprintln(os.Stderr, "Unable to request Wayland window activation")
				}
			}
		})
	}
}
