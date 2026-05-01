package bot

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/Spok95/beauty-bot/internal/dialog"
	"github.com/Spok95/beauty-bot/internal/domain/catalog"
	"github.com/Spok95/beauty-bot/internal/domain/consumption"
	"github.com/Spok95/beauty-bot/internal/domain/materials"
	subsdomain "github.com/Spok95/beauty-bot/internal/domain/subscriptions"
	"github.com/Spok95/beauty-bot/internal/domain/users"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func (b *Bot) handleCommand(ctx context.Context, msg *tgbotapi.Message) {
	chatID := msg.Chat.ID
	tgID := msg.From.ID
	switch msg.Command() {
	case "start":
		// не затираем роль, если пользователь уже существует
		existing, _ := b.users.GetByTelegramID(ctx, tgID)

		defaultRole := users.RoleMaster
		if existing != nil && existing.Role != "" {
			defaultRole = existing.Role
		}

		u, err := b.users.UpsertByTelegram(ctx, tgID, defaultRole)
		if err != nil {
			b.send(tgbotapi.NewMessage(chatID, "Ошибка: не удалось сохранить профиль"))
			return
		}
		// авто-админ
		if b.isAdminID(msg.From.ID) && (u.Role != users.RoleAdmin || u.Status != users.StatusApproved) {
			if _, err2 := b.users.Approve(ctx, msg.From.ID, users.RoleAdmin); err2 == nil {
				m := tgbotapi.NewMessage(chatID, "Привет, админ! Для управления ботом, ты можешь воспользоваться меню с кнопками и работать через них.")
				m.ReplyMarkup = adminReplyKeyboard()
				b.send(m)
				return
			}
		}

		if u.Role == users.RoleAdmin && u.Status == users.StatusApproved {
			m := tgbotapi.NewMessage(chatID, "Привет, админ! Для управления ботом, ты можешь воспользоваться меню с кнопками и работать через них.")
			m.ReplyMarkup = adminReplyKeyboard()
			b.send(m)
			return
		}

		if u.Role == users.RoleMaster && u.Status == users.StatusApproved {
			m := tgbotapi.NewMessage(chatID, "Готово! Для учёта материалов и аренды жми «Расход/Аренда».")
			m.ReplyMarkup = masterReplyKeyboard()
			b.send(m)
			return
		}

		if u.Role == users.RoleAdministrator && u.Status == users.StatusApproved {
			m := tgbotapi.NewMessage(chatID,
				"Готово! Для работы со складом «Клиентский» используйте кнопки снизу.")
			m.ReplyMarkup = salonAdminReplyKeyboard()
			b.send(m)
			return
		}

		switch u.Status {
		case users.StatusApproved:
			b.send(tgbotapi.NewMessage(chatID, "Вы уже подтверждены."))
		case users.StatusRejected:
			_ = b.states.Set(ctx, chatID, dialog.StateAwaitFIO, dialog.Payload{})
			b.askFIO(chatID)
		default:
			_ = b.states.Set(ctx, chatID, dialog.StateAwaitFIO, dialog.Payload{})
			b.askFIO(chatID)
		}
		return

	case "help":
		b.send(tgbotapi.NewMessage(chatID,
			"Команды:\n/start — начать регистрацию/работу\n/help — помощь"))
		return

	case "admin":
		// Только для admin — показываем техсообщение без меню
		u, _ := b.users.GetByTelegramID(ctx, tgID)
		if u == nil || u.Role != users.RoleAdmin || u.Status != users.StatusApproved {
			b.send(tgbotapi.NewMessage(chatID, "Доступ запрещён"))
			return
		}
		b.send(tgbotapi.NewMessage(chatID,
			"Раздел администрирования временно выключен. Настройка тарифов будет доступна через кнопку «Установка тарифов»."))
		return

	case "rent":
		u, _ := b.users.GetByTelegramID(ctx, tgID)
		if u == nil || u.Status != users.StatusApproved || u.Role != users.RoleMaster {
			b.send(tgbotapi.NewMessage(chatID, "Доступ запрещён."))
			return
		}
		_ = b.states.Set(ctx, chatID, dialog.StateConsComment, dialog.Payload{})

		kb := tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("Пропустить", "cons:comment_skip"),
			),
			navKeyboard(false, true).InlineKeyboard[0],
		)
		m := tgbotapi.NewMessage(chatID,
			"Введите дату за которую подаете данные или если дата совпадает, то нажмите «Пропустить».")
		m.ReplyMarkup = kb
		b.send(m)
		return

	default:
		b.send(tgbotapi.NewMessage(chatID, "Не знаю такую команду. Наберите /help"))
		return
	}
}

