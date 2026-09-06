package main

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"runtime"
	"strings"

	"gioui.org/app"
	"gioui.org/io/event"
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
	window   *app.Window
	explorer *explorer.Explorer
	host     *host.App

	statusText widget.Editor
	login      widget.Clickable
	choose     widget.Clickable
}

func newApplication(window *app.Window, callbackURLs <-chan *url.URL) (*application, error) {
	application := &application{window: window, explorer: explorer.NewExplorer(window)}
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
	for application.login.Clicked(gtx) {
		go application.host.Login()
	}
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
	return layout.UniformInset(unit.Dp(10)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				var label = "Login with Lichess"
				if cfg.Token != "" {
					label = "Login again"
				}
				return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(material.Button(theme, &application.login, label).Layout),
					layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						if cfg.Token == "" {
							defer paint.PushOpacity(gtx.Ops, .38).Pop()
							gtx = gtx.Disabled()
						}
						return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
							layout.Rigid(material.Button(theme, &application.choose, "Choose engine").Layout),
							layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
								if cfg.Engine == "" {
									return layout.Dimensions{}
								}
								return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
									return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
										layout.Rigid(func(gtx layout.Context) layout.Dimensions {
											label := material.Caption(theme, "Name: "+cfg.Name)
											label.MaxLines = 1
											return label.Layout(gtx)
										}),
										layout.Rigid(func(gtx layout.Context) layout.Dimensions {
											label := material.Caption(theme, "Location: "+cfg.Engine)
											label.MaxLines = 1
											return label.Layout(gtx)
										}),
									)
								})
							}),
						)
					}),
				)
			}),
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
	window.Option(app.Title("Native engine host"), app.Size(unit.Dp(720), unit.Dp(280)), app.MinSize(unit.Dp(640), unit.Dp(250)))
	application, err := newApplication(window, callbackURLs)
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
