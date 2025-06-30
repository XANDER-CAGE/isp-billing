# 💰 Автоматические списания абонентской платы

## 📋 Обзор

Система автоматических списаний абонентской платы обеспечивает периодическое списание фиксированных сумм с балансов пользователей за пользование услугами связи. Это новая функциональность в netspire-go, которая отсутствовала в старой Erlang системе.

## 🆚 Сравнение со старой системой

### **Старая система (Erlang):**
- ❌ **Нет автоматических списаний** - только ручное управление
- ❌ **Нет планировщика** для периодических операций  
- ❌ **Нет встроенной поддержки** ежемесячных платежей
- ✅ Базовые функции списания через `debit_transaction()`
- ✅ План данные (plan_data) в JSON формате

### **Новая система (Go):**
- ✅ **Полностью автоматические** ежемесячные списания
- ✅ **Встроенный планировщик** с гибкой настройкой
- ✅ **Пропорциональное списание** для новых аккаунтов
- ✅ **Отчетность и мониторинг** через API
- ✅ **CLI утилиты** для управления
- ✅ **Полная совместимость** с существующей схемой БД

---

## ⚙️ Конфигурация

### **config.yaml**
```yaml
# Subscription Billing (автоматические списания абонентской платы)
subscription:
  enabled: true                           # Включить автоматические списания
  default_monthly_fee: 25.0               # Абонентская плата по умолчанию
  grace_period_days: 3                    # Льготный период при недостатке средств
  disable_on_insufficient_funds: false    # Отключать аккаунт при недостатке средств
  processing_time: "02:00"                # Время обработки списаний (2:00 AM)
  enable_proration: true                  # Пропорциональное списание для новых аккаунтов
  
  # Планировщик автоматических списаний
  scheduler:
    enabled: true                         # Включить планировщик
    run_on_first_day: true               # Запускать 1 числа каждого месяца
    retry_failed: true                   # Повторять неудачные попытки
    retry_interval_hours: 24             # Интервал повтора в часах
    max_retries: 3                       # Максимум попыток
```

### **Plan Data конфигурация**
В поле `plan_data` аккаунта можно задать:
```json
{
  "MONTHLY_FEE": 30.0,        # Абонентская плата для этого аккаунта
  "SUBSCRIPTION_FEE": 25.0,   # Альтернативное название
  "CREDIT": 10.0,             # Кредитный лимит
  "PREPAID": 1024000000       # Предоплаченный трафик
}
```

---

## 🚀 Запуск системы

### **1. Интеграция в основное приложение**
```go
// main.go
func main() {
    // ... инициализация DB, логгера ...
    
    // Создание сервиса подписок
    subscriptionConfig := &billing.SubscriptionConfig{
        Enabled:                    true,
        DefaultMonthlyFee:         25.0,
        GracePeriodDays:          3,
        DisableOnInsufficientFunds: false,
        ProcessingTime:           "02:00",
        EnableProration:          true,
    }
    
    subscriptionService := billing.NewSubscriptionService(db, logger, subscriptionConfig)
    
    // Запуск планировщика
    processor := billing.NewScheduledProcessor(subscriptionService, logger)
    processor.StartDailyScheduler()
    
    // Регистрация API routes
    subscriptionHandler := handlers.NewSubscriptionHandler(subscriptionService, logger)
    subscriptionHandler.RegisterRoutes(router)
    
    // ... запуск сервера ...
}
```

### **2. Использование CLI утилиты**
```bash
# Сборка CLI
go build -o subscription-processor ./cmd/subscription-processor

# Запуск ежемесячных списаний
./subscription-processor process

# Списания за конкретную дату
./subscription-processor process 2024-01-01

# История списаний пользователя
./subscription-processor history 123

# Статистика
./subscription-processor stats
```

---

## 📊 HTTP API

### **Ручная обработка списаний**
```bash
# Запустить списания за текущий месяц
POST /api/v1/subscription/process

# Запустить списания за конкретную дату
POST /api/v1/subscription/process/2024-01-01
```

### **История и отчеты**
```bash
# История списаний пользователя
GET /api/v1/subscription/account/123/history?limit=10

# Статистика
GET /api/v1/subscription/stats

# Неудачные списания
GET /api/v1/subscription/failed?limit=20

# Ежемесячный отчет
GET /api/v1/subscription/report/2024/01
```

### **Тестирование**
```bash
# Предпросмотр списания
GET /api/v1/subscription/preview/123

# Тестовое списание
POST /api/v1/subscription/test/123
```

---

## 💾 База данных

### **Использование существующих таблиц**
Система полностью совместима с существующей схемой БД и использует:

- **`fin_transactions`** - для записи списаний
- **`accounts`** - для получения пользователей и plan_data
- **`contracts`** - для обновления балансов
- **`plans`** - для настроек тарифных планов

### **Функции PostgreSQL**
```sql
-- Списание абонентской платы (использует существующую функцию)
SELECT debit_transaction(account_id, 25.0, 'Monthly subscription fee for period 2024-01-01 - 2024-01-31', NULL);

-- Поиск существующих списаний
SELECT * FROM fin_transactions 
WHERE comment LIKE 'Monthly subscription fee%' 
AND created_at >= '2024-01-01' 
AND created_at <= '2024-01-31';
```

---

## 🔄 Алгоритм работы

### **1. Автоматический планировщик**
1. **Запуск каждый день в 2:00** (настраивается)
2. **Проверка: 1 число месяца?** 
3. **Если да** → запуск обработки списаний
4. **Обработка всех активных аккаунтов**
5. **Логирование результатов**

