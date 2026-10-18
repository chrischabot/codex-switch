package relay

import (
	"io"
	"net/http"
	"time"
)

type clientTimeouts struct{ headers, idle, upload time.Duration }

var defaultClientTimeouts = clientTimeouts{10 * time.Second, time.Minute, 2 * time.Minute}

// NewClientServer bounds client setup and uploads independently of streamed replies.
func NewClientServer(handler http.Handler) *http.Server {
	return newClientServer(handler, defaultClientTimeouts)
}
func newClientServer(handler http.Handler, limits clientTimeouts) *http.Server {
	return &http.Server{ReadHeaderTimeout: limits.headers, IdleTimeout: limits.idle, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Clear only the upload deadline at EOF. No write deadline applies to
		// SSE, WebSockets, or a model response waiting for its first byte.
		if r.Body != nil && r.Body != http.NoBody && r.ContentLength != 0 {
			control := http.NewResponseController(w)
			if err := control.SetReadDeadline(time.Now().Add(limits.upload)); err != nil {
				http.Error(w, "cannot bound request upload", http.StatusInternalServerError)
				return
			}
			r.Body = &uploadDeadlineBody{ReadCloser: r.Body, control: control}
		}
		handler.ServeHTTP(w, r)
	})}
}

type uploadDeadlineBody struct {
	io.ReadCloser
	control *http.ResponseController
}

func (b *uploadDeadlineBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if err == io.EOF {
		b.control.SetReadDeadline(time.Time{})
	}
	return n, err
}
func (b *uploadDeadlineBody) Close() error { return b.ReadCloser.Close() }
