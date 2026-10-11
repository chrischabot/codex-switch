package relay

import "bytes"

type responseByteObserver interface {
	ObserveBytes([]byte)
	FinishObservation()
}

type serverSentEventObserver struct {
	currentLine  []byte
	currentData  []byte
	skipLineFeed bool
	onEvent      func([]byte) bool
}

func createServerSentEventObserver(onEvent func([]byte) bool) *serverSentEventObserver {
	return &serverSentEventObserver{onEvent: onEvent}
}

func (observer *serverSentEventObserver) ObserveBytes(chunk []byte) {
	for _, currentByte := range chunk {
		if observer.skipLineFeed {
			observer.skipLineFeed = false
			if currentByte == '\n' {
				continue
			}
		}
		if currentByte != '\r' && currentByte != '\n' {
			observer.currentLine = append(observer.currentLine, currentByte)
			continue
		}
		observer.finishLine()
		observer.skipLineFeed = currentByte == '\r'
	}
}

func (observer *serverSentEventObserver) FinishObservation() {
	observer.finishLine()
	observer.dispatchEvent()
}

func (observer *serverSentEventObserver) finishLine() {
	line := observer.currentLine
	observer.currentLine = observer.currentLine[:0]
	if len(line) == 0 {
		observer.dispatchEvent()
		return
	}
	if !bytes.HasPrefix(line, []byte("data:")) {
		return
	}
	data := bytes.TrimPrefix(line, []byte("data:"))
	data = bytes.TrimPrefix(data, []byte(" "))
	observer.currentData = append(observer.currentData, data...)
	observer.currentData = append(observer.currentData, '\n')
}

func (observer *serverSentEventObserver) dispatchEvent() {
	if len(observer.currentData) == 0 {
		return
	}
	observer.onEvent(bytes.TrimSuffix(observer.currentData, []byte("\n")))
	observer.currentData = observer.currentData[:0]
}

type jsonResponseObserver struct {
	bodyBytes []byte
	onBody    func([]byte)
}

func createJSONResponseObserver(onBody func([]byte)) *jsonResponseObserver {
	return &jsonResponseObserver{onBody: onBody}
}

func (observer *jsonResponseObserver) ObserveBytes(chunk []byte) {
	observer.bodyBytes = append(observer.bodyBytes, chunk...)
}

func (observer *jsonResponseObserver) FinishObservation() {
	observer.onBody(observer.bodyBytes)
	observer.bodyBytes = nil
}
