package tunnels

import (
	"context"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

func commandHeaders(requestID string) nats.Header {
	return nats.Header{jetstream.MsgIDHeader: []string{brokerKey(requestID)}}
}

func commandMessageLimit(requestID string) int {
	// Size of a header-only message is exactly the HPUB header block size.
	return maxBrokerValueBytes - (&nats.Msg{Header: commandHeaders(requestID)}).Size()
}

func (b *Broker) encodeCommand(ctx context.Context, command queuedCommand) ([]byte, error) {
	if int64(len(command.JSONRPC)) > b.cfg.MaxBodyBytes {
		return nil, ErrPayloadLimit
	}
	limit := commandMessageLimit(command.RequestID)
	data, err := encodeTunnelJSON(command, 0)
	if err != nil || len(data) <= limit {
		return data, err
	}
	body := command.JSONRPC
	command.JSONRPC = nil
	// Reject oversized metadata before scheduling cleanup or uploading anything.
	if _, err := encodeTunnelJSON(command, limit); err != nil {
		return nil, err
	}
	ref, err := b.payloads.save(ctx, command.Scope, command.RequestID, command.ExpiresAt, body, b.cfg.MaxBodyBytes)
	if err != nil {
		return nil, err
	}
	command.PayloadRef = ref
	return encodeTunnelJSON(command, limit)
}

func (b *Broker) encodeResponse(ctx context.Context, record requestRecord, response TunnelResponse) ([]byte, error) {
	if int64(len(response.JSONResponse)) > b.cfg.MaxBodyBytes {
		return nil, ErrPayloadLimit
	}
	envelope := responseEnvelope{Key: brokerKey(response.RequestID), Response: &response}
	data, err := encodeTunnelJSON(envelope, 0)
	if err != nil || len(data) <= maxBrokerValueBytes {
		return data, err
	}
	if !b.now().Before(record.ExpiresAt) {
		// Only the origin can confirm an expired request's completion receipt.
		// Never upload an object after its request deadline, even for a duplicate.
		response.JSONResponse = nil
		envelope.ReceiptOnly = true
		return encodeTunnelJSON(envelope, maxBrokerValueBytes)
	}
	body := response.JSONResponse
	response.JSONResponse = nil
	envelope.Scope = record.Scope
	if _, err := encodeTunnelJSON(envelope, maxBrokerValueBytes); err != nil {
		return nil, err
	}
	envelope.PayloadRef, err = b.payloads.save(ctx, record.Scope, response.RequestID, record.ExpiresAt, body, b.cfg.MaxBodyBytes)
	if err != nil {
		return nil, err
	}
	return encodeTunnelJSON(envelope, maxBrokerValueBytes)
}

func (w *responseWaiter) restoreResponse(ctx context.Context, item bufferedResponse) (TunnelResponse, error) {
	response := item.response
	body, err := w.broker.payloads.restore(ctx, item.scope, response.RequestID, response.JSONResponse, item.payloadRef, w.broker.cfg.MaxBodyBytes)
	if err != nil {
		return TunnelResponse{}, err
	}
	response.JSONResponse = body
	return response, nil
}
