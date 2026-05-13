-- +goose Up

CREATE TABLE IF NOT EXISTS admin_chat_messages (
                                                   id BIGSERIAL PRIMARY KEY,

                                                   sender_user_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
    sender_telegram_id BIGINT NOT NULL,
    sender_username TEXT NOT NULL DEFAULT '',
    sender_role TEXT NOT NULL DEFAULT '',

    message_type TEXT NOT NULL,
    text TEXT NOT NULL DEFAULT '',
    caption TEXT NOT NULL DEFAULT '',

    telegram_file_id TEXT NOT NULL DEFAULT '',
    telegram_file_unique_id TEXT NOT NULL DEFAULT '',
    file_name TEXT NOT NULL DEFAULT '',
    mime_type TEXT NOT NULL DEFAULT '',
    file_size BIGINT NOT NULL DEFAULT 0,

    telegram_message_id BIGINT NOT NULL DEFAULT 0,
    telegram_media_group_id TEXT NOT NULL DEFAULT '',

    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
    );

CREATE INDEX IF NOT EXISTS idx_admin_chat_messages_created_at
    ON admin_chat_messages(created_at DESC);

CREATE INDEX IF NOT EXISTS idx_admin_chat_messages_sender_user_id
    ON admin_chat_messages(sender_user_id);

CREATE INDEX IF NOT EXISTS idx_admin_chat_messages_message_type
    ON admin_chat_messages(message_type);

-- +goose Down

DROP TABLE IF EXISTS admin_chat_messages;