func (b *Bot) handleStateMessage(ctx context.Context, msg *tgbotapi.Message) {
	chatID := msg.Chat.ID
	tgID := msg.From.ID
	// Диалоги (текстовые вводы)
	st, _ := b.states.Get(ctx, chatID)

	// Нижняя панель мастера
	if msg.Text == "Расход/Аренда" {
		u, _ := b.users.GetByTelegramID(ctx, tgID)
		if u == nil || u.Status != users.StatusApproved || u.Role != users.RoleMaster {
			return
		}
		_ = b.states.Set(ctx, chatID, dialog.StateConsComment, dialog.Payload{})

		kb := tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("Пропустить", "cons:comment_skip"),
			),
			navKeyboard(false, true).InlineKeyboard[0],
		)
		m := tgbotapi.NewMessage(chatID,
			"Введите дату за которую подаете данные или если дата совпадает, то нажмите «Пропустить».")
		m.ReplyMarkup = kb
		b.send(m)
		return
	}

	if msg.Text == "Просмотр остатков" {
		u, _ := b.users.GetByTelegramID(ctx, tgID)
		if u == nil || u.Status != users.StatusApproved || u.Role != users.RoleMaster {
			return
		}

		// Сразу работаем со складом «Расходники»
		_, err := b.getConsumablesWarehouseID(ctx)
		if err != nil {
			b.send(tgbotapi.NewMessage(chatID, "Склад «Расходники» не найден. Обратитесь к администратору."))
			return
		}

		// склад запоминать в стейте не обязательно, но можно – пока не нужен

		kb := tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("По названию", "mstock:byname"),
				tgbotapi.NewInlineKeyboardButtonData("По категории", "mstock:bycat"),
			),
			navKeyboard(false, true).InlineKeyboard[0],
		)

		m := tgbotapi.NewMessage(chatID, "Просмотр остатков (склад: Расходники) — выберите способ поиска:")
		m.ReplyMarkup = kb
		b.send(m)
		return
	}

	if msg.Text == "Мои абонементы" {
		u, _ := b.users.GetByTelegramID(ctx, tgID)
		if u == nil || u.Status != users.StatusApproved || u.Role != users.RoleMaster {
			return
		}
		month := time.Now().Format("2006-01")
		list, err := b.subs.ListByUserMonth(ctx, u.ID, month)
		if err != nil || len(list) == 0 {
			b.send(tgbotapi.NewMessage(chatID, "На текущий месяц абонементов нет."))
			return
		}
		var sb strings.Builder
		sb.WriteString("Мои абонементы (текущий месяц):\n")
		placeRU := map[string]string{"hall": "Зал", "cabinet": "Кабинет"}
		unitRU := map[string]string{"hour": "ч", "day": "дн"}
		for _, s := range list {
			left := s.TotalQty - s.UsedQty
			if left < 0 {
				left = 0
			}
			sb.WriteString(fmt.Sprintf("— %s, %s: %d/%d (остаток %d)\n",
				placeRU[s.Place], unitRU[s.Unit], s.UsedQty, s.TotalQty, left))
		}
		b.send(tgbotapi.NewMessage(chatID, sb.String()))
		return
	}

	if msg.Text == "Купить абонемент" {
		u, _ := b.users.GetByTelegramID(ctx, tgID)
		if u == nil || u.Status != users.StatusApproved || u.Role != users.RoleMaster {
			return
		}
		_ = b.states.Set(ctx, chatID, dialog.StateSubBuyPlace, dialog.Payload{})
		m := tgbotapi.NewMessage(chatID, "Выберите тип абонемента:")
		m.ReplyMarkup = b.subBuyPlaceKeyboard()
		b.send(m)
		return
	}

	// Чат с админом — доступен мастеру и администратору
	if msg.Text == "Чат с админом" {
		u, _ := b.users.GetByTelegramID(ctx, tgID)
		if u == nil || u.Status != users.StatusApproved {
			return
		}
		if u.Role != users.RoleMaster && u.Role != users.RoleAdmin && u.Role != users.RoleAdministrator {
			return
		}

		_ = b.states.Set(ctx, chatID, dialog.StateChatAdmin, dialog.Payload{})
		m := tgbotapi.NewMessage(chatID,
			"Отправьте сообщение или файл для админов одним сообщением. Оно будет переслано в админский чат.")
		b.send(m)
		return
	}

	// "Список команд" — доступно всем подтверждённым
	if msg.Text == "Список команд" {
		u, _ := b.users.GetByTelegramID(ctx, tgID)
		if u == nil || u.Status != users.StatusApproved {
			b.send(tgbotapi.NewMessage(chatID, "Сначала пройдите регистрацию: /start"))
			return
		}
		if u.Role == users.RoleMaster {
			b.send(tgbotapi.NewMessage(chatID, "Команды мастера:\n/rent — расход/аренда\n/help — помощь"))
		} else if u.Role == users.RoleAdmin {
			b.send(tgbotapi.NewMessage(chatID, "Команды админа:\n/admin — админ-меню\n/help — помощь"))
		} else {
			b.send(tgbotapi.NewMessage(chatID, "Команды:\n/help — помощь"))
		}
		return
	}

	// Кнопки нижней панели для админа
	if msg.Text == "Склады" || msg.Text == "Категории" || msg.Text == "Материалы" ||
		msg.Text == "Инвентаризация" || msg.Text == "Поставки" || msg.Text == "Абонементы" ||
		msg.Text == "Установка цен" || msg.Text == "Аренда и Расходы материалов по мастерам" ||
		msg.Text == "Оповещение всем" {
		u, _ := b.users.GetByTelegramID(ctx, tgID)
		if u == nil || u.Status != users.StatusApproved {
			// игнорируем для не-админов
			return
		}
		switch msg.Text {
		case "Склады":
			if u.Role != users.RoleAdmin {
				return
			}
			_ = b.states.Set(ctx, chatID, dialog.StateAdmWhMenu, dialog.Payload{})
			b.showWarehouseMenu(chatID, nil)
		case "Категории":
			if u.Role != users.RoleAdmin && u.Role != users.RoleAdministrator {
				return
			}
			_ = b.states.Set(ctx, chatID, dialog.StateAdmCatMenu, dialog.Payload{})
			b.showCategoryMenu(chatID, nil)
		case "Материалы":
			if u.Role != users.RoleAdmin && u.Role != users.RoleAdministrator {
				return
			}
			_ = b.states.Set(ctx, chatID, dialog.StateAdmMatMenu, dialog.Payload{})
			b.showMaterialMenu(chatID, nil)
			return
		case "Инвентаризация":
			if u.Role != users.RoleAdmin && u.Role != users.RoleAdministrator {
				return
			}
			_ = b.states.Set(ctx, chatID, dialog.StateStockMenu, dialog.Payload{})
			b.showStocksMenu(chatID, nil)
			return
		case "Поставки":
			if u.Role != users.RoleAdmin && u.Role != users.RoleAdministrator {
				return
			}
			_ = b.states.Set(ctx, chatID, dialog.StateSupMenu, dialog.Payload{})
			b.showSuppliesMenu(chatID, nil)
			return
		case "Абонементы":
			if u.Role != users.RoleAdmin {
				return
			}
			_ = b.states.Set(ctx, chatID, dialog.StateAdmSubsMenu, dialog.Payload{})
			b.showSubsMenu(chatID, nil)
			return
		case "Установка цен":
			if u.Role != users.RoleAdmin {
				return
			}
			_ = b.states.Set(ctx, chatID, dialog.StatePriceMenu, dialog.Payload{})
			b.showPriceMainMenu(chatID, nil)
			return
		case "Аренда и Расходы материалов по мастерам":
			if u.Role != users.RoleAdmin {
				return
			}
			_ = b.states.Set(ctx, chatID, dialog.StateAdmReportRentPeriod, dialog.Payload{})
			msg := tgbotapi.NewMessage(chatID,
				"Введите период для отчёта в формате ДД.ММ.ГГГГ-ДД.ММ.ГГГГ.\n"+
					"Например: 01.11.2025-30.11.2025.\n"+
					"Дата окончания включительно, данные будут взяты до конца этого дня.")
			b.send(msg)
			return
		case "Оповещение всем":
			if u.Role != users.RoleAdmin {
				return
			}
			_ = b.states.Set(ctx, chatID, dialog.StateAdmBroadcastAll, dialog.Payload{})
			b.send(tgbotapi.NewMessage(chatID,
				"Введите текст оповещения. Оно будет отправлено всем подтверждённым пользователям бота."))
			return
		}
		return
	}

	if msg.Text == "Установка тарифов" {
		u, _ := b.users.GetByTelegramID(ctx, tgID)
		if u == nil || u.Role != users.RoleAdmin || u.Status != users.StatusApproved {
			return
		}
		_ = b.states.Set(ctx, chatID, dialog.StateAdmRatesPickPU, dialog.Payload{
			"place": "hall", "unit": "hour", "with_sub": false,
		})
		kb := tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("Общий зал / час", "rates:pu:hall:hour"),
				tgbotapi.NewInlineKeyboardButtonData("Кабинет / день", "rates:pu:cabinet:day"),
			),
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("Абонемент: выкл", "rates:sub:tg"),
			),
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("📄 Показать ступени", "rates:list"),
			),
			navKeyboard(false, true).InlineKeyboard[0],
		)
		m := tgbotapi.NewMessage(chatID, "Установка тарифов — выберите набор параметров:")
		m.ReplyMarkup = kb
		b.send(m)
		return
	}

	// Триггеры расхода/аренды по тексту (доступно всем подтверждённым ролям)
	if msg.Text == "/rent" || msg.Text == "/consumption" {
		u, _ := b.users.GetByTelegramID(ctx, tgID)
		if u == nil || u.Status != users.StatusApproved || u.Role != users.RoleMaster {
			b.send(tgbotapi.NewMessage(chatID, "Доступ запрещён."))
			return
		}
		_ = b.states.Set(ctx, chatID, dialog.StateConsPlace, dialog.Payload{})
		kb := tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("Общий зал", "cons:place:hall"),
				tgbotapi.NewInlineKeyboardButtonData("Кабинет", "cons:place:cabinet"),
			),
			navKeyboard(false, true).InlineKeyboard[0],
		)
		m := tgbotapi.NewMessage(chatID, "Выберите помещение:")
		m.ReplyMarkup = kb
		b.send(m)
		return
	}

	switch st.State {
	case dialog.StateAwaitFIO:
		fio := strings.TrimSpace(msg.Text)
		if fio == "" || len(fio) < 3 {
			b.send(tgbotapi.NewMessage(chatID, "ФИО выглядит пустым. Введите корректно."))
			return
		}
		if _, err := b.users.SetFIO(ctx, tgID, fio); err != nil {
			b.send(tgbotapi.NewMessage(chatID, "Ошибка сохранения ФИО, попробуйте ещё раз."))
			return
		}
		p := st.Payload
		p["fio"] = fio
		_ = b.states.Set(ctx, chatID, dialog.StateAwaitRole, p)
		m := tgbotapi.NewMessage(chatID, "Выберите роль:")
		m.ReplyMarkup = roleKeyboard()
		b.send(m)
		return

	case dialog.StateChatAdmin:
		if len(b.adminIDs) == 0 {
			b.send(tgbotapi.NewMessage(chatID,
				"Админ-чат не настроен. Сообщение не отправлено."))
			_ = b.states.Reset(ctx, chatID)
			return
		}

		u, _ := b.users.GetByTelegramID(ctx, tgID)

		roleLabel := "пользователь"
		if u != nil {
			switch u.Role {
			case users.RoleMaster:
				roleLabel = "мастер"
			case users.RoleAdmin, users.RoleAdministrator:
				roleLabel = "администратор"
			}
		}

		fio := ""
		if u != nil {
			fio = strings.TrimSpace(u.Username)
		}

		header := fmt.Sprintf("Сообщение в чат админов от %s", roleLabel)
		if fio != "" {
			header += " " + fio
		}
		if msg.From.UserName != "" {
			header += fmt.Sprintf(" (@%s)", msg.From.UserName)
		}
		header += ":"

		for adminID := range b.adminIDs {
			// сначала шапка
			b.send(tgbotapi.NewMessage(adminID, header))

			// дальше отправляем содержимое
			switch {
			case msg.Document != nil:
				caption := strings.TrimSpace(msg.Caption)
				doc := tgbotapi.NewDocument(adminID,
					tgbotapi.FileID(msg.Document.FileID))
				if caption != "" {
					doc.Caption = caption
				}
				b.send(doc)

			case len(msg.Photo) > 0:
				photo := msg.Photo[len(msg.Photo)-1]
				caption := strings.TrimSpace(msg.Caption)
				p := tgbotapi.NewPhoto(adminID,
					tgbotapi.FileID(photo.FileID))
				if caption != "" {
					p.Caption = caption
				}
				b.send(p)

			case msg.Video != nil:
				caption := strings.TrimSpace(msg.Caption)
				v := tgbotapi.NewVideo(adminID,
					tgbotapi.FileID(msg.Video.FileID))
				if caption != "" {
					v.Caption = caption
				}
				b.send(v)

			case msg.Audio != nil:
				caption := strings.TrimSpace(msg.Caption)
				a := tgbotapi.NewAudio(adminID,
					tgbotapi.FileID(msg.Audio.FileID))
				if caption != "" {
					a.Caption = caption
				}
				b.send(a)

			case msg.Voice != nil:
				v := tgbotapi.NewVoice(adminID,
					tgbotapi.FileID(msg.Voice.FileID))
				b.send(v)

			default:
				text := strings.TrimSpace(msg.Text)
				if text != "" {
					b.send(tgbotapi.NewMessage(adminID, text))
				}
			}
		}

		b.send(tgbotapi.NewMessage(chatID, "Сообщение отправлено администраторам."))
		_ = b.states.Reset(ctx, chatID)
		return

	case dialog.StateAdmWhName:
		// ввод названия склада
		name := strings.TrimSpace(msg.Text)
		if name == "" {
			b.send(tgbotapi.NewMessage(chatID, "Название не может быть пустым. Введите ещё раз."))
			return
		}
		p := st.Payload
		p["wh_name"] = name
		_ = b.states.Set(ctx, chatID, dialog.StateAdmWhType, p)
		kb := tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("Расходники", "adm:wh:type:consumables"),
				tgbotapi.NewInlineKeyboardButtonData("Клиентский", "adm:wh:type:client_service"),
			),
			navKeyboard(true, true).InlineKeyboard[0],
		)
		m := tgbotapi.NewMessage(chatID, "Выберите тип склада:")
		m.ReplyMarkup = kb
		b.send(m)
		return

	case dialog.StateAdmCatName:
		// ввод названия категории
		name := strings.TrimSpace(msg.Text)
		if name == "" {
			b.send(tgbotapi.NewMessage(chatID, "Название не может быть пустым. Введите ещё раз."))
			return
		}
		if _, err := b.catalog.CreateCategory(ctx, name); err != nil {
			b.send(tgbotapi.NewMessage(chatID, "Ошибка при создании категории"))
			return
		}
		_ = b.states.Set(ctx, chatID, dialog.StateAdmCatMenu, dialog.Payload{})
		b.send(tgbotapi.NewMessage(chatID, fmt.Sprintf("Категория «%s» создана.", name)))
		b.showCategoryMenu(chatID, nil)
		return

	case dialog.StateAdmWhRename:
		name := strings.TrimSpace(msg.Text)
		if name == "" {
			b.send(tgbotapi.NewMessage(chatID, "Название не может быть пустым. Введите ещё раз."))
			return
		}
		idAny := st.Payload["wh_id"]
		id := int64(idAny.(float64)) // payload приходит из JSON; приведение через float64
		if _, err := b.catalog.UpdateWarehouseName(ctx, id, name); err != nil {
			b.send(tgbotapi.NewMessage(chatID, "Ошибка при переименовании склада"))
			return
		}
		_ = b.states.Set(ctx, chatID, dialog.StateAdmWhMenu, dialog.Payload{})
		b.send(tgbotapi.NewMessage(chatID, "Склад переименован."))
		// Вернём список
		b.showWarehouseMenu(chatID, nil)
		return

	case dialog.StateAdmCatRename:
		name := strings.TrimSpace(msg.Text)
		if name == "" {
			b.send(tgbotapi.NewMessage(chatID, "Название не может быть пустым. Введите ещё раз."))
			return
		}
		idAny := st.Payload["cat_id"]
		id := int64(idAny.(float64))
		if _, err := b.catalog.UpdateCategoryName(ctx, id, name); err != nil {
			b.send(tgbotapi.NewMessage(chatID, "Ошибка при переименовании категории"))
			return
		}
		_ = b.states.Set(ctx, chatID, dialog.StateAdmCatMenu, dialog.Payload{})
		b.send(tgbotapi.NewMessage(chatID, "Категория переименована."))
		b.showCategoryMenu(chatID, nil)
		return

	case dialog.StateAdmMatBrand:
		brand := strings.TrimSpace(msg.Text)
		if brand == "-" {
			brand = ""
		}
		// забираем cat_id из payload и НЕ теряем mat_wh_id
		cidAny, ok := st.Payload["cat_id"]
		if !ok {
			b.send(tgbotapi.NewMessage(chatID, "Сессия устарела, выберите категорию ещё раз."))
			_ = b.states.Set(ctx, chatID, dialog.StateAdmMatMenu, dialog.Payload{})
			b.showMaterialMenu(chatID, nil)
			return
		}

		p := dialog.Payload{
			"cat_id": cidAny,
			"brand":  brand,
		}
		if whAny, ok2 := st.Payload["mat_wh_id"]; ok2 {
			p["mat_wh_id"] = whAny
		}

		_ = b.states.Set(ctx, chatID, dialog.StateAdmMatName, p)
		b.send(tgbotapi.NewMessage(chatID, "Введите название материала сообщением."))
		return

	case dialog.StateAdmMatName:
		name := strings.TrimSpace(msg.Text)
		if name == "" {
			b.send(tgbotapi.NewMessage(chatID, "Название не может быть пустым. Введите ещё раз."))
			return
		}

		cidAny := st.Payload["cat_id"]
		catID := int64(cidAny.(float64))

		brandName := ""
		if bAny, ok := st.Payload["brand"]; ok {
			if bs, ok2 := bAny.(string); ok2 {
				brandName = bs
			}
		}

		br, err := b.brands.GetOrCreate(ctx, catID, brandName)
		if err != nil {
			b.send(tgbotapi.NewMessage(chatID, fmt.Sprintf("Ошибка при обработке бренда: %v", err)))
			return
		}

		// вытаскиваем выбранный склад
		var whID int64
		if whAny, ok := st.Payload["mat_wh_id"]; ok {
			switch v := whAny.(type) {
			case float64:
				whID = int64(v)
			case int64:
				whID = v
			case string:
				if parsed, err := strconv.ParseInt(v, 10, 64); err == nil {
					whID = parsed
				}
			}
		}

		mat, err := b.materials.Create(ctx, name, catID, br.ID, materials.UnitG)
		if err != nil {
			b.send(tgbotapi.NewMessage(chatID, "Ошибка при создании материала"))
			return
		}

		// создаём нулевой остаток только для выбранного склада
		if whID != 0 {
			if err := b.materials.InitBalanceForWarehouse(ctx, whID, mat.ID); err != nil {
				b.log.Error("failed to init balance for new material",
					"err", err, "warehouse_id", whID, "material_id", mat.ID)
			}
		}

		_ = b.states.Set(ctx, chatID, dialog.StateAdmMatMenu, dialog.Payload{})
		b.send(tgbotapi.NewMessage(chatID, fmt.Sprintf("Материал «%s» создан.", name)))
		b.showMaterialMenu(chatID, nil)
		return

	case dialog.StateAdmMatRename:
		name := strings.TrimSpace(msg.Text)
		if name == "" {
			b.send(tgbotapi.NewMessage(chatID, "Название не может быть пустым. Введите ещё раз."))
			return
		}
		idAny := st.Payload["mat_id"]
		id := int64(idAny.(float64))
		if _, err := b.materials.UpdateName(ctx, id, name); err != nil {
			b.send(tgbotapi.NewMessage(chatID, "Ошибка при переименовании материала"))
			return
		}
		_ = b.states.Set(ctx, chatID, dialog.StateAdmMatMenu, dialog.Payload{})
		b.send(tgbotapi.NewMessage(chatID, "Материал переименован."))
		b.showMaterialMenu(chatID, nil)
		return
	case dialog.StateStockInQty:
		qtyStr := strings.TrimSpace(msg.Text)
		qty, err := strconv.ParseFloat(strings.ReplaceAll(qtyStr, ",", "."), 64)
		if err != nil || qty <= 0 {
			b.send(tgbotapi.NewMessage(chatID, "Некорректное число. Введите положительное значение."))
			return
		}
		wh := int64(st.Payload["wh_id"].(float64))
		mat := int64(st.Payload["mat_id"].(float64))
		// actorID — ID из users, получим по telegram_id
		u, _ := b.users.GetByTelegramID(ctx, tgID)
		if u == nil {
			b.send(tgbotapi.NewMessage(chatID, "Пользователь не найден"))
			return
		}
		if err := b.inventory.Receive(ctx, u.ID, wh, mat, qty, "bot"); err != nil {
			b.send(tgbotapi.NewMessage(chatID, "Ошибка прихода: "+err.Error()))
			return
		}
		_ = b.states.Set(ctx, chatID, dialog.StateStockItem, dialog.Payload{"wh_id": float64(wh), "mat_id": float64(mat)})
		b.send(tgbotapi.NewMessage(chatID, "Приход проведён"))
		// перерисуем карточку
		b.showStockItem(ctx, chatID, msg.MessageID, wh, mat)
		b.maybeNotifyLowOrNegative(ctx, chatID, wh, mat)
		return

	case dialog.StateStockOutQty:
		qtyStr := strings.TrimSpace(msg.Text)
		qty, err := strconv.ParseFloat(strings.ReplaceAll(qtyStr, ",", "."), 64)
		if err != nil || qty <= 0 {
			b.send(tgbotapi.NewMessage(chatID, "Некорректное число. Введите положительное значение."))
			return
		}
		wh := int64(st.Payload["wh_id"].(float64))
		mat := int64(st.Payload["mat_id"].(float64))
		u, _ := b.users.GetByTelegramID(ctx, tgID)
		if u == nil {
			b.send(tgbotapi.NewMessage(chatID, "Пользователь не найден"))
			return
		}
		if err := b.inventory.WriteOff(ctx, u.ID, wh, mat, qty, "bot"); err != nil {
			b.send(tgbotapi.NewMessage(chatID, "Ошибка списания: "+err.Error()))
			return
		}
		_ = b.states.Set(ctx, chatID, dialog.StateStockItem, dialog.Payload{"wh_id": float64(wh), "mat_id": float64(mat)})
		b.send(tgbotapi.NewMessage(chatID, "Списание проведено"))
		b.showStockItem(ctx, chatID, msg.MessageID, wh, mat)
		b.maybeNotifyLowOrNegative(ctx, chatID, wh, mat)
		return

	case dialog.StateSupQty:
		// Чистим прошлую клавиатуру под сообщением шага "количество"
		b.clearPrevStep(ctx, chatID)

		qtyStr := strings.TrimSpace(msg.Text)
		qtyStr = strings.ReplaceAll(qtyStr, ",", ".")
		// только целые числа: граммы/шт, без дробной части
		if strings.Contains(qtyStr, ".") {
			b.send(tgbotapi.NewMessage(chatID, "Введите целое число без дробной части (используем граммы/шт)."))
			return
		}
		n, err := strconv.ParseInt(qtyStr, 10, 64)
		if err != nil || n <= 0 {
			b.send(tgbotapi.NewMessage(chatID, "Некорректное число. Введите целое положительное значение."))
			return
		}
		// сохраняем целое значение; payload сериализуется как float64 — это ок
		st.Payload["qty"] = float64(n)
		_ = b.states.Set(ctx, chatID, dialog.StateSupUnitPrice, st.Payload)
		m := tgbotapi.NewMessage(chatID, "Введите цену за единицу (руб)")
		m.ReplyMarkup = navKeyboard(true, true)
		sent, _ := b.api.Send(m)

		// сохраняем last_mid и переключаемся на шаг цены
		b.saveLastStep(ctx, chatID, dialog.StateSupUnitPrice, st.Payload, sent.MessageID)
		return

	case dialog.StateSupUnitPrice:
		b.clearPrevStep(ctx, chatID)

		st, _ := b.states.Get(ctx, chatID)
		if st == nil || st.Payload == nil {
			// начнем заново
			_ = b.states.Set(ctx, chatID, dialog.StateSupPickWh, dialog.Payload{})
			b.showSuppliesPickWarehouse(ctx, chatID, nil)
			return
		}
		whF, okWh := st.Payload["wh_id"].(float64)
		matF, okMat := st.Payload["mat_id"].(float64)
		if !okWh || !okMat {
			// контекст потерян — возвращаем на выбор склада
			_ = b.states.Set(ctx, chatID, dialog.StateSupPickWh, dialog.Payload{})
			b.showSuppliesPickWarehouse(ctx, chatID, nil)
			return
		}
		whID := int64(whF)
		matID := int64(matF)

		priceStr := strings.TrimSpace(msg.Text)
		price, err := strconv.ParseFloat(strings.ReplaceAll(priceStr, ",", "."), 64)
		if err != nil || price < 0 {
			b.send(tgbotapi.NewMessage(chatID, "Некорректное число. Введите цену (руб)."))
			return
		}
		qty := int64(st.Payload["qty"].(float64)) // мы сохраняли как float64, но значение целое

		// Добавляем позицию в payload["items"]
		items := b.parseSupItems(st.Payload["items"])
		items = append(items, map[string]any{
			"mat_id": float64(matID), // через float64, чтобы без проблем сериализовалось
			"qty":    float64(qty),
			"price":  price,
		})
		st.Payload["items"] = items

		// Переходим в корзину
		_ = b.states.Set(ctx, chatID, dialog.StateSupCart, st.Payload)
		b.showSuppliesCart(ctx, chatID, nil, whID, items)
		return

	case dialog.StateSupImportComment:
		// ввод комментария к поставке (поставщик и т.п.)
		text := strings.TrimSpace(msg.Text)
		if text == "" {
			b.send(tgbotapi.NewMessage(chatID,
				"Комментарий не может быть пустым. Введите текст или «-», если комментарий не нужен."))
			return
		}
		if text == "-" {
			text = ""
		}

		payload := dialog.Payload{"comment": text}
		_ = b.states.Set(ctx, chatID, dialog.StateSupImportFile, payload)
		b.send(tgbotapi.NewMessage(chatID,
			"Теперь отправьте Excel-файл (.xlsx) с поступлением, который вы выгрузили через «Выгрузить материалы» и заполнили колонку «Количество»."))
		return

	case dialog.StateSupImportFile:
		// ждём документ Excel
		if msg.Document == nil {
			b.send(tgbotapi.NewMessage(chatID,
				"Пожалуйста, отправьте Excel-файл (.xlsx) с поступлением, который вы выгрузили через «Выгрузить материалы» и в котором заполнена колонка «Количество»."))
			return
		}

		// комментарий, введённый на предыдущем шаге (может быть пустым)
		comment := ""
		if st != nil && st.Payload != nil {
			if c, ok := st.Payload["comment"].(string); ok {
				comment = c
			}
		}

		// ищем пользователя
		u, err := b.users.GetByTelegramID(ctx, msg.From.ID)
		if err != nil || u == nil {
			b.send(tgbotapi.NewMessage(chatID, "Пользователь не найден или нет доступа."))
			return
		}

		// скачиваем файл из Telegram
		data, err := b.downloadTelegramFile(msg.Document.FileID)
		if err != nil {
			b.send(tgbotapi.NewMessage(chatID, "Не удалось скачать файл из Telegram: "+err.Error()))
			return
		}

		// обрабатываем Excel
		b.handleSuppliesImportExcel(ctx, chatID, u, data, comment)
		return

	case dialog.StateSupJournalFrom:
		// ввод даты начала
		fromStr := strings.TrimSpace(msg.Text)
		from, err := time.Parse("02.01.2006", fromStr)
		if err != nil {
			b.send(tgbotapi.NewMessage(chatID,
				"Некорректная дата. Введите в формате ДД.ММ.ГГГГ, например 01.11.2025."))
			return
		}

		payload := dialog.Payload{"from": from.Format(time.RFC3339)}
		_ = b.states.Set(ctx, chatID, dialog.StateSupJournalTo, payload)
		b.send(tgbotapi.NewMessage(chatID,
			"Введите дату конца периода в формате ДД.ММ.ГГГГ (данные включительно, до конца этого дня)."))
		return

	case dialog.StateSupJournalTo:
		// ввод даты конца + показ списка поставок
		if st == nil || st.Payload == nil {
			_ = b.states.Reset(ctx, chatID)
			b.send(tgbotapi.NewMessage(chatID,
				"Состояние потеряно. Начните заново: «Поставки» → «Журнал»."))
			return
		}

		toStr := strings.TrimSpace(msg.Text)
		to, err := time.Parse("02.01.2006", toStr)
		if err != nil {
			b.send(tgbotapi.NewMessage(chatID,
				"Некорректная дата. Введите в формате ДД.ММ.ГГГГ, например 30.11.2025."))
			return
		}

		fromRFC, ok := dialog.GetString(st.Payload, "from")
		if !ok {
			_ = b.states.Reset(ctx, chatID)
			b.send(tgbotapi.NewMessage(chatID,
				"Не удалось прочитать дату начала. Начните заново: «Поставки» → «Журнал»."))
			return
		}
		from, err := time.Parse(time.RFC3339, fromRFC)
		if err != nil {
			_ = b.states.Reset(ctx, chatID)
			b.send(tgbotapi.NewMessage(chatID,
				"Не удалось прочитать дату начала. Начните заново: «Поставки» → «Журнал»."))
			return
		}

		// конец дня включительно → делаем верхнюю границу «to + 1 день»
		toEnd := to.AddDate(0, 0, 1)

		// показываем список поставок за период
		_ = b.states.Set(ctx, chatID, dialog.StateSupMenu, dialog.Payload{})
		b.showSuppliesJournalList(ctx, chatID, nil, from, toEnd)
		return

	case dialog.StateStockImportFile:
		// ждём документ Excel
		if msg.Document == nil {
			b.send(tgbotapi.NewMessage(chatID,
				"Пожалуйста, отправьте Excel-файл (.xlsx) с остатками, который был выгружен через «Выгрузить остатки» и в котором заполнен столбец qty."))
			return
		}

		u, err := b.users.GetByTelegramID(ctx, tgID)
		if err != nil || u == nil {
			b.send(tgbotapi.NewMessage(chatID, "Пользователь не найден или нет доступа."))
			return
		}

		data, err := b.downloadTelegramFile(msg.Document.FileID)
		if err != nil {
			b.send(tgbotapi.NewMessage(chatID, "Не удалось скачать файл из Telegram: "+err.Error()))
			return
		}

		b.handleStocksImportExcel(ctx, chatID, u, data)
		return

	case dialog.StatePriceMatImportFile:
		if msg.Document == nil {
			b.send(tgbotapi.NewMessage(chatID,
				"Пожалуйста, отправьте Excel-файл (.xlsx) с ценами материалов, который был выгружен через «Выгрузить цены на материалы» и в котором заполнена колонка price_per_unit."))
			return
		}

		u, err := b.users.GetByTelegramID(ctx, tgID)
		if err != nil || u == nil {
			b.send(tgbotapi.NewMessage(chatID, "Пользователь не найден или нет доступа."))
			return
		}

		data, err := b.downloadTelegramFile(msg.Document.FileID)
		if err != nil {
			b.send(tgbotapi.NewMessage(chatID, "Не удалось скачать файл из Telegram: "+err.Error()))
			return
		}

		b.handlePriceMatImportExcel(ctx, chatID, data)
		return

	case dialog.StatePriceRentImportFile:
		if msg.Document == nil {
			b.send(tgbotapi.NewMessage(chatID,
				"Пожалуйста, отправьте Excel-файл (.xlsx) с тарифами аренды, который был выгружен через «Выгрузить цены на аренду» и в котором заполнены threshold_materials / price_with_materials / price_own_materials."))
			return
		}

		u, err := b.users.GetByTelegramID(ctx, tgID)
		if err != nil || u == nil {
			b.send(tgbotapi.NewMessage(chatID, "Пользователь не найден или нет доступа."))
			return
		}

		data, err := b.downloadTelegramFile(msg.Document.FileID)
		if err != nil {
			b.send(tgbotapi.NewMessage(chatID, "Не удалось скачать файл из Telegram: "+err.Error()))
			return
		}

		b.handlePriceRentImportExcel(ctx, chatID, data)
		return

	case dialog.StateConsComment:
		text := strings.TrimSpace(msg.Text)
		if text == "" {
			b.send(tgbotapi.NewMessage(chatID,
				"Комментарий не может быть пустым. Введите дату или нажмите «Пропустить»."))
			return
		}

		payload := dialog.Payload{"comment": text}
		_ = b.states.Set(ctx, chatID, dialog.StateConsPlace, payload)

		kb := tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("Общий зал", "cons:place:hall"),
				tgbotapi.NewInlineKeyboardButtonData("Кабинет", "cons:place:cabinet"),
			),
			navKeyboard(false, true).InlineKeyboard[0],
		)
		m := tgbotapi.NewMessage(chatID, "Выберите помещение:")
		m.ReplyMarkup = kb
		b.send(m)
		return

	case dialog.StateConsQty:
		s := strings.TrimSpace(msg.Text)
		s = strings.ReplaceAll(s, ",", ".")
		if strings.Contains(s, ".") {
			b.send(tgbotapi.NewMessage(chatID, "Введите целое число (часов/дней)."))
			return
		}
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil || n <= 0 {
			b.send(tgbotapi.NewMessage(chatID, "Некорректное значение. Введите целое положительное число."))
			return
		}
		st.Payload["qty"] = float64(n)
		// корзина пустая
		st.Payload["items"] = []map[string]any{}
		_ = b.states.Set(ctx, chatID, dialog.StateConsCart, st.Payload)
		b.showConsCart(ctx, chatID, nil, st.Payload["place"].(string), st.Payload["unit"].(string), int(n), []map[string]any{})
		return

	case dialog.StateConsSearchByName:
		query := strings.TrimSpace(msg.Text)
		if query == "" {
			b.send(tgbotapi.NewMessage(chatID, "Введите часть названия материала."))
			return
		}

		mats, err := b.materials.SearchByName(ctx, query, true)
		if err != nil {
			b.send(tgbotapi.NewMessage(chatID, "Ошибка поиска материалов. Попробуйте позже."))
			return
		}
		if len(mats) == 0 {
			b.send(tgbotapi.NewMessage(chatID, "Материалы не найдены, попробуйте другую часть названия."))
			return
		}

		rows := [][]tgbotapi.InlineKeyboardButton{}
		for _, m := range mats {
			label := materialDisplayName(m.Brand, m.Name)
			rows = append(rows, tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData(label, fmt.Sprintf("cons:mat:%d", m.ID)),
			))
		}
		rows = append(rows, navKeyboard(true, true).InlineKeyboard[0])
		kb := tgbotapi.NewInlineKeyboardMarkup(rows...)

		// переходим к выбору материала из найденного списка
		_ = b.states.Set(ctx, chatID, dialog.StateConsMatPick, st.Payload)
		msgOut := tgbotapi.NewMessage(chatID, "Выберите материал:")
		msgOut.ReplyMarkup = kb
		b.send(msgOut)
		return

	case dialog.StateConsMatQty:
		s := strings.TrimSpace(msg.Text)
		s = strings.ReplaceAll(s, ",", ".")
		if strings.Contains(s, ".") {
			b.send(tgbotapi.NewMessage(chatID, "Введите целое число (граммы/шт)."))
			return
		}
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil || n <= 0 {
			b.send(tgbotapi.NewMessage(chatID, "Некорректное значение. Введите целое положительное число."))
			return
		}
		items := b.consParseItems(st.Payload["items"])
		items = append(items, map[string]any{
			"mat_id": st.Payload["mat_id"],
			"qty":    float64(n),
		})
		st.Payload["items"] = items
		_ = b.states.Set(ctx, chatID, dialog.StateConsCart, st.Payload)
		b.showConsCart(ctx, chatID, nil, st.Payload["place"].(string), st.Payload["unit"].(string), int(st.Payload["qty"].(float64)), items)
		return

	case dialog.StateAdmSubsEnterQty:
		s := strings.TrimSpace(msg.Text)
		if strings.Contains(s, ",") {
			s = strings.ReplaceAll(s, ",", ".")
		}
		if strings.Contains(s, ".") {
			b.send(tgbotapi.NewMessage(chatID, "Введите целое число (без дробной части)."))
			return
		}
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil || n <= 0 {
			b.send(tgbotapi.NewMessage(chatID, "Некорректное значение. Введите целое положительное число."))
			return
		}

		st.Payload["total"] = float64(n)
		_ = b.states.Set(ctx, chatID, dialog.StateAdmSubsConfirm, st.Payload)

		place := st.Payload["place"].(string)
		unit := st.Payload["unit"].(string)
		uid := int64(st.Payload["uid"].(float64))
		month := time.Now().Format("2006-01")

		// Для превью: найдём пользователя по uid
		var title string
		if u, _ := b.users.GetByID(ctx, uid); u != nil {
			title = strings.TrimSpace(u.Username) // у нас «ФИО/отображаемое имя» хранится в Username
			if title == "" {
				title = fmt.Sprintf("id %d", u.ID)
			}
		} else {
			title = fmt.Sprintf("id %d", uid)
		}

		preview := fmt.Sprintf(
			"Подтвердите создание абонемента:\nМастер: %s\nМесяц: %s\nМесто: %s\nЕдиница: %s\nОбъём: %d",
			title, month,
			map[string]string{"hall": "Зал", "cabinet": "Кабинет"}[place],
			map[string]string{"hour": "часы", "day": "дни"}[unit],
			n,
		)
		kb := tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("✅ Подтвердить", "adm:subs:confirm"),
			),
			navKeyboard(true, true).InlineKeyboard[0],
		)
		m := tgbotapi.NewMessage(chatID, preview)
		m.ReplyMarkup = kb
		b.send(m)
		return

	case dialog.StateAdmRatesCreateMin:
		s := strings.TrimSpace(msg.Text)
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil || n <= 0 {
			b.send(tgbotapi.NewMessage(chatID, "Введите целое положительное число"))
			return
		}
		st.Payload["min"] = float64(n)
		_ = b.states.Set(ctx, chatID, dialog.StateAdmRatesCreateMax, st.Payload)

		m := tgbotapi.NewMessage(chatID, "Введите максимальное значение диапазона или «-» для бесконечности")
		m.ReplyMarkup = navKeyboard(true, true)
		b.send(m)
		return

	case dialog.StateAdmRatesCreateMax:
		s := strings.TrimSpace(msg.Text)
		if s == "-" {
			st.Payload["max"] = nil
		} else {
			n, err := strconv.ParseInt(s, 10, 64)
			if err != nil || n <= 0 {
				b.send(tgbotapi.NewMessage(chatID, "Введите целое положительное число или «-»"))
				return
			}
			st.Payload["max"] = float64(n)
		}
		_ = b.states.Set(ctx, chatID, dialog.StateAdmRatesCreateThreshold, st.Payload)

		m := tgbotapi.NewMessage(chatID, "Введите порог материалов на единицу (например 100 или 1000)")
		m.ReplyMarkup = navKeyboard(true, true)
		b.send(m)
		return

	case dialog.StateAdmRatesCreateThreshold:
		s := strings.TrimSpace(msg.Text)
		x, err := strconv.ParseFloat(strings.ReplaceAll(s, ",", "."), 64)
		if err != nil || x < 0 {
			b.send(tgbotapi.NewMessage(chatID, "Введите число (>= 0)"))
			return
		}
		st.Payload["thr"] = x
		_ = b.states.Set(ctx, chatID, dialog.StateAdmRatesCreatePriceWith, st.Payload)

		m := tgbotapi.NewMessage(chatID, "Цена за ед., если порог выполнен (руб)")
		m.ReplyMarkup = navKeyboard(true, true)
		b.send(m)
		return

	case dialog.StateAdmRatesCreatePriceWith:
		s := strings.TrimSpace(msg.Text)
		x, err := strconv.ParseFloat(strings.ReplaceAll(s, ",", "."), 64)
		if err != nil || x < 0 {
			b.send(tgbotapi.NewMessage(chatID, "Введите число (>= 0)"))
			return
		}
		st.Payload["pwith"] = x
		_ = b.states.Set(ctx, chatID, dialog.StateAdmRatesCreatePriceOwn, st.Payload)

		m := tgbotapi.NewMessage(chatID, "Цена за ед., если порог НЕ выполнен (руб)")
		m.ReplyMarkup = navKeyboard(true, true)
		b.send(m)
		return

	case dialog.StateAdmRatesCreatePriceOwn:
		s := strings.TrimSpace(msg.Text)
		x, err := strconv.ParseFloat(strings.ReplaceAll(s, ",", "."), 64)
		if err != nil || x < 0 {
			b.send(tgbotapi.NewMessage(chatID, "Введите число (>= 0)"))
			return
		}
		st.Payload["pown"] = x
		_ = b.states.Set(ctx, chatID, dialog.StateAdmRatesConfirm, st.Payload)

		place := st.Payload["place"].(string)
		unit := st.Payload["unit"].(string)
		withSub := false
		if v, ok := st.Payload["with_sub"].(bool); ok {
			withSub = v
		}

		minQty := int(st.Payload["min"].(float64))
		var maxTxt string
		if st.Payload["max"] == nil {
			maxTxt = "∞"
		} else {
			maxTxt = fmt.Sprintf("%d", int(st.Payload["max"].(float64)))
		}
		thr := st.Payload["thr"].(float64)
		pwith := st.Payload["pwith"].(float64)
		pown := st.Payload["pown"].(float64)

		preview := fmt.Sprintf(
			"Ступень:\n— %s / %s (%s)\n— Диапазон: %d–%s\n— Порог: %.0f\n— Цена с материалами: %.2f\n— Цена со своими: %.2f\n\nСохранить?",
			map[string]string{"hall": "Зал", "cabinet": "Кабинет"}[place],
			map[string]string{"hour": "час", "day": "день"}[unit],
			map[bool]string{true: "с абонементом", false: "без абонемента"}[withSub],
			minQty, maxTxt, thr, pwith, pown,
		)

		kb := tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(tgbotapi.NewInlineKeyboardButtonData("💾 Сохранить", "rates:save")),
			navKeyboard(true, true).InlineKeyboard[0],
		)
		m := tgbotapi.NewMessage(chatID, preview)
		m.ReplyMarkup = kb
		b.send(m)
		return

	case dialog.StateAdmReportRentPeriod:
		period := strings.TrimSpace(msg.Text)
		dates := strings.Split(period, "-")
		if len(dates) != 2 {
			b.send(tgbotapi.NewMessage(chatID, "Неверный формат. Используйте ДД.ММ.ГГГГ-ДД.ММ.ГГГГ, например 01.11.2025-30.11.2025."))
			return
		}
		const layout = "02.01.2006"
		fromStr := strings.TrimSpace(dates[0])
		toStr := strings.TrimSpace(dates[1])

		from, err1 := time.Parse(layout, fromStr)
		to, err2 := time.Parse(layout, toStr)
		if err1 != nil || err2 != nil {
			b.send(tgbotapi.NewMessage(chatID, "Не удалось разобрать дату. Проверьте формат ДД.ММ.ГГГГ."))
			return
		}
		if !to.After(from) && !to.Equal(from) {
			b.send(tgbotapi.NewMessage(chatID, "Дата окончания должна быть не раньше даты начала."))
			return
		}

		// делаем to эксклюзивной границей: +1 день
		toExclusive := to.Add(24 * time.Hour)

		if err := b.handleAdmRentMaterialsReport(ctx, chatID, from, toExclusive); err != nil {
			b.send(tgbotapi.NewMessage(chatID, fmt.Sprintf("Ошибка формирования отчёта: %v", err)))
			return
		}

		_ = b.states.Set(ctx, chatID, dialog.StateIdle, dialog.Payload{})
		return

	case dialog.StateAdmBroadcastAll:
		textToSend := strings.TrimSpace(msg.Text)
		if textToSend == "" {
			b.send(tgbotapi.NewMessage(chatID, "Текст оповещения не может быть пустым. Введите сообщение."))
			return
		}

		u, _ := b.users.GetByTelegramID(ctx, tgID)
		if u == nil || u.Status != users.StatusApproved || u.Role != users.RoleAdmin {
			b.send(tgbotapi.NewMessage(chatID, "Недостаточно прав для рассылки."))
			_ = b.states.Reset(ctx, chatID)
			return
		}

		ids, err := b.users.ListApprovedTelegramIDs(ctx)
		if err != nil {
			b.send(tgbotapi.NewMessage(chatID,
				fmt.Sprintf("Не удалось получить список пользователей: %v", err)))
			_ = b.states.Reset(ctx, chatID)
			return
		}

		sent := 0
		for _, id := range ids {
			if id == 0 {
				continue
			}
			b.send(tgbotapi.NewMessage(id, textToSend))
			sent++
		}

		b.send(tgbotapi.NewMessage(chatID,
			fmt.Sprintf("Оповещение отправлено %d пользователям.", sent)))
		_ = b.states.Reset(ctx, chatID)
		return

	case dialog.StateMasterStockSearchByName:
		query := strings.TrimSpace(msg.Text)
		if query == "" {
			b.send(tgbotapi.NewMessage(chatID, "Введите название материала или его часть."))
			return
		}

		whID, err := b.getConsumablesWarehouseID(ctx)
		if err != nil {
			b.send(tgbotapi.NewMessage(chatID, "Склад «Расходники» не найден. Обратитесь к администратору."))
			_ = b.states.Reset(ctx, chatID)
			return
		}

		items, err := b.materials.ListWithBalanceByWarehouse(ctx, whID)
		if err != nil {
			b.send(tgbotapi.NewMessage(chatID, "Ошибка загрузки материалов со склада."))
			_ = b.states.Reset(ctx, chatID)
			return
		}

		qLower := strings.ToLower(query)
		var sb strings.Builder
		_, _ = fmt.Fprintf(&sb, "Склад: Расходники\nПоиск по названию: %s\n\n", query)

		found := 0
		for _, it := range items {
			if !strings.Contains(strings.ToLower(it.Name), qLower) {
				continue
			}
			found++
			_, _ = fmt.Fprintf(&sb, "• %s — %d %s\n", it.Name, it.Balance, it.Unit)
		}

		if found == 0 {
			sb.WriteString("По этому запросу ничего не найдено.")
		}

		b.send(tgbotapi.NewMessage(chatID, sb.String()))
		_ = b.states.Reset(ctx, chatID)
		return
	}
}

