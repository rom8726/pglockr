# pglockr — живой визуализатор локов и блокировок PostgreSQL

> Техническое задание. Документ самодостаточен: его можно вставить в новый чат как полный контекст проекта.

---

## 1. Идея и контекст

### Проблема
Когда продовая база встаёт колом из-за блокировок, дежурный инженер обычно действует так: лезет в `psql`, копипастит «тот самый» blocking-tree запрос со StackOverflow, вручную сопоставляет PID'ы с текстами запросов, прикидывает, кто корень проблемы, и гадает, кого безопасно убить. Это медленно и нервно — ровно в тот момент, когда счёт идёт на минуты.

Существующие инструменты тему не закрывают:
- `pgAdmin` показывает сессии плоским списком с кривым намёком на блокировки, без дерева и без истории.
- Коммерческие APM (pganalyze, Datadog DBM) дают аналитику постфактум, но не «живую» картину «кто кого держит прямо сейчас» с действиями в один клик, и стоят денег.
- EXPLAIN-визуализаторы (pev2, depesz, pgMustard) — про планы запросов, не про блокировки.

Нормального опенсорсного инструмента «живое дерево блокировок + история + действия», заточенного под инцидент, по сути нет. Это и есть ниша.

### Что это
`pglockr` — самостоятельный инструмент (один Go-бинарь с embedded web-UI), который подключается к целевой базе PostgreSQL и в реальном времени показывает **wait-for forest** (лес деревьев ожидания блокировок): кто кого блокирует, как давно, на каком объекте и каким локом. По клику на сессию — её запрос, удерживаемые и ожидаемые локи, и действия (cancel / terminate). Есть скраббер по истории снапшотов для разбора инцидента постфактум.

### Ценность
Сокращение time-to-resolution на инциденте. Дежурный сразу видит граф, видит голову цепочки (корневого блокировщика) и бьёт точно по ней, а не по жертвам. История даёт ответ на вопрос «с чего всё началось».

### Целевой пользователь
Backend-инженеры on-call, SRE, DBA — те, кто разбирает инциденты с блокировками на проде.

### Нейминг
Рабочее имя — `pglockr`. Альтернативы: `gridlock`, `logjam`, `pgtangle`, `knot`. Выбрали `pglockr`.

---

## 2. Цели и не-цели

### Цели (в скоупе)
- Живое дерево блокировок с обновлением раз в ~1 секунду.
- Корректное построение графа через `pg_blocking_pids()` (не наивный self-join).
- Детали по сессии: запрос, удерживаемые/ожидаемые локи, возраст, wait event, клиент.
- Действия: `pg_cancel_backend` / `pg_terminate_backend` с подтверждением и аудитом.
- История снапшотов (time-travel) с ползунком по времени.
- Lock inspector: сырой `pg_locks`, сгруппированный по объектам.
- Hot objects: самые контендящиеся таблицы.
- Алерты по порогам (глубина цепочки, длительность ожидания) в webhook/Slack.
- Несколько целевых кластеров в одном инстансе.
- Собственная аутентификация и RBAC (инструмент видит тексты чужих запросов и умеет рвать коннекты).

### Не-цели (явно вне скоупа на старте)
- Анализ планов запросов / EXPLAIN (это отдельный домен).
- Полноценный мониторинг метрик БД (это делает Prometheus/Grafana/pganalyze).
- Тюнинг конфигурации Postgres, советы по индексам.
- Управление схемой/миграциями.
- Поддержка СУБД кроме PostgreSQL (MySQL и т.п. — потенциально позже, но архитектурно не закладываемся).

---

## 3. Доменная модель PostgreSQL (фундамент)

Три источника данных в целевой БД:

**`pg_stat_activity`** — по каждому бэкенду: `pid`, `usename`, `application_name`, `client_addr`, `state`, `wait_event_type`, `wait_event`, `backend_type`, `xact_start`, `query_start`, `query`, `backend_xid`, `query_id` (PG14+).

