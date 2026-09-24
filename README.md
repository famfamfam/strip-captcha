# strip-captcha

Числовая капча для форм регистрации: Go-сервер рисует код и отдаёт картинку
перемешанными плитками, React-компонент собирает её обратно через CSS.

![Собранная капча и сырой атлас](docs/preview.png)

Слева — что видит человек. Справа — что лежит в PNG, если сохранить картинку
или открыть её во вкладке Network: плитки вперемешку, а среди них куски
картинки с другим кодом.

## Как это работает

1. **Рендер.** Сервер генерирует код из `crypto/rand` и рисует каждую цифру
   своим шрифтом, размером, поворотом, наклоном и растяжением. Поверх — тёмные
   кривые толщиной со штрих цифры (мешают разрезать код на символы), затем
   вся картинка изгибается волной, сверху ложится шум.
2. **Нарезка.** Картинка режется «кирпичной кладкой»: 3–4 строки случайной
   высоты, в каждой свои швы шириной 12–28 px, поэтому ни один шов не проходит
   через всю картинку. Плитки перемешиваются внутри строк, строки — между
   собой, и к ним подмешиваются плитки-приманки из картинки с другим кодом.
3. **Сборка.** Клиент получает атлас и список плиток `[x, y, w, h, sx, sy]` и
   ставит каждую плитку на место через `background-position`. Собранное
   изображение существует только на экране.
4. **Проверка.** Ответ хранится на сервере и проверяется только там.
   Капча одноразовая (сгорает и после неверного ответа), ответ быстрее 2 с
   отклоняется, выдача ограничена по клиенту; по желанию ответ принимается
   только от того же клиента, которому выдана капча.

### Что это даёт и чего не даёт

Список плиток лежит в том же ответе, так что бот, написанный специально под
эту капчу, соберёт картинку сам — сборка отсекает универсальные решатели,
которые берут «картинку капчи» и отправляют её в OCR или на ферму. Против
целевой атаки работают искажения картинки, одноразовость, минимальное время
ответа и лимиты. Это защита от массовых регистраций, а не от человека с
бюджетом: если вас атакуют целенаправленно, ставьте рядом серверные сигналы
(лимиты по IP и почте, проверку домена, поведенческие метки).

Капча визуальная: для незрячих пользователей нужен запасной путь
(подтверждение по почте, вход через OAuth).

## Сервер (Go)

```sh
go get github.com/famfamfam/strip-captcha@latest
```

```go
import stripcaptcha "github.com/famfamfam/strip-captcha"

captcha, err := stripcaptcha.New(stripcaptcha.NewMemoryStore(), stripcaptcha.Options{})
if err != nil {
	log.Fatal(err)
}

// Выдача: JSON в формате, который ждёт компонент
mux.Handle("GET /api/captcha", captcha.Handler(nil))

// Проверка — в обработчике формы
err = captcha.Verify(ctx, stripcaptcha.RemoteIP(r), req.CaptchaID, req.CaptchaAnswer)
switch {
case errors.Is(err, stripcaptcha.ErrInvalid):
	// 400: неверный код — клиент должен запросить новую капчу
case err != nil:
	// 500: сбой хранилища, пользователь не виноват
}
```

Без `Handler` — вызывайте `captcha.Generate(ctx, clientKey)` сами и отдавайте
`*Challenge` JSON-ом (`ErrRateLimited` → 429).

**За прокси** передайте в `Handler` и `Verify` реальный IP клиента из
доверенного заголовка: `RemoteIP` вернёт адрес прокси, и лимит станет общим на
всех. Не берите `X-Forwarded-For` как есть — его подделывает сам клиент.

### Хранилище

`NewMemoryStore()` — для одного процесса. Несколько экземпляров сервера —
Redis 6.2+:

```go
import "github.com/famfamfam/strip-captcha/redisstore"

store := redisstore.New(redis.NewClient(&redis.Options{Addr: "localhost:6379"}))
captcha, err := stripcaptcha.New(store, stripcaptcha.Options{})
```

Своё хранилище — реализуйте `Store` (`Save`, атомарный `Take`), и `Counter`
(`Incr`), если нужен лимит выдач.

### Настройки