func (b *Bot) handleCallback(ctx context.Context, cb *tgbotapi.CallbackQuery) {
	data := cb.Data
	fromChat := cb.Message.Chat.ID

	// Общая навигация
	if data == "nav:cancel" {
		_ = b.states.Reset(ctx, fromChat)
		b.editTextAndClear(fromChat, cb.Message.MessageID, "Операция отменена.")
		_ = b.answerCallback(cb, "Отменено", false)
		return
	}
	if data == "nav:back" {
		st, _ := b.states.Get(ctx, fromChat)
		switch st.State {
		case dialog.StateAwaitRole:
			_ = b.states.Set(ctx, fromChat, dialog.StateAwaitFIO, dialog.Payload{})
			b.editTextAndClear(fromChat, cb.Message.MessageID, "Измените ФИО и отправьте сообщением.")
			b.askFIO(fromChat)
		case dialog.StateAwaitConfirm:
			_ = b.states.Set(ctx, fromChat, dialog.StateAwaitRole, st.Payload)
			edit := tgbotapi.NewEditMessageTextAndMarkup(fromChat, cb.Message.MessageID, "Выберите роль:", roleKeyboard())
			b.send(edit)
		case dialog.StateAdmWhType:
			_ = b.states.Set(ctx, fromChat, dialog.StateAdmWhName, st.Payload)
			b.editTextAndClear(fromChat, cb.Message.MessageID, "Введите название склада сообщением.")
		case dialog.StateAdmWhMenu:
			b.showWarehouseMenu(fromChat, &cb.Message.MessageID)
			_ = b.states.Set(ctx, fromChat, dialog.StateAdmWhMenu, dialog.Payload{})
		case dialog.StateAdmCatMenu:
			b.showCategoryMenu(fromChat, &cb.Message.MessageID)
			_ = b.states.Set(ctx, fromChat, dialog.StateAdmCatMenu, dialog.Payload{})
		case dialog.StateAdmCatRename:
			if idAny, ok := st.Payload["cat_id"]; ok {
				id := int64(idAny.(float64))
				b.showCategoryItemMenu(ctx, fromChat, cb.Message.MessageID, id)
				_ = b.states.Set(ctx, fromChat, dialog.StateAdmCatMenu, dialog.Payload{"cat_id": id})
			} else {
				b.showCategoryMenu(fromChat, &cb.Message.MessageID)
				_ = b.states.Set(ctx, fromChat, dialog.StateAdmCatMenu, dialog.Payload{})
			}
		case dialog.StateAdmWhRename:
			if idAny, ok := st.Payload["wh_id"]; ok {
				id := int64(idAny.(float64))
				b.showWarehouseItemMenu(ctx, fromChat, cb.Message.MessageID, id)
				_ = b.states.Set(ctx, fromChat, dialog.StateAdmWhMenu, dialog.Payload{"wh_id": id})
			} else {
				b.showWarehouseMenu(fromChat, &cb.Message.MessageID)
				_ = b.states.Set(ctx, fromChat, dialog.StateAdmWhMenu, dialog.Payload{})
			}
		case dialog.StateAdmMatMenu:
			b.showMaterialMenu(fromChat, &cb.Message.MessageID)
			_ = b.states.Set(ctx, fromChat, dialog.StateAdmMatMenu, dialog.Payload{})
		case dialog.StateAdmCatName:
			b.showCategoryMenu(fromChat, &cb.Message.MessageID)
			_ = b.states.Set(ctx, fromChat, dialog.StateAdmCatMenu, dialog.Payload{})
		case dialog.StateAdmWhName:
			b.showWarehouseMenu(fromChat, &cb.Message.MessageID)
			_ = b.states.Set(ctx, fromChat, dialog.StateAdmWhMenu, dialog.Payload{})
		case dialog.StateAdmMatList:
			level, _ := st.Payload["level"].(string)

			switch level {
			case "materials":
				catID := int64(st.Payload["cat_id"].(float64))
				b.showMaterialBrandList(ctx, fromChat, cb.Message.MessageID, catID)
				_ = b.states.Set(ctx, fromChat, dialog.StateAdmMatList, dialog.Payload{
					"level":  "brands",
					"cat_id": float64(catID),
				})

			case "brands":
				b.showMaterialList(ctx, fromChat, cb.Message.MessageID)
				_ = b.states.Set(ctx, fromChat, dialog.StateAdmMatList, dialog.Payload{})

			default:
				b.showMaterialMenu(fromChat, &cb.Message.MessageID)
				_ = b.states.Set(ctx, fromChat, dialog.StateAdmMatMenu, dialog.Payload{})
			}
		case dialog.StateAdmMatItem:
			if idAny, ok := st.Payload["mat_id"]; ok {
				id := int64(idAny.(float64))
				m, _ := b.materials.GetByID(ctx, id)
				if m != nil {
					b.showMaterialListByBrand(ctx, fromChat, cb.Message.MessageID, m.CategoryID, m.Brand)
					_ = b.states.Set(ctx, fromChat, dialog.StateAdmMatList, dialog.Payload{
						"level":  "materials",
						"cat_id": float64(m.CategoryID),
						"brand":  m.Brand,
					})
					return
				}
			}

			b.showMaterialList(ctx, fromChat, cb.Message.MessageID)
			_ = b.states.Set(ctx, fromChat, dialog.StateAdmMatList, dialog.Payload{})
		case dialog.StateAdmMatUnit:
			// из выбора единицы — назад в карточку
			if idAny, ok := st.Payload["mat_id"]; ok {
				id := int64(idAny.(float64))
				b.showMaterialItemMenu(ctx, fromChat, cb.Message.MessageID, id)
				_ = b.states.Set(ctx, fromChat, dialog.StateAdmMatItem, dialog.Payload{"mat_id": id})
			} else {
				b.showMaterialMenu(fromChat, &cb.Message.MessageID)
				_ = b.states.Set(ctx, fromChat, dialog.StateAdmMatMenu, dialog.Payload{})
			}
		case dialog.StateAdmMatRename:
			// из переименования — назад в карточку
			if idAny, ok := st.Payload["mat_id"]; ok {
				id := int64(idAny.(float64))
				b.showMaterialItemMenu(ctx, fromChat, cb.Message.MessageID, id)
				_ = b.states.Set(ctx, fromChat, dialog.StateAdmMatItem, dialog.Payload{"mat_id": id})
			} else {
				b.showMaterialMenu(fromChat, &cb.Message.MessageID)
				_ = b.states.Set(ctx, fromChat, dialog.StateAdmMatMenu, dialog.Payload{})
			}
		case dialog.StateAdmMatPickCat:
			// из выбора категории при создании — назад в меню материалов
			b.showMaterialMenu(fromChat, &cb.Message.MessageID)
			_ = b.states.Set(ctx, fromChat, dialog.StateAdmMatMenu, dialog.Payload{})
		case dialog.StateAdmMatName:
			stCur, _ := b.states.Get(ctx, fromChat)
			payload := dialog.Payload{}
			if stCur != nil && stCur.Payload != nil {
				if whAny, ok := stCur.Payload["mat_wh_id"]; ok {
					payload["mat_wh_id"] = whAny
				}
			}
			// из ввода имени — назад к выбору категории
			b.showCategoryPick(ctx, fromChat, cb.Message.MessageID)
			_ = b.states.Set(ctx, fromChat, dialog.StateAdmMatPickCat, payload)
		case dialog.StateStockMenu:
			b.showStocksMenu(fromChat, &cb.Message.MessageID)
			_ = b.states.Set(ctx, fromChat, dialog.StateStockMenu, dialog.Payload{})

		case dialog.StateStockExportPickWh:
			b.showStocksMenu(fromChat, &cb.Message.MessageID)
			_ = b.states.Set(ctx, fromChat, dialog.StateStockMenu, dialog.Payload{})

		case dialog.StateStockImportFile:
			b.showStocksMenu(fromChat, &cb.Message.MessageID)
			_ = b.states.Set(ctx, fromChat, dialog.StateStockMenu, dialog.Payload{})
		case dialog.StateStockList:
			b.showStockWarehouseList(ctx, fromChat, &cb.Message.MessageID)
			_ = b.states.Set(ctx, fromChat, dialog.StateStockPickWh, dialog.Payload{})
		case dialog.StateStockItem:
			if whAny, ok := st.Payload["wh_id"]; ok {
				wh := int64(whAny.(float64))
				b.showStockMaterialList(ctx, fromChat, cb.Message.MessageID, wh)
				_ = b.states.Set(ctx, fromChat, dialog.StateStockList, dialog.Payload{"wh_id": wh})
			} else {
				b.showStockWarehouseList(ctx, fromChat, &cb.Message.MessageID)
				_ = b.states.Set(ctx, fromChat, dialog.StateStockPickWh, dialog.Payload{})
			}
		case dialog.StateStockInQty, dialog.StateStockOutQty:
			if whAny, ok := st.Payload["wh_id"]; ok {
				wh := int64(whAny.(float64))
				mat := int64(st.Payload["mat_id"].(float64))
				b.showStockItem(ctx, fromChat, cb.Message.MessageID, wh, mat)
				_ = b.states.Set(ctx, fromChat, dialog.StateStockItem, dialog.Payload{"wh_id": wh, "mat_id": mat})
			} else {
				b.showStockWarehouseList(ctx, fromChat, &cb.Message.MessageID)
				_ = b.states.Set(ctx, fromChat, dialog.StateStockPickWh, dialog.Payload{})
			}
		case dialog.StateSupPickWh, dialog.StateSupMenu:
			b.showSuppliesMenu(fromChat, &cb.Message.MessageID)
			_ = b.states.Set(ctx, fromChat, dialog.StateSupMenu, dialog.Payload{})
		case dialog.StateSupPickMat:
			b.showSuppliesPickWarehouse(ctx, fromChat, &cb.Message.MessageID)
			_ = b.states.Set(ctx, fromChat, dialog.StateSupPickWh, dialog.Payload{})
		case dialog.StateSupQty:
			b.showSuppliesPickMaterial(ctx, fromChat, cb.Message.MessageID)
			_ = b.states.Set(ctx, fromChat, dialog.StateSupPickMat, st.Payload)
		case dialog.StateSupUnitPrice:
			b.editTextWithNav(fromChat, cb.Message.MessageID, "Введите количество (число, например 250)")
			_ = b.states.Set(ctx, fromChat, dialog.StateSupQty, st.Payload)
		case dialog.StateSupConfirm:
			b.editTextWithNav(fromChat, cb.Message.MessageID, "Введите цену за единицу (руб)")
			_ = b.states.Set(ctx, fromChat, dialog.StateSupUnitPrice, st.Payload)
		case dialog.StateSupCart:
			// Возврат к редактированию последней добавленной позиции
			items := b.parseSupItems(st.Payload["items"])
			if len(items) == 0 {
				// Корзина пуста — вернём меню поставок
				_ = b.states.Set(ctx, fromChat, dialog.StateSupMenu, dialog.Payload{})
				b.showSuppliesMenu(fromChat, &cb.Message.MessageID)
				return
			}
			last := items[len(items)-1]
			// Удаляем последнюю позицию из корзины — будем вводить её заново
			items = items[:len(items)-1]

			// Собираем payload для шага ввода цены (предыдущий шаг после qty)
			payload := dialog.Payload{
				"wh_id":  st.Payload["wh_id"],
				"mat_id": last["mat_id"],
				"qty":    last["qty"],
				"items":  items,
			}
			_ = b.states.Set(ctx, fromChat, dialog.StateSupUnitPrice, payload)
			b.editTextWithNav(fromChat, cb.Message.MessageID, "Введите цену за единицу (руб)")
			return
		case dialog.StateConsQty:
			// назад к выбору помещения
			_ = b.states.Set(ctx, fromChat, dialog.StateConsPlace, st.Payload)
			kb := tgbotapi.NewInlineKeyboardMarkup(
				tgbotapi.NewInlineKeyboardRow(
					tgbotapi.NewInlineKeyboardButtonData("Общий зал", "cons:place:hall"),
					tgbotapi.NewInlineKeyboardButtonData("Кабинет", "cons:place:cabinet"),
				),
				navKeyboard(false, true).InlineKeyboard[0],
			)
			b.send(tgbotapi.NewEditMessageTextAndMarkup(fromChat, cb.Message.MessageID, "Выберите помещение:", kb))
		case dialog.StateConsCart:
			// назад к вводу количества часов/дней
			b.editTextWithNav(fromChat, cb.Message.MessageID, fmt.Sprintf("Введите количество (%s):", map[string]string{"hour": "часы", "day": "дни"}[st.Payload["unit"].(string)]))
			_ = b.states.Set(ctx, fromChat, dialog.StateConsQty, st.Payload)
		case dialog.StateConsMatSearch:
			// назад — корзина
			items := b.consParseItems(st.Payload["items"])
			_ = b.states.Set(ctx, fromChat, dialog.StateConsCart, st.Payload)
			b.showConsCart(ctx, fromChat, &cb.Message.MessageID,
				st.Payload["place"].(string),
				st.Payload["unit"].(string),
				int(st.Payload["qty"].(float64)),
				items)
		case dialog.StateConsSearchByName:
			// назад — корзина
			items := b.consParseItems(st.Payload["items"])
			_ = b.states.Set(ctx, fromChat, dialog.StateConsCart, st.Payload)
			b.showConsCart(ctx, fromChat, &cb.Message.MessageID,
				st.Payload["place"].(string),
				st.Payload["unit"].(string),
				int(st.Payload["qty"].(float64)),
				items)
		case dialog.StateConsMatPick:
			// назад — снова корзина
			items := b.consParseItems(st.Payload["items"])
			_ = b.states.Set(ctx, fromChat, dialog.StateConsCart, st.Payload)
			b.showConsCart(ctx, fromChat, &cb.Message.MessageID, st.Payload["place"].(string), st.Payload["unit"].(string), int(st.Payload["qty"].(float64)), items)
		case dialog.StateConsMatQty:
			// назад к выбору способа поиска материала
			_ = b.states.Set(ctx, fromChat, dialog.StateConsMatSearch, st.Payload)
			rows := [][]tgbotapi.InlineKeyboardButton{
				{
					tgbotapi.NewInlineKeyboardButtonData("Поиск по названию", "cons:search:name"),
				},
				{
					tgbotapi.NewInlineKeyboardButtonData("Поиск по параметрам", "cons:search:params"),
				},
				navKeyboard(true, true).InlineKeyboard[0],
			}
			kb := tgbotapi.NewInlineKeyboardMarkup(rows...)
			b.send(tgbotapi.NewEditMessageTextAndMarkup(fromChat, cb.Message.MessageID, "Как искать материал?", kb))
		case dialog.StateConsSummary:
			// назад в корзину
			items := b.consParseItems(st.Payload["items"])
			_ = b.states.Set(ctx, fromChat, dialog.StateConsCart, st.Payload)
			b.showConsCart(ctx, fromChat, &cb.Message.MessageID, st.Payload["place"].(string), st.Payload["unit"].(string), int(st.Payload["qty"].(float64)), items)

		case dialog.StateAdmSubsMenu:
			b.showSubsMenu(fromChat, &cb.Message.MessageID)
			_ = b.states.Set(ctx, fromChat, dialog.StateAdmSubsMenu, dialog.Payload{})

		case dialog.StateAdmSubsPickUser:
			b.showSubsMenu(fromChat, &cb.Message.MessageID)
			_ = b.states.Set(ctx, fromChat, dialog.StateAdmSubsMenu, dialog.Payload{})

		case dialog.StateAdmSubsPickPlaceUnit:
			b.showSubsPickUser(ctx, fromChat, cb.Message.MessageID)
			_ = b.states.Set(ctx, fromChat, dialog.StateAdmSubsPickUser, dialog.Payload{})

		case dialog.StateAdmSubsEnterQty:
			// Назад к выбору места/единицы
			if v, ok := st.Payload["uid"]; ok {
				uid := int64(v.(float64))
				b.showSubsPickPlaceUnit(fromChat, cb.Message.MessageID, uid)
				_ = b.states.Set(ctx, fromChat, dialog.StateAdmSubsPickPlaceUnit, st.Payload)
			} else {
				b.showSubsPickUser(ctx, fromChat, cb.Message.MessageID)
				_ = b.states.Set(ctx, fromChat, dialog.StateAdmSubsPickUser, dialog.Payload{})
			}

		case dialog.StateAdmSubsConfirm:
			// назад к вводу количества
			b.editTextWithNav(fromChat, cb.Message.MessageID, "Введите объём на месяц (целое число):")
			_ = b.states.Set(ctx, fromChat, dialog.StateAdmSubsEnterQty, st.Payload)

		case dialog.StateSubBuyQty:
			_ = b.states.Set(ctx, fromChat, dialog.StateSubBuyPlace, st.Payload)
			b.send(tgbotapi.NewEditMessageTextAndMarkup(fromChat, cb.Message.MessageID,
				"Выберите тип абонемента:", b.subBuyPlaceKeyboard()))
		case dialog.StateSubBuyConfirm:
			_ = b.states.Set(ctx, fromChat, dialog.StateSubBuyQty, st.Payload)
			b.editTextWithNav(fromChat, cb.Message.MessageID,
				fmt.Sprintf("Введите объём (%s):", map[string]string{"hour": "часы", "day": "дни"}[st.Payload["unit"].(string)]))
		case dialog.StatePriceMenu:
			b.showPriceMainMenu(fromChat, &cb.Message.MessageID)
			_ = b.states.Set(ctx, fromChat, dialog.StatePriceMenu, dialog.Payload{})

		case dialog.StatePriceMatMenu:
			b.showPriceMainMenu(fromChat, &cb.Message.MessageID)
			_ = b.states.Set(ctx, fromChat, dialog.StatePriceMenu, dialog.Payload{})

		case dialog.StatePriceMatExportPickWh:
			b.showPriceMatMenu(fromChat, &cb.Message.MessageID)
			_ = b.states.Set(ctx, fromChat, dialog.StatePriceMatMenu, dialog.Payload{})

		case dialog.StatePriceMatImportFile:
			b.showPriceMatMenu(fromChat, &cb.Message.MessageID)
			_ = b.states.Set(ctx, fromChat, dialog.StatePriceMatMenu, dialog.Payload{})

		case dialog.StatePriceRentMenu:
			b.showPriceMainMenu(fromChat, &cb.Message.MessageID)
			_ = b.states.Set(ctx, fromChat, dialog.StatePriceMenu, dialog.Payload{})

		case dialog.StatePriceRentImportFile:
			b.showPriceRentMenu(fromChat, &cb.Message.MessageID)
			_ = b.states.Set(ctx, fromChat, dialog.StatePriceRentMenu, dialog.Payload{})

		default:
			b.editTextAndClear(fromChat, cb.Message.MessageID, "Действие неактуально.")
		}
		_ = b.answerCallback(cb, "Назад", false)
		return
	}

	switch {
	case strings.HasPrefix(data, "role:"):
		roleStr := strings.TrimPrefix(data, "role:")
		var role users.Role
		if roleStr == "administrator" {
			role = users.RoleAdministrator
		} else {
			role = users.RoleMaster
		}
		st, _ := b.states.Get(ctx, fromChat)
		if st.State != dialog.StateAwaitRole {
			_ = b.answerCallback(cb, "Неактуально", false)
			return
		}
		fio, _ := dialog.GetString(st.Payload, "fio")
		p := st.Payload
		p["role"] = string(role)
		_ = b.states.Set(ctx, fromChat, dialog.StateAwaitConfirm, p)
		confirmText := fmt.Sprintf("Проверьте данные:\n— ФИО: %s\n— Роль: %s\n\nОтправить заявку администратору?", fio, role)
		edit := tgbotapi.NewEditMessageTextAndMarkup(fromChat, cb.Message.MessageID, confirmText, confirmKeyboard())
		b.send(edit)
		_ = b.answerCallback(cb, "Ок", false)
		return

	case data == "rq:send":
		st, _ := b.states.Get(ctx, fromChat)
		if st.State != dialog.StateAwaitConfirm {
			_ = b.answerCallback(cb, "Неактуально", false)
			return
		}
		fio, _ := dialog.GetString(st.Payload, "fio")
		roleStr, _ := dialog.GetString(st.Payload, "role")
		role := users.Role(roleStr)
		_, _ = b.users.UpsertByTelegram(ctx, cb.From.ID, role)
		b.editTextAndClear(fromChat, cb.Message.MessageID, "Заявка отправлена администратору. Ожидайте решения.")
		_ = b.states.Reset(ctx, fromChat)

		text := fmt.Sprintf(
			"Новая заявка на доступ:\n— ФИО: %s\n— Telegram: @%s (id %d)\n— Роль: %s\n\nОдобрить?",
			fio, cb.From.UserName, cb.From.ID, role,
		)
		kb := tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("✅ Одобрить", fmt.Sprintf("approve:%d:%s", cb.From.ID, role)),
				tgbotapi.NewInlineKeyboardButtonData("⛔ Отклонить", fmt.Sprintf("reject:%d", cb.From.ID)),
			),
		)
		m := tgbotapi.NewMessage(b.adminChat, text)
		m.ReplyMarkup = kb
		b.send(m)
		_ = b.answerCallback(cb, "Отправлено", false)
		return

	case strings.HasPrefix(data, "approve:"):
		if !b.isAdminID(fromChat) {
			_ = b.answerCallback(cb, "Недостаточно прав", true)
			return
		}
		parts := strings.Split(strings.TrimPrefix(data, "approve:"), ":")
		if len(parts) != 2 {
			_ = b.answerCallback(cb, "Некорректные данные", true)
			return
		}
		tgID, _ := strconv.ParseInt(parts[0], 10, 64)
		role := users.Role(parts[1])
		if _, err := b.users.Approve(ctx, tgID, role); err != nil {
			_ = b.answerCallback(cb, "Ошибка при одобрении", true)
			return
		}
		newText := cb.Message.Text + "\n\n✅ Заявка подтверждена"
		b.editTextAndClear(fromChat, cb.Message.MessageID, newText)
		b.send(tgbotapi.NewMessage(tgID, fmt.Sprintf("Заявка подтверждена, нажмите /start, чтобы обновить меню. Ваша роль: %s", role)))
		_ = b.answerCallback(cb, "Одобрено", false)
		return

	case strings.HasPrefix(data, "reject:"):
		if !b.isAdminID(fromChat) {
			_ = b.answerCallback(cb, "Недостаточно прав", true)
			return
		}
		tgID, _ := strconv.ParseInt(strings.TrimPrefix(data, "reject:"), 10, 64)
		if _, err := b.users.Reject(ctx, tgID); err != nil {
			_ = b.answerCallback(cb, "Ошибка при отклонении", true)
			return
		}
		newText := cb.Message.Text + "\n\n⛔ Заявка отклонена"
		b.editTextAndClear(fromChat, cb.Message.MessageID, newText)
		b.send(tgbotapi.NewMessage(tgID, "Заявка отклонена. Введите ФИО, чтобы подать заявку ещё раз."))
		_ = b.states.Set(ctx, tgID, dialog.StateAwaitFIO, dialog.Payload{})
		b.askFIO(tgID)
		_ = b.answerCallback(cb, "Отклонено", false)
		return

	case strings.HasPrefix(data, "subrq:approve:"):
		if fromChat != b.adminChat {
			_ = b.answerCallback(cb, "Недостаточно прав", true)
			return
		}
		rest := strings.TrimPrefix(data, "subrq:approve:")
		parts := strings.Split(rest, ":")
		// tgID : place : unit : qty : thresholdTotal
		if len(parts) != 5 {
			_ = b.answerCallback(cb, "Некорректные данные", true)
			return
		}

		tgID, err1 := strconv.ParseInt(parts[0], 10, 64)
		place := parts[1]
		unit := parts[2]
		qty, err2 := strconv.Atoi(parts[3])
		thresholdTotal, err3 := strconv.ParseFloat(parts[4], 64)
		if err1 != nil || err2 != nil || err3 != nil {
			_ = b.answerCallback(cb, "Некорректные параметры", true)
			return
		}

		u, err := b.users.GetByTelegramID(ctx, tgID)
		if err != nil || u == nil {
			_ = b.answerCallback(cb, "Мастер не найден", true)
			return
		}

		month := time.Now().Format("2006-01")
		if _, err := b.subs.AddOrCreateTotal(ctx, u.ID, place, unit, month, qty, thresholdTotal); err != nil {
			_ = b.answerCallback(cb, "Ошибка при оформлении", true)
			return
		}

		// мастеру — что абонемент оформлен
		b.send(tgbotapi.NewMessage(
			tgID,
			"Абонемент оформлен/пополнен, посмотреть свои абонементы вы можете, нажав кнопку «Мои абонементы».",
		))

		// админу — пометка в заявке
		newText := cb.Message.Text + "\n\n✅ Приобретение абонемента подтверждено."
		b.editTextAndClear(fromChat, cb.Message.MessageID, newText)
		_ = b.answerCallback(cb, "Подтверждено", false)
		return

	case strings.HasPrefix(data, "subrq:reject:"):
		if fromChat != b.adminChat {
			_ = b.answerCallback(cb, "Недостаточно прав", true)
			return
		}
		rest := strings.TrimPrefix(data, "subrq:reject:")
		tgID, err := strconv.ParseInt(rest, 10, 64)
		if err != nil {
			_ = b.answerCallback(cb, "Некорректные данные", true)
			return
		}

		// мастеру — отказ
		b.send(tgbotapi.NewMessage(
			tgID,
			"Приобретение абонемента было отклонено, возможно не прошла ваша оплата, свяжитесь с администрацией для уточнения причины.",
		))

		// админу — пометка в заявке
		newText := cb.Message.Text + "\n\n⛔ Приобретение абонемента отклонено."
		b.editTextAndClear(fromChat, cb.Message.MessageID, newText)
		_ = b.answerCallback(cb, "Отклонено", false)
		return

	/* ===== Админ-меню: склады/категории ===== */

	case data == "adm:wh:add":
		_ = b.states.Set(ctx, fromChat, dialog.StateAdmWhName, dialog.Payload{})
		b.editTextAndClear(fromChat, cb.Message.MessageID, "Введите название склада сообщением.")
		_ = b.answerCallback(cb, "Ок", false)
		return

	case data == "adm:wh:list":
		_ = b.states.Set(ctx, fromChat, dialog.StateAdmWhMenu, dialog.Payload{})
		// показываем список с кнопками-элементами
		b.showWarehouseList(ctx, fromChat, cb.Message.MessageID)
		_ = b.answerCallback(cb, "Ок", false)
		return

	case strings.HasPrefix(data, "adm:wh:menu:"):
		id, _ := strconv.ParseInt(strings.TrimPrefix(data, "adm:wh:menu:"), 10, 64)
		_ = b.states.Set(ctx, fromChat, dialog.StateAdmWhMenu, dialog.Payload{"wh_id": id})
		b.showWarehouseItemMenu(ctx, fromChat, cb.Message.MessageID, id)
		_ = b.answerCallback(cb, "Ок", false)
		return

	case strings.HasPrefix(data, "adm:wh:rn:"):
		id, _ := strconv.ParseInt(strings.TrimPrefix(data, "adm:wh:rn:"), 10, 64)
		w, _ := b.catalog.GetWarehouseByID(ctx, id)
		if w == nil {
			b.editTextAndClear(fromChat, cb.Message.MessageID, "Склад не найден")
			_ = b.answerCallback(cb, "Ошибка", true)
			return
		}
		if !w.Active {
			b.showWarehouseItemMenu(ctx, fromChat, cb.Message.MessageID, id)
			_ = b.answerCallback(cb, "Склад скрыт. Сначала включите его.", true)
			return
		}
		_ = b.states.Set(ctx, fromChat, dialog.StateAdmWhRename, dialog.Payload{"wh_id": id})
		b.editTextWithNav(fromChat, cb.Message.MessageID, "Введите новое название склада сообщением.")
		_ = b.answerCallback(cb, "Ок", false)
		return

	case strings.HasPrefix(data, "adm:wh:tg:"):
		id, _ := strconv.ParseInt(strings.TrimPrefix(data, "adm:wh:tg:"), 10, 64)
		w, _ := b.catalog.GetWarehouseByID(ctx, id)
		if w == nil {
			b.editTextAndClear(fromChat, cb.Message.MessageID, "Склад не найден")
			_ = b.answerCallback(cb, "Ошибка", true)
			return
		}
		_, err := b.catalog.SetWarehouseActive(ctx, id, !w.Active)
		if err != nil {
			_ = b.answerCallback(cb, "Ошибка", true)
			return
		}
		// перерисовываем меню элемента
		b.showWarehouseItemMenu(ctx, fromChat, cb.Message.MessageID, id)
		_ = b.answerCallback(cb, "Готово", false)
		return

	case strings.HasPrefix(data, "adm:wh:type:"):
		// выбор типа при создании
		st, _ := b.states.Get(ctx, fromChat)
		if st.State != dialog.StateAdmWhType {
			_ = b.answerCallback(cb, "Неактуально", false)
			return
		}
		whName, _ := dialog.GetString(st.Payload, "wh_name")
		tStr := strings.TrimPrefix(data, "adm:wh:type:")
		var t catalog.WarehouseType
		if tStr == "client_service" {
			t = catalog.WHTClientService
		} else {
			t = catalog.WHTConsumables
		}

		if _, err := b.catalog.CreateWarehouse(ctx, whName, t); err != nil {
			_ = b.answerCallback(cb, "Ошибка создания склада", true)
			return
		}
		// подтверждение и возврат в меню «Склады»
		b.editTextAndClear(fromChat, cb.Message.MessageID, fmt.Sprintf("Склад «%s» создан (%s).", whName, t))
		_ = b.states.Set(ctx, fromChat, dialog.StateAdmWhMenu, dialog.Payload{})
		b.showWarehouseMenu(fromChat, nil)
		_ = b.answerCallback(cb, "Создано", false)
		return

	case data == "adm:cat:add":
		_ = b.states.Set(ctx, fromChat, dialog.StateAdmCatName, dialog.Payload{})
		b.editTextWithNav(fromChat, cb.Message.MessageID, "Введите название категории сообщением.")
		_ = b.answerCallback(cb, "Ок", false)
		return

	case data == "adm:cat:list":
		_ = b.states.Set(ctx, fromChat, dialog.StateAdmCatMenu, dialog.Payload{})
		b.showCategoryList(ctx, fromChat, cb.Message.MessageID)
		_ = b.answerCallback(cb, "Ок", false)
		return

	case strings.HasPrefix(data, "adm:cat:menu:"):
		id, _ := strconv.ParseInt(strings.TrimPrefix(data, "adm:cat:menu:"), 10, 64)
		_ = b.states.Set(ctx, fromChat, dialog.StateAdmCatMenu, dialog.Payload{"cat_id": id})
		b.showCategoryItemMenu(ctx, fromChat, cb.Message.MessageID, id)
		_ = b.answerCallback(cb, "Ок", false)
		return

	case strings.HasPrefix(data, "adm:cat:rn:"):
		id, _ := strconv.ParseInt(strings.TrimPrefix(data, "adm:cat:rn:"), 10, 64)
		c, _ := b.catalog.GetCategoryByID(ctx, id)
		if c == nil {
			b.editTextAndClear(fromChat, cb.Message.MessageID, "Категория не найдена")
			_ = b.answerCallback(cb, "Ошибка", true)
			return
		}
		if !c.Active {
			b.showCategoryItemMenu(ctx, fromChat, cb.Message.MessageID, id)
			_ = b.answerCallback(cb, "Категория скрыта. Сначала включите её.", true)
			return
		}
		_ = b.states.Set(ctx, fromChat, dialog.StateAdmCatRename, dialog.Payload{"cat_id": id})
		b.editTextWithNav(fromChat, cb.Message.MessageID, "Введите новое название категории сообщением.")
		_ = b.answerCallback(cb, "Ок", false)
		return

	case strings.HasPrefix(data, "adm:cat:tg:"):
		id, _ := strconv.ParseInt(strings.TrimPrefix(data, "adm:cat:tg:"), 10, 64)
		c, _ := b.catalog.GetCategoryByID(ctx, id)
		if c == nil {
			b.editTextAndClear(fromChat, cb.Message.MessageID, "Категория не найдена")
			_ = b.answerCallback(cb, "Ошибка", true)
			return
		}
		_, err := b.catalog.SetCategoryActive(ctx, id, !c.Active)
		if err != nil {
			_ = b.answerCallback(cb, "Ошибка", true)
			return
		}
		b.showCategoryItemMenu(ctx, fromChat, cb.Message.MessageID, id)
		_ = b.answerCallback(cb, "Готово", false)
		return

	case data == "adm:mat:add":
		// сначала выбираем склад
		if err := b.states.Set(ctx, fromChat, dialog.StateAdmMatPickWarehouse, dialog.Payload{}); err != nil {
			b.editTextAndClear(fromChat, cb.Message.MessageID, "Ошибка: не удалось начать создание материала")
			_ = b.answerCallback(cb, "Ошибка", true)
			return
		}
		b.showMaterialWarehousePicker(fromChat, cb.Message.MessageID)
		_ = b.answerCallback(cb, "Ок", false)
		return

	case strings.HasPrefix(data, "adm:mat:wh:"):
		whIDStr := strings.TrimPrefix(data, "adm:mat:wh:")
		whID, err := strconv.ParseInt(whIDStr, 10, 64)
		if err != nil {
			_ = b.answerCallback(cb, "Неверный склад", true)
			return
		}

		// сохраним wh_id в payload, но уже перейдём к следующему шагу — выбору категории
		if err := b.states.Set(ctx, fromChat, dialog.StateAdmMatPickCat, dialog.Payload{
			"mat_wh_id": whID,
		}); err != nil {
			b.editTextAndClear(fromChat, cb.Message.MessageID, "Ошибка: не удалось сохранить выбор склада")
			_ = b.answerCallback(cb, "Ошибка", true)
			return
		}

		b.showCategoryPick(ctx, fromChat, cb.Message.MessageID)

		_ = b.answerCallback(cb, "Склад выбран", false)
		return

	case data == "noop":
		_ = b.answerCallback(cb, "", false)
		return

	case data == "adm:mat:list":
		_ = b.states.Set(ctx, fromChat, dialog.StateAdmMatList, dialog.Payload{})
		b.showMaterialList(ctx, fromChat, cb.Message.MessageID)
		_ = b.answerCallback(cb, "Ок", false)
		return

	case strings.HasPrefix(data, "adm:mat:list:cat:"):
		cid, err := strconv.ParseInt(strings.TrimPrefix(data, "adm:mat:list:cat:"), 10, 64)
		if err != nil {
			_ = b.answerCallback(cb, "Некорректная категория", true)
			return
		}

		_ = b.states.Set(ctx, fromChat, dialog.StateAdmMatList, dialog.Payload{
			"level":  "brands",
			"cat_id": float64(cid),
		})

		b.showMaterialBrandList(ctx, fromChat, cb.Message.MessageID, cid)
		_ = b.answerCallback(cb, "Ок", false)
		return

	case strings.HasPrefix(data, "adm:mat:list:brand:"):
		tail := strings.TrimPrefix(data, "adm:mat:list:brand:")
		parts := strings.SplitN(tail, ":", 2)
		if len(parts) != 2 {
			_ = b.answerCallback(cb, "Некорректные данные", true)
			return
		}

		cid, err := strconv.ParseInt(parts[0], 10, 64)
		if err != nil {
			_ = b.answerCallback(cb, "Некорректная категория", true)
			return
		}

		decoded, err := base64.StdEncoding.DecodeString(parts[1])
		if err != nil {
			_ = b.answerCallback(cb, "Некорректный бренд", true)
			return
		}

		brand := string(decoded)

		_ = b.states.Set(ctx, fromChat, dialog.StateAdmMatList, dialog.Payload{
			"level":  "materials",
			"cat_id": float64(cid),
			"brand":  brand,
		})

		b.showMaterialListByBrand(ctx, fromChat, cb.Message.MessageID, cid, brand)
		_ = b.answerCallback(cb, "Ок", false)
		return

	case strings.HasPrefix(data, "adm:mat:menu:"):
		id, _ := strconv.ParseInt(strings.TrimPrefix(data, "adm:mat:menu:"), 10, 64)
		_ = b.states.Set(ctx, fromChat, dialog.StateAdmMatItem, dialog.Payload{"mat_id": id})
		b.showMaterialItemMenu(ctx, fromChat, cb.Message.MessageID, id)
		_ = b.answerCallback(cb, "Ок", false)
		return

	case strings.HasPrefix(data, "adm:mat:pickcat:"):
		cid, _ := strconv.ParseInt(strings.TrimPrefix(data, "adm:mat:pickcat:"), 10, 64)

		// достаём текущий payload, чтобы не потерять mat_wh_id
		stCur, _ := b.states.Get(ctx, fromChat)
		p := dialog.Payload{
			"cat_id": float64(cid),
		}
		if stCur != nil && stCur.Payload != nil {
			if whAny, ok := stCur.Payload["mat_wh_id"]; ok {
				p["mat_wh_id"] = whAny
			}
		}

		_ = b.states.Set(ctx, fromChat, dialog.StateAdmMatBrand, p)

		b.showMaterialBrandPick(ctx, fromChat, cb.Message.MessageID, cid)
		_ = b.answerCallback(cb, "Ок", false)
		return

	case strings.HasPrefix(data, "adm:mat:brand:new:"):
		cid, _ := strconv.ParseInt(strings.TrimPrefix(data, "adm:mat:brand:new:"), 10, 64)

		stCur, _ := b.states.Get(ctx, fromChat)
		p := dialog.Payload{
			"cat_id": float64(cid),
		}
		if stCur != nil && stCur.Payload != nil {
			if whAny, ok := stCur.Payload["mat_wh_id"]; ok {
				p["mat_wh_id"] = whAny
			}
		}

		_ = b.states.Set(ctx, fromChat, dialog.StateAdmMatBrand, p)
		b.editTextWithNav(fromChat, cb.Message.MessageID,
			"Введите название бренда сообщением (или «-» чтобы оставить без бренда).")
		_ = b.answerCallback(cb, "Ок", false)
		return

	case strings.HasPrefix(data, "adm:mat:brand:"):
		// формат: adm:mat:brand:<catID>:<b64(brand)>
		tail := strings.TrimPrefix(data, "adm:mat:brand:")
		parts := strings.SplitN(tail, ":", 2)
		if len(parts) != 2 {
			_ = b.answerCallback(cb, "Некорректные данные", true)
			return
		}
		cid, _ := strconv.ParseInt(parts[0], 10, 64)
		decoded, err := base64.StdEncoding.DecodeString(parts[1])
		if err != nil {
			_ = b.answerCallback(cb, "Некорректные данные", true)
			return
		}
		brand := string(decoded)

		stCur, _ := b.states.Get(ctx, fromChat)
		p := dialog.Payload{
			"cat_id": float64(cid),
			"brand":  brand,
		}
		if stCur != nil && stCur.Payload != nil {
			if whAny, ok := stCur.Payload["mat_wh_id"]; ok {
				p["mat_wh_id"] = whAny
			}
		}

		// сразу переходим к вводу названия материала
		_ = b.states.Set(ctx, fromChat, dialog.StateAdmMatName, p)
		b.editTextWithNav(
			fromChat,
			cb.Message.MessageID,
			fmt.Sprintf("Выбран бренд: %s\n\nВведите название материала сообщением.", brand),
		)
		_ = b.answerCallback(cb, "Ок", false)
		return

	case strings.HasPrefix(data, "adm:mat:rn:"):
		id, _ := strconv.ParseInt(strings.TrimPrefix(data, "adm:mat:rn:"), 10, 64)
		m, _ := b.materials.GetByID(ctx, id)
		if m == nil {
			b.editTextAndClear(fromChat, cb.Message.MessageID, "Материал не найден")
			_ = b.answerCallback(cb, "Ошибка", true)
			return
		}
		if !m.Active {
			b.showMaterialItemMenu(ctx, fromChat, cb.Message.MessageID, id)
			_ = b.answerCallback(cb, "Материал скрыт. Сначала включите его.", true)
			return
		}
		_ = b.states.Set(ctx, fromChat, dialog.StateAdmMatRename, dialog.Payload{"mat_id": id})
		b.editTextWithNav(fromChat, cb.Message.MessageID, "Введите новое название материала сообщением.")
		_ = b.answerCallback(cb, "Ок", false)
		return

	case strings.HasPrefix(data, "adm:mat:tg:"):
		id, _ := strconv.ParseInt(strings.TrimPrefix(data, "adm:mat:tg:"), 10, 64)
		m, _ := b.materials.GetByID(ctx, id)
		if m == nil {
			b.editTextAndClear(fromChat, cb.Message.MessageID, "Материал не найден")
			_ = b.answerCallback(cb, "Ошибка", true)
			return
		}
		_, err := b.materials.SetActive(ctx, id, !m.Active)
		if err != nil {
			_ = b.answerCallback(cb, "Ошибка", true)
			return
		}
		b.showMaterialItemMenu(ctx, fromChat, cb.Message.MessageID, id)
		_ = b.answerCallback(cb, "Готово", false)
		return

	case strings.HasPrefix(data, "adm:mat:unit:set:"):
		// формат: adm:mat:unit:set:<id>:<unit>
		payload := strings.TrimPrefix(data, "adm:mat:unit:set:")
		parts := strings.SplitN(payload, ":", 2)
		if len(parts) != 2 {
			_ = b.answerCallback(cb, "Некорректные данные", true)
			return
		}
		id, err := strconv.ParseInt(parts[0], 10, 64)
		if err != nil || id <= 0 {
			_ = b.answerCallback(cb, "Некорректный ID", true)
			return
		}
		unit := materials.Unit(parts[1])

		if _, err := b.materials.UpdateUnit(ctx, id, unit); err != nil {
			_ = b.answerCallback(cb, "Ошибка", true)
			return
		}
		// Показать карточку и зафиксировать состояние, чтобы Back вернул в неё
		b.showMaterialItemMenu(ctx, fromChat, cb.Message.MessageID, id)
		_ = b.states.Set(ctx, fromChat, dialog.StateAdmMatItem, dialog.Payload{"mat_id": id})
		_ = b.answerCallback(cb, "Обновлено", false)
		return

	case strings.HasPrefix(data, "adm:mat:unit:"):
		tail := strings.TrimPrefix(data, "adm:mat:unit:")
		if strings.HasPrefix(tail, "set:") {
			// этот колбэк обрабатывается в кейсе выше
			return
		}
		id, err := strconv.ParseInt(tail, 10, 64)
		if err != nil || id <= 0 {
			_ = b.answerCallback(cb, "Некорректные данные", true)
			return
		}
		_ = b.states.Set(ctx, fromChat, dialog.StateAdmMatUnit, dialog.Payload{"mat_id": id})
		kb := b.unitKeyboard(id)
		edit := tgbotapi.NewEditMessageTextAndMarkup(fromChat, cb.Message.MessageID, "Выберите единицу измерения:", kb)
		b.send(edit)
		_ = b.answerCallback(cb, "Ок", false)
		return

	case data == "adm:subs:add":
		_ = b.states.Set(ctx, fromChat, dialog.StateAdmSubsPickUser, dialog.Payload{})
		b.showSubsPickUser(ctx, fromChat, cb.Message.MessageID)
		_ = b.answerCallback(cb, "Ок", false)
		return

	case strings.HasPrefix(data, "adm:subs:user:"):
		uid, _ := strconv.ParseInt(strings.TrimPrefix(data, "adm:subs:user:"), 10, 64)
		_ = b.states.Set(ctx, fromChat, dialog.StateAdmSubsPickPlaceUnit, dialog.Payload{"uid": float64(uid)})
		b.showSubsPickPlaceUnit(fromChat, cb.Message.MessageID, uid)
		_ = b.answerCallback(cb, "Ок", false)
		return

	case strings.HasPrefix(data, "adm:subs:pu:"):
		// формат: adm:subs:pu:<uid>:<place>:<unit>
		parts := strings.Split(strings.TrimPrefix(data, "adm:subs:pu:"), ":")
		if len(parts) != 3 {
			_ = b.answerCallback(cb, "Некорректные данные", true)
			return
		}
		uid, _ := strconv.ParseInt(parts[0], 10, 64)
		place := parts[1] // "hall"|"cabinet"
		unit := parts[2]  // "hour"|"day"
		_ = b.states.Set(ctx, fromChat, dialog.StateAdmSubsEnterQty, dialog.Payload{
			"uid": float64(uid), "place": place, "unit": unit,
		})
		b.editTextWithNav(fromChat, cb.Message.MessageID, "Введите объём на месяц (целое число):")
		_ = b.answerCallback(cb, "Ок", false)
		return

	case data == "adm:subs:confirm":
		st, _ := b.states.Get(ctx, fromChat)
		uid := int64(st.Payload["uid"].(float64))
		place := st.Payload["place"].(string)
		unit := st.Payload["unit"].(string)
		total := int(st.Payload["total"].(float64))
		month := time.Now().Format("2006-01")

		if _, err := b.subs.CreateOrSetTotal(ctx, uid, place, unit, month, total); err != nil {
			b.editTextAndClear(fromChat, cb.Message.MessageID, "Ошибка сохранения абонемента")
			_ = b.answerCallback(cb, "Ошибка", true)
			return
		}

		b.editTextAndClear(fromChat, cb.Message.MessageID, "Абонемент сохранён.")
		_ = b.states.Set(ctx, fromChat, dialog.StateAdmSubsMenu, dialog.Payload{})
		b.showSubsMenu(fromChat, nil)
		_ = b.answerCallback(cb, "Готово", false)
		return

		// Остатки: экспорт / импорт
	case data == "stock:export":
		b.clearPrevStep(ctx, fromChat)

		_ = b.states.Set(ctx, fromChat, dialog.StateStockExportPickWh, dialog.Payload{})
		b.showStockExportPickWarehouse(ctx, fromChat, &cb.Message.MessageID)
		_ = b.answerCallback(cb, "Ок", false)
		return

	case data == "stock:import":
		_ = b.states.Set(ctx, fromChat, dialog.StateStockImportFile, dialog.Payload{})
		b.editTextWithNav(fromChat, cb.Message.MessageID,
			"Загрузите Excel-файл с остатками (тот, что вы выгрузили через «Выгрузить остатки» и отредактировали колонку qty).")
		_ = b.answerCallback(cb, "Ок", false)
		return

	case strings.HasPrefix(data, "stock:expwh:"):
		whID, _ := strconv.ParseInt(strings.TrimPrefix(data, "stock:expwh:"), 10, 64)
		b.exportWarehouseStocksExcel(ctx, fromChat, cb.Message.MessageID, whID)
		_ = b.answerCallback(cb, "Файл сформирован", false)
		return

		// Просмотр остатков мастером (склад «Расходники»)
	case data == "mstock:byname":
		u, _ := b.users.GetByTelegramID(ctx, cb.From.ID)
		if u == nil || u.Role != users.RoleMaster || u.Status != users.StatusApproved {
			_ = b.answerCallback(cb, "Нет доступа", true)
			return
		}

		if _, err := b.getConsumablesWarehouseID(ctx); err != nil {
			b.editTextAndClear(fromChat, cb.Message.MessageID, "Склад «Расходники» не найден. Обратитесь к администратору.")
			_ = b.answerCallback(cb, "Ошибка", true)
			return
		}

		// ждём текст от мастера
		_ = b.states.Set(ctx, fromChat, dialog.StateMasterStockSearchByName, dialog.Payload{})
		b.editTextWithNav(fromChat, cb.Message.MessageID,
			"Введите часть названия материала для поиска по складу «Расходники».")
		_ = b.answerCallback(cb, "Ок", false)
		return

	case data == "mstock:bycat":
		u, _ := b.users.GetByTelegramID(ctx, cb.From.ID)
		if u == nil || u.Role != users.RoleMaster || u.Status != users.StatusApproved {
			_ = b.answerCallback(cb, "Нет доступа", true)
			return
		}

		whID, err := b.getConsumablesWarehouseID(ctx)
		if err != nil {
			b.editTextAndClear(fromChat, cb.Message.MessageID, "Склад «Расходники» не найден. Обратитесь к администратору.")
			_ = b.answerCallback(cb, "Ошибка", true)
			return
		}

		cats, err := b.catalog.ListCategories(ctx)
		if err != nil || len(cats) == 0 {
			b.editTextAndClear(fromChat, cb.Message.MessageID, "Не удалось загрузить категории материалов.")
			_ = b.answerCallback(cb, "Ошибка", true)
			return
		}

		rows := make([][]tgbotapi.InlineKeyboardButton, 0, len(cats)+1)
		for _, c := range cats {
			rows = append(rows, tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData(c.Name, fmt.Sprintf("mstock:cat:%d", c.ID)),
			))
		}
		rows = append(rows, navKeyboard(false, true).InlineKeyboard[0])

		kb := tgbotapi.NewInlineKeyboardMarkup(rows...)
		text := fmt.Sprintf("Склад: Расходники (ID %d)\nВыберите категорию:", whID)
		b.send(tgbotapi.NewEditMessageTextAndMarkup(fromChat, cb.Message.MessageID, text, kb))

		_ = b.answerCallback(cb, "Ок", false)
		return

	case strings.HasPrefix(data, "mstock:cat:"):
		u, _ := b.users.GetByTelegramID(ctx, cb.From.ID)
		if u == nil || u.Role != users.RoleMaster || u.Status != users.StatusApproved {
			_ = b.answerCallback(cb, "Нет доступа", true)
			return
		}

		whID, err := b.getConsumablesWarehouseID(ctx)
		if err != nil {
			b.editTextAndClear(fromChat, cb.Message.MessageID, "Склад «Расходники» не найден. Обратитесь к администратору.")
			_ = b.answerCallback(cb, "Ошибка", true)
			return
		}

		catStr := strings.TrimPrefix(data, "mstock:cat:")
		catID, err := strconv.ParseInt(catStr, 10, 64)
		if err != nil {
			b.editTextAndClear(fromChat, cb.Message.MessageID, "Некорректная категория.")
			_ = b.answerCallback(cb, "Ошибка", true)
			return
		}

		items, err := b.materials.ListWithBalanceByWarehouse(ctx, whID)
		if err != nil {
			b.editTextAndClear(fromChat, cb.Message.MessageID, "Ошибка загрузки материалов.")
			_ = b.answerCallback(cb, "Ошибка", true)
			return
		}

		cats, err := b.catalog.ListCategories(ctx)
		if err != nil {
			b.editTextAndClear(fromChat, cb.Message.MessageID, "Ошибка загрузки категорий.")
			_ = b.answerCallback(cb, "Ошибка", true)
			return
		}
		catName := fmt.Sprintf("ID %d", catID)
		for _, c := range cats {
			if c.ID == catID {
				catName = c.Name
				break
			}
		}

		var sb strings.Builder
		_, _ = fmt.Fprintf(&sb, "Склад: Расходники\nКатегория: %s\n\n", catName)

		found := 0
		for _, it := range items {
			if it.CategoryID != catID {
				continue
			}
			found++
			_, _ = fmt.Fprintf(&sb, "• %s — %d %s\n", it.Name, it.Balance, it.Unit)
		}
		if found == 0 {
			sb.WriteString("В этой категории на складе нет материалов.")
		}

		b.editTextAndClear(fromChat, cb.Message.MessageID, sb.String())
		_ = b.answerCallback(cb, "Готово", false)
		return

		// Остатки: выбор склада -> список
	case strings.HasPrefix(data, "st:list:"):
		whID, _ := strconv.ParseInt(strings.TrimPrefix(data, "st:list:"), 10, 64)
		_ = b.states.Set(ctx, fromChat, dialog.StateStockList, dialog.Payload{"wh_id": whID})
		b.showStockMaterialList(ctx, fromChat, cb.Message.MessageID, whID)
		_ = b.answerCallback(cb, "Ок", false)
		return

		// Выбор строки из списка -> карточка
	case strings.HasPrefix(data, "st:item:"):
		parts := strings.Split(strings.TrimPrefix(data, "st:item:"), ":")
		if len(parts) != 2 {
			_ = b.answerCallback(cb, "Некорректные данные", true)
			return
		}
		whID, _ := strconv.ParseInt(parts[0], 10, 64)
		matID, _ := strconv.ParseInt(parts[1], 10, 64)
		_ = b.states.Set(ctx, fromChat, dialog.StateStockItem, dialog.Payload{"wh_id": whID, "mat_id": matID})
		b.showStockItem(ctx, fromChat, cb.Message.MessageID, whID, matID)
		_ = b.answerCallback(cb, "Ок", false)
		return

		// Приход: запрос количества
	case strings.HasPrefix(data, "st:in:"):
		parts := strings.Split(strings.TrimPrefix(data, "st:in:"), ":")
		whID, _ := strconv.ParseInt(parts[0], 10, 64)
		matID, _ := strconv.ParseInt(parts[1], 10, 64)
		_ = b.states.Set(ctx, fromChat, dialog.StateStockInQty, dialog.Payload{"wh_id": whID, "mat_id": matID})
		b.editTextWithNav(fromChat, cb.Message.MessageID, "Введите количество для прихода (число, например 10.5)")
		_ = b.answerCallback(cb, "Ок", false)
		return

		// Списание: запрос количества
	case strings.HasPrefix(data, "st:out:"):
		parts := strings.Split(strings.TrimPrefix(data, "st:out:"), ":")
		whID, _ := strconv.ParseInt(parts[0], 10, 64)
		matID, _ := strconv.ParseInt(parts[1], 10, 64)
		_ = b.states.Set(ctx, fromChat, dialog.StateStockOutQty, dialog.Payload{"wh_id": whID, "mat_id": matID})
		b.editTextWithNav(fromChat, cb.Message.MessageID, "Введите количество для списания (число, например 3)")
		_ = b.answerCallback(cb, "Ок", false)
		return

		// Поставки: выгрузка / загрузка / журнал
	case data == "sup:export":
		b.clearPrevStep(ctx, fromChat)

		_ = b.states.Set(ctx, fromChat, dialog.StateSupExportPickWh, dialog.Payload{})
		b.showSuppliesExportPickWarehouse(ctx, fromChat, &cb.Message.MessageID)
		_ = b.answerCallback(cb, "Ок", false)
		return

	case data == "sup:import":
		// сначала спрашиваем комментарий (поставщика), затем ожидаем файл
		_ = b.states.Set(ctx, fromChat, dialog.StateSupImportComment, dialog.Payload{})
		b.editTextWithNav(fromChat, cb.Message.MessageID,
			"Введите комментарий к поставке (например, поставщик). Если комментарий не нужен, отправьте «-».")
		_ = b.answerCallback(cb, "Ок", false)
		return

	case data == "sup:add":
		b.clearPrevStep(ctx, fromChat)

		_ = b.states.Set(ctx, fromChat, dialog.StateSupPickWh, dialog.Payload{})
		b.showSuppliesPickWarehouse(ctx, fromChat, &cb.Message.MessageID)
		_ = b.answerCallback(cb, "Ок", false)
		return

	case data == "sup:additem":
		b.clearPrevStep(ctx, fromChat)

		st, _ := b.states.Get(ctx, fromChat)
		_ = b.states.Set(ctx, fromChat, dialog.StateSupPickMat, st.Payload) // wh_id и items остаются
		b.showSuppliesPickMaterial(ctx, fromChat, cb.Message.MessageID)
		_ = b.answerCallback(cb, "Ок", false)
		return

	case strings.HasPrefix(data, "sup:wh:"):
		whID, _ := strconv.ParseInt(strings.TrimPrefix(data, "sup:wh:"), 10, 64)
		_ = b.states.Set(ctx, fromChat, dialog.StateSupPickMat, dialog.Payload{"wh_id": whID})
		b.showSuppliesPickMaterial(ctx, fromChat, cb.Message.MessageID)
		_ = b.answerCallback(cb, "Ок", false)
		return

	case strings.HasPrefix(data, "sup:expwh:"):
		whID, _ := strconv.ParseInt(strings.TrimPrefix(data, "sup:expwh:"), 10, 64)
		b.exportWarehouseMaterialsExcel(ctx, fromChat, cb.Message.MessageID, whID)
		_ = b.answerCallback(cb, "Файл сформирован", false)
		return

	case strings.HasPrefix(data, "sup:mat:"):
		matID, _ := strconv.ParseInt(strings.TrimPrefix(data, "sup:mat:"), 10, 64)
		st, _ := b.states.Get(ctx, fromChat)
		wh := int64(st.Payload["wh_id"].(float64))
		// ВАЖНО: переносим корзину, иначе она теряется
		payload := dialog.Payload{
			"wh_id":  wh,
			"mat_id": matID,
		}
		if items, ok := st.Payload["items"]; ok {
			payload["items"] = items
		}
		_ = b.states.Set(ctx, fromChat, dialog.StateSupQty, payload)
		b.editTextWithNav(fromChat, cb.Message.MessageID, "Введите количество (число, например 250)")
		b.saveLastStep(ctx, fromChat, dialog.StateSupQty, payload, cb.Message.MessageID)
		_ = b.answerCallback(cb, "Ок", false)
		return

	case data == "sup:list":
		_ = b.states.Set(ctx, fromChat, dialog.StateSupJournalFrom, dialog.Payload{})
		b.editTextWithNav(fromChat, cb.Message.MessageID,
			"Журнал поставок.\nВведите дату начала периода в формате ДД.ММ.ГГГГ (например, 01.11.2025).")
		_ = b.answerCallback(cb, "Ок", false)
		return

	case strings.HasPrefix(data, "sup:journal:"):
		idStr := strings.TrimPrefix(data, "sup:journal:")
		supplyID, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil || supplyID <= 0 {
			_ = b.answerCallback(cb, "Некорректный идентификатор поставки", true)
			return
		}

		b.exportSupplyExcel(ctx, fromChat, cb.Message.MessageID, supplyID)
		_ = b.answerCallback(cb, "Файл сформирован", false)
		return

	case data == "sup:confirm":
		st, _ := b.states.Get(ctx, fromChat)
		wh := int64(st.Payload["wh_id"].(float64))
		items := b.parseSupItems(st.Payload["items"])
		if len(items) == 0 {
			b.editTextAndClear(fromChat, cb.Message.MessageID, "Корзина пуста. Добавьте хотя бы одну позицию.")
			_ = b.answerCallback(cb, "Пусто", true)
			return
		}
		u, err := b.users.GetByTelegramID(ctx, cb.From.ID)
		if err != nil || u == nil {
			b.editTextAndClear(fromChat, cb.Message.MessageID, "Пользователь не найден")
			_ = b.answerCallback(cb, "Ошибка", true)
			return
		}

		// один batch на всю ручную поставку
		batchID, err := b.inventory.CreateSupplyBatch(ctx, u.ID, wh, "")
		if err != nil {
			b.editTextAndClear(fromChat, cb.Message.MessageID, "Не удалось создать запись поставки.")
			_ = b.answerCallback(cb, "Ошибка", true)
			return
		}

		// Проводим каждую позицию одной транзакцией на позицию
		for _, it := range items {
			mat := int64(it["mat_id"].(float64))
			qty := int64(it["qty"].(float64))
			price := it["price"].(float64)
			if err := b.inventory.ReceiveWithCost(ctx, u.ID, wh, mat, float64(qty), price, "supply", "", batchID); err != nil {
				b.editTextAndClear(fromChat, cb.Message.MessageID, "Ошибка приёмки: "+err.Error())
				_ = b.answerCallback(cb, "Ошибка", true)
				return
			}
			// Обновим цену на последнюю закупочную
			_, _ = b.materials.UpdatePrice(ctx, mat, price)
		}

		// Очистим корзину и вернёмся в меню поставок
		b.editTextAndClear(fromChat, cb.Message.MessageID, "Поставка проведена.")
		_ = b.states.Set(ctx, fromChat, dialog.StateSupMenu, dialog.Payload{})
		b.showSuppliesMenu(fromChat, nil)
		_ = b.answerCallback(cb, "Готово", false)
		return

		// Установка цен
	case data == "price:mat:menu":
		_ = b.states.Set(ctx, fromChat, dialog.StatePriceMatMenu, dialog.Payload{})
		b.showPriceMatMenu(fromChat, &cb.Message.MessageID)
		_ = b.answerCallback(cb, "Ок", false)
		return

	case data == "price:mat:export":
		_ = b.states.Set(ctx, fromChat, dialog.StatePriceMatExportPickWh, dialog.Payload{})
		b.showPriceMatExportPickWarehouse(ctx, fromChat, &cb.Message.MessageID)
		_ = b.answerCallback(cb, "Ок", false)
		return

	case strings.HasPrefix(data, "price:mat:expwh:"):
		whID, _ := strconv.ParseInt(strings.TrimPrefix(data, "price:mat:expwh:"), 10, 64)
		b.exportWarehouseMaterialPricesExcel(ctx, fromChat, cb.Message.MessageID, whID)
		_ = b.answerCallback(cb, "Файл сформирован", false)
		return

	case data == "price:mat:import":
		_ = b.states.Set(ctx, fromChat, dialog.StatePriceMatImportFile, dialog.Payload{})
		b.editTextWithNav(fromChat, cb.Message.MessageID,
			"Загрузите Excel-файл с ценами материалов (тот, что вы выгрузили через «Выгрузить цены на материалы» и отредактировали колонку price_per_unit).")
		_ = b.answerCallback(cb, "Ок", false)
		return

		// Установка цен: тарифы аренды
	case data == "price:rent:menu":
		_ = b.states.Set(ctx, fromChat, dialog.StatePriceRentMenu, dialog.Payload{})
		b.showPriceRentMenu(fromChat, &cb.Message.MessageID)
		_ = b.answerCallback(cb, "Ок", false)
		return

	case data == "price:rent:export":
		b.exportRentRatesExcel(ctx, fromChat, cb.Message.MessageID)
		_ = b.answerCallback(cb, "Файл сформирован", false)
		return

	case data == "price:rent:import":
		_ = b.states.Set(ctx, fromChat, dialog.StatePriceRentImportFile, dialog.Payload{})
		b.editTextWithNav(fromChat, cb.Message.MessageID,
			"Загрузите Excel-файл с тарифами аренды (тот, что вы выгрузили через «Выгрузить цены на аренду» и изменили threshold/price_with/price_own).")
		_ = b.answerCallback(cb, "Ок", false)
		return

		// Расход/Аренда: пропустить ввод комментария
	case data == "cons:comment_skip":
		// пустой комментарий, сразу переходим к выбору помещения
		payload := dialog.Payload{"comment": ""}
		_ = b.states.Set(ctx, fromChat, dialog.StateConsPlace, payload)

		kb := tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("Общий зал", "cons:place:hall"),
				tgbotapi.NewInlineKeyboardButtonData("Кабинет", "cons:place:cabinet"),
			),
			navKeyboard(false, true).InlineKeyboard[0],
		)
		msg := tgbotapi.NewMessage(fromChat, "Выберите помещение:")
		msg.ReplyMarkup = kb
		b.send(msg)
		_ = b.answerCallback(cb, "Ок", false)
		return

		// Выбор помещения
	case strings.HasPrefix(data, "cons:place:"):
		place := strings.TrimPrefix(data, "cons:place:")
		unit := "hour"
		if place == "cabinet" {
			unit = "day"
		}

		st, _ := b.states.Get(ctx, fromChat)

		withSub := false
		comment := ""

		if st != nil && st.Payload != nil {
			if v, ok := st.Payload["with_sub"].(bool); ok {
				withSub = v
			}
			if c, ok := st.Payload["comment"].(string); ok {
				comment = c
			}
		}

		payload := dialog.Payload{
			"place":    place,
			"unit":     unit,
			"with_sub": withSub,
		}
		if comment != "" {
			payload["comment"] = comment
		}

		_ = b.states.Set(ctx, fromChat, dialog.StateConsQty, payload)
		b.editTextWithNav(fromChat, cb.Message.MessageID,
			fmt.Sprintf("Введите количество (%s):", map[string]string{"hour": "часы", "day": "дни"}[unit]))
		_ = b.answerCallback(cb, "Ок", false)
		return

		// Добавить материал
	case data == "cons:additem":
		st, _ := b.states.Get(ctx, fromChat)
		_ = b.states.Set(ctx, fromChat, dialog.StateConsMatSearch, st.Payload)

		rows := [][]tgbotapi.InlineKeyboardButton{
			{
				tgbotapi.NewInlineKeyboardButtonData("Поиск по названию", "cons:search:name"),
			},
			{
				tgbotapi.NewInlineKeyboardButtonData("Поиск по параметрам", "cons:search:params"),
			},
			navKeyboard(true, true).InlineKeyboard[0],
		}
		kb := tgbotapi.NewInlineKeyboardMarkup(rows...)
		b.send(tgbotapi.NewEditMessageTextAndMarkup(fromChat, cb.Message.MessageID, "Как искать материал?", kb))
		_ = b.answerCallback(cb, "Ок", false)
		return

	case data == "cons:search:name":
		st, _ := b.states.Get(ctx, fromChat)
		_ = b.states.Set(ctx, fromChat, dialog.StateConsSearchByName, st.Payload)
		b.editTextWithNav(fromChat, cb.Message.MessageID,
			"Введите часть названия материала (поиск без учёта регистра).")
		_ = b.answerCallback(cb, "Ок", false)
		return

	case data == "cons:search:params":
		st, _ := b.states.Get(ctx, fromChat)
		_ = b.states.Set(ctx, fromChat, dialog.StateConsMatPick, st.Payload)

		cats, err := b.catalog.ListCategories(ctx)
		if err != nil || len(cats) == 0 {
			b.editTextAndClear(fromChat, cb.Message.MessageID, "Не удалось загрузить категории материалов.")
			_ = b.answerCallback(cb, "Ошибка", true)
			return
		}

		rows := [][]tgbotapi.InlineKeyboardButton{}
		for _, c := range cats {
			rows = append(rows, tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData(c.Name, fmt.Sprintf("cons:cat:%d", c.ID)),
			))
		}
		rows = append(rows, navKeyboard(true, true).InlineKeyboard[0])
		kb := tgbotapi.NewInlineKeyboardMarkup(rows...)
		b.send(tgbotapi.NewEditMessageTextAndMarkup(fromChat, cb.Message.MessageID, "Выберите категорию:", kb))
		_ = b.answerCallback(cb, "Ок", false)
		return

	case strings.HasPrefix(data, "cons:cat:"):
		catID, _ := strconv.ParseInt(strings.TrimPrefix(data, "cons:cat:"), 10, 64)
		st, _ := b.states.Get(ctx, fromChat)
		if st.Payload == nil {
			st.Payload = dialog.Payload{}
		}
		st.Payload["cons_cat_id"] = float64(catID)

		brands, err := b.brands.ListByCategory(ctx, catID, true)
		if err != nil {
			b.editTextAndClear(fromChat, cb.Message.MessageID, "Ошибка загрузки брендов.")
			_ = b.answerCallback(cb, "Ошибка", true)
			return
		}
		if len(brands) == 0 {
			b.editTextAndClear(fromChat, cb.Message.MessageID, "В этой категории пока нет брендов.")
			_ = b.answerCallback(cb, "Готово", false)
			return
		}

		// сохраняем бренды в payload
		var ids []any
		var names []any
		for _, br := range brands {
			ids = append(ids, br.ID)
			names = append(names, br.Name)
		}
		st.Payload["cons_brand_ids"] = ids
		st.Payload["cons_brand_names"] = names
		_ = b.states.Set(ctx, fromChat, dialog.StateConsMatPick, st.Payload)

		rows := [][]tgbotapi.InlineKeyboardButton{}
		for i, br := range brands {
			label := br.Name
			if label == "" {
				label = "Без бренда"
			}
			rows = append(rows, tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData(label, fmt.Sprintf("cons:brand:%d", i)),
			))
		}
		rows = append(rows, navKeyboard(true, true).InlineKeyboard[0])
		kb := tgbotapi.NewInlineKeyboardMarkup(rows...)
		b.send(tgbotapi.NewEditMessageTextAndMarkup(fromChat, cb.Message.MessageID, "Выберите бренд:", kb))
		_ = b.answerCallback(cb, "Ок", false)
		return

	case strings.HasPrefix(data, "cons:brand:"):
		idxStr := strings.TrimPrefix(data, "cons:brand:")
		i, _ := strconv.Atoi(idxStr)

		st, _ := b.states.Get(ctx, fromChat)
		if st == nil || st.Payload == nil {
			b.editTextAndClear(fromChat, cb.Message.MessageID, "Состояние утеряно, начните заново.")
			_ = b.answerCallback(cb, "Ошибка", true)
			return
		}

		rawIDs, ok := st.Payload["cons_brand_ids"].([]any)
		if !ok || i < 0 || i >= len(rawIDs) {
			b.editTextAndClear(fromChat, cb.Message.MessageID, "Ошибка выбора бренда.")
			_ = b.answerCallback(cb, "Ошибка", true)
			return
		}
		brandID := int64(rawIDs[i].(float64))

		mats, err := b.materials.ListByBrand(ctx, brandID)
		if err != nil {
			b.editTextAndClear(fromChat, cb.Message.MessageID, "Ошибка загрузки материалов.")
			_ = b.answerCallback(cb, "Ошибка", true)
			return
		}
		if len(mats) == 0 {
			b.editTextAndClear(fromChat, cb.Message.MessageID, "В этом бренде нет материалов.")
			_ = b.answerCallback(cb, "Готово", false)
			return
		}

		rows := [][]tgbotapi.InlineKeyboardButton{}
		for _, m := range mats {
			label := materialDisplayName(m.Brand, m.Name)
			rows = append(rows, tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData(label, fmt.Sprintf("cons:mat:%d", m.ID)),
			))
		}
		rows = append(rows, navKeyboard(true, true).InlineKeyboard[0])
		kb := tgbotapi.NewInlineKeyboardMarkup(rows...)
		_ = b.states.Set(ctx, fromChat, dialog.StateConsMatPick, st.Payload)
		b.send(tgbotapi.NewEditMessageTextAndMarkup(fromChat, cb.Message.MessageID, "Выберите материал:", kb))
		_ = b.answerCallback(cb, "Ок", false)
		return

	case strings.HasPrefix(data, "cons:mat:"):
		matID, _ := strconv.ParseInt(strings.TrimPrefix(data, "cons:mat:"), 10, 64)
		st, _ := b.states.Get(ctx, fromChat)
		st.Payload["mat_id"] = float64(matID)
		_ = b.states.Set(ctx, fromChat, dialog.StateConsMatQty, st.Payload)
		b.editTextWithNav(fromChat, cb.Message.MessageID, "Введите количество (целое, g/шт)")
		_ = b.answerCallback(cb, "Ок", false)
		return

	case data == "cons:calc":
		st, _ := b.states.Get(ctx, fromChat)
		if st == nil || st.Payload == nil {
			// сессия потерялась — аккуратно выходим
			b.editTextAndClear(fromChat, cb.Message.MessageID,
				"Сессия устарела. Начните заново через кнопку «Расход/Аренда».")
			_ = b.answerCallback(cb, "Ошибка", true)
			return
		}

		placeRaw, okP := st.Payload["place"]
		unitRaw, okU := st.Payload["unit"]
		qtyRaw, okQ := st.Payload["qty"]
		itemsRaw, okItems := st.Payload["items"]
		if !okP || !okU || !okQ || !okItems {
			b.editTextAndClear(fromChat, cb.Message.MessageID,
				"Эта корзина уже неактуальна. Начните новую сессию через меню «Расход/Аренда».")
			_ = b.answerCallback(cb, "Сессия устарела", true)
			return
		}

		place, ok1 := placeRaw.(string)
		unit, ok2 := unitRaw.(string)
		qtyF, ok3 := qtyRaw.(float64)
		if !ok1 || !ok2 || !ok3 {
			b.editTextAndClear(fromChat, cb.Message.MessageID,
				"Эта корзина уже неактуальна. Начните новую сессию через меню «Расход/Аренда».")
			_ = b.answerCallback(cb, "Сессия устарела", true)
			return
		}
		qty := int(qtyF)
		items := b.consParseItems(itemsRaw)

		// 1) стоимость материалов
		var mats float64
		for _, it := range items {
			matID := int64(it["mat_id"].(float64))
			q := int64(it["qty"].(float64))
			price, _ := b.materials.GetPrice(ctx, matID)
			mats += float64(q) * price
		}

		// 2) разрезаем сессию на части: старые абонементы / новые / без абонемента
		u, _ := b.users.GetByTelegramID(ctx, cb.From.ID)
		var metas []rentPartMeta
		if u != nil {
			metas, _ = b.splitQtyBySubscriptions(ctx, u.ID, place, unit, qty)
		}
		if len(metas) == 0 {
			// на всякий случай — считаем всё без абонемента
			metas = []rentPartMeta{{
				WithSub:   false,
				Qty:       qty,
				SubID:     0,
				PlanLimit: 0,
			}}
		}

		// есть ли вообще части по абонементу
		withSub := false
		for _, m := range metas {
			if m.WithSub {
				withSub = true
				break
			}
		}

		// подготовим части для биллинга
		parts := make([]consumption.RentSplitPartInput, 0, len(metas))
		for _, m := range metas {
			p := consumption.RentSplitPartInput{
				WithSub: m.WithSub,
				Qty:     m.Qty,
			}
			if m.WithSub && m.PlanLimit > 0 {
				// тариф по лимиту плана (30, 50, ...)
				p.SubLimitForPricing = m.PlanLimit
			} else {
				// без абонемента тариф по самому куску
				p.SubLimitForPricing = m.Qty
			}
			parts = append(parts, p)
		}

		// 3) расчёт по ступеням для всех частей
		calcRent, rounded, needTotal, partResults, err := b.cons.ComputeRentSplit(ctx, place, unit, mats, parts)
		if err != nil || len(partResults) == 0 {
			b.send(tgbotapi.NewMessage(fromChat,
				fmt.Sprintf("⚠️ Нет активных тарифов для: %s / %s (%s). Настройте тарифы.",
					map[string]string{"hall": "Зал", "cabinet": "Кабинет"}[place],
					map[string]string{"hour": "час", "day": "день"}[unit],
					map[bool]string{true: "с абонементом", false: "без абонемента"}[withSub],
				)))
			return
		}
		// аренда к оплате: берем только части БЕЗ абонемента
		rentToPay := 0.0
		for i, pr := range partResults {
			if !metas[i].WithSub {
				rentToPay += pr.Rent
			}
		}
		total := mats + rentToPay

		// 4) сохраняем в payload
		st.Payload["with_sub"] = withSub
		st.Payload["mats_sum"] = mats
		st.Payload["mats_rounded"] = rounded
		st.Payload["need_total"] = needTotal

		// при желании можно хранить оба значения
		st.Payload["rent_calc"] = calcRent // «номинальная» аренда, необязательно использовать
		st.Payload["rent"] = rentToPay     // аренда именно к оплате
		st.Payload["total"] = total

		// детальная разбивка (для прозрачности и на будущее, плюс для confirm)
		partsPayload := make([]map[string]any, 0, len(partResults))
		for i, pr := range partResults {
			m := metas[i]
			mp := map[string]any{
				"with_sub":       m.WithSub,
				"qty":            m.Qty,
				"rent":           pr.Rent,
				"tariff":         pr.Tariff,
				"need":           pr.Need,
				"materials_used": pr.MaterialsUsed,
				"threshold_met":  pr.ThresholdMet,
			}
			if m.WithSub {
				mp["sub_id"] = m.SubID
				mp["plan_limit"] = m.PlanLimit
			}
			partsPayload = append(partsPayload, mp)
		}
		st.Payload["rent_parts"] = partsPayload
		_ = b.states.Set(ctx, fromChat, dialog.StateConsSummary, st.Payload)

		// 5) вывод сводки с детализацией по частям
		txt := b.buildConsumptionReceipt(ctx, st.Payload, "Проверь перед подтверждением:")

		rows := [][]tgbotapi.InlineKeyboardButton{
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("✅ Подтвердить", "cons:confirm"),
			),
		}

		if withSub {
			for _, part := range parseRentParts(st.Payload["rent_parts"]) {
				if !payloadBool(part, "with_sub") {
					rows = append(rows, tgbotapi.NewInlineKeyboardRow(
						tgbotapi.NewInlineKeyboardButtonData("Купить абонемент", "cons:buy_sub"),
					))
					break
				}
			}
		}

		rows = append(rows, navKeyboard(true, true).InlineKeyboard[0])

		kb := tgbotapi.NewInlineKeyboardMarkup(rows...)
		b.send(tgbotapi.NewEditMessageTextAndMarkup(fromChat, cb.Message.MessageID, txt, kb))
		_ = b.answerCallback(cb, "Ок", false)
		return

	case data == "cons:edit":
		st, _ := b.states.Get(ctx, fromChat)

		placeRaw, okPlace := st.Payload["place"]
		unitRaw, okUnit := st.Payload["unit"]
		qtyRaw, okQty := st.Payload["qty"]
		itemsRaw, okItems := st.Payload["items"]

		if !okPlace || !okUnit || !okQty || !okItems {
			// Старая/неактуальная сводка – предложим начать заново
			b.editTextAndClear(fromChat, cb.Message.MessageID,
				"Эта сводка уже неактуальна. Начните новую сессию через меню «Расход/Аренда».")
			_ = b.answerCallback(cb, "Сводка устарела", true)
			return
		}

		place, ok1 := placeRaw.(string)
		unit, ok2 := unitRaw.(string)
		qtyF, ok3 := qtyRaw.(float64)
		if !ok1 || !ok2 || !ok3 {
			b.editTextAndClear(fromChat, cb.Message.MessageID,
				"Эта сводка уже неактуальна. Начните новую сессию через меню «Расход/Аренда».")
			_ = b.answerCallback(cb, "Сводка устарела", true)
			return
		}

		qty := int(qtyF)
		items := b.consParseItems(itemsRaw)

		_ = b.states.Set(ctx, fromChat, dialog.StateConsCart, st.Payload)
		b.showConsCart(ctx, fromChat, &cb.Message.MessageID, place, unit, qty, items)
		_ = b.answerCallback(cb, "Ок", false)
		return

	case data == "cons:confirm":
		st, _ := b.states.Get(ctx, fromChat)
		if st == nil || st.Payload == nil {
			// вся сессия потерялась / устарела — аккуратно выходим
			b.editTextAndClear(fromChat, cb.Message.MessageID,
				"Сессия устарела. Начните заново через кнопку «Расход/Аренда».")
			_ = b.answerCallback(cb, "Ошибка", true)
			return
		}

		u, _ := b.users.GetByTelegramID(ctx, cb.From.ID)
		if u == nil || u.Status != "approved" {
			b.editTextAndClear(fromChat, cb.Message.MessageID, "Нет доступа")
			_ = b.answerCallback(cb, "Ошибка", true)
			return
		}

		place := st.Payload["place"].(string)
		unit := st.Payload["unit"].(string)
		qty := int(st.Payload["qty"].(float64))
		items := b.consParseItems(st.Payload["items"])
		mats := st.Payload["mats_sum"].(float64)
		rounded := st.Payload["mats_rounded"].(float64)
		rent := st.Payload["rent"].(float64)
		total := st.Payload["total"].(float64)

		var comment string
		if v, ok := st.Payload["comment"].(string); ok {
			comment = v
		}

		// найдём склад Расходники (только с него списываем)
		whID, err := b.getConsumablesWarehouseID(ctx)
		if err != nil {
			b.editTextAndClear(fromChat, cb.Message.MessageID, "Склад 'Расходники' не найден")
			_ = b.answerCallback(cb, "Ошибка", true)
			return
		}

		withSub := false
		if v, ok := st.Payload["with_sub"].(bool); ok {
			withSub = v
		}

		// создаём сессию + позиции
		sessionPayload := map[string]any{
			"items_count": len(items),
		}
		if comment != "" {
			sessionPayload["comment"] = comment
		}

		sid, err := b.cons.CreateSession(ctx, u.ID, place, unit, qty, withSub, mats, rounded, rent, total, sessionPayload)

		if err != nil {
			b.editTextAndClear(fromChat, cb.Message.MessageID, "Не удалось создать сессию")
			_ = b.answerCallback(cb, "Ошибка", true)
			return
		}
		// Учёт абонемента: спишем использованное количество (часы/дни) за текущий месяц
		if withSub && b.subs != nil {
			// разбиваем сессию на части по тем же правилам (старые/новые абонементы + без абонемента)
			metas, _ := b.splitQtyBySubscriptions(ctx, u.ID, place, unit, qty)
			month := time.Now().Format("2006-01")

			for _, m := range metas {
				if !m.WithSub || m.SubID == 0 || m.Qty <= 0 {
					continue
				}

				if err := b.subs.AddUsage(ctx, m.SubID, m.Qty); err != nil {
					if errors.Is(err, subsdomain.ErrInsufficientLimit) && b.adminChat != 0 {
						// сигнал админу, что по конкретному абонементу лимит уже выбит
						b.send(tgbotapi.NewMessage(b.adminChat,
							fmt.Sprintf("⚠️ Не удалось списать %d %s абонемента (id=%d) для мастера id %d: недостаточно лимита.",
								m.Qty,
								map[string]string{"hour": "часов", "day": "дней"}[unit],
								m.SubID,
								u.ID,
							)))
					}
				}
			}

			// после списаний проверим, есть ли ещё активные абонементы по этому месту/единице
			if subsAfter, err := b.subs.ListActiveByPlaceUnitMonth(ctx, u.ID, place, unit, month); err == nil && len(subsAfter) == 0 {
				// всё по этому помещению выработано — предложим купить новый абонемент
				msg := tgbotapi.NewMessage(fromChat,
					"Абонемент по этому помещению полностью использован.\nХотите приобрести новый абонемент?")
				msg.ReplyMarkup = b.subBuyPlaceKeyboard()
				b.send(msg)
			}
		}

		// Добавляем сумму материалов к абонементам
		if partsRaw, ok := st.Payload["rent_parts"]; ok && partsRaw != nil && b.subs != nil && mats > 0 {
			if parts, ok := partsRaw.([]any); ok {
				// Считаем общий объём часов/дней по частям с абонементом
				totalSubQty := 0
				for _, pr := range parts {
					mp, ok := pr.(map[string]any)
					if !ok {
						continue
					}
					withSub, _ := mp["with_sub"].(bool)
					if !withSub {
						continue
					}
					qtyF, okQty := mp["qty"].(float64)
					if !okQty {
						continue
					}
					totalSubQty += int(qtyF)
				}

				if totalSubQty > 0 {
					for _, pr := range parts {
						mp, ok := pr.(map[string]any)
						if !ok {
							continue
						}

						withSub, _ := mp["with_sub"].(bool)
						if !withSub {
							continue
						}

						subIDF, okID := mp["sub_id"].(float64)
						qtyF, okQty := mp["qty"].(float64)
						if !okID || !okQty {
							continue
						}
						partQty := int(qtyF)
						if partQty <= 0 {
							continue
						}

						subID := int64(subIDF)
						// Фактическая сумма материалов, приходящаяся на этот абонемент
						matsForSub := mats * float64(partQty) / float64(totalSubQty)

						// Ошибку можно залогировать, но не валить весь консумпшен
						_ = b.subs.AddMaterialsUsage(ctx, subID, matsForSub)
					}
				}
			}
		}

		pairs := make([][2]int64, 0, len(items))
		// позиции + списание
		for _, it := range items {
			matID := int64(it["mat_id"].(float64))
			q := int64(it["qty"].(float64))
			price, _ := b.materials.GetPrice(ctx, matID)
			cost := float64(q) * price

			// списание (разрешено уходить в минус)
			if err := b.inventory.Consume(ctx, u.ID, whID, matID, float64(q), "consumption"); err != nil {
				b.editTextAndClear(fromChat, cb.Message.MessageID, "Ошибка списания")
				_ = b.answerCallback(cb, "Ошибка", true)
				return
			}
			_ = b.cons.AddItem(ctx, sid, matID, float64(q), price, cost)
			pairs = append(pairs, [2]int64{whID, matID})
		}

		// инвойс (pending)
		invoiceID, err := b.cons.CreateInvoice(ctx, u.ID, sid, total, comment)
		if err != nil {
			b.editTextAndClear(fromChat, cb.Message.MessageID, "Не удалось создать счёт.")
			_ = b.answerCallback(cb, "Ошибка", true)
			return
		}

		// пробуем сформировать ссылку на оплату (эмулятор платежей)
		var payURL string
		if b.payments != nil {
			// здесь НЕ используем placeRU/unitRU, только тех.описание
			desc := fmt.Sprintf("Расход/аренда: place=%s, qty=%d %s", place, qty, unit)

			if url, err := b.payments.CreatePayment(ctx, invoiceID, total, desc); err != nil {
				b.log.Error("failed to create payment link",
					"invoice_id", invoiceID,
					"err", err,
				)
			} else {
				payURL = url
				if err := b.cons.SetInvoicePaymentLink(ctx, invoiceID, payURL); err != nil {
					b.log.Error("failed to store payment link",
						"invoice_id", invoiceID,
						"err", err,
					)
				}
			}
		}

		b.notifyLowOrNegativeBatch(ctx, pairs)
		// уведомление админу о подтверждённой сессии расхода/аренды
		if b.adminChat != 0 {
			// кто подтвердил
			u, _ := b.users.GetByTelegramID(ctx, cb.From.ID)

			// соберём удобочитаемый текст
			placeRU := map[string]string{"hall": "Зал", "cabinet": "Кабинет"}
			unitRU := map[string]string{"hour": "ч", "day": "дн"}
			var sb strings.Builder

			_, _ = fmt.Fprintf(&sb, "✅ Подтверждена сессия расхода/аренды\n")
			if u != nil {
				_, _ = fmt.Fprintf(&sb, "Мастер: %s (@%s, id %d)\n", strings.TrimSpace(u.Username), cb.From.UserName, cb.From.ID)
			} else {
				_, _ = fmt.Fprintf(&sb, "Мастер: @%s (id %d)\n", cb.From.UserName, cb.From.ID)
			}
			_, _ = fmt.Fprintf(&sb, "Помещение: %s\nКол-во: %d %s\n", placeRU[place], qty, unitRU[unit])

			// материалы
			_, _ = fmt.Fprintf(&sb, "Материалы:\n")
			var matsSum float64
			for _, it := range items {
				matID := int64(it["mat_id"].(float64))
				q := int64(it["qty"].(float64))
				name := fmt.Sprintf("ID:%d", matID)
				if m, _ := b.materials.GetByID(ctx, matID); m != nil { // repo уже есть
					name = m.Name
				}
				price, _ := b.materials.GetPrice(ctx, matID)
				line := float64(q) * price
				matsSum += line
				_, _ = fmt.Fprintf(&sb, "• %s — %d × %.2f = %.2f ₽\n", name, q, price, line)
			}

			// финансы: округлённая сумма материалов, аренда, итого — у нас уже посчитаны
			_, _ = fmt.Fprintf(&sb, "\nМатериалы (факт): %.2f ₽, округл.: %.2f ₽\nАренда: %.2f ₽\nИтого: %.2f ₽",
				mats, rounded, rent, total)

			b.send(tgbotapi.NewMessage(b.adminChat, sb.String()))
		}

		// сообщение мастеру о завершении расчёта
		receiptText := b.buildConsumptionReceipt(ctx, st.Payload, "✅ Сессия подтверждена.\n\nЧек:")

		b.editTextAndClear(fromChat, cb.Message.MessageID, receiptText)

		// если сформировалась ссылка на оплату – даём кнопку мастеру
		if payURL != "" {
			kb := tgbotapi.NewInlineKeyboardMarkup(
				tgbotapi.NewInlineKeyboardRow(
					tgbotapi.NewInlineKeyboardButtonURL(
						fmt.Sprintf("Оплатить %.2f ₽", total),
						payURL,
					),
				),
				navKeyboard(true, true).InlineKeyboard[0],
			)

			msg := tgbotapi.NewMessage(fromChat, "Перейти к оплате:")
			msg.ReplyMarkup = kb
			b.send(msg)
		}

		_ = b.states.Set(ctx, fromChat, dialog.StateIdle, dialog.Payload{})
		_ = b.answerCallback(cb, "Готово", false)
		return

		// Покупка абонемента из сводки расхода/аренды
	case data == "cons:buy_sub":
		u, _ := b.users.GetByTelegramID(ctx, cb.From.ID)
		if u == nil || u.Status != users.StatusApproved || u.Role != users.RoleMaster {
			_ = b.answerCallback(cb, "Недоступно", true)
			return
		}

		// Сбросим состояние под покупку абонемента
		_ = b.states.Set(ctx, fromChat, dialog.StateSubBuyPlace, dialog.Payload{})

		msg := tgbotapi.NewMessage(fromChat, "Выберите тип абонемента:")
		msg.ReplyMarkup = b.subBuyPlaceKeyboard()
		b.send(msg)

		_ = b.answerCallback(cb, "Ок", false)
		return

		// Покупка абонемента — выбор места
	case strings.HasPrefix(data, "subbuy:place:"):
		place := strings.TrimPrefix(data, "subbuy:place:")
		unit := "hour"
		if place == "cabinet" {
			unit = "day"
		}

		// Текущий мастер
		u, _ := b.users.GetByTelegramID(ctx, cb.From.ID)
		if u == nil || u.Status != users.StatusApproved || u.Role != users.RoleMaster {
			b.editTextAndClear(fromChat, cb.Message.MessageID, "Доступ запрещён.")
			_ = b.answerCallback(cb, "Ошибка", true)
			return
		}

		// Проверяем, есть ли активный абонемент по этому помещению в текущем месяце
		month := time.Now().Format("2006-01")
		subs, err := b.subs.ListByUserMonth(ctx, u.ID, month)
		if err != nil {
			b.editTextAndClear(fromChat, cb.Message.MessageID, "Ошибка загрузки абонементов.")
			_ = b.answerCallback(cb, "Ошибка", true)
			return
		}

		for _, s := range subs {
			if s.Place == place && s.Unit == unit {
				left := s.TotalQty - s.UsedQty
				if left > 0 {
					if left < 0 {
						left = 0
					}
					unitRU := map[string]string{"hour": "ч", "day": "дн"}[unit]
					placeName := map[string]string{"hall": "общего зала", "cabinet": "кабинета"}[place]

					b.editTextAndClear(fromChat, cb.Message.MessageID,
						fmt.Sprintf(
							"У вас уже есть действующий абонемент для %s на текущий месяц: %d/%d (остаток %d %s).\n"+
								"Новый абонемент можно купить только после полного использования текущего.",
							placeName, s.UsedQty, s.TotalQty, left, unitRU,
						),
					)
					_ = b.answerCallback(cb, "Абонемент ещё активен", true)
					return
				}
			}
		}

		// Тарифы-абонементы для выбранного помещения: одна строка = один конкретный объём
		rates, err := b.cons.ListRates(ctx, place, unit, true)
		if err != nil {
			b.editTextAndClear(fromChat, cb.Message.MessageID, "Ошибка загрузки тарифов абонементов.")
			_ = b.answerCallback(cb, "Ошибка", true)
			return
		}
		if len(rates) == 0 {
			b.editTextAndClear(fromChat, cb.Message.MessageID, "Для этого помещения нет настроенных абонементов.")
			_ = b.answerCallback(cb, "Нет тарифов", true)
			return
		}

		// Сохраняем место/единицу в состоянии
		st, _ := b.states.Get(ctx, fromChat)
		if st.Payload == nil {
			st.Payload = dialog.Payload{}
		}
		st.Payload["place"] = place
		st.Payload["unit"] = unit
		_ = b.states.Set(ctx, fromChat, dialog.StateSubBuyPlace, st.Payload)

		// Кнопки: одна строка rent_rates = один готовый абонемент
		rows := [][]tgbotapi.InlineKeyboardButton{}
		unitFull := map[string]string{"hour": "часов", "day": "дней"}[unit]
		unitShort := map[string]string{"hour": "ч", "day": "дн"}[unit]

		for _, r := range rates {
			qty := r.MinQty // по новой концепции min_qty == max_qty == объём абонемента

			text := fmt.Sprintf(
				"%d %s в месяц: с мат. %.0f ₽/%s, свои %.0f ₽/%s",
				qty, unitFull,
				r.PriceWith, unitShort,
				r.PriceOwn, unitShort,
			)
			data := fmt.Sprintf("subbuy:plan:%d", r.ID) // выбираем конкретный план
			rows = append(rows, tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData(text, data),
			))
		}

		// Навигация Назад/Отменить
		rows = append(rows, navKeyboard(true, true).InlineKeyboard[0])

		kb := tgbotapi.NewInlineKeyboardMarkup(rows...)
		title := fmt.Sprintf("Выберите абонемент для %s:",
			map[string]string{"hall": "общего зала", "cabinet": "кабинета"}[place])

		b.send(tgbotapi.NewEditMessageTextAndMarkup(fromChat, cb.Message.MessageID, title, kb))
		_ = b.answerCallback(cb, "Ок", false)
		return

		// Покупка абонемента — выбор конкретного плана
	case strings.HasPrefix(data, "subbuy:plan:"):
		idStr := strings.TrimPrefix(data, "subbuy:plan:")
		rateID, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			_ = b.answerCallback(cb, "Некорректные данные", true)
			return
		}

		st, _ := b.states.Get(ctx, fromChat)
		if st.Payload == nil {
			st.Payload = dialog.Payload{}
		}
		place, ok1 := st.Payload["place"].(string)
		unit, ok2 := st.Payload["unit"].(string)
		if !ok1 || !ok2 {
			b.editTextAndClear(fromChat, cb.Message.MessageID, "Сессия покупки абонемента потеряна. Начните заново.")
			_ = b.answerCallback(cb, "Ошибка", true)
			return
		}

		// Ищем выбранный тариф
		rates, err := b.cons.ListRates(ctx, place, unit, true)
		if err != nil {
			b.editTextAndClear(fromChat, cb.Message.MessageID, "Ошибка загрузки тарифов.")
			_ = b.answerCallback(cb, "Ошибка", true)
			return
		}
		var rate *consumption.TierRate
		for i := range rates {
			if rates[i].ID == rateID {
				rate = &rates[i]
				break
			}
		}
		if rate == nil {
			b.editTextAndClear(fromChat, cb.Message.MessageID, "Тариф не найден.")
			_ = b.answerCallback(cb, "Ошибка", true)
			return
		}

		// Объём абонемента = min_qty (min_qty == max_qty по нашей модели)
		qty := rate.MinQty

		// Текущий мастер
		u, _ := b.users.GetByTelegramID(ctx, cb.From.ID)
		if u == nil || u.Status != users.StatusApproved || u.Role != users.RoleMaster {
			b.editTextAndClear(fromChat, cb.Message.MessageID, "Доступ запрещён.")
			_ = b.answerCallback(cb, "Ошибка", true)
			return
		}

		// Ищем последний абонемент по этому помещению и единице
		lastSub, err := b.subs.LastByUserPlaceUnit(ctx, u.ID, place, unit)
		if err != nil {
			b.editTextAndClear(fromChat, cb.Message.MessageID, "Ошибка загрузки абонементов.")
			_ = b.answerCallback(cb, "Ошибка", true)
			return
		}

		// Порог и стоимость для нового абонемента
		thresholdPerUnit := rate.Threshold                // 100 / 1000 и т.п.
		thresholdTotal := float64(qty) * thresholdPerUnit // общий порог по абонементу
		var pricePerUnit float64                          // цена аренды за час/день
		if lastSub == nil || lastSub.ThresholdMet {       // абонемента не было или условие выполнено
			pricePerUnit = rate.PriceWith
		} else { // прошлый абонемент не выполнил порог
			pricePerUnit = rate.PriceOwn
		}
		totalCost := float64(qty) * pricePerUnit

		// Сохраняем всё нужное в состоянии (используем позже в confirm + заявка админу)
		st.Payload["qty"] = float64(qty)
		st.Payload["threshold_per_unit"] = thresholdPerUnit
		st.Payload["threshold_total"] = thresholdTotal
		st.Payload["price_per_unit"] = pricePerUnit
		st.Payload["total_cost"] = totalCost
		_ = b.states.Set(ctx, fromChat, dialog.StateSubBuyConfirm, st.Payload)

		unitFull := map[string]string{"hour": "часов", "day": "дней"}[unit]
		unitShort := map[string]string{"hour": "ч", "day": "дн"}[unit]

		placeName := map[string]string{"hall": "Общий зал", "cabinet": "Кабинет"}[place]

		txt := fmt.Sprintf(
			"Абонемент:\n"+
				"Помещение: %s\n"+
				"Лимит: %d %s в месяц\n"+
				"Порог материалов: %.2f ₽ по %.2f ₽ за %s\n"+
				"Цена аренды за %s: %.2f ₽\n"+
				"Стоимость абонемента: %.2f ₽\n\n"+
				"Желаете оплатить и приобрести этот абонемент?",
			placeName,
			qty, unitFull,
			thresholdTotal, thresholdPerUnit, unitShort,
			unitShort, pricePerUnit,
			totalCost,
		)

		kb := tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("✅ Оформить", "subbuy:confirm"),
			),
			navKeyboard(true, true).InlineKeyboard[0],
		)

		b.send(tgbotapi.NewEditMessageTextAndMarkup(fromChat, cb.Message.MessageID, txt, kb))
		_ = b.answerCallback(cb, "Ок", false)
		return

		// Покупка абонемента — подтверждение (мастер → заявка админу)
	case data == "subbuy:confirm":
		st, _ := b.states.Get(ctx, fromChat)
		u, _ := b.users.GetByTelegramID(ctx, cb.From.ID)
		if u == nil || u.Status != users.StatusApproved || u.Role != users.RoleMaster {
			b.editTextAndClear(fromChat, cb.Message.MessageID, "Доступ запрещён.")
			_ = b.answerCallback(cb, "Ошибка", true)
			return
		}
		if st == nil || st.Payload == nil {
			b.editTextAndClear(fromChat, cb.Message.MessageID, "Состояние утеряно. Начните оформление заново.")
			_ = b.answerCallback(cb, "Ошибка", true)
			return
		}

		place, _ := st.Payload["place"].(string)
		unit, _ := st.Payload["unit"].(string)
		qty := int(st.Payload["qty"].(float64))

		thresholdTotal := 0.0
		if v, ok := st.Payload["threshold_total"].(float64); ok {
			thresholdTotal = v
		} else if thrPerUnit, ok2 := st.Payload["threshold_per_unit"].(float64); ok2 {
			thresholdTotal = float64(qty) * thrPerUnit
		}

		pricePerUnit := 0.0
		if v, ok := st.Payload["price_per_unit"].(float64); ok {
			pricePerUnit = v
		}
		totalCost := 0.0
		if v, ok := st.Payload["total_cost"].(float64); ok {
			totalCost = v
		} else if pricePerUnit > 0 {
			totalCost = float64(qty) * pricePerUnit
		}

		// Сообщение мастеру
		b.editTextAndClear(fromChat, cb.Message.MessageID,
			"Запрос на приобретение абонемента отправлен администратору. Ожидайте подтверждения.")
		_ = b.states.Set(ctx, fromChat, dialog.StateIdle, dialog.Payload{})

		// Текст для админа
		displayName := strings.TrimSpace(u.Username)
		if displayName == "" {
			displayName = fmt.Sprintf("id %d", u.ID)
		}

		placeRU := map[string]string{"hall": "Общий зал", "cabinet": "Кабинет"}
		unitRU := map[string]string{"hour": "ч", "day": "дн"}

		txt := fmt.Sprintf(
			"Мастер: %s хочет приобрести абонемент:\n\n"+
				"Помещение: %s\n"+
				"Количество: %d %s\n"+
				"Цена аренды за %s: %.2f ₽\n"+
				"На сумму: %.2f ₽\n\n"+
				"Проверьте оплату мастером и подтвердите или отклоните приобретение.",
			displayName,
			placeRU[place],
			qty, unitRU[unit],
			unitRU[unit], pricePerUnit,
			totalCost,
		)

		// коллбеки для админа
		cbApprove := fmt.Sprintf("subrq:approve:%d:%s:%s:%d:%.2f",
			cb.From.ID, place, unit, qty, thresholdTotal,
		)
		cbReject := fmt.Sprintf("subrq:reject:%d", cb.From.ID)

		msg := tgbotapi.NewMessage(b.adminChat, txt)
		msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("Подтвердить", cbApprove),
				tgbotapi.NewInlineKeyboardButtonData("Отклонить", cbReject),
			),
		)
		b.send(msg)

		_ = b.answerCallback(cb, "Отправлено админу", false)
		return

	// Переключение place/unit
	case strings.HasPrefix(data, "rates:pu:"):
		parts := strings.Split(strings.TrimPrefix(data, "rates:pu:"), ":")
		if len(parts) != 2 {
			_ = b.answerCallback(cb, "Некорректные данные", true)
			return
		}
		place, unit := parts[0], parts[1]
		st, _ := b.states.Get(ctx, fromChat)
		if st.Payload == nil {
			st.Payload = dialog.Payload{}
		}
		st.Payload["place"] = place
		st.Payload["unit"] = unit
		_ = b.states.Set(ctx, fromChat, dialog.StateAdmRatesPickPU, st.Payload)

		withSub := false
		if v, ok := st.Payload["with_sub"].(bool); ok {
			withSub = v
		}
		toggle := "Абонемент: выкл"
		if withSub {
			toggle = "Абонемент: вкл"
		}

		kb := tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("Общий зал / час", "rates:pu:hall:hour"),
				tgbotapi.NewInlineKeyboardButtonData("Кабинет / день", "rates:pu:cabinet:day"),
			),
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData(toggle, "rates:sub:tg"),
			),
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("📄 Показать ступени", "rates:list"),
			),
			navKeyboard(true, true).InlineKeyboard[0],
		)
		b.send(tgbotapi.NewEditMessageTextAndMarkup(fromChat, cb.Message.MessageID, "Установка тарифов — выберите набор параметров:", kb))
		_ = b.answerCallback(cb, "Ок", false)
		return

		// Тумблер абонемента
	case data == "rates:sub:tg":
		st, _ := b.states.Get(ctx, fromChat)
		cur := false
		if v, ok := st.Payload["with_sub"].(bool); ok {
			cur = v
		}
		st.Payload["with_sub"] = !cur
		_ = b.states.Set(ctx, fromChat, dialog.StateAdmRatesPickSub, st.Payload)

		place := st.Payload["place"].(string)
		unit := st.Payload["unit"].(string)
		toggle := "Абонемент: выкл"
		if !cur {
			toggle = "Абонемент: вкл"
		}

		kb := tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("Общий зал / час", "rates:pu:hall:hour"),
				tgbotapi.NewInlineKeyboardButtonData("Кабинет / день", "rates:pu:cabinet:day"),
			),
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData(toggle, "rates:sub:tg"),
			),
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("📄 Показать ступени", "rates:list"),
			),
			navKeyboard(true, true).InlineKeyboard[0],
		)
		b.send(tgbotapi.NewEditMessageTextAndMarkup(fromChat, cb.Message.MessageID,
			fmt.Sprintf("Установка тарифов — %s / %s", map[string]string{"hall": "Зал", "cabinet": "Кабинет"}[place], map[string]string{"hour": "час", "day": "день"}[unit]), kb))
		_ = b.answerCallback(cb, "Ок", false)
		return

		// Показ списка ступеней
	case data == "rates:list":
		st, _ := b.states.Get(ctx, fromChat)
		place := st.Payload["place"].(string)
		unit := st.Payload["unit"].(string)
		withSub := false
		if v, ok := st.Payload["with_sub"].(bool); ok {
			withSub = v
		}

		rates, err := b.cons.ListRates(ctx, place, unit, withSub)
		if err != nil {
			b.editTextAndClear(fromChat, cb.Message.MessageID, "Ошибка загрузки тарифов")
			_ = b.answerCallback(cb, "Ошибка", true)
			return
		}

		lines := []string{
			fmt.Sprintf("Тарифы: %s / %s (%s)",
				map[string]string{"hall": "Зал", "cabinet": "Кабинет"}[place],
				map[string]string{"hour": "час", "day": "день"}[unit],
				map[bool]string{true: "с абонементом", false: "без абонемента"}[withSub],
			),
		}
		for _, r := range rates {
			maxTxt := "∞"
			if r.MaxQty != nil {
				maxTxt = fmt.Sprintf("%d", *r.MaxQty)
			}
			status := "🟢"
			if !r.Active {
				status = "🚫"
			}
			lines = append(lines,
				fmt.Sprintf("%s %d–%s: порог %.0f; с мат. %.2f; свои %.2f",
					status, r.MinQty, maxTxt, r.Threshold, r.PriceWith, r.PriceOwn),
			)
		}

		text := strings.Join(lines, "\n")
		kb := tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(tgbotapi.NewInlineKeyboardButtonData("➕ Добавить ступень", "rates:add")),
			navKeyboard(true, true).InlineKeyboard[0],
		)
		b.send(tgbotapi.NewEditMessageTextAndMarkup(fromChat, cb.Message.MessageID, text, kb))
		_ = b.states.Set(ctx, fromChat, dialog.StateAdmRatesList, st.Payload)
		_ = b.answerCallback(cb, "Ок", false)
		return

		// Старт добавления ступени
	case data == "rates:add":
		st, _ := b.states.Get(ctx, fromChat) // <-- додали
		if st.Payload == nil {
			st.Payload = dialog.Payload{}
		}
		_ = b.states.Set(ctx, fromChat, dialog.StateAdmRatesCreateMin, st.Payload)
		b.editTextWithNav(fromChat, cb.Message.MessageID, "Введите минимальное значение диапазона (целое число, например 1)")
		_ = b.answerCallback(cb, "Ок", false)
		return

	case data == "rates:save":
		st, _ := b.states.Get(ctx, fromChat)
		place := st.Payload["place"].(string)
		unit := st.Payload["unit"].(string)
		withSub := false
		if v, ok := st.Payload["with_sub"].(bool); ok {
			withSub = v
		}
		minQty := int(st.Payload["min"].(float64))

		var maxPtr *int
		if st.Payload["max"] != nil {
			m := int(st.Payload["max"].(float64))
			maxPtr = &m
		}
		thr := st.Payload["thr"].(float64)
		pwith := st.Payload["pwith"].(float64)
		pown := st.Payload["pown"].(float64)

		if _, err := b.cons.CreateRate(ctx, place, unit, withSub, minQty, maxPtr, thr, pwith, pown); err != nil {
			b.editTextAndClear(fromChat, cb.Message.MessageID, "Ошибка сохранения тарифной ступени")
			_ = b.answerCallback(cb, "Ошибка", true)
			return
		}

		b.editTextAndClear(fromChat, cb.Message.MessageID, "Ступень сохранена.")

		rates, _ := b.cons.ListRates(ctx, place, unit, withSub)
		lines := []string{"Обновлённый список:"}
		for _, r := range rates {
			maxTxt := "∞"
			if r.MaxQty != nil {
				maxTxt = fmt.Sprintf("%d", *r.MaxQty)
			}
			status := "🟢"
			if !r.Active {
				status = "🚫"
			}
			lines = append(lines,
				fmt.Sprintf("%s %d–%s: порог %.0f; с мат. %.2f; свои %.2f",
					status, r.MinQty, maxTxt, r.Threshold, r.PriceWith, r.PriceOwn),
			)
		}
		text := strings.Join(lines, "\n")
		kb := tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(tgbotapi.NewInlineKeyboardButtonData("➕ Добавить ступень", "rates:add")),
			navKeyboard(true, true).InlineKeyboard[0],
		)
		m := tgbotapi.NewMessage(fromChat, text)
		m.ReplyMarkup = kb
		b.send(m)
		_ = b.states.Set(ctx, fromChat, dialog.StateAdmRatesList, st.Payload)
		_ = b.answerCallback(cb, "Сохранено", false)
		return
	}
}
