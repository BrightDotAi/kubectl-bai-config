package profile

import (
	"encoding/base64"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/BrightDotAi/kubectl-bai-config/internal"
	"github.com/pkg/errors"
	"github.com/spacelift-io/spacectl/client/session"
)

const (
	cliServerPort      = 8020
	cliBrowserPath     = "/cli_login"
	cliAuthSuccessPage = "/auth_success"
	cliAuthFailurePage = "/auth_failure"
)

func LoginUsingWebBrowser(creds *session.StoredCredentials) error {
	pubKey, privKey, err := internal.GenerateRSAKeyPair()
	if err != nil {
		return errors.Wrap(err, "could not generate RSA key pair")
	}

	keyBase64 := base64.RawURLEncoding.EncodeToString(pubKey)

	browserURL, err := buildBrowserURL(creds.Endpoint, keyBase64)
	if err != nil {
		return errors.Wrap(err, "could not build browser URL")
	}

	done := make(chan bool, 1)
	handler := func(w http.ResponseWriter, r *http.Request) {
		handlerErr := func() error {
			base64Token := r.URL.Query().Get("token")
			if base64Token == "" {
				return errors.New("missing token parameter")
			}

			base64Key := r.URL.Query().Get("key")
			if base64Key == "" {
				return errors.New("missing key parameter")
			}

			encToken, err := base64.RawURLEncoding.DecodeString(base64Token)
			if err != nil {
				return errors.Wrap(err, "could not decode session token")
			}

			encKey, err := base64.RawURLEncoding.DecodeString(base64Key)
			if err != nil {
				return errors.Wrap(err, "could not decode key")
			}

			key, err := internal.DecryptRSA(privKey, []byte(encKey))
			if err != nil {
				return errors.Wrap(err, "could not decrypt key")
			}

			jwt, err := internal.DecryptAES(key, []byte(encToken))
			if err != nil {
				return errors.Wrap(err, "could not decrypt session token")
			}

			creds.AccessToken = string(jwt)

			return nil
		}()

		infoPage, err := url.Parse(creds.Endpoint)
		if err != nil {
			log.Fatal(err)
		}

		if handlerErr != nil {
			log.Println(handlerErr)
			infoPage.Path = cliAuthFailurePage
			http.Redirect(w, r, infoPage.String(), http.StatusTemporaryRedirect)
		} else {
			fmt.Println("Done!")
			infoPage.Path = cliAuthSuccessPage
			http.Redirect(w, r, infoPage.String(), http.StatusTemporaryRedirect)
		}

		done <- true
	}

	m := http.NewServeMux()
	server := &http.Server{Addr: fmt.Sprintf(":%d", cliServerPort), Handler: m}
	m.HandleFunc("/", handler)

	fmt.Printf("\nOpening browser to %s\n\n", browserURL)

	go func() {
		if err := server.ListenAndServe(); err != http.ErrServerClosed {
			log.Fatalf("could not start local server: %s", err)
		}
	}()

	if err := openWebBrowser(browserURL); err != nil {
		return err
	}

	fmt.Println("Waiting for login...")

	select {
	case <-done:
		server.Close()
	case <-time.After(2 * time.Minute):
		server.Close()
		return errors.New("login timeout exceeded")
	}

	return nil
}

func buildBrowserURL(endpoint, pubKey string) (string, error) {
	base, err := url.Parse(endpoint)
	if err != nil {
		return "", err
	}
	base.Path = cliBrowserPath

	q := url.Values{}
	q.Add("key", pubKey)

	base.RawQuery = q.Encode()

	return base.String(), nil
}

func openWebBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "linux":
		cmd = exec.Command("xdg-open", url)
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		r := strings.NewReplacer("&", "^&")
		cmd = exec.Command("cmd", "/c", "start", r.Replace(url))
	default:
		return errors.New("unsupported platform")
	}

	err := cmd.Start()
	if err != nil {
		return errors.Wrap(err, "could not open the browser")
	}

	err = cmd.Wait()
	if err != nil {
		return errors.Wrap(err, "could not wait for the opening browser")
	}

	return nil
}
