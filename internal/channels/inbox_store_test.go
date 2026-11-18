package channels

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"slices"
	"time"
	"uuid"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func inputStoreKey(scope db.ChannelInputScope, messageID string) string {
	return fmt.Sprintf("%v/%s", scope, messageID)
}

func (f *fakeRuntimeStore) ReceiveChannelInput(_ context.Context, scope db.ChannelInputScope, input db.ChannelInput) (db.ChannelInput, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.receiveInputErr != nil {
		return db.ChannelInput{}, false, f.receiveInputErr
	}
	if f.inputs == nil {
		f.inputs = make(map[string]db.ChannelInput)
		f.inputKeys = make(map[string]string)
	}
	key := inputStoreKey(scope, input.MessageID)
	if f.inputKeys[key] != "" {
		return db.ChannelInput{}, false, nil
	}
	input.UUID = uuid.NewV4().String()
	input.State = "received"
	f.inputs[input.UUID] = input
	f.inputKeys[key] = input.UUID
	f.inputOrder = append(f.inputOrder, input.UUID)
	return input, true, nil
}

func (f *fakeRuntimeStore) ClaimChannelInput(_ context.Context, scope db.ChannelInputScope, inputUUID, owner string) (db.ChannelInput, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	row, found := f.inputs[inputUUID]
	if !found || f.inputKeys[inputStoreKey(scope, row.MessageID)] != inputUUID || row.LeaseUntil.After(time.Now()) || !slices.Contains([]string{"received", "buffering", "prepared", "accepted"}, row.State) {
		return db.ChannelInput{}, false, nil
	}
	if row.State == "received" {
		row.State = "buffering"
	}
	row.LeaseOwner = owner
	row.LeaseUntil = time.Now().Add(30 * time.Second)
	f.inputs[inputUUID] = row
	return row, true, nil
}

func (f *fakeRuntimeStore) HoldChannelInputForPairing(_ context.Context, scope db.ChannelInputScope, id, owner, pairing string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	row := f.inputs[id]
	if f.inputKeys[inputStoreKey(scope, row.MessageID)] != id || row.State != "buffering" || row.LeaseOwner != owner {
		return db.ErrInvalidState
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(row.Payload), &payload); err != nil {
		return err
	}
	payload["pairing_id"] = pairing
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	row.State, row.LeaseOwner, row.Payload = "pairing_wait", "", string(raw)
	f.inputs[id] = row
	return nil
}

func (f *fakeRuntimeStore) ResumeChannelInputAfterPairing(_ context.Context, scope db.ChannelInputScope, input db.ChannelPairingResume) (string, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	id := f.inputKeys[inputStoreKey(scope, input.MessageID)]
	row := f.inputs[id]
	if row.State != "pairing_wait" || row.ScopeKey != input.ScopeKey {
		return "", false, nil
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(row.Payload), &payload); err != nil {
		return "", false, err
	}
	if payload["user_id"] != input.ActorID || payload["pairing_id"] != input.PairingID {
		return "", false, nil
	}
	row.State, row.LeaseUntil = "received", time.Now()
	f.inputs[id] = row
	return id, true, nil
}

func (f *fakeRuntimeStore) ListRecoverableChannelInputs(_ context.Context, scope db.ChannelInputScope) ([]db.ChannelInput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var rows []db.ChannelInput
	for _, id := range f.inputOrder {
		row := f.inputs[id]
		if f.inputKeys[inputStoreKey(scope, row.MessageID)] == row.UUID && !row.LeaseUntil.After(time.Now()) && slices.Contains([]string{"received", "buffering", "prepared", "accepted"}, row.State) {
			rows = append(rows, row)
		}
	}
	return rows, nil
}

