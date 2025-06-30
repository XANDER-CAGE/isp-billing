# Session Management в Netspire-Go

## 📋 **Обзор**

Session Management является третьим критически важным компонентом netspire-go, обеспечивающим полную совместимость с Erlang системой (`iptraffic_session.erl`, `iptraffic_sup.erl`).

## 🔄 **Архитектура**

### **Основные компоненты:**

1. **Session Service** (`internal/services/session/service.go`)
   - Управление жизненным циклом сессий
   - Интеграция с NetFlow
   - Синхронизация с PostgreSQL
   - Supervisor для session workers

2. **Session Models** (`internal/models/session.go`)
   - Полная модель IPTrafficSession
   - SessionContext для инициализации
   - TrafficClassDetail для детализации

3. **HTTP API** (`internal/handlers/session.go`)
   - RESTful API для управления сессиями
   - Интеграция с FreeRADIUS

## 🚀 **Жизненный цикл сессии**

### **1. Инициализация (Init)**
```http
POST /api/v1/session/init
{
  "username": "testuser"
}
```
- Создает новую сессию в состоянии "new"
- Генерирует UUID сессии
- Проверяет отсутствие активных сессий пользователя

### **2. Подготовка (Prepare)**
```http
POST /api/v1/session/prepare
{
  "session_uuid": "...",
  "account_id": 123,
  "username": "testuser",
  "plan_id": 1,
  "plan_data": {...},
  "currency": 1,
  "balance": 100.0,
  "auth_algo": "prepaid_auth",
  "acct_algo": "prepaid_auth",
  "replies": [...],
  "nas_spec": {...}
}
```
- Загружает контекст биллинга из RADIUS авторизации
- Сохраняет plan_data и алгоритмы
- Готовит сессию к активации

### **3. Активация (Start)**
```http
POST /api/v1/session/start
{
  "username": "testuser",
  "sid": "session123",
  "cid": "AA:BB:CC:DD:EE:FF",
  "ip": "192.168.100.10"
}
```
- Переводит сессию в состояние "active"
- Создает запись в БД `iptraffic_sessions`
- Запускает session worker для timeout'ов
- Индексирует по IP, SID, username

### **4. Interim Updates**
```http
POST /api/v1/session/interim
{
  "sid": "session123"
}
```
- Продлевает timeout сессии
- Обновляет аренду IP (если IP pool активен)
- Синхронизирует счетчики

### **5. Завершение (Stop)**
```http
POST /api/v1/session/stop
{
  "sid": "session123"
}
```
- Переводит в состояние "stopping"
- Запускает delayed stop (delay_stop секунд)
- Освобождает IP адрес
- Финализирует в БД

## 🌐 **NetFlow интеграция**

### **Обработка трафика:**
```http
POST /api/v1/session/netflow
{
  "direction": "in",
  "src_ip": "8.8.8.8",
  "dst_ip": "192.168.100.10",
  "octets": 1024,
  "packets": 10
}
```

**Алгоритм обработки:**
1. Определение target IP по направлению
2. Поиск активной сессии по IP
3. Классификация трафика
4. Вызов биллингового алгоритма
5. Обновление счетчиков сессии
6. Сохранение детализации по классам

## 📊 **API Endpoints**

### **Управление сессиями:**
- `POST /api/v1/session/init` - Инициализация
- `POST /api/v1/session/prepare` - Подготовка
- `POST /api/v1/session/start` - Активация
- `POST /api/v1/session/interim` - Interim update
- `POST /api/v1/session/stop` - Завершение
- `POST /api/v1/session/expire` - Принудительное истечение

### **Поиск сессий:**
- `GET /api/v1/session/ip/{ip}` - По IP адресу
- `GET /api/v1/session/username/{username}` - По имени пользователя
- `GET /api/v1/session/sid/{sid}` - По Session ID
- `GET /api/v1/sessions` - Список всех сессий (с пагинацией)
- `GET /api/v1/sessions/stats` - Статистика

### **NetFlow:**
- `POST /api/v1/session/netflow` - Обработка NetFlow данных

## ⚙️ **Конфигурация**

```yaml
session:
  session_timeout: 3600           # Timeout сессии (секунды)
  sync_interval: 30               # Синхронизация с БД
  delay_stop: 5                   # Задержка перед stop
  disconnect_on_shutdown: true    # Disconnect при завершении
  max_sessions: 10000             # Лимит сессий
  cleanup_interval: 60            # Очистка expired
  max_sessions_per_user: 1        # Лимит на пользователя
```

## 🔧 **Background Tasks**

### **1. Sync Task**
- Периодическая синхронизация с PostgreSQL
- Обновляет счетчики трафика в БД
- Сохраняет изменения plan_data
- Записывает детализацию по классам

### **2. Cleanup Task**
- Очистка expired сессий
- Отправка disconnect requests
- Освобождение ресурсов
- Удаление из кеша

