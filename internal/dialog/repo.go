package dialog

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Repo struct {
	pool *pgxpool.Pool
}

func NewRepo(pool *pgxpool.Pool) *Repo { return &Repo{pool: pool} }

func (r *Repo) Get(ctx context.Context, chatID int64) (*Item, error) {
	row := r.pool.QueryRow(ctx, `SELECT state, payload FROM dialog_states WHERE chat_id = $1`, chatID)
	var state string
	var raw []byte
	if err := row.Scan(&state, &raw); err != nil {
		// если строки нет — считаем, что состояния пока нет
		return &Item{ChatID: chatID, State: StateIdle, Payload: Payload{}}, nil
	}
	var p Payload
	_ = json.Unmarshal(raw, &p)
	return &Item{ChatID: chatID, State: State(state), Payload: p}, nil
}

func (r *Repo) Set(ctx context.Context, chatID int64, state State, payload Payload) error {
	raw, _ := json.Marshal(payload)
	_, err := r.pool.Exec(ctx, `
		INSERT INTO dialog_states (chat_id, state, payload, updated_at)
		VALUES ($1,$2,$3,now())
		ON CONFLICT (chat_id) DO UPDATE SET
		  state=$2, payload=$3, updated_at=now()
	`, chatID, string(state), raw)
	return err
}

func (r *Repo) Reset(ctx context.Context, chatID int64) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM dialog_states WHERE chat_id = $1`, chatID)
	return err
}

// GetString Helper для безопасного чтения строк из payload
func GetString(p Payload, key string) (string, bool) {
	v, ok := p[key]
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	return s, ok
}