**`pg_locks`** — все запрошенные и выданные блокировки: `locktype`, `mode`, `granted`, `relation` (OID), `pid`, `virtualtransaction`, `waitstart` (момент начала ожидания, PG14+), плюс идентификаторы объекта (`database`, `relation`, `page`, `tuple`, `virtualxid`, `transactionid`, `classid`, `objid`, `objsubid`).

**`pg_blocking_pids(pid)`** — функция, возвращающая массив PID'ов, реально блокирующих данный бэкенд. **Это ключевой примитив для построения рёбер графа.** Она корректно учитывает мягкие/жёсткие конфликты, group locking параллельных воркеров и fastpath-локи — то, на чём самописные self-join'ы по `pg_locks` врут.

### Принцип построения графа
1. Берём бэкенды, которые реально стоят в ожидании лока (`wait_event_type = 'Lock'`).
2. Для каждого такого PID зовём `pg_blocking_pids(pid)` → получаем рёбра `blocked → blocker` (источник истины по структуре графа).
3. Узлы обогащаем данными из `pg_stat_activity`.
4. Метку ребра (объект + конфликтующие моды локов) достаём отдельным join'ом `pg_locks` ждущего (`granted = false`) с `pg_locks` блокера (`granted = true`) по общему объекту, отфильтровав по уже известным парам `(waiter, blocker)`.

Корни леса — «головные блокировщики»: держат чужие локи, но сами ни на кого не ждут.

---

## 4. Архитектура

Один Go-бинарь, embedded UI через `go:embed`. Подключение к целевым базам по DSN.

```
┌──────────────────────────────────────────────────────┐
│ pglockr (single binary)                                │
│                                                        │
│  ┌─────────┐   ┌──────────┐   ┌──────────┐            │
│  │ Poller  │──▶│  Store   │──▶│  Graph   │            │
│  │ (pgx)   │   │ (ring +  │   │ builder  │            │
│  └────┬────┘   │  sqlite) │   └────┬─────┘            │
│       │        └──────────┘        │                  │
│       │                            ▼                  │
│  ┌────▼─────┐                ┌──────────┐   ┌───────┐ │
│  │ Signal   │                │   API    │──▶│  WS   │ │
│  │ (cancel/ │◀───────────────│ (REST +  │   │ /SSE  │ │
│  │ kill)    │                │  WS)     │   └───┬───┘ │
│  └──────────┘                └────┬─────┘       │     │
│                                   │       ┌─────▼────┐│
│                              ┌────▼─────┐ │ embedded ││
│                              │  Auth /  │ │   UI     ││
│                              │  RBAC    │ │ (React)  ││
│                              └──────────┘ └──────────┘│
└──────────────────┬─────────────────────────────────────┘
                   │ pgx pool (read-only role + signal role)
                   ▼
         ┌───────────────────────┐
         │ target PostgreSQL      │ (один или несколько кластеров)
         └───────────────────────┘
```

Компоненты:
- **Poller** — раз в N секунд снимает снапшот целевой БД, строит граф, кладёт в Store, пушит подписчикам.
- **Store** — кольцевой буфер последних M снапшотов в памяти + опциональная персистентность в SQLite (для истории за пределами процесса).
- **Graph builder** — превращает снапшот в wait-for forest (узлы + рёбра + метки).
- **API** — REST для запросов/действий, WebSocket (или SSE) для live-стрима.
- **Signal** — выполняет cancel/terminate под отдельной ролью, пишет аудит.
- **Auth/RBAC** — аутентификация пользователей инструмента и разграничение прав (view vs act).
- **Embedded UI** — React-приложение, вшитое в бинарь.

---

## 5. Бэкенд (детально)

### 5.1 Стек
- Go 1.26+.
- `pgxpool` для подключения к целевым БД.
- `chi` (или stdlib `net/http` + роутер) для HTTP API.
- `nhooyr.io/websocket` (или `coder/websocket`) для стрима.
- `modernc.org/sqlite` или `mattn/go-sqlite3` для персистентности истории.
- `slog` для логирования.

### 5.2 Модель данных (sketch)

