//go:build !linux || !cgo

package main

import (
	"gioui.org/app"
	"gioui.org/io/event"
	"gioui.org/io/system"
)

func foregroundWindow(window *app.Window) func(event.Event) {
	raised := false
	return func(ev event.Event) {
		if _, ok := ev.(app.FrameEvent); ok && !raised {
			window.Perform(system.ActionRaise)
			raised = true
		}
	}
}
