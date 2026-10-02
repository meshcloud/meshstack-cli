package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	gohttp "net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// The CLI prints its "Opening your browser" lines first, and the gif should show them.
const browserDelay = 1500 * time.Millisecond

var nonceInput = regexp.MustCompile(`name="nonce" value="([^"]+)"`)

// browse plays the person on the CLI's loopback page, see internal/oidc/browser. HTTPS_PROXY and
// SSL_CERT_FILE, inherited from the CLI, lead it to serve.
func browse(startURL string) error {
	time.Sleep(browserDelay)
	jar, err := cookiejar.New(nil)
	if err != nil {
		return err
	}
	browser := &gohttp.Client{Jar: jar, Timeout: 30 * time.Second}

	page, err := load(browser, gohttp.MethodGet, startURL, nil)
	if err != nil {
		return err
	}
	nonce := nonceInput.FindStringSubmatch(page)
	if nonce == nil {
		return errors.New("the start page carries no nonce: " + page)
	}
	_, err = load(browser, gohttp.MethodPost, startURL, url.Values{"nonce": {nonce[1]}, "access": {"full"}})
	return err
}

func load(browser *gohttp.Client, method, pageUrl string, form url.Values) (string, error) {
	request, err := gohttp.NewRequestWithContext(context.Background(), method, pageUrl, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	if form != nil {
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	resp, err := browser.Do(request)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err == nil && resp.StatusCode != gohttp.StatusOK {
		err = fmt.Errorf("%s answered %s: %s", resp.Request.URL, resp.Status, body)
	}
	return string(body), err
}
