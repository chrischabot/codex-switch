package relay

import "bytes"

func (observer *terminalResponseObserver) shouldObserveStreamEvent(eventType []byte) bool {
	if observer.claudeUsage != nil {
		switch string(eventType) {
		case "message_start", "message_delta", "message_stop", "message", "error":
			return true
		}
		return false
	}
	switch string(eventType) {
	case "response.created", "response.completed", "response.failed", "response.incomplete", "error":
		return true
	}
	return false
}

func (observer *terminalResponseObserver) observeWebSocketServerEvent(payload []byte) bool {
	if eventType, found := leadingStreamEventType(payload); found && !observer.shouldObserveStreamEvent(eventType) {
		return false
	}
	return observer.observeStructuredEvent(payload)
}

// WebSocket provider events put type first. Unfamiliar JSON layouts still use
// the existing decoder; content bodies never need to be decoded here.
func leadingStreamEventType(payload []byte) ([]byte, bool) {
	payload = bytes.TrimLeft(payload, " \t\r\n")
	if len(payload) == 0 || payload[0] != '{' {
		return nil, false
	}
	payload = bytes.TrimLeft(payload[1:], " \t\r\n")
	payload, found := bytes.CutPrefix(payload, []byte(`"type"`))
	if !found {
		return nil, false
	}
	payload = bytes.TrimLeft(payload, " \t\r\n")
	if len(payload) == 0 || payload[0] != ':' {
		return nil, false
	}
	payload = bytes.TrimLeft(payload[1:], " \t\r\n")
	if len(payload) == 0 || payload[0] != '"' {
		return nil, false
	}
	payload = payload[1:]
	end := bytes.IndexByte(payload, '"')
	if end < 0 || bytes.IndexByte(payload[:end], '\\') >= 0 {
		return nil, false
	}
	eventType := payload[:end]
	rest := bytes.TrimLeft(payload[end+1:], " \t\r\n")
	if len(rest) == 0 || rest[0] != ',' && rest[0] != '}' {
		return nil, false
	}
	return eventType, true
}
