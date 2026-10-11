package relay

import (
	"errors"
	"io"
	"sync"
)

type observedResponseBody struct {
	io.ReadCloser
	observer        responseByteObserver
	finishOnce      sync.Once
	reportReadError func(error)
}

func createObservedResponseBody(body io.ReadCloser, observer responseByteObserver) *observedResponseBody {
	return &observedResponseBody{ReadCloser: body, observer: observer}
}

func (body *observedResponseBody) Read(destination []byte) (int, error) {
	bytesRead, readError := body.ReadCloser.Read(destination)
	if bytesRead > 0 {
		body.observer.ObserveBytes(destination[:bytesRead])
	}
	if errors.Is(readError, io.EOF) {
		body.finishOnce.Do(body.observer.FinishObservation)
	}
	if readError != nil && body.reportReadError != nil {
		body.reportReadError(readError)
	}
	return bytesRead, readError
}

func (body *observedResponseBody) Close() error {
	return body.ReadCloser.Close()
}
