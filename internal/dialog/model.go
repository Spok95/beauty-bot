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

	// Материалы
	StateAdmMatMenu    State = "adm_mat_menu" // уже есть
	StateAdmMatList    State = "adm_mat_list" // НОВОЕ: экран "Список материалов"
	StateAdmMatItem    State = "adm_mat_item" // НОВОЕ: карточка конкретного материала
	StateAdmMatPickCat State = "adm_mat_pick_cat"
	StateAdmMatName    State = "adm_mat_name"
	StateAdmMatUnit    State = "adm_mat_unit"
	StateAdmMatRename  State = "adm_mat_rename"

	// Остатки/движения
	StateStockPickWh State = "stock_pick_wh"
	StateStockList   State = "stock_list"    // список материалов с остатком в выбранном складе
	StateStockItem   State = "stock_item"    // карточка материала (остаток + действия)
	StateStockInQty  State = "stock_in_qty"  // ввод количества для прихода
	StateStockOutQty State = "stock_out_qty" // ввод количества для списания
)

type Payload map[string]any

type Item struct {
	ChatID  int64
	State   State
	Payload Payload
}
