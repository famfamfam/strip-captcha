// Package stripcaptcha выдаёт и проверяет одноразовые числовые капчи.
//
// Картинка отдаётся клиенту не целиком, а атласом: исходник режется на плитки
// случайного размера, плитки перемешиваются и разбавляются плитками-приманками
// из картинки с другим кодом. Собирает изображение только клиент — по списку
// плиток, через CSS (background-position). Сырой PNG (Network, «сохранить
// картинку») остаётся мешаниной, в которой к тому же больше цифр, чем в коде.
//
// Сборка на клиенте — не секрет: список плиток лежит в том же ответе, и бот,
// написанный под эту капчу, соберёт картинку сам. Она отсекает универсальные
// решатели, а основная защита — искажения самой картинки против OCR,
// одноразовость, минимальное время ответа и лимит выдач. Ответ проверяется
// только на сервере.
package stripcaptcha

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"image/png"
	mrand "math/rand/v2"
	"strconv"
	"strings"
	"time"
)

// ErrInvalid — ответ не принят: код неверный, капча просрочена или уже
// использована, ответ пришёл подозрительно быстро либо от другого клиента
// (при Options.BindClient).
var ErrInvalid = errors.New("stripcaptcha: invalid answer")

// ErrRateLimited — этот клиент за Options.RateWindow запросил больше
// Options.RateLimit капч.
var ErrRateLimited = errors.New("stripcaptcha: rate limited")

// Options настраивают сервис. Нулевое значение поля — значение по умолчанию.
type Options struct {
	// Размер капчи на странице в CSS-пикселях. По умолчанию 204×64.
	Width, Height int
	// Число цифр в коде, 3–10. По умолчанию 5.
	Length int
	// Плотность пикселей PNG относительно CSS-размера, 1–4. По умолчанию 2 —
	// чётко на экранах высокой плотности; 1 — вдвое легче картинка.
	Scale int

	// Сколько живёт выданная капча. По умолчанию 10 минут. Берите с запасом,
	// если с формы можно уйти читать документы и вернуться.
	TTL time.Duration
	// Ответ быстрее этого считается ботом. По умолчанию 2 с; отрицательное —
	// проверка выключена.
	MinSolveTime time.Duration

	// Не больше RateLimit выдач на клиента за RateWindow (окно фиксированное,
	// от первой выдачи). По умолчанию 30 за 10 минут; отрицательное — без
	// лимита. Лимит работает, только если хранилище реализует Counter.
	RateLimit  int
	RateWindow time.Duration

	// Принимать ответ только от того же клиента, которому выдана капча.
	// Мешает пересылать картинки на ферму решателей, но у мобильных клиентов
	// IP может смениться между выдачей и отправкой формы — тогда верный
	// ответ будет отклонён. По умолчанию выключено.
	BindClient bool

	// Не подмешивать в атлас плитки-приманки. Атлас станет меньше, но сырой
	// PNG будет содержать ровно цифры кода.
	NoDecoys bool

	// Префикс ключей в хранилище. По умолчанию "captcha:".
	KeyPrefix string
}

func (o Options) withDefaults() (Options, error) {
	if o.Width == 0 {
		o.Width = 204
	}
	if o.Height == 0 {
		o.Height = 64
	}
	if o.Length == 0 {
		o.Length = 5
	}
	if o.Scale == 0 {
		o.Scale = 2
	}
	if o.TTL == 0 {
		o.TTL = 10 * time.Minute
	}
	if o.MinSolveTime == 0 {
		o.MinSolveTime = 2 * time.Second
	}
	if o.RateLimit == 0 {
		o.RateLimit = 30
	}
	if o.RateWindow == 0 {
		o.RateWindow = 10 * time.Minute
	}
	if o.KeyPrefix == "" {
		o.KeyPrefix = "captcha:"
	}

	switch {
	case o.Length < 3 || o.Length > 10:
		return o, fmt.Errorf("stripcaptcha: Length must be 3..10, got %d", o.Length)
	case o.Scale < 1 || o.Scale > 4:
		return o, fmt.Errorf("stripcaptcha: Scale must be 1..4, got %d", o.Scale)
	case o.Height < 40 || o.Height > 200:
		return o, fmt.Errorf("stripcaptcha: Height must be 40..200, got %d", o.Height)
	// На цифру нужно хотя бы ~24 px, иначе после поворота они сливаются
	case o.Width < o.Length*24 || o.Width > 800:
		return o, fmt.Errorf("stripcaptcha: Width must be %d..800 for %d digits, got %d", o.Length*24, o.Length, o.Width)
	case o.TTL < 0:
		return o, fmt.Errorf("stripcaptcha: TTL must be positive")
	}
	return o, nil
}

