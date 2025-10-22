package dialog

type State string

const (
	StateIdle         State = "idle"
	StateAwaitFIO     State = "await_fio"
	StateAwaitRole    State = "await_role"
	StateAwaitConfirm State = "await_confirm"
)

type Payload map[string]any

type Item struct {
	ChatID  int64
	State   State
	Payload Payload
}
