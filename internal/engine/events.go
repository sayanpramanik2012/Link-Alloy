package engine

import "time"

type EngineEvent struct {
	FlowID        uint64    `json:"flowId,omitempty"`
	At            time.Time `json:"at"`
	Level         string    `json:"level"`
	Category      string    `json:"category"`
	Message       string    `json:"message"`
	LinkIndex     uint32    `json:"linkIndex,omitempty"`
	BytesSent     uint64    `json:"bytesSent,omitempty"`
	BytesReceived uint64    `json:"bytesReceived,omitempty"`
}

type EventSink func(EngineEvent)