func (f *fakeRuntimeStore) PrepareChannelInputBatch(_ context.Context, scope db.ChannelInputScope, owner string, batch db.ChannelInputBatch) (db.ChannelInput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var rows []db.ChannelInput
	for _, id := range batch.MessageIDs {
		row, found := f.inputs[f.inputKeys[inputStoreKey(scope, id)]]
		if !found || row.State != "buffering" || row.ScopeKey != batch.ScopeKey || row.LeaseOwner != owner || !row.LeaseUntil.After(time.Now()) {
			return db.ChannelInput{}, db.ErrInvalidState
		}
		rows = append(rows, row)
	}
	for i, row := range rows {
		if i != len(rows)-1 {
			row.State = "grouped"
		} else {
			row.State = "prepared"
			row.Payload = batch.Payload
		}
		f.inputs[row.UUID] = row
		rows[i] = row
	}
	return rows[len(rows)-1], nil
}

func (f *fakeRuntimeStore) AdvanceChannelInput(_ context.Context, scope db.ChannelInputScope, inputUUID, owner string, change db.ChannelInputAdvance) (db.ChannelInput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	row := f.inputs[inputUUID]
	if row.LeaseOwner != owner || !row.LeaseUntil.After(time.Now()) || !slices.Contains([]string{"prepared", "accepted"}, row.State) {
		return db.ChannelInput{}, db.ErrInvalidState
	}
	row.State = change.State
	row.SessionExternalID = change.SessionExternalID
	row.PreparedContent = change.PreparedContent
	row.InputEventID = change.InputEventID
	if change.State == "accepted" {
		var msg channelMessage
		if err := json.Unmarshal([]byte(row.Payload), &msg); err != nil {
			return db.ChannelInput{}, err
		}
		for id, source := range f.inputs {
			if f.inputKeys[inputStoreKey(scope, source.MessageID)] != id || !slices.Contains(msg.MessageIDs, source.MessageID) {
				continue
			}
			payload, err := stripInputBody(source.Payload)
			if err != nil {
				return db.ChannelInput{}, err
			}
			source.Payload = payload
			f.inputs[id] = source
		}
		var err error
		row.Payload, err = stripInputBody(row.Payload)
		if err != nil {
			return db.ChannelInput{}, err
		}
	}
	f.inputs[inputUUID] = row
	return row, nil
}

func (f *fakeRuntimeStore) CompleteChannelInput(_ context.Context, scope db.ChannelInputScope, inputUUID, owner string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	row := f.inputs[inputUUID]
	if row.LeaseOwner != owner || !row.LeaseUntil.After(time.Now()) {
		return db.ErrInvalidState
	}
	var msg channelMessage
	if err := json.Unmarshal([]byte(row.Payload), &msg); err != nil {
		return err
	}
	for id, source := range f.inputs {
		if f.inputKeys[inputStoreKey(scope, source.MessageID)] != id {
			continue
		}
		if id != inputUUID && !slices.Contains(msg.MessageIDs, source.MessageID) {
			continue
		}
		payload, err := stripInputBody(source.Payload)
		if err != nil {
			return err
		}
		source.Payload = payload
		source.PreparedContent = ""
		source.State = "handed_off"
		source.LeaseOwner = ""
		f.inputs[id] = source
	}
	return nil
}

func stripInputBody(payload string) (string, error) {
	var body map[string]jsontext.Value
	if err := json.Unmarshal([]byte(payload), &body); err != nil {
		return "", err
	}
	delete(body, "content")
	delete(body, "resources")
	encoded, err := json.Marshal(body)
	return string(encoded), err
}

func (f *fakeRuntimeStore) ReleaseChannelInput(_ context.Context, _ db.ChannelInputScope, inputUUID, owner string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	row := f.inputs[inputUUID]
	if row.LeaseOwner == owner && row.State != "handed_off" {
		row.LeaseOwner = ""
		row.LeaseUntil = time.Now().Add(30 * time.Second)
		f.inputs[inputUUID] = row
	}
	return nil
}

func (f *fakeRuntimeStore) RenewChannelInputs(_ context.Context, _ db.ChannelInputScope, owner string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for id, row := range f.inputs {
		if row.LeaseOwner == owner && row.LeaseUntil.After(time.Now()) && slices.Contains([]string{"prepared", "accepted", "command_sending"}, row.State) {
			row.LeaseUntil = time.Now().Add(30 * time.Second)
			f.inputs[id] = row
		}
	}
	return nil
}
