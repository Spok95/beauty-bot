package adminchat

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Repo struct {
	pool *pgxpool.Pool
}

func NewRepo(pool *pgxpool.Pool) *Repo {
	return &Repo{pool: pool}
}

func (r *Repo) Create(ctx context.Context, in CreateMessageInput) (*Message, error) {
	var senderUserID any
	if in.SenderUserID > 0 {
		senderUserID = in.SenderUserID
	}

	row := r.pool.QueryRow(ctx, `
		INSERT INTO admin_chat_messages (
			sender_user_id,
			sender_telegram_id,
			sender_username,
			sender_role,
			message_type,
			text,
			caption,
			telegram_file_id,
			telegram_file_unique_id,
			file_name,
			mime_type,
			file_size,
			telegram_message_id,
			telegram_media_group_id
		)
		VALUES (
			$1, $2, $3, $4,
			$5, $6, $7,
			$8, $9, $10, $11, $12,
			$13, $14
		)
		RETURNING
			id,
			COALESCE(sender_user_id, 0),
			sender_telegram_id,
			sender_username,
			sender_role,
			message_type,
			text,
			caption,
			telegram_file_id,
			telegram_file_unique_id,
			file_name,
			mime_type,
			file_size,
			telegram_message_id,
			telegram_media_group_id,
			created_at
	`,
		senderUserID,
		in.SenderTelegramID,
		in.SenderUsername,
		in.SenderRole,
		in.MessageType,
		in.Text,
		in.Caption,
		in.TelegramFileID,
		in.TelegramFileUniqueID,
		in.FileName,
		in.MimeType,
		in.FileSize,
		in.TelegramMessageID,
		in.TelegramMediaGroupID,
	)

	var m Message
	if err := row.Scan(
		&m.ID,
		&m.SenderUserID,
		&m.SenderTelegramID,
		&m.SenderUsername,
		&m.SenderRole,
		&m.MessageType,
		&m.Text,
		&m.Caption,
		&m.TelegramFileID,
		&m.TelegramFileUniqueID,
		&m.FileName,
		&m.MimeType,
		&m.FileSize,
		&m.TelegramMessageID,
		&m.TelegramMediaGroupID,
		&m.CreatedAt,
	); err != nil {
		return nil, err
	}

	return &m, nil
}

func (r *Repo) Last(ctx context.Context, limit, offset int) ([]Message, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT
			id,
			COALESCE(sender_user_id, 0),
			sender_telegram_id,
			sender_username,
			sender_role,
			message_type,
			text,
			caption,
			telegram_file_id,
			telegram_file_unique_id,
			file_name,
			mime_type,
			file_size,
			telegram_message_id,
			telegram_media_group_id,
			created_at
		FROM admin_chat_messages
		ORDER BY id DESC
		LIMIT $1 OFFSET $2
	`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Message

	for rows.Next() {
		var m Message

		if err := rows.Scan(
			&m.ID,
			&m.SenderUserID,
			&m.SenderTelegramID,
			&m.SenderUsername,
			&m.SenderRole,
			&m.MessageType,
			&m.Text,
			&m.Caption,
			&m.TelegramFileID,
			&m.TelegramFileUniqueID,
			&m.FileName,
			&m.MimeType,
			&m.FileSize,
			&m.TelegramMessageID,
			&m.TelegramMediaGroupID,
			&m.CreatedAt,
		); err != nil {
			return nil, err
		}

		out = append(out, m)
	}

	return out, rows.Err()
}
