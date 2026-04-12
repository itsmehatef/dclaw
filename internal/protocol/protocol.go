package protocol

// Version is the current wire protocol version.
const Version = 1

// ComponentType identifies what kind of component is connecting.
type ComponentType string

const (
	ComponentMain       ComponentType = "main"
	ComponentDispatcher ComponentType = "dispatcher"
	ComponentWorker     ComponentType = "worker"
	ComponentChannel    ComponentType = "channel"
)

// Handshake is the first message sent on any dclaw socket connection.
type Handshake struct {
	ProtocolVersion  int           `json:"protocol_version"`
	ComponentType    ComponentType `json:"component_type"`
	ComponentVersion string        `json:"component_version"`
	ComponentID      string        `json:"component_id"`
}

// HandshakeResult is the response to a Handshake.
type HandshakeResult struct {
	Accepted          bool `json:"accepted"`
	NegotiatedVersion int  `json:"negotiated_version"`
}