| Поле | По умолчанию | Что делает |
|---|---|---|
| `Width`, `Height` | 204×64 | Размер капчи на странице, CSS px |
| `Length` | 5 | Цифр в коде, 3–10 |
| `Scale` | 2 | Плотность пикселей PNG; 1 — вдвое легче картинка, но мыльно на ретине |
| `TTL` | 10 мин | Сколько живёт капча |
| `MinSolveTime` | 2 с | Ответ быстрее — бот; отрицательное значение выключает |
| `RateLimit`, `RateWindow` | 30 за 10 мин | Выдач на клиента; отрицательный `RateLimit` выключает |
| `BindClient` | false | Принимать ответ только от клиента, которому выдана капча. Мешает фермам, но мобильный клиент со сменившимся IP получит отказ |
| `NoDecoys` | false | Не подмешивать приманки: атлас меньше, но в сыром PNG ровно цифры кода |
| `KeyPrefix` | `captcha:` | Префикс ключей в хранилище |

## Клиент (React 18+)

```sh
npm install github:famfamfam/strip-captcha#v0.1.0
```

npm соберёт пакет сам при установке (скрипт `prepare`).

```tsx
import { StripCaptcha, type StripCaptchaValue } from 'strip-captcha';
import 'strip-captcha/styles.css'; // необязательно

function RegisterForm() {
  const [captcha, setCaptcha] = useState<StripCaptchaValue | null>(null);
  const [captchaEnabled, setCaptchaEnabled] = useState(true);
  const [reloadKey, setReloadKey] = useState(0);

  async function submit() {
    const res = await api.register({ ...fields, captcha_id: captcha?.id, captcha_answer: captcha?.answer });
    // Капча одноразовая: после отказа сервера нужна новая
    if (!res.ok) setReloadKey((k) => k + 1);
  }

  return (
    <form onSubmit={submit}>
      {/* ...поля... */}
      <StripCaptcha
        fetchChallenge={() => fetch('/api/captcha').then((r) => (r.ok ? r.json() : null))}
        onChange={setCaptcha}
        onAvailabilityChange={setCaptchaEnabled}
        reloadKey={reloadKey}
        labels={{ label: t('Код с картинки'), placeholder: t('Введите {n} цифр') }}
      />
      <button disabled={captchaEnabled && !captcha}>Зарегистрироваться</button>
    </form>
  );
}
```

`onChange` отдаёт `{ id, answer }`, когда введены все цифры, и `null` до
этого. Ввод нормализуется: арабские, персидские и полноширинные цифры
превращаются в ASCII, остальное отбрасывается. Капча обновляется сама
незадолго до истечения.

Если капча у вас включается настройкой, сервер может ответить
`{ "enabled": false }` — компонент ничего не покажет и вызовет
`onAvailabilityChange(false)`.

### Пропсы

| Проп | Что делает |
|---|---|
| `fetchChallenge` | `() => Promise<ответ сервера \| null>`; `null` или исключение — ошибка загрузки |
| `onChange` | `(value: { id, answer } \| null) => void` |
| `onAvailabilityChange` | `(enabled: boolean) => void` |
| `reloadKey` | Смените, чтобы запросить новую капчу |
| `autoRefresh` | Обновлять перед истечением, по умолчанию `true` |
| `labels` | `label`, `placeholder` (`{n}` — число цифр), `refresh`, `loadError`, `imageAlt` |
| `classNames` | Классы для `root`, `label`, `row`, `image`, `status`, `refresh`, `input` — например, Tailwind |
| `refreshIcon`, `loadingIndicator` | Свои иконки |
| `inputProps` | Атрибуты поля ввода (`name`, `autoFocus`, …) |

Свой интерфейс — из частей: `useStripCaptcha({ fetchChallenge, reloadKey })`
отдаёт `{ status, challenge, refresh }`, `<StripCaptchaImage challenge={...} />`
рисует картинку.

Стили по умолчанию (`strip-captcha/styles.css`) настраиваются переменными
`--strip-captcha-border`, `--strip-captcha-bg`, `--strip-captcha-focus` и др.
на `.strip-captcha`.

## Демо

```sh
npm install && npm run build
go run ./examples/server
# http://localhost:8080
```

## Разработка

```sh
go test ./...
npm test
STRIPCAPTCHA_DUMP=/tmp/samples go test -run TestDumpSamples  # примеры картинок и атласов
```

## Лицензия

MIT
