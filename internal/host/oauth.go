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

const clientID = "native-engine-host"

func oauthLogin(baseURL string) (string, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	defer listener.Close()
	redirectURI := "http://" + listener.Addr().String() + "/oauth/callback"
	verifier, state := randomString(64), randomString(32)
	digest := sha256.Sum256([]byte(verifier))
	query := url.Values{
		"response_type": {"code"}, "client_id": {clientID}, "redirect_uri": {redirectURI},
		"code_challenge_method": {"S256"}, "code_challenge": {base64.RawURLEncoding.EncodeToString(digest[:])},
		"scope": {"engine:read engine:write"}, "state": {state},
	}

	type callback struct{ code, err string }
	result := make(chan callback, 1)
	server := &http.Server{ReadHeaderTimeout: 5 * time.Second}
	server.Handler = http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/oauth/callback" {
			http.NotFound(writer, request)
			return
		}
		got := callback{code: request.URL.Query().Get("code"), err: request.URL.Query().Get("error")}
		if request.URL.Query().Get("state") != state {
			got.err = "invalid OAuth state"
		}
		if got.code == "" && got.err == "" {
			got.err = "missing authorization code"
		}
		result <- got
		writer.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(writer, "<!doctype html><title>Engine host</title><p>You can close this window.</p>")
	})
	go server.Serve(listener)
	if err := openBrowser(baseURL + "/oauth?" + query.Encode()); err != nil {
		server.Close()
		return "", err
	}

	var got callback
	select {
	case got = <-result:
	case <-time.After(5 * time.Minute):
		server.Close()
		return "", errors.New("authorization timed out")
	}
	server.Close()
	if got.err != "" {
		return "", errors.New(got.err)
	}

	form := url.Values{
		"grant_type": {"authorization_code"}, "code": {got.code}, "code_verifier": {verifier},
		"redirect_uri": {redirectURI}, "client_id": {clientID},
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