```go
type Snapshot struct {
    Cluster  string
    TakenAt  time.Time
    Sessions map[int]Session   // keyed by PID
    Edges    []Edge
    Roots    []int             // head blockers
}

type Session struct {
    PID           int
    User          string
    AppName       string
    ClientAddr    string
    State         string        // active | idle in transaction | ...
    WaitEventType string
    WaitEvent     string
    BackendType   string
    XactStart     time.Time
    QueryStart    time.Time
    WaitStart     time.Time      // PG14+, zero on older
    Query         string
    BlockedBy     []int          // from pg_blocking_pids
    IsRoot        bool           // holds locks, waits for nobody
}

type Edge struct {
    WaiterPID   int
    BlockerPID  int
    LockType    string           // relation | tuple | transactionid | ...
    Relation    string           // resolved name or OID fallback
    WaiterMode  string           // e.g. AccessExclusiveLock
    BlockerMode string
}
```

### 5.3 Алгоритм построения графа
1. Один запрос → все client-бэкенды + `pg_blocking_pids(pid)` (см. 5.4).
2. Узлы с непустым `BlockedBy` — ждущие; формируем рёбра `(waiter, blocker)`.
3. Узлы, которые присутствуют как `blocker` у кого-то, но сами с пустым `BlockedBy` — корни (`IsRoot = true`).
4. Метки рёбер — отдельным запросом (5.5), мапим по паре `(waiter, blocker)`.
5. Защита от циклов: дедлок Postgres разрулит сам, но в моменте граф может содержать цикл между снапшотами — строим как DAG, при обнаружении цикла помечаем и не зацикливаемся в обходе (`visited` set).

### 5.4 SQL: снапшот сессий

```sql
SELECT
    a.pid,
    a.usename,
    a.application_name,
    a.client_addr,
    a.state,
    a.wait_event_type,
    a.wait_event,
    a.backend_type,
    a.xact_start,
    a.query_start,
    a.query,
    pg_blocking_pids(a.pid) AS blocked_by
FROM pg_stat_activity a
WHERE a.backend_type = 'client backend'
  AND a.pid <> pg_backend_pid();
```

> На PG14+ длительность ожидания берётся из `pg_locks.waitstart`; на старых версиях аппроксимируется через `query_start` ждущего бэкенда (см. 5.11).

### 5.5 SQL: метки рёбер (объект + моды)

Структуру графа даёт `pg_blocking_pids`. Этот запрос только обогащает рёбра — какой объект и какие конфликтующие моды:

```sql
SELECT
    w.pid                       AS waiter_pid,
    b.pid                       AS blocker_pid,
    w.locktype,
    w.mode                      AS waiter_mode,
    b.mode                      AS blocker_mode,
    w.relation::regclass::text  AS relation
FROM pg_locks w
JOIN pg_locks b
  ON  w.locktype      = b.locktype
  AND w.database      IS NOT DISTINCT FROM b.database
  AND w.relation      IS NOT DISTINCT FROM b.relation
  AND w.page          IS NOT DISTINCT FROM b.page
  AND w.tuple         IS NOT DISTINCT FROM b.tuple
  AND w.virtualxid    IS NOT DISTINCT FROM b.virtualxid
  AND w.transactionid IS NOT DISTINCT FROM b.transactionid
  AND w.classid       IS NOT DISTINCT FROM b.classid
  AND w.objid         IS NOT DISTINCT FROM b.objid
  AND w.objsubid      IS NOT DISTINCT FROM b.objsubid
  AND w.pid <> b.pid
WHERE NOT w.granted
  AND b.granted;
```

> В коде фильтруем результат по парам `(waiter, blocker)`, уже подтверждённым `pg_blocking_pids`, — чтобы не нарисовать ложные рёбра там, где конфликта на самом деле нет.

### 5.6 SQL: lock inspector

```sql
SELECT
    l.locktype,
    COALESCE(l.relation::regclass::text, l.locktype) AS object,
    l.mode,
    l.granted,
    l.pid
FROM pg_locks l
WHERE l.pid <> pg_backend_pid()
ORDER BY object, l.granted DESC, l.mode;
```

### 5.7 SQL: hot objects

