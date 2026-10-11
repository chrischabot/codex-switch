package relay

import (
	"bytes"
	"compress/flate"
	"encoding/binary"
	"errors"
	"io"
	"strings"
	"sync"
)

var errCloseWebSocketAfterTerminalLimit = errors.New("close WebSocket after terminal provider limit event")

const (
	websocketContinuationOpcode = 0x0
	websocketTextOpcode         = 0x1
	websocketBinaryOpcode       = 0x2
	websocketMaximumDictionary  = 32 << 10
)

type observedWebSocketConnection struct {
	io.ReadWriteCloser
	frameObserver        websocketServerFrameObserver
	pendingFrames        bytes.Buffer
	readyFrames          *bytes.Reader
	closeOnce            sync.Once
	closeError           error
	negotiatedExtensions string
	clientFrameObserver  *websocketServerFrameObserver
	clientInput          []byte
	clientPendingFrames  bytes.Buffer
	reportReadError      func(error)
}

func createObservedWebSocketConnection(
	connection io.ReadWriteCloser,
	negotiatedExtensions string,
	onEvent func([]byte) bool,
) *observedWebSocketConnection {
	return &observedWebSocketConnection{
		ReadWriteCloser:      connection,
		negotiatedExtensions: negotiatedExtensions,
		frameObserver: websocketServerFrameObserver{
			onMessage:               onEvent,
			perMessageDeflate:       headerContainsExtension(negotiatedExtensions, "permessage-deflate"),
			serverNoContextTakeover: strings.Contains(strings.ToLower(negotiatedExtensions), "server_no_context_takeover"),
		},
	}
}

func (connection *observedWebSocketConnection) Read(destination []byte) (int, error) {
	if connection.readyFrames != nil && connection.readyFrames.Len() > 0 {
		return connection.readyFrames.Read(destination)
	}
	for {
		frame, err := readUpstreamWebSocketFrame(connection.ReadWriteCloser)
		if err != nil {
			if connection.reportReadError != nil {
				connection.reportReadError(err)
			}
			return 0, err
		}
		if frame[0]&0x08 != 0 {
			connection.readyFrames = bytes.NewReader(frame)
			return connection.readyFrames.Read(destination)
		}
		connection.pendingFrames.Write(frame)
		if err := connection.frameObserver.ObserveFrames(frame); err != nil && connection.reportReadError != nil {
			connection.reportReadError(err)
		}
		if connection.frameObserver.TerminalLimitObserved() {
			// The provider rejected this response. Withhold its terminal limit and
			// reconnect so Codex supplies continuation on the newly routed account.
			// Successful response messages have already passed through unchanged.
			return 0, errCloseWebSocketAfterTerminalLimit
		}
		if !connection.frameObserver.messageActive {
			connection.readyFrames = bytes.NewReader(connection.pendingFrames.Bytes())
			connection.pendingFrames = bytes.Buffer{}
			return connection.readyFrames.Read(destination)
		}
	}
}

func readUpstreamWebSocketFrame(source io.Reader) ([]byte, error) {
	var frame bytes.Buffer
	var header [2]byte
	if _, err := io.ReadFull(source, header[:]); err != nil {
		return nil, err
	}
	frame.Write(header[:])
	payloadLength := int64(header[1] & 0x7f)
	switch payloadLength {
	case 126:
		var length [2]byte
		if _, err := io.ReadFull(source, length[:]); err != nil {
			return nil, err
		}
		frame.Write(length[:])
		payloadLength = int64(binary.BigEndian.Uint16(length[:]))
	case 127:
		var length [8]byte
		if _, err := io.ReadFull(source, length[:]); err != nil {
			return nil, err
		}
		frame.Write(length[:])
		payloadLength = int64(binary.BigEndian.Uint64(length[:]))
		if payloadLength < 0 {
			return nil, errors.New("invalid WebSocket payload length")
		}
	}
	if header[1]&0x80 != 0 {
		payloadLength += 4
	}
	if _, err := io.CopyN(&frame, source, payloadLength); err != nil {
		return nil, err
	}
	return frame.Bytes(), nil
}

func (connection *observedWebSocketConnection) Close() error {
	connection.closeOnce.Do(func() {
		connection.closeError = connection.ReadWriteCloser.Close()
	})
	return connection.closeError
}

func (connection *observedWebSocketConnection) CloseWrite() error {
	if halfCloser, ok := connection.ReadWriteCloser.(interface{ CloseWrite() error }); ok {
		return halfCloser.CloseWrite()
	}
	return nil
}

func headerContainsExtension(headerValue, extensionName string) bool {
	for extension := range strings.SplitSeq(headerValue, ",") {
		name, _, _ := strings.Cut(strings.TrimSpace(extension), ";")
		if strings.EqualFold(strings.TrimSpace(name), extensionName) {
			return true
		}
	}
	return false
}

