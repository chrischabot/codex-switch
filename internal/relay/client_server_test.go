package relay

import (
	"bufio"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestClientDeadlinesDoNotLimitModelResponses(t *testing.T) {
	for _, mode := range []string{"headers", "upload", "response"} {
		t.Run(mode, func(t *testing.T) {
			server := newClientServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if _, err := io.Copy(io.Discard, r.Body); err != nil {
					http.Error(w, "upload timed out", 408)
					return
				}
				time.Sleep(150 * time.Millisecond)
				io.WriteString(w, "complete")
			}), clientTimeouts{40 * time.Millisecond, 40 * time.Millisecond, 40 * time.Millisecond})
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			go server.Serve(listener)
			defer server.Close()
			conn, err := net.Dial("tcp", listener.Addr().String())
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			conn.SetDeadline(time.Now().Add(time.Second))
			switch mode {
			case "headers":
				io.WriteString(conn, "POST / HTTP/1.1\r\nHost:")
			case "upload":
				io.WriteString(conn, "POST / HTTP/1.1\r\nHost: localhost\r\nContent-Length: 4\r\n\r\nx")
			case "response":
				io.WriteString(conn, "POST / HTTP/1.1\r\nHost: localhost\r\nContent-Length: 4\r\n\r\ndone")
			}
			reply, err := http.ReadResponse(bufio.NewReader(conn), nil)
			if mode == "headers" {
				if e, ok := err.(net.Error); ok && e.Timeout() {
					t.Fatal("incomplete headers retained the socket")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			defer reply.Body.Close()
			body, _ := io.ReadAll(reply.Body)
			if mode == "upload" && reply.StatusCode != 408 {
				t.Fatalf("upload status %d", reply.StatusCode)
			}
			if mode == "response" && (reply.StatusCode != 200 || strings.TrimSpace(string(body)) != "complete") {
				t.Fatalf("stream was interrupted: %d %s", reply.StatusCode, body)
			}
		})
	}
}

func TestClientServerBoundsTLSHandshakeAndIdleConnections(t *testing.T) {
	if server := NewClientServer(http.NotFoundHandler()); server.ReadHeaderTimeout <= 0 || server.IdleTimeout <= 0 || server.WriteTimeout != 0 {
		t.Fatal("unsafe default stream deadlines")
	}
	for _, tlsHandshake := range []bool{false, true} {
		server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "ok") }))
		server.Config = newClientServer(server.Config.Handler, clientTimeouts{40 * time.Millisecond, 40 * time.Millisecond, time.Second})
		if tlsHandshake {
			server.StartTLS()
		} else {
			server.Start()
		}
		conn, err := net.Dial("tcp", server.Listener.Addr().String())
		if err != nil {
			t.Fatal(err)
		}
		conn.SetDeadline(time.Now().Add(time.Second))
		reader := bufio.NewReader(conn)
		if !tlsHandshake {
			io.WriteString(conn, "GET / HTTP/1.1\r\nHost: localhost\r\n\r\n")
			response, err := http.ReadResponse(reader, nil)
			if err != nil {
				t.Fatal(err)
			}
			io.Copy(io.Discard, response.Body)
			response.Body.Close()
		}
		_, err = reader.ReadByte()
		conn.Close()
		server.Close()
		if e, ok := err.(net.Error); ok && e.Timeout() {
			t.Fatalf("TLS=%v connection was retained indefinitely", tlsHandshake)
		}
		if err == nil {
			t.Fatal("expected connection closure")
		}
	}
}
