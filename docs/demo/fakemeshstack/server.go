package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	gohttp "net/http"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/meshcloud/meshstack-cli/internal/testutil/fakemeshstack"
)

// lifetime ends a server that the tape failed to stop.
const lifetime = 10 * time.Minute

// serve answers for every installation's host names on one TLS listener. The CLI reaches it
// through a proxy that tunnels only those names, so the endpoints are real-looking https URLs
// under example.com without DNS or /etc/hosts entries, and nothing else leaves the machine.
func serve(dir string) error {
	cert, certPEM, err := selfSignedCert(hostNames())
	if err != nil {
		return err
	}
	var listener net.ListenConfig
	backend, err := listener.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	proxy, err := listener.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}

	mux := gohttp.NewServeMux()
	for _, i := range installations {
		meshStack := i.meshStack()
		mux.Handle(i.api+"/", meshStack)
		mux.Handle(i.sso+"/", meshStack)
	}
	mux.Handle(docsHost+"/", fakemeshstack.New(fakemeshstack.Options{ApiDocs: apiDocs}))
	failed := make(chan error, 2)
	go func() {
		server := &gohttp.Server{
			Handler:           logRequests(mux),
			TLSConfig:         &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12},
			ReadHeaderTimeout: 10 * time.Second,
		}
		failed <- server.ServeTLS(backend, "", "")
	}()
	go func() {
		server := &gohttp.Server{Handler: tunnel(backend.Addr().String()), ReadHeaderTimeout: 10 * time.Second}
		failed <- server.Serve(proxy)
	}()

	if err := writeDemoConfig(dir, certPEM, proxy.Addr().String()); err != nil {
		return err
	}
	select {
	case err := <-failed:
		return err
	case <-time.After(lifetime):
		return nil
	}
}

func hostNames() (hosts []string) {
	for _, i := range installations {
		hosts = append(hosts, i.api, i.sso)
	}
	return append(hosts, docsHost)
}

// tunnel is an HTTPS proxy for the installations' host names only. Refusing every other host,
// such as api.github.com for the CLI's release check, keeps the demo off the network.
func tunnel(backendAddr string) gohttp.Handler {
	return gohttp.HandlerFunc(func(w gohttp.ResponseWriter, r *gohttp.Request) {
		host, _, _ := net.SplitHostPort(r.Host)
		if r.Method != gohttp.MethodConnect || !slices.Contains(hostNames(), host) {
			log.Printf("proxy refused %s %s", r.Method, r.Host)
			gohttp.Error(w, "this proxy only reaches the demo's meshStack installations", gohttp.StatusForbidden)
			return
		}
		var dialer net.Dialer
		upstream, err := dialer.DialContext(r.Context(), "tcp", backendAddr)
		if err != nil {
			gohttp.Error(w, err.Error(), gohttp.StatusBadGateway)
			return
		}
		defer func() { _ = upstream.Close() }()
		client, _, err := gohttp.NewResponseController(w).Hijack()
		if err != nil {
			gohttp.Error(w, err.Error(), gohttp.StatusInternalServerError)
			return
		}
		defer func() { _ = client.Close() }()
		if _, err := io.WriteString(client, "HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
			return
		}
		go func() {
			_, _ = io.Copy(upstream, client)
			_ = upstream.Close()
		}()
		_, _ = io.Copy(client, upstream)
	})
}

func logRequests(next gohttp.Handler) gohttp.Handler {
	return gohttp.HandlerFunc(func(w gohttp.ResponseWriter, r *gohttp.Request) {
		log.Printf("%s https://%s%s", r.Method, r.Host, r.URL.RequestURI())
		next.ServeHTTP(w, r)
	})
}

// selfSignedCert is its own CA, so SSL_CERT_FILE can name it as the one root the CLI trusts.
func selfSignedCert(hosts []string) (tls.Certificate, []byte, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, nil, err
	}
	template := &x509.Certificate{
		Subject:               pkix.Name{CommonName: "fakemeshstack"},
		DNSNames:              hosts,
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, nil, err
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, certPEM, nil
}

// writeDemoConfig writes the env file last, and by rename, since the tape waits for it.
func writeDemoConfig(dir string, certPEM []byte, proxyAddr string) error {
	configDir, caFile := filepath.Join(dir, "config"), filepath.Join(dir, "ca.pem")
	type profile struct {
		Endpoint         string `json:"endpoint"`
		DefaultWorkspace string `json:"default_workspace,omitempty"`
	}
	profiles := map[string]profile{}
	for _, i := range installations {
		profiles[i.profile] = profile{Endpoint: i.endpoint(), DefaultWorkspace: i.defaultWorkspace}
	}
	files := []struct {
		path    string
		content any
	}{
		{filepath.Join(configDir, "profiles.json"), map[string]any{
			"version": 1, "currentProfile": installations[0].profile, "profiles": profiles,
		}},
		// A release check done a moment ago is not done again, see internal/auth/version_check.go.
		{filepath.Join(configDir, "versionCheck.json"), map[string]any{"lastCheck": time.Now()}},
	}
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		return err
	}
	var errs []error
	for _, file := range files {
		content, err := json.Marshal(file.content, jsontext.WithIndent("  "))
		errs = append(errs, err, os.WriteFile(file.path, content, 0o600))
	}
	errs = append(errs, os.WriteFile(caFile, certPEM, 0o600))
	if err := errors.Join(errs...); err != nil {
		return err
	}

	env := fmt.Sprintf("export MESHSTACK_CONFIG_DIR=%q\nexport HTTPS_PROXY=%q\nexport SSL_CERT_FILE=%q\nexport MESHSTACK_API_DOCS_URL=%q\n",
		configDir, "http://"+proxyAddr, caFile, "https://"+docsHost+fakemeshstack.ApiDocsPath)
	if err := os.WriteFile(filepath.Join(dir, "env.tmp"), []byte(env), 0o600); err != nil {
		return err
	}
	return os.Rename(filepath.Join(dir, "env.tmp"), filepath.Join(dir, "env"))
}
