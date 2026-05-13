-- +goose Up

ALTER TABLE admin_chat_messages
    ADD COLUMN IF NOT EXISTS reply_to_message_id BIGINT
    REFERENCES admin_chat_messages(id)
    ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS idx_admin_chat_messages_reply_to
    ON admin_chat_messages(reply_to_message_id);

-- +goose Down

DROP INDEX IF EXISTS idx_admin_chat_messages_reply_to;

ALTER TABLE admin_chat_messages
DROP COLUMN IF EXISTS reply_to_message_id;