-- +goose Up
ALTER TABLE runtime_resource_bindings
    DROP CONSTRAINT IF EXISTS runtime_resource_bindings_resource_type_check;

ALTER TABLE runtime_resource_bindings
    ADD CONSTRAINT runtime_resource_bindings_resource_type_check
    CHECK (resource_type IN (
        'agent', 'environment', 'session', 'file', 'skill', 'vault', 'memory_store', 'deployment', 'deployment_run'
    ));

-- +goose Down
ALTER TABLE runtime_resource_bindings
    DROP CONSTRAINT IF EXISTS runtime_resource_bindings_resource_type_check;

ALTER TABLE runtime_resource_bindings
    ADD CONSTRAINT runtime_resource_bindings_resource_type_check
    CHECK (resource_type IN (
        'agent', 'environment', 'session', 'file', 'skill', 'vault', 'memory_store'
    ));
