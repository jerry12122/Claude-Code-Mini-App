package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"fmt"
	"log"
	"math/big"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type proxySpan struct {
	Event       string  `json:"event"`
	UTC         string  `json:"utc"`
	MonotonicMS float64 `json:"monotonic_ms"`
	Method      string  `json:"method,omitempty"`
	SafePath    string  `json:"safe_path,omitempty"`
	Status      int     `json:"status,omitempty"`
	ErrorType   string  `json:"error_type,omitempty"`
	Correlation string  `json:"correlation"`
}

type diagLogger struct {
	mu        sync.Mutex
	file      *os.File
	startTime time.Time
}

func isSessionPath(p string) bool {
	return p == "/sessions" || strings.HasPrefix(p, "/sessions/")
}

func newDiagLogger() *diagLogger {
	if os.Getenv("CMA_UI_DIAGNOSTICS") != "1" {
		return nil
	}
	dir := os.Getenv("CMA_UI_DIAG_DIR")
	if dir == "" {
		dir = filepath.Join("test-results", "ui", "webkit")
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "DIAGNOSTIC_ERROR: failed to create proxy log dir %s: %v\n", dir, err)
		return nil
	}
	logPath := filepath.Join(dir, "proxy-spans.jsonl")
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "DIAGNOSTIC_ERROR: failed to open proxy log file %s: %v\n", logPath, err)
		return nil
	}
	return &diagLogger{file: f, startTime: time.Now()}
}

func (l *diagLogger) Log(span proxySpan) {
	if l == nil || l.file == nil {
		return
	}
	span.Correlation = "non-atomic (path+method+time); note: http_handler_entry observes HTTP request dispatch, not TCP/TLS transport accept"
	span.UTC = time.Now().UTC().Format(time.RFC3339Nano)
	span.MonotonicMS = float64(time.Since(l.startTime).Microseconds()) / 1000.0
	data, err := json.Marshal(span)
	if err != nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	_, _ = l.file.Write(append(data, '\n'))
}

func (l *diagLogger) Close() {
	if l != nil && l.file != nil {
		l.mu.Lock()
		defer l.mu.Unlock()
		_ = l.file.Close()
	}
}

func main() {
	// A loopback-only fixture: never used by the production server.
	if len(os.Args) != 2 {
		log.Fatal("expected a loopback HTTP target")
	}
	target, err := url.Parse(os.Args[1])
	if err != nil {
		log.Fatal(err)
	}
	if target.Scheme != "http" || target.Hostname() != "127.0.0.1" || target.Port() == "" {
		log.Fatal("expected a loopback HTTP target")
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		log.Fatal(err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		log.Fatal(err)
	}
	template := x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: "bs-local.com"}, DNSNames: []string{"bs-local.com", "localhost"}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		log.Fatal(err)
	}
	listener, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("https://" + listener.Addr().String())

	proxy := httputil.NewSingleHostReverseProxy(target)
	diag := newDiagLogger()
	var handler http.Handler = proxy
	if diag != nil {
		defer diag.Close()
		origDirector := proxy.Director
		proxy.Director = func(req *http.Request) {
			origDirector(req)
			if isSessionPath(req.URL.Path) {
				diag.Log(proxySpan{
					Event:    "proxy_upstream_start",
					Method:   req.Method,
					SafePath: req.URL.Path,
				})
			}
		}
		proxy.ModifyResponse = func(resp *http.Response) error {
			if resp.Request != nil && isSessionPath(resp.Request.URL.Path) {
				diag.Log(proxySpan{
					Event:    "proxy_upstream_response",
					Method:   resp.Request.Method,
					SafePath: resp.Request.URL.Path,
					Status:   resp.StatusCode,
				})
			}
			return nil
		}
		proxy.ErrorHandler = func(w http.ResponseWriter, req *http.Request, err error) {
			if isSessionPath(req.URL.Path) {
				errType := "unknown"
				if err != nil {
					s := err.Error()
					if strings.Contains(s, "context canceled") {
						errType = "context_canceled"
					} else if strings.Contains(s, "timeout") || strings.Contains(s, "deadline exceeded") {
						errType = "timeout"
					} else if strings.Contains(s, "EOF") {
						errType = "eof"
					} else if strings.Contains(s, "connection refused") {
						errType = "connection_refused"
					} else if strings.Contains(s, "connection reset") {
						errType = "connection_reset"
					} else {
						errType = "upstream_error"
					}
				}
				diag.Log(proxySpan{
					Event:     "proxy_upstream_error",
					Method:    req.Method,
					SafePath:  req.URL.Path,
					ErrorType: errType,
				})
			}
			w.WriteHeader(http.StatusBadGateway)
		}
		handler = http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			if isSessionPath(req.URL.Path) {
				diag.Log(proxySpan{
					Event:    "http_handler_entry",
					Method:   req.Method,
					SafePath: req.URL.Path,
				})
			}
			proxy.ServeHTTP(w, req)
		})
	}

	server := &http.Server{Handler: handler, ReadHeaderTimeout: 10 * time.Second}
	log.Fatal(server.Serve(listener))
}
