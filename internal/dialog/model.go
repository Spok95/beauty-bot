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

	// Поставки (админ/администратор)
	StateSupMenu      State = "sup_menu"
	StateSupPickWh    State = "sup_pick_wh"
	StateSupPickMat   State = "sup_pick_mat"
	StateSupQty       State = "sup_qty"
	StateSupUnitPrice State = "sup_unit_price"
	StateSupCart      State = "sup_cart" // НОВОЕ: корзина с позициями
	StateSupConfirm   State = "sup_confirm"

	// Расход/Аренда (мастер)
	StateConsPlace   State = "cons_place" // выбор: зал/кабинет
	StateConsQty     State = "cons_qty"   // кол-во часов/дней (int)
	StateConsMatPick State = "cons_mat_pick"
	StateConsMatQty  State = "cons_mat_qty" // int (г/шт)
	StateConsCart    State = "cons_cart"    // корзина материалов
	StateConsSummary State = "cons_summary" // сводка и итог

	// Абонементы (админ)
	StateAdmSubsMenu          State = "adm_subs_menu"
	StateAdmSubsPickUser      State = "adm_subs_pick_user"
	StateAdmSubsPickPlaceUnit State = "adm_subs_pick_place_unit"
	StateAdmSubsEnterQty      State = "adm_subs_enter_qty"
	StateAdmSubsConfirm       State = "adm_subs_confirm"

	// Админка тарифов аренды
	StateAdmRatesMenu    State = "adm:rates:menu"
	StateAdmRatesPickPU  State = "adm:rates:pick_pu"  // выбор место/единица
	StateAdmRatesPickSub State = "adm:rates:pick_sub" // тумблер абонемента
	StateAdmRatesList    State = "adm:rates:list"     // список ступеней

	// Создание ступени
	StateAdmRatesCreateMin       State = "adm:rates:create:min"
	StateAdmRatesCreateMax       State = "adm:rates:create:max"
	StateAdmRatesCreateThreshold State = "adm:rates:create:thr"
	StateAdmRatesCreatePriceWith State = "adm:rates:create:pwith"
	StateAdmRatesCreatePriceOwn  State = "adm:rates:create:pown"
	StateAdmRatesConfirm         State = "adm:rates:confirm"
)

type Payload map[string]any

type Item struct {
	ChatID  int64
	State   State
	Payload Payload
}
