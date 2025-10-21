-- +goose Up
CREATE TABLE IF NOT EXISTS dialog_states(
                                            chat_id BIGINT PRIMARY KEY,
                                            state TEXT NOT NULL,
                                            payload JSONB NOT NULL DEFAULT '{}'::jsonb,
                                            updated_at TIMESTAMPTZ DEFAULT now()
    );

-- +goose Down
DROP TABLE IF EXISTS dialog_states;