```sql
SELECT
    l.relation::regclass::text AS object,
    count(*) FILTER (WHERE NOT l.granted) AS waiters,
    count(*) FILTER (WHERE l.granted)     AS holders
FROM pg_locks l
WHERE l.relation IS NOT NULL
GROUP BY object
HAVING count(*) FILTER (WHERE NOT l.granted) > 0
ORDER BY waiters DESC, holders DESC;
```

### 5.8 Poller
- Конфигурируемый интервал (default 1s), на узел — backoff при ошибках.
- **`pg_blocking_pids` дороговат** на нагруженной базе — не дёргать чаще необходимого и желательно только для бэкендов в lock-wait. На старте допустимо звать для всех, но заложить флаг «звать только для ждущих» и адаптивный интервал.
- Снимать снапшот в одном соединении из пула, короткие запросы, statement_timeout на стороне инструмента.
- Диффовать снапшоты по PID для эффективного пуша во фронт (что появилось/исчезло/изменилось).

### 5.9 Store / история (time-travel)
- Ring buffer последних M снапшотов в памяти (например, 5 минут с шагом 1s ≈ 300 снапшотов).
- Опциональная персистентность в SQLite: писать снапшоты (сжатые JSON или нормализованные таблицы) для разбора инцидента после рестарта инструмента.
- API для выборки снапшота на момент T и диапазона `[from, to]`.

### 5.10 Signal layer (действия)
- `pg_cancel_backend(pid)` — мягкая отмена текущего запроса.
- `pg_terminate_backend(pid)` — обрыв соединения.
- Доступно только пользователям с ролью `operator` (RBAC, см. 7).
- Каждое действие → запись в аудит-лог (кто, когда, какой PID, какой запрос был, результат).
- UX «убить голову цепочки»: подсветить корень и предложить действие именно по нему.

### 5.11 Версии PostgreSQL
- `pg_blocking_pids` доступен с PG 9.6.
- `pg_locks.waitstart` — с PG 14. На старых версиях длительность ожидания аппроксимируем по `query_start`.
- `query_id` в `pg_stat_activity` — с PG 14.
- При коннекте определять версию (`SHOW server_version_num`) и выбирать вариант запросов. Матрица поддержки: целимся в PG 13–17, мягко деградируя на 12.

### 5.12 OID → имя объекта
- `relation::regclass` резолвится только в той базе, к которой подключён бэкенд инструмента.
- `pg_locks` показывает объекты по **всем** базам кластера — для чужих баз имя по OID не разрезолвится.
- На старте: показывать имя для текущей базы, для остальных — OID + имя базы (по `pg_database`). Позже — пул коннектов на каждую базу или кэш каталога. Это известная заноза, фиксируем как осознанное ограничение MVP.

### 5.13 Алерты
- Правила по порогам: глубина цепочки ≥ N, максимальное ожидание ≥ X секунд, число заблокированных ≥ K.
- Каналы: webhook (generic JSON), Slack incoming webhook.
- Дедуп: не спамить одинаковыми алертами по одной и той же цепочке; слать «resolved», когда цепочка распалась.

### 5.14 Конфигурация
YAML/env. Минимум:
- список кластеров: `name`, `dsn` (через env/secret, не в открытом конфиге);
- интервал опроса, размер ring buffer, включение SQLite-истории и путь;
- пороги алертов и каналы;
- настройки auth (см. 7);
- флаг redaction текстов запросов.

### 5.15 HTTP API (черновик контракта)

```
GET  /api/clusters                      → список целевых кластеров + статус
GET  /api/snapshot?cluster=NAME         → текущий снапшот (forest + sessions)
GET  /api/snapshot?cluster=NAME&at=TS   → ближайший снапшот к моменту TS (history)
GET  /api/history?cluster=NAME&from&to  → метаданные доступных снапшотов в окне
WS   /api/stream?cluster=NAME           → live-поток снапшотов/диффов
GET  /api/locks?cluster=NAME            → lock inspector
GET  /api/hot-objects?cluster=NAME      → hot objects
POST /api/sessions/{pid}/cancel         → pg_cancel_backend  (RBAC: operator)
POST /api/sessions/{pid}/terminate      → pg_terminate_backend (RBAC: operator)
GET  /api/audit                         → журнал действий (RBAC: operator/admin)
GET  /healthz                           → liveness
```

