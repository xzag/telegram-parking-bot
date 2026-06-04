# Parking Bot

Telegram-бот для бронирования парковочных мест на текущий день.

## Возможности

* Бронирование парковочного места через Telegram.
* Одно место может быть забронировано только одним человеком.
* Один человек может забронировать только одно место.
* Отмена бронирования через интерфейс бота.
* Отображение текущей занятости парковки.
* Отображение имени и Telegram username владельца брони.
* Автоматическое хранение истории бронирований.
* Работа по времени Новосибирска (`Asia/Novosibirsk`).
* Бронирование открывается ежедневно в 06:00.

## Технологии

* Go
* SQLite
* Telegram Bot API
* systemd

## Архитектура

```text
Telegram
    |
    v
Parking Bot (Go)
    |
    v
SQLite
```

Бот использует Long Polling.

Webhook, HTTPS, домен и внешний IP не требуются.

---

## Создание Telegram-бота

Открыть Telegram:

```text
@BotFather
```

Создать нового бота:

```text
/newbot
```

Задать имя:

```text
Parking Bot
```

Задать username:

```text
company_parking_bot
```

Получить токен:

```text
123456789:AAxxxxxxxxxxxxxxxxxxxxxxxxxxxx
```

Сохранить токен.

Настроить команды:

```text
/setcommands
```

Список команд:

```text
parking - открыть парковку
start - открыть парковку
```

---

## Сборка

Локальная сборка Linux-бинарника:

```bash
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o parking-bot ./cmd/bot
```

---

## Подготовка сервера

Создать пользователя:

```bash
sudo useradd --system \
  --home /opt/parking-bot \
  --shell /usr/sbin/nologin \
  parking
```

Создать каталог:

```bash
sudo mkdir -p /opt/parking-bot
sudo chown parking:parking /opt/parking-bot
```

Загрузить бинарник:

```bash
scp parking-bot root@SERVER_IP:/opt/parking-bot/
```

Установить права:

```bash
sudo chown parking:parking /opt/parking-bot/parking-bot
sudo chmod +x /opt/parking-bot/parking-bot
```

---

## Конфигурация

Создать файл:

```bash
sudo nano /opt/parking-bot/.env
```

Содержимое:

```env
BOT_TOKEN=123456789:AAxxxxxxxxxxxxxxxxxxxxxxxxxxxx
DB_PATH=/opt/parking-bot/parking.db
```

Права:

```bash
sudo chown parking:parking /opt/parking-bot/.env
sudo chmod 600 /opt/parking-bot/.env
```

---

## Настройка systemd

Создать сервис:

```bash
sudo nano /etc/systemd/system/parking-bot.service
```

Содержимое:

```ini
[Unit]
Description=Parking Telegram Bot
After=network-online.target
Wants=network-online.target

[Service]
User=parking
Group=parking

WorkingDirectory=/opt/parking-bot
EnvironmentFile=/opt/parking-bot/.env

ExecStart=/opt/parking-bot/parking-bot

Restart=always
RestartSec=5

NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/opt/parking-bot

[Install]
WantedBy=multi-user.target
```

Активировать:

```bash
sudo systemctl daemon-reload
sudo systemctl enable parking-bot
sudo systemctl start parking-bot
```

Проверить статус:

```bash
sudo systemctl status parking-bot
```

Посмотреть логи:

```bash
journalctl -u parking-bot -f
```

---

## Первоначальная настройка мест

Список мест задаётся в коде через:

```go
store.SeedSpots(ctx, []string{
    "1",
    "2",
    "3",
    "4",
    "5",
    "6",
})
```

При запуске отсутствующие места будут автоматически созданы.

---

## Обновление приложения

Собрать новую версию:

```bash
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o parking-bot ./cmd/bot
```

Скопировать на сервер:

```bash
scp parking-bot root@SERVER_IP:/tmp/parking-bot
```

Обновить:

```bash
sudo systemctl stop parking-bot

sudo cp /tmp/parking-bot /opt/parking-bot/parking-bot
sudo chown parking:parking /opt/parking-bot/parking-bot
sudo chmod +x /opt/parking-bot/parking-bot

sudo systemctl start parking-bot
```

---

## Хранение данных

База данных:

```text
/opt/parking-bot/parking.db
```

SQLite автоматически создаётся при первом запуске.

История бронирований хранится в таблице:

```text
bookings
```

---

## Резервное копирование

Создать каталог:

```bash
mkdir -p /opt/parking-bot/backups
```

Ручной бэкап:

```bash
sqlite3 /opt/parking-bot/parking.db \
  ".backup '/opt/parking-bot/backups/parking.db'"
```

Ежедневный cron:

```bash
sudo crontab -e
```

Добавить:

```cron
0 3 * * * sqlite3 /opt/parking-bot/parking.db ".backup '/opt/parking-bot/backups/parking-$(date +\%F).db'"
```

---

## Защита от гонок

Для предотвращения конфликтов используются уникальные ограничения SQLite:

```sql
UNIQUE (booking_date, spot_number)
```

Не позволяет двум пользователям занять одно место.

```sql
UNIQUE (booking_date, telegram_id)
```

Не позволяет одному пользователю занять несколько мест.

Даже при одновременном нажатии кнопок разными пользователями консистентность данных сохраняется на уровне БД.

---

## Использование

Открыть бот:

```text
/parking
```

Возможности:

* посмотреть свободные места;
* забронировать место;
* отменить собственную бронь;
* обновить состояние парковки.

Все действия выполняются через inline-кнопки Telegram.
