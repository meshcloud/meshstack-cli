package main

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"time"
)

// The CLI prints its "Opening your browser" lines first, and the gif should show them.
const browserDelay = 1500 * time.Millisecond

var nonceInput = regexp.MustCompile(`name="nonce" value="([^"]+)"`)

// browse does what a person does on the CLI's loopback page, see internal/oidc/browser: it submits
// the access level form and follows the redirects through the identity provider back to the
// loopback callback. HTTPS_PROXY and SSL_CERT_FILE, inherited from the CLI, lead it to serve.
func browse(startURL string) error {
	time.Sleep(browserDelay)
	jar, err := cookiejar.New(nil)
	if err != nil {
		return err
	}
	browser := &http.Client{Jar: jar, Timeout: 30 * time.Second}

	page, err := read(browser.Get(startURL))
	if err != nil {
		return err
	}
	nonce := nonceInput.FindStringSubmatch(page)
	if nonce == nil {
		return errors.New("the start page carries no nonce: " + page)
	}
	_, err = read(browser.PostForm(startURL, url.Values{"nonce": {nonce[1]}, "access": {"full"}}))
	return err
}

func read(resp *http.Response, err error) (string, error) {
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err == nil && resp.StatusCode != http.StatusOK {
		err = fmt.Errorf("%s answered %s: %s", resp.Request.URL, resp.Status, body)
	}
	return string(body), err
}