Все ответы — JSON. Действия требуют CSRF/anti-replay и подтверждения на фронте.

---

## 6. Фронтенд (детально)

### 6.1 Стек
- React + TypeScript, Vite, Tailwind.
- Граф: **React Flow** (готовые кастомные ноды, хэндлы, зум/пан) + **dagre** для иерархической раскладки леса. Альтернатива при необходимости тонкого контроля раскладки — Cytoscape.js.
- Состояние графа диффится по PID, чтобы ноды не моргали на каждом тике.
- Live через WebSocket (fallback SSE).
- Dark mode обязателен.

### 6.2 Вьюхи
1. **Blocking forest (герой-вью)** — дерево/лес: корни сверху, рёбра подписаны конфликтом (объект + моды). Цвет кодирует роль: корень-блокировщик — danger, ждущие — warning. Размер/акцент узла — по длительности ожидания цепочки. Клик → деталь-панель.
2. **Detail panel** — для выбранной сессии: полный текст запроса (моноширинный), state, возраст транзакции, wait_event, клиент, удерживаемые и ожидаемые локи; кнопки cancel/terminate (для роли operator).
3. **History scrubber** — ползунок по снапшотам; отмотка к моменту складывания цепочки. Кнопки play/pause для проигрывания истории.
4. **Lock inspector** — отдельная вкладка: `pg_locks`, сгруппированный по объектам, granted vs waiting.
5. **Hot objects** — таблица самых контендящихся таблиц.
6. **Alerts** — текущие активные алерты + настройка порогов.
7. **Cluster switcher** — переключение между целевыми кластерами.

### 6.3 Поведение
- Особо подсвечивать `idle in transaction` у корня — частый виновник.
- Тикающий таймер ожидания на рёбрах/узлах в реальном времени.
- Кнопки действий — с обязательным подтверждением (показать, по кому именно бьём, и текст его запроса).
- Accessibility: семантическая разметка, читаемость в обоих цветовых режимах, навигация с клавиатуры по узлам.

---

## 7. Безопасность и приватность

Инструмент читает **тексты всех запросов** (там бывают чувствительные данные) и умеет **рвать соединения** на проде. Поэтому безопасность — не опция.

### Права в целевой БД
- Для чтения чужих запросов в `pg_stat_activity` нужна роль `pg_monitor` (или `pg_read_all_stats`).
- Для `pg_cancel_backend` / `pg_terminate_backend` чужих бэкендов — роль `pg_signal_backend`. Суперюзеровские бэкенды обычным `pg_signal_backend` не прибить — это ограничение Postgres, отразить в UX.
- Рекомендуемая модель: отдельная роль `pglockr_ro` (`pg_monitor`) для опроса и отдельная `pglockr_op` (`pg_signal_backend`) для действий; действия выполняются под второй ролью только при наличии прав у пользователя инструмента.
- В онбординге дать готовые `GRANT`-скрипты.

### Аутентификация и RBAC в самом инструменте
- Своя аутентификация (на старте — статические пользователи/токены или OIDC позже).
- Роли: `viewer` (только смотреть), `operator` (+ cancel/terminate), `admin` (+ конфиг, аудит).
- Аудит-лог всех действий (неизменяемый, с привязкой к пользователю).

### Приватность
- Опция **redaction** текстов запросов (маскировать литералы / показывать только нормализованный запрос по `query_id`).
- Не складывать чувствительные тексты в персистентную историю, если включён redaction.
- Не светить DSN/секреты в логах и UI.

---

## 8. Подводные камни (где обычно ломаются)

