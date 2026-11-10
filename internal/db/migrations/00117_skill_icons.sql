-- +goose Up
ALTER TABLE skills ADD COLUMN icon_key text NOT NULL DEFAULT 'blocks';
ALTER TABLE builtin_skills ADD COLUMN icon_key text NOT NULL DEFAULT 'blocks';
UPDATE builtin_skills SET icon_key = CASE external_id
    WHEN 'docx' THEN 'file-text' WHEN 'pdf' THEN 'file-text'
    WHEN 'typst-pdf' THEN 'book-open' WHEN 'imagegen' THEN 'image'
    WHEN 'image' THEN 'image' WHEN 'video-editing' THEN 'clapperboard'
    WHEN 'xlsx' THEN 'file-text' WHEN 'skill-authoring' THEN 'pen-line'
    ELSE 'blocks' END;


-- +goose Down
ALTER TABLE builtin_skills DROP COLUMN icon_key;
ALTER TABLE skills DROP COLUMN icon_key;
