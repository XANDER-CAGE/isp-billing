# Traffic Classification System (tclass)

Система классификации трафика обеспечивает продвинутую категоризацию сетевого трафика на основе IP-адресов и протоколов. Это полная 1:1 реализация оригинального `tclass.erl` модуля из Netspire.

## Возможности

### IP-based Classification
- Быстрый поиск с использованием binary search tree
- Поддержка CIDR нотации
- Классификация локальных, CDN, premium и интернет сетей
- Проверка на пересекающиеся диапазоны IP

### Protocol-based Classification  
- Классификация по портам назначения и источника
- Поддержка популярных протоколов (HTTP/HTTPS, FTP, SSH, VoIP и др.)
- Определение зашифрованного трафика
- Конфигурируемые правила протоколов

### Enhanced Classification
- Комбинирование IP и протокольной классификации
- Расчет приоритета трафика
- Определение типа шифрования
- Подробная информация о классификации

## Архитектура

```
internal/services/billing/tclass/
├── types.go       - Общие типы и константы
├── advanced.go    - IP классификация с binary search tree
├── config.go      - Загрузка и управление конфигурацией
└── protocols.go   - Протокольная классификация
```

## Использование

### Основное API

```go
// Создание сервиса
tclassService := tclass.New(logger)
protocolClassifier := tclass.NewProtocolClassifier(logger)
enhancedClassifier := tclass.NewEnhancedClassifier(tclassService, protocolClassifier, logger)

// Загрузка конфигурации
config := tclass.GetDefaultConfig()
err := tclassService.Load(config.Classes)

// Классификация IP
class := tclassService.Classify(net.ParseIP("192.168.1.1"), tclass.ClassDefault)

// Расширенная классификация
classification := enhancedClassifier.ClassifyTraffic(
    net.ParseIP("192.168.1.1"),  // src IP
    net.ParseIP("8.8.8.8"),      // dst IP  
    12345,                       // src port
    443,                         // dst port (HTTPS)
)
```

### HTTP API Endpoints

**GET /api/v1/tclass/stats** - Статистика классификации
```json
{
  "classification_stats": {
    "loaded": true,
    "depth": 3,
    "nodes": 7
  },
  "timestamp": 1699123456
}
```

**POST /api/v1/tclass/classify** - Классификация трафика
```json
{
  "src_ip": "192.168.1.100",
  "dst_ip": "8.8.8.8", 
  "src_port": 12345,
  "dst_port": 443
}
```

Ответ:
```json
{
  "ip_class": "cdn",
  "protocol_class": "https",
  "port": 443,
  "is_encrypted": true,
  "priority": 8
}
```

**GET /api/v1/tclass/classify/{ip}** - Классификация IP адреса
```json
{
  "ip": "192.168.1.1",
  "class": "local", 
  "found": true
}
```

## Конфигурация

### config.yaml

```yaml
traffic_classification:
  enabled: true
  default_class: "default"
  reload_interval: 300
  classes:
    - class: "local"
      networks:
        - "192.168.0.0/16"
        - "10.0.0.0/8"
        - "172.16.0.0/12"
    - class: "cdn"
      networks:
        - "8.8.8.0/24"       # Google DNS
        - "1.1.1.0/24"       # Cloudflare
    - class: "premium"
      networks:
        - "91.108.56.0/24"   # Telegram
        - "157.240.0.0/17"   # Facebook/Meta
    - class: "internet"
      networks:
        - "0.0.0.0/0"        # Default route
  protocol_rules:
    - protocol: "voip"
      ports: [5060, 5061, 1720, 2427]
      priority: 10
    - protocol: "dns"
      ports: [53]
      priority: 9
```

## Классы трафика

### IP Classes
- **local** - Локальные сети (RFC 1918)
- **cdn** - CDN и DNS сервисы
- **premium** - Премиум сервисы (социальные сети, мессенджеры)
- **internet** - Общий интернет трафик
- **default** - Класс по умолчанию

### Protocol Classes
- **http/https** - Web трафик
- **voip** - VoIP протоколы (высокий приоритет)
- **dns** - DNS запросы (высокий приоритет)
- **ssh** - SSH соединения
- **ftp** - File Transfer Protocol
- **gaming** - Игровой трафик
- **p2p** - Peer-to-peer (низкий приоритет)
- **streaming** - Потоковое видео

## Совместимость с Erlang

Система полностью совместима с оригинальным `tclass.erl`:

- Такой же алгоритм binary search tree
- Совместимый формат конфигурации
- Эквивалентные функции API
- Аналогичная проверка пересечений IP диапазонов

## Производительность

- **O(log n)** время поиска для IP классификации
- **O(1)** время поиска для протокольной классификации  
- Малое потребление памяти
- Thread-safe операции с RWMutex

## Мониторинг

Система предоставляет метрики:
- Количество загруженных правил
- Глубина дерева поиска
- Статистика классификации по протоколам
- Время последнего обновления конфигурации

## Тестирование

```go
// Тест классификации
tclassService.TestClassification()

// Проверка в логах:
// INFO Classification result ip=192.168.1.1 class=local
// INFO Classification result ip=8.8.8.8 class=cdn
```

## Расширение

Система легко расширяется:

1. Добавление новых классов трафика в `types.go`
2. Создание кастомных правил протоколов
3. Интеграция с внешними источниками IP списков
4. Добавление метрик и мониторинга

## Миграция с Erlang

Для миграции с оригинального `tclass.erl`:

1. Экспортировать конфигурацию из Erlang формата
2. Адаптировать к YAML формату
3. Загрузить через `LoadFromYAML()`
4. Протестировать совместимость

Система поддерживает все оригинальные функции Erlang модуля и может быть прямой заменой без изменения логики приложения. 