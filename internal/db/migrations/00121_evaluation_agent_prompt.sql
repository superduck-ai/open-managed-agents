-- +goose Up

ALTER TABLE evaluation_evaluator_versions DROP CONSTRAINT evaluation_evaluator_versions_shape;
ALTER TABLE evaluation_evaluator_versions ADD CONSTRAINT evaluation_evaluator_versions_shape CHECK (
	(
		evaluator_type = 'llm'
		AND char_length(model) BETWEEN 1 AND 255
		AND agent_id = ''
		AND char_length(prompt_template) BETWEEN 1 AND 16000
	)
	OR (
		evaluator_type = 'agent'
		AND char_length(agent_id) BETWEEN 1 AND 255
		AND model = ''
		AND char_length(prompt_template) <= 16000
	)
);

-- +goose Down

ALTER TABLE evaluation_evaluator_versions DROP CONSTRAINT evaluation_evaluator_versions_shape;
ALTER TABLE evaluation_evaluator_versions ADD CONSTRAINT evaluation_evaluator_versions_shape CHECK (
	(
		evaluator_type = 'llm'
		AND char_length(model) BETWEEN 1 AND 255
		AND agent_id = ''
		AND char_length(prompt_template) BETWEEN 1 AND 16000
	)
	OR (
		evaluator_type = 'agent'
		AND char_length(agent_id) BETWEEN 1 AND 255
		AND model = ''
		AND prompt_template = ''
	)
);
