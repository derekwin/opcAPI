package main

import (
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"
)

// Keep HTTP-over-SSH clients working when the HTTPS listener shares their port.
// Both socket addresses come from the kernel, independent of HTTP headers.
type gatewayListener struct {
	net.Listener
	config       *tls.Config
	loopbackHTTP bool
}

func (l gatewayListener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	if l.loopbackHTTP && c.LocalAddr().(*net.TCPAddr).IP.IsLoopback() && c.RemoteAddr().(*net.TCPAddr).IP.IsLoopback() {
		return c, nil
	}
	return tls.Server(c, l.config), nil
}

func listenTCP(address string) (net.Listener, error) {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	if net.ParseIP(host).To4() != nil {
		return net.Listen("tcp4", address)
	}
	return net.Listen("tcp", address)
}

func run() error {
	g := newGateway()
	socket := flag.String("admin-socket", "/run/key-manager/admin.sock", "administrator's Unix socket")
	adminAddr := flag.String("admin-listen", "", "additional administrator address")
	adminHTTP := flag.Bool("admin-http", false, "serve the administrator TCP listener over HTTP")
	addr := flag.String("listen", "127.0.0.1:8443", "model API address")
	apiHTTP := flag.Bool("api-http", false, "serve the model API over HTTP")
	tunnelPort := flag.Uint("tunnel-port", 0, "loopback HTTP model gateway port for SSH forwarding (0 disables)")
	cert := flag.String("cert", "", "TLS certificate file")
	key := flag.String("key", "", "TLS private key file")
	flag.StringVar(&g.upstream, "upstream", g.upstream, "model server origin")
	flag.StringVar(&g.file, "store", g.file, "key store path")
	flag.Int64Var(&g.maxBody, "max-body-bytes", defaultMaxBody, "maximum chat request size in bytes")
	flag.Parse()
	if g.maxBody < 1 {
		return errors.New("max-body-bytes must be positive")
	}
	adminHandler, err := authenticatedAdmin(filepath.Join(filepath.Dir(g.file), "admin-account.json"), http.HandlerFunc(g.admin))
	if err != nil {
		return err
	}
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12, NextProtos: []string{"h2", "http/1.1"}}
	if !*apiHTTP || (*adminAddr != "" && !*adminHTTP) {
		pair, err := tls.LoadX509KeyPair(*cert, *key)
		if err != nil {
			return err
		}
		tlsConfig.Certificates = []tls.Certificate{pair}
	}
	publicListener, err := listenTCP(*addr)
	if err != nil {
		return err
	}
	defer publicListener.Close()
	// systemd provides a private runtime directory. Remove a stale socket after a crash.
	if fi, err := os.Lstat(*socket); err == nil && fi.Mode()&os.ModeSocket != 0 {
		if err = os.Remove(*socket); err != nil {
			return err
		}
	}
	local, err := net.Listen("unix", *socket)
	if err != nil {
		return err
	}
	defer local.Close()
	if err = os.Chmod(*socket, 0600); err != nil {
		return err
	}
	if err = g.withRecords(false, func(map[string]*keyRecord) error { return nil }); err != nil {
		return err
	}
	admin := &http.Server{Handler: adminHandler, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 90 * time.Second, TLSConfig: tlsConfig.Clone()}
	public := &http.Server{Handler: http.HandlerFunc(g.public), ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 90 * time.Second, TLSConfig: tlsConfig.Clone()}
	stop, done := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer done()
	errs := make(chan error, 4)
	gatewayAddr := publicListener.Addr().(*net.TCPAddr)
	shared := *tunnelPort != 0 && uint(gatewayAddr.Port) == *tunnelPort && (gatewayAddr.IP.IsUnspecified() || gatewayAddr.IP.Equal(net.IPv4(127, 0, 0, 1)))
	var adminTCP net.Listener
	if *adminAddr != "" {
		adminTCP, err = listenTCP(*adminAddr)
		if err != nil {
			return err
		}
		defer adminTCP.Close()
	}
	if *tunnelPort != 0 && !shared {
		listener, err := net.Listen("tcp4", net.JoinHostPort("127.0.0.1", strconv.FormatUint(uint64(*tunnelPort), 10)))
		if err != nil {
			return err
		}
		defer listener.Close()
		go func() { errs <- public.Serve(listener) }()
	}
	if adminTCP != nil {
		if *adminHTTP {
			go func() { errs <- admin.Serve(adminTCP) }()
		} else {
			go func() { errs <- admin.ServeTLS(adminTCP, "", "") }()
		}
	}
	go func() { errs <- admin.Serve(local) }()
	var apiListener net.Listener = publicListener
	if !*apiHTTP {
		apiListener = gatewayListener{publicListener, tlsConfig, shared}
	}
	go func() { errs <- public.Serve(apiListener) }()

	select {
	case <-stop.Done():
	case err = <-errs:
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	return errors.Join(err, admin.Shutdown(ctx), public.Shutdown(ctx))
}
func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}
