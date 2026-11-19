-- +goose Up
ALTER TABLE channel_inputs DROP CONSTRAINT channel_input_state;
ALTER TABLE channel_inputs ADD CONSTRAINT channel_input_state
    CHECK (state IN ('received','buffering','prepared','accepted','grouped','handed_off','command_sending','pairing_wait'));

-- +goose Down
UPDATE channel_inputs SET state='handed_off',payload=payload-'content'-'resources'-'pairing_id'
    WHERE state='pairing_wait';
ALTER TABLE channel_inputs DROP CONSTRAINT channel_input_state;
ALTER TABLE channel_inputs ADD CONSTRAINT channel_input_state
    CHECK (state IN ('received','buffering','prepared','accepted','grouped','handed_off','command_sending'));
