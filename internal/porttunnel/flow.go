package porttunnel

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	CapabilitiesHeader         = "X-Tunnel-Capabilities"
	FlowControlCapability      = "flow-control-v1"
	FlowControlWindow          = 1 << 20
	FlowControlFrameOverhead   = 64
	mergedChunkLimit           = 32 << 10
	FlowControlledWriteTimeout = 30 * time.Second
	outboundQueueBytes         = 64 << 20
)

func frameCost(dataBytes int) int {
	return dataBytes + FlowControlFrameOverhead
}

func requestsFlowControl(header http.Header) bool {
	for _, value := range header.Values(CapabilitiesHeader) {
		for _, capability := range strings.Split(value, ",") {
			if strings.TrimSpace(capability) == FlowControlCapability {
				return true
			}
		}
	}
	return false
}

type outboundFrames struct {
	mu     sync.Mutex
	frames [][]byte
	bytes  int
	limit  int
	ready  chan struct{}
	space  chan struct{}
}

func newOutboundFrames(limit int) *outboundFrames {
	return &outboundFrames{limit: limit, ready: make(chan struct{}, 1), space: make(chan struct{}, 1)}
}

func (o *outboundFrames) push(ctx context.Context, closed <-chan struct{}, frame []byte) error {
	for {
		o.mu.Lock()
		if o.bytes == 0 || o.bytes+len(frame) <= o.limit {
			o.frames = append(o.frames, frame)
			o.bytes += len(frame)
			o.mu.Unlock()
			signal(o.ready)
			return nil
		}
		o.mu.Unlock()
		select {
		case <-o.space:
		case <-closed:
			return ErrTunnelDisconnected
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (o *outboundFrames) pushControl(closed <-chan struct{}, frame []byte) {
	select {
	case <-closed:
		return
	default:
	}
	o.mu.Lock()
	o.frames = append(o.frames, frame)
	o.bytes += len(frame)
	o.mu.Unlock()
	signal(o.ready)
}

func (o *outboundFrames) pop() ([]byte, bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if len(o.frames) == 0 {
		return nil, false
	}
	frame := o.frames[0]
	o.frames[0] = nil
	o.frames = o.frames[1:]
	o.bytes -= len(frame)
	signal(o.space)
	return frame, true
}

func (c *Conn) FlowControl() bool {
	return c.flowControl
}

func (c *Conn) sendWindowUpdate(streamID string, bytes int64) {
	c.enqueueControl(tunnelRequest{WindowUpdate: &windowUpdateMsg{StreamID: streamID, Bytes: uint32(bytes)}})
}

func (c *Conn) grantSendCredit(update *windowUpdateMsg) {
	c.mu.Lock()
	stream := c.wsStreams[update.StreamID]
	c.mu.Unlock()
	if stream != nil {
		stream.grantCredit(int64(update.Bytes))
	}
}
