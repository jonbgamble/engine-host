package host

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"runtime"
	"time"
)

const clientID = "lichess-engine-host"
const oauthCallbackScheme = "org.lichess.engine-host"
const oauthCallbackURI = oauthCallbackScheme + "://oauth/callback"

type oauthCallback struct {
	code string
	err  string
}

type oauthReceiver struct {
	redirectURI  string
	result       <-chan oauthCallback
	callbackURLs <-chan *url.URL
	state        string
	close        func()
}

func oauthLogin(baseURL string, callbackURLs <-chan *url.URL) (string, error) {
	verifier, state := randomString(64), randomString(32)
	receiver, err := newOAuthReceiver(state, callbackURLs)
	if err != nil {
		return "", err
	}
	defer receiver.close()

	digest := sha256.Sum256([]byte(verifier))
	query := url.Values{
		"response_type": {"code"}, "client_id": {clientID}, "redirect_uri": {receiver.redirectURI},
		"code_challenge_method": {"S256"}, "code_challenge": {base64.RawURLEncoding.EncodeToString(digest[:])},
		"scope": {"engine:read engine:write"}, "state": {state},
	}
	if err := openBrowser(baseURL + "/oauth?" + query.Encode()); err != nil {
		return "", err
	}

	got, err := receiver.wait(5 * time.Minute)
	if err != nil {
		return "", err
	}
	if got.err != "" {
		return "", errors.New(got.err)
	}

	form := url.Values{
		"grant_type": {"authorization_code"}, "code": {got.code}, "code_verifier": {verifier},
		"redirect_uri": {receiver.redirectURI}, "client_id": {clientID},
	}
	response, err := http.PostForm(baseURL+"/api/token", form)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if err := checkResponse(response); err != nil {
		return "", err
	}
	var token struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(response.Body).Decode(&token); err != nil {
		return "", err
	}
	return token.AccessToken, nil
}

func newOAuthReceiver(state string, callbackURLs <-chan *url.URL) (*oauthReceiver, error) {
	if runtime.GOOS == "darwin" && callbackURLs != nil {
		return &oauthReceiver{
			redirectURI: oauthCallbackURI, callbackURLs: callbackURLs, state: state, close: func() {},
		}, nil
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	result := make(chan oauthCallback, 1)
	server := &http.Server{ReadHeaderTimeout: 5 * time.Second}
	server.Handler = http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/oauth/callback" {
			http.NotFound(writer, request)
			return
		}
		got := parseOAuthCallback(request.URL, state)
		result <- got
		writer.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(writer, "<!doctype html><title>Lichess Engine Host</title><p>You can close this window.</p>")
	})
	go server.Serve(listener)
	return &oauthReceiver{
		redirectURI: "http://" + listener.Addr().String() + "/oauth/callback",
		result:      result,
		state:       state,
		close:       func() { server.Close() },
	}, nil
}

func (receiver *oauthReceiver) wait(timeout time.Duration) (oauthCallback, error) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		select {
		case got := <-receiver.result:
			return got, nil
		case callbackURL := <-receiver.callbackURLs:
			if callbackURL == nil || callbackURL.Scheme != oauthCallbackScheme || callbackURL.Host != "oauth" || callbackURL.Path != "/callback" {
				continue
			}
			return parseOAuthCallback(callbackURL, receiver.state), nil
		case <-timer.C:
			return oauthCallback{}, errors.New("authorization timed out")
		}
	}
}

func parseOAuthCallback(callbackURL *url.URL, state string) oauthCallback {
	got := oauthCallback{code: callbackURL.Query().Get("code"), err: callbackURL.Query().Get("error")}
	if callbackURL.Query().Get("state") != state {
		got.err = "invalid OAuth state"
	}
	if got.code == "" && got.err == "" {
		got.err = "missing authorization code"
	}
	return got
}

func openBrowser(target string) error {
	var command string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		command = "open"
	case "windows":
		command, args = "rundll32", []string{"url.dll,FileProtocolHandler"}
	default:
		command = "xdg-open"
	}
	return exec.Command(command, append(args, target)...).Start()
}

func randomString(length int) string {
	data := make([]byte, length)
	if _, err := rand.Read(data); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(data)
}
