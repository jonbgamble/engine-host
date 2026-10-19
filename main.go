package main

import (
	"errors"
	"fmt"
	"image/color"
	"net/url"
	"os"
	"runtime"
	"strings"

	"gioui.org/app"
	"gioui.org/io/event"
	"gioui.org/io/system"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
	"gioui.org/x/explorer"

	"github.com/jonbgamble/engine-host/internal/host"
)

const appID = "org.lichess.engine-host"

type application struct {
	explorer *explorer.Explorer
	host     *host.App

	statusText widget.Editor
	disconnect widget.Clickable
	external   widget.Clickable
	login      widget.Clickable
	direct     widget.Clickable
	choose     widget.Clickable
}

func newApplication(window *app.Window, callbackURLs <-chan *url.URL) (*application, error) {
	application := &application{explorer: explorer.NewExplorer(window)}
	shared, err := host.New(window.Invalidate, callbackURLs)
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
	for application.disconnect.Clicked(gtx) {
		application.host.SetMode(host.ModeDisconnected)
	}
	for application.external.Clicked(gtx) {
		application.host.SetMode(host.ModeExternal)
	}
	for application.direct.Clicked(gtx) {
		application.host.SetMode(host.ModeDirect)
	}
	for application.login.Clicked(gtx) {
		go application.host.Login()
	}
	for application.choose.Clicked(gtx) {
		application.chooseEngine()
	}

	cfg := application.host.Config()
	mode, connection := application.host.Connection()
	modeColor := color.NRGBA{R: 0x60, G: 0x67, B: 0x6c, A: 0xff}
	if mode == host.ModeExternal {
		modeColor = color.NRGBA{R: 0x16, G: 0x70, B: 0x43, A: 0xff}
	} else if mode == host.ModeDirect {
		modeColor = color.NRGBA{R: 0x08, G: 0x68, B: 0xb2, A: 0xff}
	}
	status := application.host.Status()
	if application.statusText.Text() != status {
		application.statusText.SetText(status)
		application.statusText.SetCaret(len(status), len(status))
	}

	paint.FillShape(gtx.Ops, theme.Bg, clip.Rect{Max: gtx.Constraints.Max}.Op())
	return layout.UniformInset(unit.Dp(16)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				modeButton := func(clickable *widget.Clickable, label string, value host.Mode) layout.Widget {
					return func(gtx layout.Context) layout.Dimensions {
						gtx.Constraints.Min.X = gtx.Constraints.Max.X
						button := material.Button(theme, clickable, label)
						button.TextSize = unit.Sp(15)
						button.Background = color.NRGBA{R: 0xe9, G: 0xec, B: 0xef, A: 0xff}
						button.Color = theme.Fg
						if mode == value {
							button.Background = modeColor
							button.Color = color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
						}
						return button.Layout(gtx)
					}
				}
				return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
					layout.Flexed(1, modeButton(&application.disconnect, "Disconnected", host.ModeDisconnected)),
					layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
					layout.Flexed(1, modeButton(&application.external, "External", host.ModeExternal)),
					layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
					layout.Flexed(1, modeButton(&application.direct, "Direct", host.ModeDirect)),
				)
			}),
			layout.Rigid(layout.Spacer{Height: unit.Dp(16)}.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				label := material.H6(theme, connection)
				label.Color = modeColor
				return label.Layout(gtx)
			}),
			layout.Rigid(layout.Spacer{Height: unit.Dp(16)}.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(material.Button(theme, &application.choose, "Choose engine").Layout),
					layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						if cfg.Engine == "" {
							return material.Body1(theme, "No engine selected").Layout(gtx)
						}
						return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								label := material.Body1(theme, cfg.Name)
								label.MaxLines = 1
								return label.Layout(gtx)
							}),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								label := material.Caption(theme, "Location: "+cfg.Engine)
								label.MaxLines = 1
								return label.Layout(gtx)
							}),
						)
					}),
				)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				if cfg.Mode != host.ModeExternal {
					return layout.Dimensions{}
				}
				label := "Login with Lichess"
				if cfg.Token != "" {
					label = "Login again"
				}
				return layout.Inset{Top: unit.Dp(12)}.Layout(gtx, material.Button(theme, &application.login, label).Layout)
			}),
			layout.Rigid(layout.Spacer{Height: unit.Dp(16)}.Layout),
			layout.Rigid(material.Body2(theme, "Activity").Layout),
			layout.Rigid(layout.Spacer{Height: unit.Dp(6)}.Layout),
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				text := material.Editor(theme, &application.statusText, "Status")
				text.TextSize = unit.Sp(12)
				return text.Layout(gtx)
			}),
		)
	})
}

func run(callbackURLs <-chan *url.URL) error {
	window := new(app.Window)
	window.Option(app.Title("Lichess engine host"), app.Size(unit.Dp(720), unit.Dp(400)), app.MinSize(unit.Dp(560), unit.Dp(360)))
	application, err := newApplication(window, callbackURLs)
	if err != nil {
		return err
	}
	application.host.Start()
	defer application.host.Close()

	theme := material.NewTheme()
	var operations op.Ops
	raised := false
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
			if !raised {
				window.Perform(system.ActionRaise)
				raised = true
			}
		}
	}
}

func main() {
	var callbackURLs chan *url.URL
	if runtime.GOOS == "darwin" && app.ID == appID {
		callbackURLs = make(chan *url.URL)
	}
	go func() {
		if err := run(callbackURLs); err != nil && !strings.Contains(err.Error(), "window closed") {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}()
	app.Events(func(evt event.Event) bool {
		if evt, ok := evt.(app.URLEvent); ok {
			select {
			case callbackURLs <- evt.URL:
			default:
			}
		}
		return true
	})
}