type websocketServerFrameObserver struct {
	headerBytes      [14]byte
	headerLength     int
	headerNeeded     int
	payloadRemaining uint64
	payloadMask      [4]byte
	payloadMaskIndex uint64
	frameOpcode      byte
	frameFinal       bool

	messageActive     bool
	messageCompressed bool
	messageBytes      []byte
	onMessage         func([]byte) bool
	terminalLimit     bool

	perMessageDeflate       bool
	serverNoContextTakeover bool
	compressionDictionary   []byte
}

func (observer *websocketServerFrameObserver) ObserveFrames(frameBytes []byte) error {
	for len(frameBytes) > 0 {
		if observer.headerNeeded == 0 && observer.payloadRemaining == 0 {
			observer.headerNeeded = 2
		}
		if observer.headerLength < observer.headerNeeded {
			copied := min(len(frameBytes), observer.headerNeeded-observer.headerLength)
			copy(observer.headerBytes[observer.headerLength:], frameBytes[:copied])
			observer.headerLength += copied
			frameBytes = frameBytes[copied:]
			if observer.headerLength < observer.headerNeeded {
				continue
			}
			if observer.headerNeeded == 2 {
				observer.headerNeeded += websocketExtendedHeaderLength(observer.headerBytes[1])
				if observer.headerLength < observer.headerNeeded {
					continue
				}
			}
			observer.beginFrame()
			if observer.payloadRemaining == 0 {
				if err := observer.finishFrame(); err != nil {
					return err
				}
				continue
			}
		}

		payloadLength := len(frameBytes)
		if uint64(payloadLength) > observer.payloadRemaining {
			payloadLength = int(observer.payloadRemaining)
		}
		observer.observeFramePayload(frameBytes[:payloadLength])
		frameBytes = frameBytes[payloadLength:]
		observer.payloadRemaining -= uint64(payloadLength)
		if observer.payloadRemaining == 0 {
			if err := observer.finishFrame(); err != nil {
				return err
			}
		}
	}
	return nil
}

func (observer *websocketServerFrameObserver) TerminalLimitObserved() bool {
	return observer.terminalLimit
}

func websocketExtendedHeaderLength(secondHeaderByte byte) int {
	extraLength := 0
	switch secondHeaderByte & 0x7f {
	case 126:
		extraLength = 2
	case 127:
		extraLength = 8
	}
	if secondHeaderByte&0x80 != 0 {
		extraLength += 4
	}
	return extraLength
}

func (observer *websocketServerFrameObserver) beginFrame() {
	observer.frameFinal = observer.headerBytes[0]&0x80 != 0
	observer.frameOpcode = observer.headerBytes[0] & 0x0f
	payloadLengthCode := observer.headerBytes[1] & 0x7f
	headerIndex := 2
	switch payloadLengthCode {
	case 126:
		observer.payloadRemaining = uint64(binary.BigEndian.Uint16(observer.headerBytes[headerIndex : headerIndex+2]))
		headerIndex += 2
	case 127:
		observer.payloadRemaining = binary.BigEndian.Uint64(observer.headerBytes[headerIndex : headerIndex+8])
		headerIndex += 8
	default:
		observer.payloadRemaining = uint64(payloadLengthCode)
	}
	if observer.headerBytes[1]&0x80 != 0 {
		copy(observer.payloadMask[:], observer.headerBytes[headerIndex:headerIndex+4])
	} else {
		observer.payloadMask = [4]byte{}
	}
	observer.payloadMaskIndex = 0
	if observer.frameOpcode == websocketTextOpcode || observer.frameOpcode == websocketBinaryOpcode {
		observer.messageActive = true
		observer.messageCompressed = observer.perMessageDeflate && observer.headerBytes[0]&0x40 != 0
		observer.messageBytes = observer.messageBytes[:0]
	}
}

func (observer *websocketServerFrameObserver) observeFramePayload(payload []byte) {
	if !observer.messageActive || observer.frameOpcode != websocketTextOpcode &&
		observer.frameOpcode != websocketBinaryOpcode && observer.frameOpcode != websocketContinuationOpcode {
		return
	}
	decodedPayload := payload
	if observer.headerBytes[1]&0x80 != 0 {
		decodedPayload = append([]byte(nil), payload...)
		for index := range decodedPayload {
			decodedPayload[index] ^= observer.payloadMask[observer.payloadMaskIndex%4]
			observer.payloadMaskIndex++
		}
	}
	observer.messageBytes = append(observer.messageBytes, decodedPayload...)
}

func (observer *websocketServerFrameObserver) finishFrame() error {
	defer func() {
		observer.headerLength = 0
		observer.headerNeeded = 0
		observer.payloadRemaining = 0
	}()
	if !observer.messageActive || !observer.frameFinal || observer.frameOpcode != websocketTextOpcode &&
		observer.frameOpcode != websocketBinaryOpcode && observer.frameOpcode != websocketContinuationOpcode {
		return nil
	}

	defer func() {
		observer.messageActive = false
		observer.messageCompressed = false
		observer.messageBytes = observer.messageBytes[:0]
	}()

	message := observer.messageBytes
	if observer.messageCompressed {
		decompressed, err := observer.decompressMessage(message)
		if err != nil {
			return err
		}
		message = decompressed
	}
	if observer.onMessage != nil && observer.onMessage(message) {
		observer.terminalLimit = true
	}
	return nil
}

