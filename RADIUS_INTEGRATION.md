# RADIUS Authentication Integration

Вместо реализации встроенного RADIUS сервера мы используем **FreeRADIUS** с REST API интеграцией. Это обеспечивает:

- ✅ Полную поддержку всех методов аутентификации (PAP, CHAP, MS-CHAP-v2, EAP-MD5)
- ✅ Зрелое и проверенное временем решение
- ✅ Меньше кода для поддержки
- ✅ Высокую производительность и надежность

## Архитектура

```
NAS Device ←→ FreeRADIUS ←→ REST API ←→ Netspire-Go
```

**FreeRADIUS** обрабатывает RADIUS протокол и все методы аутентификации, а **Netspire-Go** предоставляет биллинговую логику через REST API.

## Поддерживаемые методы аутентификации

### 1. PAP (Password Authentication Protocol)
- FreeRADIUS получает `User-Password` 
- Отправляет в REST API с `Cleartext-Password`
- Netspire-Go возвращает пароль для сравнения

### 2. CHAP (Challenge Handshake Authentication Protocol)
- FreeRADIUS обрабатывает `CHAP-Password` и `CHAP-Challenge`
- Получает `Cleartext-Password` от API
- Выполняет CHAP проверку локально

### 3. MS-CHAP-v2 (Microsoft CHAP version 2)
- FreeRADIUS обрабатывает `MS-CHAP-Challenge` и `MS-CHAP2-Response`
- Поддержка MPPE ключей для шифрования
- Генерация `MS-CHAP2-Success` ответов

### 4. EAP-MD5 (Extensible Authentication Protocol - MD5)
- FreeRADIUS управляет EAP состоянием
- Обработка `EAP-Message` и `State` атрибутов
- Challenge/Response механизм

## REST API Endpoints

### Authorization - `/api/v1/radius/authorize`
**POST** запрос с полными RADIUS атрибутами:

```json
{
  "username": "user@domain.com",
  "password": "cleartext_password",
  "nas_ip_address": "192.168.1.1", 
  "nas_port": 1,
  "auth_type": "PAP",
  "attributes": {
    "CHAP-Password": "...",
    "MS-CHAP-Challenge": "...",
    "EAP-Message": "..."
  }
}
```

**Response:**
```json
{
  "result": "accept",
  "attributes": {
    "Cleartext-Password": "user_password",
    "Service-Type": "Framed-User",
    "Pool-Name": "pool1",
    "Download-Speed": "10000000",
    "Upload-Speed": "1000000"
  }
}
```

### Accounting - `/api/v1/radius/accounting`
**POST** запрос для Start/Stop/Interim-Update:

```json
{
  "username": "user@domain.com",
  "session_id": "50000001",
  "acct_status_type": "Start",
  "framed_ip_address": "10.0.0.100",
  "acct_input_octets": 1000000,
  "acct_output_octets": 5000000
}
```

### Post-Auth - `/api/v1/radius/post-auth`
**POST** запрос после успешной аутентификации:

```json
{
  "username": "user@domain.com",
  "auth_type": "MS-CHAP-v2",
  "session_id": "50000001"
}
```

## FreeRADIUS Configuration

### 1. sites-available/default
```
server default {
    listen {
        type = auth
        ipaddr = *
        port = 1812
    }
    
    listen {
        type = acct
        ipaddr = *
        port = 1813
    }
    
    authorize {
        netspire_rest
    }
    
    authenticate {
        Auth-Type PAP {
            pap
        }
        Auth-Type CHAP {
            chap
        }
        Auth-Type MS-CHAP {
            mschap
        }
        Auth-Type EAP {
            eap
        }
    }
    
    post-auth {
        netspire_rest
    }
    
    accounting {
        netspire_rest
    }
}
```

### 2. mods-enabled/netspire_rest
Символическая ссылка на `netspire-rest-enhanced` конфигурацию.

### 3. clients.conf
```
client nas1 {
    ipaddr = 192.168.1.0/24
    secret = shared_secret_key
    shortname = nas1
    nastype = cisco
}
```

## Особенности реализации

### Обработка паролей
- **PAP**: Пароль передается в открытом виде в `User-Password`
- **CHAP**: FreeRADIUS требует `Cleartext-Password` для CHAP вычислений
- **MS-CHAP-v2**: FreeRADIUS выполняет все MS-CHAP операции локально
- **EAP-MD5**: Поддержка challenge/response через `State` атрибут

### IP Pool Integration
```json
{
  "result": "accept",
  "attributes": {
    "Pool-Name": "pool1"
  }
}
```

FreeRADIUS передает `Pool-Name` в sqlippool модуль для назначения IP.

### Session Management
1. **Start**: Создание сессии в Redis
2. **Interim**: Обновление счетчиков трафика
3. **Stop**: Завершение сессии и биллинг

### Bandwidth Control
```json
{
  "attributes": {
    "Download-Speed": "10000000",
    "Upload-Speed": "1000000"
  }
}
```

Атрибуты скорости передаются в NAS для применения QoS.

## Безопасность

### 1. Shared Secrets
- Каждый NAS имеет уникальный secret
- Secrets настраиваются в `clients.conf`

### 2. Message Authentication
- RADIUS пакеты аутентифицируются Message-Authenticator
- Защита от replay атак

### 3. REST API Security
- HTTP Basic Auth для REST API
- TLS шифрование (HTTPS)
- IP whitelist для FreeRADIUS

## Monitoring

### Health Check
```bash
curl http://localhost:8080/api/v1/radius/health
```

### RADIUS Statistics
FreeRADIUS предоставляет статистику через `radmin`:
```bash
echo "stats detail" | radmin
```

### Logs
- FreeRADIUS: `/var/log/freeradius/`
- Netspire-Go: через zap logger

## Debugging

### 1. FreeRADIUS Debug Mode
```bash
freeradius -X
```

### 2. REST Module Debug
```bash
tail -f /var/log/freeradius/radius.log | grep rest
```

### 3. Test Authentication
```bash
echo "User-Name=test,User-Password=test123" | radclient localhost:1812 auth secret
```

## Performance

### Connection Pooling
- REST модуль использует connection pool
- Максимум 20 соединений к Netspire-Go
- Automatic retry при ошибках

### Caching
- FreeRADIUS кеширует авторизацию
- TTL настраивается в cache модуле

### Load Balancing
Можно настроить несколько Netspire-Go инстансов:
```
uri = "http://netspire1:8080/api/v1"
# Fallback to:
# uri = "http://netspire2:8080/api/v1"
```

## Migration Benefits

✅ **Zero Risk**: FreeRADIUS проверен в production  
✅ **Feature Complete**: Все методы аутентификации работают  
✅ **Scalable**: Легко масштабируется  
✅ **Maintainable**: Меньше кода для поддержки  
✅ **Compatible**: Работает со всеми NAS устройствами  

Такой подход позволяет сосредоточиться на биллинговой логике, не тратя время на реализацию RADIUS протокола с нуля. 