### **3. Session Workers**
- Индивидуальный worker на каждую сессию
- Отслеживание timeout'ов
- Автоматическое истечение
- Graceful shutdown

## 💾 **Интеграция с БД**

### **Таблица `iptraffic_sessions`:**
- Полная совместимость с Erlang схемой
- Сохранение всех счетчиков трафика
- Tracking expired статуса
- Интеграция с биллингом

### **Таблица `session_details`:**
- Детализация по классам трафика
- Раздельные счетчики in/out
- Поддержка множественных классов

## 🔄 **Совместимость с Erlang**

### **100% эквивалентные функции:**
- `init_session/1` → `InitSession()`
- `prepare/5` → `PrepareSession()`  
- `start/4` → `StartSession()`
- `interim/1` → `InterimUpdate()`
- `stop/1` → `StopSession()`
- `expire/1` → `ExpireSession()`
- `handle_cast({netflow, ...})` → `HandleNetFlow()`

### **Состояния сессий:**
- `new` - Инициализирована, не активна
- `starting` - Процесс активации
- `active` - Активная сессия
- `stopping` - Процесс завершения
- `stopped` - Завершена
- `expired` - Истекла по timeout

## 📈 **Мониторинг**

### **Статистика сессий:**
```json
{
  "total_sessions": 150,
  "active_sessions": 120,
  "expired_sessions": 20,
  "stopped_sessions": 10,
  "max_sessions": 10000
}
```

### **Метрики для мониторинга:**
- Количество активных сессий
- Скорость создания/завершения
- Использование памяти Redis
- Нагрузка на БД синхронизации

## 🧪 **Тестирование**

### **Пример жизненного цикла:**
```bash
# 1. Инициализация
curl -X POST http://localhost:8080/api/v1/session/init \
  -H "Content-Type: application/json" \
  -d '{"username":"testuser"}'

# 2. Подготовка  
curl -X POST http://localhost:8080/api/v1/session/prepare \
  -H "Content-Type: application/json" \
  -d '{
    "session_uuid":"uuid-here",
    "account_id":123,
    "username":"testuser",
    "plan_id":1,
    "currency":1,
    "balance":100.0,
    "auth_algo":"prepaid_auth",
    "acct_algo":"prepaid_auth"
  }'

# 3. Активация
curl -X POST http://localhost:8080/api/v1/session/start \
  -H "Content-Type: application/json" \
  -d '{
    "username":"testuser",
    "sid":"session123",
    "cid":"AA:BB:CC:DD:EE:FF", 
    "ip":"192.168.100.10"
  }'

# 4. NetFlow данные
curl -X POST http://localhost:8080/api/v1/session/netflow \
  -H "Content-Type: application/json" \
  -d '{
    "direction":"in",
    "src_ip":"8.8.8.8",
    "dst_ip":"192.168.100.10",
    "octets":1024,
    "packets":10
  }'

# 5. Завершение
curl -X POST http://localhost:8080/api/v1/session/stop \
  -H "Content-Type: application/json" \
  -d '{"sid":"session123"}'
```

## 🔌 **Интеграция с другими компонентами**

### **IP Pool Management:**
- Автоматическое получение IP при старте
- Продление аренды при interim
- Освобождение при stop

### **Disconnect Management:**
- Автоматический disconnect expired сессий
- Интеграция с RADIUS Disconnect-Request
- Script-based disconnect поддержка

### **Billing Service:**
- Вызов алгоритмов при NetFlow
- Обновление plan_data
- Списание средств

## 🚨 **Error Handling**

### **Типичные ошибки:**
- Дублирование активных сессий пользователя
- Timeout подключения к БД при синхронизации
- Невалидные IP адреса в NetFlow
- Expired сессии в запросах

### **Recovery механизмы:**
- Graceful degradation при недоступности БД
- Retry логика для синхронизации
- Автоматическая очистка зависших сессий

## 📋 **Status**

- ✅ **Session Models** - 100% Complete
- ✅ **Session Service** - 100% Complete  
- ✅ **HTTP Handlers** - 100% Complete
- ✅ **Configuration** - 100% Complete
- ✅ **DB Integration** - 100% Complete
- ✅ **NetFlow Integration** - 100% Complete
- ✅ **Background Tasks** - 100% Complete
- ✅ **Erlang Compatibility** - 100% Complete

## 🎯 **Итог**

Session Management полностью реализован и готов к продакшн использованию. Обеспечивает:

- **100% совместимость** с Erlang системой
- **Полный жизненный цикл** сессий
- **NetFlow интеграцию** для биллинга
- **Надежную синхронизацию** с БД
- **Масштабируемость** до 10,000+ сессий
- **Graceful shutdown** и recovery

Система готова к интеграции с FreeRADIUS и может полностью заменить Erlang компоненты. 