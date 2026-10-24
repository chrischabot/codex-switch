package relay

import "bytes"

type responseByteObserver interface {
	ObserveBytes([]byte)
	FinishObservation()
}

type serverSentEventObserver struct {
	currentLine        []byte
	currentData        []byte
	skipLineFeed       bool
	discardLine        bool
	skipEvent          bool
	onEvent            func([]byte) bool
	shouldObserveEvent func([]byte) bool
}

func createServerSentEventObserver(onEvent func([]byte) bool, shouldObserveEvent func([]byte) bool) *serverSentEventObserver {
	return &serverSentEventObserver{onEvent: onEvent, shouldObserveEvent: shouldObserveEvent}
}

func (observer *serverSentEventObserver) ObserveBytes(chunk []byte) {
	for len(chunk) > 0 {
		if observer.skipLineFeed {
			observer.skipLineFeed = false
			if chunk[0] == '\n' {
				chunk = chunk[1:]
				continue
			}
		}
		end := bytes.IndexAny(chunk, "\r\n")
		if end < 0 {
			if !observer.discardLine {
				observer.currentLine = append(observer.currentLine, chunk...)
				if observer.skipEvent && bytes.HasPrefix(observer.currentLine, []byte("data:")) {
					observer.currentLine = observer.currentLine[:0]
					observer.discardLine = true
				}
			}
			return
		}
		if !observer.discardLine {
			line := chunk[:end]
			if len(observer.currentLine) > 0 {
				observer.currentLine = append(observer.currentLine, line...)
				line = observer.currentLine
			}
			observer.finishLine(line)
		}
		observer.currentLine = observer.currentLine[:0]
		observer.discardLine = false
		observer.skipLineFeed = chunk[end] == '\r'
		chunk = chunk[end+1:]
	}
}

func (observer *serverSentEventObserver) FinishObservation() {
	if !observer.discardLine {
		observer.finishLine(observer.currentLine)
	}
	observer.dispatchEvent()
}

func (observer *serverSentEventObserver) finishLine(line []byte) {
	if len(line) == 0 {
		observer.dispatchEvent()
		return
	}
	if eventName, found := bytes.CutPrefix(line, []byte("event:")); found {
		eventName = bytes.TrimPrefix(eventName, []byte(" "))
		observer.skipEvent = len(eventName) > 0 && observer.shouldObserveEvent != nil && !observer.shouldObserveEvent(eventName)
		return
	}
	if observer.skipEvent {
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
	if len(observer.currentData) > 0 && !observer.skipEvent {
		observer.onEvent(bytes.TrimSuffix(observer.currentData, []byte("\n")))
	}
	observer.currentData = observer.currentData[:0]
	observer.skipEvent = false
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
