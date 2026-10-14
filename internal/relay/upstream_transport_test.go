package relay

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"syscall"
	"testing"
)

func TestUpstreamTransportResendsBeforeTLSHandshakeWithoutHTTP2(t *testing.T) {
	var requests atomic.Int64
	upstream := httptest.NewUnstartedServer(http.HandlerFunc(func(output http.ResponseWriter, request *http.Request) {
		requests.Add(1)
		body, err := io.ReadAll(request.Body)
		if err != nil || string(body) != "model input" {
			t.Errorf("provider body = %q, %v", body, err)
		}
		if request.ProtoMajor != 1 {
			t.Errorf("provider protocol = %s; concurrent sessions must not share HTTP/2 streams", request.Proto)
		}
		io.WriteString(output, "complete")
	}))
	upstream.EnableHTTP2 = true
	upstream.StartTLS()
	defer upstream.Close()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	var connections atomic.Int64
	go func() {
		for {
			connection, err := listener.Accept()
			if err != nil {
				return
			}
			if connections.Add(1) == 1 {
				connection.(*net.TCPConn).SetLinger(0)
				connection.Close()
				continue
			}
			go func() {
				defer connection.Close()
				providerConnection, err := net.Dial("tcp", upstream.Listener.Addr().String())
				if err != nil {
					return
				}
				defer providerConnection.Close()
				go func() { io.Copy(providerConnection, connection); providerConnection.Close() }()
				io.Copy(connection, providerConnection)
			}()
		}
	}()
	transport := newUpstreamTransport()
	defer transport.CloseIdleConnections()
	roots := x509.NewCertPool()
	roots.AddCert(upstream.Certificate())
	transport.transport.TLSClientConfig = &tls.Config{RootCAs: roots}
	request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "https://"+listener.Addr().String(), bytes.NewBufferString("model input"))
	if err != nil {
		t.Fatal(err)
	}
	response, err := (&http.Client{Transport: transport}).Do(request)
	if err != nil {
		t.Fatalf("request after a reset before TLS handshake: %v", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil || string(body) != "complete" || requests.Load() != 1 || connections.Load() != 2 {
		t.Fatalf("safe retry = %q, %v; %d provider requests, %d connections", body, err, requests.Load(), connections.Load())
	}
}

// The TCP connection and provider are real. This write shim reproduces Go's
// zero-byte failure on a reused socket deterministically, before bytes reach it.
func TestUpstreamTransportResendsAfterZeroByteWriteOnReusedSocket(t *testing.T) {
	var requests atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(output http.ResponseWriter, request *http.Request) {
		body, err := io.ReadAll(request.Body)
		if err != nil || string(body) != "model input" {
			t.Errorf("provider body = %q, %v", body, err)
		}
		requests.Add(1)
		io.WriteString(output, "complete")
	}))
	defer upstream.Close()
	transport := newUpstreamTransport()
	defer transport.CloseIdleConnections()
	var connections atomic.Int64
	var firstConnection *zeroByteWriteFailureConn
	dialer := &net.Dialer{}
	transport.transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		connection, err := dialer.DialContext(ctx, network, address)
		if err != nil {
			return nil, err
		}
		wrapped := &zeroByteWriteFailureConn{Conn: connection}
		if connections.Add(1) == 1 {
			firstConnection = wrapped
		}
		return wrapped, nil
	}
	client := &http.Client{Transport: transport}
	for attempt := range 2 {
		if attempt == 1 {
			firstConnection.failNextWrite.Store(true)
		}
		request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, upstream.URL, bytes.NewBufferString("model input"))
		if err != nil {
			t.Fatal(err)
		}
		response, err := client.Do(request)
		if err != nil {
			t.Fatalf("request %d: %v", attempt+1, err)
		}
		body, err := io.ReadAll(response.Body)
		response.Body.Close()
		if err != nil || string(body) != "complete" {
			t.Fatalf("response %d: %q, %v", attempt+1, body, err)
		}
	}
	if !firstConnection.failed.Load() || connections.Load() != 2 || requests.Load() != 2 {
		t.Fatalf("zero-byte socket retry: failure injected %t, connections %d, provider requests %d", firstConnection.failed.Load(), connections.Load(), requests.Load())
	}
}

type zeroByteWriteFailureConn struct {
	net.Conn
	failNextWrite atomic.Bool
	failed        atomic.Bool
}

func (connection *zeroByteWriteFailureConn) Write(payload []byte) (int, error) {
	if connection.failNextWrite.CompareAndSwap(true, false) {
		connection.failed.Store(true)
		return 0, &net.OpError{Op: "write", Net: "tcp", Err: syscall.ECONNRESET}
	}
	return connection.Conn.Write(payload)
}