func (observer *websocketServerFrameObserver) decompressMessage(compressed []byte) ([]byte, error) {
	compressedWithTail := make([]byte, 0, len(compressed)+4)
	compressedWithTail = append(compressedWithTail, compressed...)
	compressedWithTail = append(compressedWithTail, 0x00, 0x00, 0xff, 0xff)

	var decompressor io.ReadCloser
	if len(observer.compressionDictionary) > 0 {
		decompressor = flate.NewReaderDict(bytes.NewReader(compressedWithTail), observer.compressionDictionary)
	} else {
		decompressor = flate.NewReader(bytes.NewReader(compressedWithTail))
	}
	decompressed, err := io.ReadAll(decompressor)
	closeError := decompressor.Close()
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
		return nil, err
	}
	if closeError != nil && !errors.Is(closeError, io.ErrUnexpectedEOF) {
		return nil, closeError
	}
	if observer.serverNoContextTakeover {
		observer.compressionDictionary = observer.compressionDictionary[:0]
	} else {
		observer.compressionDictionary = append(observer.compressionDictionary, decompressed...)
		if len(observer.compressionDictionary) > websocketMaximumDictionary {
			lastWindow := len(observer.compressionDictionary) - websocketMaximumDictionary
			copy(observer.compressionDictionary, observer.compressionDictionary[lastWindow:])
			observer.compressionDictionary = observer.compressionDictionary[:websocketMaximumDictionary]
		}
	}
	return decompressed, nil
}

func (connection *observedWebSocketConnection) observeClientMessages(onMessage func([]byte) bool) {
	connection.clientFrameObserver = &websocketServerFrameObserver{
		onMessage:               onMessage,
		perMessageDeflate:       headerContainsExtension(connection.negotiatedExtensions, "permessage-deflate"),
		serverNoContextTakeover: strings.Contains(strings.ToLower(connection.negotiatedExtensions), "client_no_context_takeover"),
	}
}

func (connection *observedWebSocketConnection) Write(data []byte) (int, error) {
	if connection.clientFrameObserver == nil {
		return connection.ReadWriteCloser.Write(data)
	}
	connection.clientInput = append(connection.clientInput, data...)
	for len(connection.clientInput) > 0 {
		frameLength, err := completeWebSocketFrameLength(connection.clientInput)
		if err != nil {
			return 0, err
		}
		if frameLength == 0 {
			break
		}
		source := bytes.NewReader(connection.clientInput[:frameLength])
		frame, err := readUpstreamWebSocketFrame(source)
		if err != nil {
			return 0, err
		}
		connection.clientInput = connection.clientInput[frameLength:]
		// Control frames must pass even while a fragmented message is buffered.
		if frame[0]&0x08 != 0 {
			if err := writeAllWebSocketBytes(connection.ReadWriteCloser, frame); err != nil {
				return 0, err
			}
			continue
		}
		connection.clientPendingFrames.Write(frame)
		if err := connection.clientFrameObserver.ObserveFrames(frame); err != nil {
			return 0, err
		}
		if connection.clientFrameObserver.TerminalLimitObserved() {
			return 0, errCloseWebSocketAfterTerminalLimit
		}
		if !connection.clientFrameObserver.messageActive {
			if err := writeAllWebSocketBytes(connection.ReadWriteCloser, connection.clientPendingFrames.Bytes()); err != nil {
				return 0, err
			}
			connection.clientPendingFrames.Reset()
		}
	}
	return len(data), nil
}

func writeAllWebSocketBytes(destination io.Writer, data []byte) error {
	for len(data) > 0 {
		written, err := destination.Write(data)
		if err != nil {
			return err
		}
		if written == 0 {
			return io.ErrShortWrite
		}
		data = data[written:]
	}
	return nil
}

func completeWebSocketFrameLength(data []byte) (int, error) {
	if len(data) < 2 {
		return 0, nil
	}
	headerLength := 2 + websocketExtendedHeaderLength(data[1])
	if len(data) < headerLength {
		return 0, nil
	}
	payloadLength := uint64(data[1] & 0x7f)
	switch payloadLength {
	case 126:
		payloadLength = uint64(binary.BigEndian.Uint16(data[2:4]))
	case 127:
		payloadLength = binary.BigEndian.Uint64(data[2:10])
	}
	if payloadLength > uint64(int(^uint(0)>>1)-headerLength) {
		return 0, errors.New("invalid WebSocket payload length")
	}
	frameLength := headerLength + int(payloadLength)
	if len(data) < frameLength {
		return 0, nil
	}
	return frameLength, nil
}