### **2. Обработка отдельного аккаунта**
1. **Получение plan_data** и извлечение monthly_fee
2. **Проверка уже списанного** за текущий период
3. **Расчет пропорциональной суммы** (если включено)
4. **Проверка баланса** (баланс + кредит >= сумма)
5. **Выполнение списания** через `debit_transaction()`
6. **Запись в логи** и обновление статистики

### **3. Пропорциональное списание**
```go
// Если аккаунт создан 15 января, а списание за январь,
// то списывается только за вторую половину месяца
totalDays := 31       // дней в январе
remainingDays := 17   // с 15 по 31 января
amount := 25.0 * (17/31) = 13.71  // пропорциональная сумма
```

---

## 📈 Мониторинг и логирование

### **Логи**
```
2024-01-01 02:00:15 INFO  Starting monthly subscription charges processing target_date=2024-01-01
2024-01-01 02:00:16 INFO  Found accounts for billing count=150
2024-01-01 02:00:17 INFO  Processed account charge account_id=123 login=user123 status=success amount=25.0
2024-01-01 02:00:18 ERROR Failed to process account charge account_id=124 login=user124 error="insufficient funds"
2024-01-01 02:00:25 INFO  Monthly charges processing completed success=145 failures=5 total=150
```

### **Метрики через API**
```json
{
  "stats": {
    "total_accounts": 150,
    "active_accounts": 148,
    "charges_this_month": 145,
    "failed_charges": 5,
    "total_revenue": 3625.0,
    "success_rate": 96.7
  }
}
```

---

## 🔧 Настройка планов

### **Пример настройки план данных**
```json
{
  "MONTHLY_FEE": 30.0,
  "CREDIT": 10.0,
  "PREPAID": 1073741824,
  "INTERVALS": [
    [86400, {"internet": [1, 0.01, 0.015]}]
  ],
  "SHAPER": "1024k",
  "ACCESS_INTERVALS": [
    [86400, "accept", "unlimited"]
  ]
}
```

### **Различные типы тарификации**
1. **Фиксированная абонплата**: `MONTHLY_FEE: 25.0`
2. **Без абонплаты**: не указывать `MONTHLY_FEE` или `0`
3. **Индивидуальная плата**: задавать в plan_data каждого аккаунта
4. **Корпоративные тарифы**: использовать `default_monthly_fee` в конфиге

---

## 🚨 Обработка ошибок

### **Недостаток средств**
```json
{
  "account_id": 123,
  "status": "failed",
  "failure_reason": "insufficient_funds",
  "amount": 25.0,
  "balance": 10.0,
  "credit": 5.0
}
```

### **Опции при недостатке средств**
1. **Оставить активным** (`disable_on_insufficient_funds: false`)
2. **Отключить аккаунт** (`disable_on_insufficient_funds: true`)
3. **Льготный период** (`grace_period_days: 3`)

---

## 🔄 Миграция со старой системы

### **Шаги миграции**
1. **✅ Схема БД не изменяется** - полная совместимость
2. **✅ Активные сессии сохраняются** 
3. **✅ Балансы и транзакции не затрагиваются**
4. **✅ Plan_data формат остается прежним**

### **Добавление новой функциональности**
```sql
-- Добавить MONTHLY_FEE в существующие планы
UPDATE accounts SET plan_data = 
    CASE 
        WHEN plan_data = '' THEN '{"MONTHLY_FEE": 25.0}'
        ELSE plan_data::jsonb || '{"MONTHLY_FEE": 25.0}'::jsonb
    END
WHERE plan_id IN (1, 2, 3);  -- ID тарифных планов с абонплатой
```

---

## 📋 Примеры использования

### **1. Настройка автоматических списаний**
```bash
# 1. Обновить config.yaml
subscription:
  enabled: true
  default_monthly_fee: 25.0

# 2. Перезапустить сервис
systemctl restart netspire-go

# 3. Проверить логи
tail -f /var/log/netspire-go.log | grep "subscription"
```

### **2. Ручной запуск списаний**
```bash
# Через CLI
./subscription-processor process 2024-01-01

# Через API
curl -X POST http://localhost:8080/api/v1/subscription/process/2024-01-01
```

### **3. Мониторинг результатов**
```bash
# Статистика через CLI
./subscription-processor stats

# История конкретного пользователя
./subscription-processor history 123

# Через API
curl http://localhost:8080/api/v1/subscription/stats
```

---

## 🔐 Безопасность

### **Защита от двойных списаний**
- ✅ Проверка существующих транзакций за период
- ✅ Уникальные комментарии с датами периода
- ✅ Транзакционность операций в PostgreSQL

### **Аудит операций**
- ✅ Полное логирование всех операций
- ✅ Сохранение в `fin_transactions` с детальными комментариями
- ✅ API для получения истории списаний

### **Отказоустойчивость**
- ✅ Обработка каждого аккаунта в отдельной транзакции
- ✅ Продолжение работы при ошибках отдельных аккаунтов
- ✅ Детальное логирование ошибок

---

## 📞 Поддержка

Система автоматических списаний полностью интегрирована в netspire-go и обеспечивает:

- **100% совместимость** с существующей базой данных
- **Простую настройку** через config.yaml
- **Гибкое управление** через API и CLI
- **Детальную отчетность** и мониторинг
- **Надежную работу** с обработкой ошибок

Для получения поддержки обращайтесь к логам системы и документации API. 