// Challenge — ответ клиенту. Все размеры и координаты — в CSS-пикселях.
type Challenge struct {
	ID string `json:"id"`
	// data:image/png;base64,... — атлас с перемешанными плитками
	Image       string `json:"image"`
	ImageWidth  int    `json:"imageWidth"`
	ImageHeight int    `json:"imageHeight"`
	// Размер собранной капчи
	Width  int `json:"width"`
	Height int `json:"height"`
	// Число цифр в ответе
	Length int `json:"length"`
	// Через сколько секунд капча протухнет — клиент может обновить её сам
	ExpiresIn int `json:"expiresIn"`
	// Плитки [x, y, w, h, sx, sy]: прямоугольник (x, y, w, h) собранной
	// картинки берётся из атласа с позиции (sx, sy). Порядок случайный.
	Tiles [][6]int `json:"tiles"`
}

// Service выдаёт и проверяет капчи. Безопасен для параллельного использования.
type Service struct {
	store Store
	opt   Options
	fonts fontSet
}

// New создаёт сервис. store — где хранить ответы до проверки: MemoryStore для
// одного процесса, redisstore для нескольких.
func New(store Store, opt Options) (*Service, error) {
	if store == nil {
		return nil, errors.New("stripcaptcha: store is nil")
	}
	opt, err := opt.withDefaults()
	if err != nil {
		return nil, err
	}
	fonts, err := loadFonts()
	if err != nil {
		return nil, err
	}
	return &Service{store: store, opt: opt, fonts: fonts}, nil
}

// Generate выдаёт новую капчу. client — ключ клиента для лимита выдач и
// привязки (обычно IP); пустая строка отключает для этой выдачи и то, и другое.
func (s *Service) Generate(ctx context.Context, client string) (*Challenge, error) {
	if client != "" && s.opt.RateLimit > 0 {
		if counter, ok := s.store.(Counter); ok {
			n, err := counter.Incr(ctx, s.opt.KeyPrefix+"rate:"+hashClient(client), s.opt.RateWindow)
			if err != nil {
				return nil, fmt.Errorf("stripcaptcha: rate limit: %w", err)
			}
			if n > int64(s.opt.RateLimit) {
				return nil, ErrRateLimited
			}
		}
	}

	code, err := randomDigits(s.opt.Length)
	if err != nil {
		return nil, fmt.Errorf("stripcaptcha: generate code: %w", err)
	}

	// Для рисования нужна только визуальная непредсказуемость, не секретность
	// кода, но зерно всё равно берём из crypto/rand: зерно от времени
	// угадывается, а по нему — и приманки
	rng := newRNG()
	img, err := s.render(code, rng)
	if err != nil {
		return nil, err
	}
	var decoy = img
	if !s.opt.NoDecoys {
		if decoy, err = s.render(decoyCode(code, rng), rng); err != nil {
			return nil, err
		}
	}
	atlas, tiles := buildAtlas(img, decoy, !s.opt.NoDecoys, s.opt.Width, s.opt.Height, s.opt.Scale, rng)

	var buf bytes.Buffer
	if err := (&png.Encoder{CompressionLevel: png.BestCompression}).Encode(&buf, atlas); err != nil {
		return nil, fmt.Errorf("stripcaptcha: encode png: %w", err)
	}

	id, err := randomID()
	if err != nil {
		return nil, fmt.Errorf("stripcaptcha: generate id: %w", err)
	}
	// Время выдачи — в миллисекундах: с секундами порог MinSolveTime плавал
	// бы на целую секунду
	bound := ""
	if s.opt.BindClient && client != "" {
		bound = hashClient(client)
	}
	value := code + "|" + strconv.FormatInt(time.Now().UnixMilli(), 10) + "|" + bound
	if err := s.store.Save(ctx, s.opt.KeyPrefix+id, value, s.opt.TTL); err != nil {
		return nil, fmt.Errorf("stripcaptcha: save: %w", err)
	}

	return &Challenge{
		ID:          id,
		Image:       "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes()),
		ImageWidth:  atlas.Bounds().Dx() / s.opt.Scale,
		ImageHeight: atlas.Bounds().Dy() / s.opt.Scale,
		Width:       s.opt.Width,
		Height:      s.opt.Height,
		Length:      s.opt.Length,
		ExpiresIn:   int(s.opt.TTL / time.Second),
		Tiles:       tiles,
	}, nil
}

