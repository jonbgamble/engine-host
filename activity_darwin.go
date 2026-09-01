//go:build darwin && !ios && cgo

package main

/*
#cgo CFLAGS: -fobjc-arc -x objective-c
#cgo LDFLAGS: -framework Foundation
#include <Foundation/Foundation.h>

static void setHostName(void) {
	@autoreleasepool {
		[[NSProcessInfo processInfo] setProcessName:@"Direct Engine Host"];
	}
}

static void *beginHostActivity(void) {
	@autoreleasepool {
		id activity = [[NSProcessInfo processInfo]
			beginActivityWithOptions:NSActivityUserInitiatedAllowingIdleSystemSleep
			reason:@"Hosting a local chess engine"];
		return (__bridge_retained void *)activity;
	}
}

static void endHostActivity(void *activity) {
	@autoreleasepool {
		id token = (__bridge_transfer id)activity;
		[[NSProcessInfo processInfo] endActivity:token];
	}
}
*/
import "C"

func init() { C.setHostName() }

func preventAppNap() func() {
	activity := C.beginHostActivity()
	return func() { C.endHostActivity(activity) }
}
