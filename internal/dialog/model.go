package dialog

type State string

const (
	// Регистрация
	StateIdle         State = "idle"
	StateAwaitFIO     State = "await_fio"
	StateAwaitRole    State = "await_role"
	StateAwaitConfirm State = "await_confirm"

	// Админ-меню
	StateAdmMenu State = "adm_menu"

	// Склады
	StateAdmWhMenu   State = "adm_wh_menu"
	StateAdmWhName   State = "adm_wh_name"   // ввод названия при создании
	StateAdmWhType   State = "adm_wh_type"   // выбор типа при создании
	StateAdmWhRename State = "adm_wh_rename" // ввод нового имени выбранного склада

	// Категории
	StateAdmCatMenu   State = "adm_cat_menu"
	StateAdmCatName   State = "adm_cat_name"   // ввод названия при создании
	StateAdmCatRename State = "adm_cat_rename" // ввод нового имени выбранной категории
)

type Payload map[string]any

type Item struct {
	ChatID  int64
	State   State
	Payload Payload
}