// Verify проверяет ответ и сжигает капчу: второй попытки нет ни у верного,
// ни у неверного ответа. client — тот же ключ, что был в Generate (важен
// только при Options.BindClient). Нецифровые символы в answer игнорируются.
//
// ErrInvalid — ответ не принят; любая другая ошибка — сбой хранилища.
func (s *Service) Verify(ctx context.Context, client, id, answer string) error {
	// id выдаём мы (32 hex-символа), ответ — несколько цифр: всё заметно
	// длиннее прислал не наш клиент, в хранилище с таким не ходим
	if id == "" || len(id) > 64 || len(answer) > 64 {
		return ErrInvalid
	}
	stored, ok, err := s.store.Take(ctx, s.opt.KeyPrefix+id)
	if err != nil {
		return fmt.Errorf("stripcaptcha: take: %w", err)
	}
	if !ok {
		return ErrInvalid
	}

	parts := strings.SplitN(stored, "|", 3)
	if len(parts) != 3 {
		return ErrInvalid
	}
	code, issuedStr, bound := parts[0], parts[1], parts[2]

	if s.opt.MinSolveTime > 0 {
		issuedMs, err := strconv.ParseInt(issuedStr, 10, 64)
		if err != nil || time.Since(time.UnixMilli(issuedMs)) < s.opt.MinSolveTime {
			return ErrInvalid
		}
	}
	if bound != "" && subtle.ConstantTimeCompare([]byte(bound), []byte(hashClient(client))) != 1 {
		return ErrInvalid
	}
	if subtle.ConstantTimeCompare([]byte(code), []byte(onlyDigits(answer))) != 1 {
		return ErrInvalid
	}
	return nil
}

// randomDigits — n цифр из crypto/rand. Только цифры, чтобы не заставлять
// переключать раскладку. rand.Int равномерен, в отличие от байт%10, где 0–5
// выпадали бы чаще 6–9.
func randomDigits(n int) (string, error) {
	b := make([]byte, n)
	for i := range b {
		var x [1]byte
		// Отбрасываем 250..255, чтобы остаток от деления на 10 был равномерным
		for {
			if _, err := rand.Read(x[:]); err != nil {
				return "", err
			}
			if x[0] < 250 {
				break
			}
		}
		b[i] = '0' + x[0]%10
	}
	return string(b), nil
}

// decoyCode — код для приманок той же длины, отличающийся от настоящего в
// каждой позиции: иначе куски приманки могли бы совпасть с настоящими цифрами.
func decoyCode(code string, rng *mrand.Rand) string {
	b := []byte(code)
	for i := range b {
		b[i] = '0' + (b[i]-'0'+byte(1+rng.IntN(9)))%10
	}
	return string(b)
}

func randomID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

func newRNG() *mrand.Rand {
	var seed [16]byte
	if _, err := rand.Read(seed[:]); err != nil {
		// crypto/rand в Go 1.24+ не возвращает ошибок, в старых — только при
		// сломанной ОС; картинка от этого не становится небезопасной
		return mrand.New(mrand.NewPCG(uint64(time.Now().UnixNano()), 0))
	}
	return mrand.New(mrand.NewPCG(binary.LittleEndian.Uint64(seed[:8]), binary.LittleEndian.Uint64(seed[8:])))
}

// hashClient — в хранилище не пишем IP как есть, и длина ключа ограничена.
func hashClient(client string) string {
	sum := sha256.Sum256([]byte(client))
	return hex.EncodeToString(sum[:12])
}

func onlyDigits(s string) string {
	var sb strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			sb.WriteRune(r)
		}
	}
	return sb.String()
}
