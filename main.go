package main

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"gioui.org/app"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
	"gioui.org/x/explorer"

	"github.com/jonbgamble/direct-engine-host/internal/host"
)

type application struct {
	explorer *explorer.Explorer
	host     *host.App

	statusText widget.Editor
	choose     widget.Clickable
}

func newApplication(window *app.Window) (*application, error) {
	application := &application{explorer: explorer.NewExplorer(window)}
	shared, err := host.New(window.Invalidate)
	if err != nil {
		return nil, err
	}
	application.host = shared
	application.statusText.ReadOnly = true
	return application, nil
}

func (application *application) chooseEngine() {
	go func() {
		file, err := application.explorer.ChooseFile()
		if errors.Is(err, explorer.ErrUserDecline) {
			return
		}
		if err != nil {
			application.host.Log("Choosing engine: %v", err)
			return
		}
		defer file.Close()
		named, ok := file.(interface{ Name() string })
		if !ok || named.Name() == "" {
			application.host.Log("The selected engine has no local executable path")
			return
		}
		application.host.SetEngine(named.Name())
	}()
}

func (application *application) frame(gtx layout.Context, theme *material.Theme) layout.Dimensions {
	for application.choose.Clicked(gtx) {
		application.chooseEngine()
	}

	cfg := application.host.Config()
	status := application.host.Status()
	if application.statusText.Text() != status {
		application.statusText.SetText(status)
		application.statusText.SetCaret(len(status), len(status))
	}

	paint.FillShape(gtx.Ops, theme.Bg, clip.Rect{Max: gtx.Constraints.Max}.Op())
	return layout.UniformInset(unit.Dp(16)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(material.Button(theme, &application.choose, "Choose engine").Layout),
					layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						if cfg.Engine == "" {
							return material.Body1(theme, "No engine selected").Layout(gtx)
						}
						label := material.Caption(theme, cfg.Engine)
						label.MaxLines = 1
						return label.Layout(gtx)
					}),
				)
			}),
			layout.Rigid(layout.Spacer{Height: unit.Dp(16)}.Layout),
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				text := material.Editor(theme, &application.statusText, "Status")
				text.TextSize = unit.Sp(12)
				return text.Layout(gtx)
			}),
		)
	})
}

func run() error {
	endActivity := preventAppNap()
	defer endActivity()
	window := new(app.Window)
	foreground := foregroundWindow(window)
	window.Option(app.Title("Direct Engine Host"), app.Size(unit.Dp(720), unit.Dp(400)), app.MinSize(unit.Dp(560), unit.Dp(360)))
	application, err := newApplication(window)
	if err != nil {
		return err
	}
	application.host.Start()
	defer application.host.Close()

	theme := material.NewTheme()
	var operations op.Ops
	for {
		event := window.Event()
		application.explorer.ListenEvents(event)
		switch event := event.(type) {
		case app.DestroyEvent:
			return event.Err
		case app.FrameEvent:
			gtx := app.NewContext(&operations, event)
			application.frame(gtx, theme)
			event.Frame(gtx.Ops)
		}
		foreground(event)
	}
}

func main() {
	go func() {
		if err := run(); err != nil && !strings.Contains(err.Error(), "window closed") {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}()
	app.Main()
}