1. **Стоимость `pg_blocking_pids`** на нагруженной базе — звать адресно (только для ждущих) и не слишком часто; backoff.
2. **Межбазовый OID-резолв** — `pg_locks` по всему кластеру, а имя резолвится только в текущей базе (см. 5.12).
3. **Версии PG** — `waitstart`/`query_id` только с 14+; нужна деградация и матрица версий (5.11).
4. **Транзитные дедлоки** — Postgres убивает их сам через `deadlock_timeout`, и они исчезают до следующего опроса. Чтобы показать убитый цикл, нужно тейлить серверный лог (`log_lock_waits = on`) — это отдельная фича, не из вьюх.
5. **Права** — без `pg_monitor` тексты чужих запросов скрыты; UX должен внятно объяснить, какие `GRANT` нужны.
6. **Безопасность самого инструмента** — он мощный; без auth/RBAC/redaction/audit его нельзя ставить рядом с продом.
7. **Нагрузка опроса** — частые `pg_stat_activity` + `pg_locks` на огромном числе сессий не бесплатны; держать запросы лёгкими, мерить собственный оверхед.

---

## 9. MVP (первая итерация)

Цель MVP — уже лучше всего, что есть в опенсорсе, на одном инциденте.

1. Подключение к одной целевой БД по DSN (роль `pg_monitor`).
2. Poller с интервалом 1s; построение wait-for forest через `pg_blocking_pids` + обогащение из `pg_stat_activity`.
3. Герой-вью дерева (React Flow + dagre), окраска по роли/длительности, клик → деталь-панель с текстом запроса и локами.
4. Live-обновление по WebSocket с диффом по PID.
5. Действия cancel/terminate с подтверждением (роль `pg_signal_backend`) + простой аудит в лог.
6. Базовая аутентификация (один токен/пароль) — без полноценного RBAC.
7. Один бинарь, embedded React UI.

Намеренно **вне MVP**: история/скраббер, lock inspector, hot objects, алерты, мультикластер, OIDC, redaction. Но модель данных и API проектируем так, чтобы это докидывалось без переделки.

---

## 10. Роадмап (после MVP)

- **Фаза 2:** история снапшотов + скраббер (ring buffer → SQLite), lock inspector, hot objects.
- **Фаза 3:** алерты (webhook/Slack), RBAC (viewer/operator/admin), redaction текстов запросов.
- **Фаза 4:** мультикластер, межбазовый OID-резолв, тейлинг серверного лога для дедлоков (`log_lock_waits`).
- **Фаза 5:** OIDC/SSO, экспорт инцидента (снимок цепочки + таймлайн), интеграция с алерт-менеджерами.

---

## 11. Нефункциональные требования

- Собственный оверхед опроса на целевую БД — минимальный; запросы лёгкие, с таймаутом со стороны инструмента.
- Время отрисовки графа на типовой цепочке (< 50 узлов) — мгновенное; до ~500 сессий — без деградации UI.
- Один статически слинкованный бинарь, кросс-платформенно (Linux/macOS).
- Реконнект к целевой БД при обрыве, корректная деградация при недоступности кластера.
- Конфиг и секреты — из env/secret-store, не из репозитория.

---

## 12. Развёртывание

- Один бинарь `pglockr` + опционально Docker-образ.
- Запуск: бинарь читает конфиг (путь через флаг/env), поднимает HTTP-сервер с embedded UI.
- Целевые DSN — через env/secret. Никогда не хранить в открытом конфиге.
- Готовые `GRANT`-скрипты для ролей `pglockr_ro` и `pglockr_op` в комплекте.

---

## 13. Структура Go-проекта (предложение)

```
cmd/pglockr/main.go          // entrypoint, config load, wiring
internal/poller/             // snapshot polling, backoff
internal/graph/              // wait-for forest builder
internal/store/              // ring buffer + sqlite history
internal/signal/             // cancel/terminate + audit
internal/api/                // REST + WS handlers
internal/auth/               // authn + RBAC
internal/pg/                 // version-aware queries, oid resolver
web/                         // React app, embedded via go:embed
migrations/                  // sqlite schema for history/audit
```

---

## 14. Открытые вопросы (решить на старте отдельного чата)

1. Финальный нейминг.
2. Персистентность истории на MVP — только in-memory ring buffer, или сразу SQLite?
3. Auth на MVP — один статический токен или сразу несколько пользователей?
4. Стрим — WebSocket или SSE (SSE проще, WS гибче для будущих действий из UI).
5. Раскладка графа — React Flow + dagre или Cytoscape.js (зависит от того, насколько сложные леса ожидаем).
6. Целевая матрица версий PG (минимальная поддерживаемая).
