package stripcaptcha

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"image"
	"image/png"
	mrand "math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func testService(t *testing.T, store Store, opt Options) *Service {
	t.Helper()
	if store == nil {
		store = NewMemoryStore()
	}
	s, err := New(store, opt)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return s
}

func seeded(seed uint64) *mrand.Rand { return mrand.New(mrand.NewPCG(seed, seed^0x9e3779b97f4a7c15)) }

func TestOptionsValidation(t *testing.T) {
	bad := []Options{
		{Length: 2},
		{Length: 11},
		{Scale: 5},
		{Height: 30},
		{Width: 100, Length: 5},
		{TTL: -time.Second},
	}
	for _, o := range bad {
		if _, err := New(NewMemoryStore(), o); err == nil {
			t.Errorf("New(%+v) succeeded, want error", o)
		}
	}
	if _, err := New(nil, Options{}); err == nil {
		t.Error("New(nil store) succeeded, want error")
	}
}

// Плитки обязаны покрыть картинку ровно один раз, а сборка по ним — вернуть
// исходник попиксельно: именно так собирает её клиент.
func TestAtlasRoundTrip(t *testing.T) {
	for _, tc := range []struct {
		name   string
		opt    Options
		decoys bool
	}{
		{"default", Options{}, true},
		{"no decoys", Options{NoDecoys: true}, false},
		{"scale 1, wide", Options{Scale: 1, Width: 300, Height: 80, Length: 7}, true},
		{"scale 3, tall", Options{Scale: 3, Width: 160, Height: 100, Length: 4}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := testService(t, nil, tc.opt)
			o := s.opt
			for seed := uint64(0); seed < 50; seed++ {
				rng := seeded(seed)
				real, err := s.render("12345"[:min(5, o.Length)], rng)
				if err != nil {
					t.Fatal(err)
				}
				decoy, err := s.render("67890"[:min(5, o.Length)], rng)
				if err != nil {
					t.Fatal(err)
				}
				atlas, tiles := buildAtlas(real, decoy, tc.decoys, o.Width, o.Height, o.Scale, rng)
				checkReassembly(t, real, atlas, tiles, o)
				if !tc.decoys && atlas.Bounds() != real.Bounds() {
					t.Fatalf("seed %d: atlas without decoys has size %v, want %v", seed, atlas.Bounds(), real.Bounds())
				}
			}
		})
	}
}

func checkReassembly(t *testing.T, real, atlas *image.RGBA, tiles [][6]int, o Options) {
	t.Helper()
	sc := o.Scale
	covered := make([]int, o.Width*o.Height)
	out := image.NewRGBA(real.Bounds())
	for _, tl := range tiles {
		x, y, w, h, sx, sy := tl[0], tl[1], tl[2], tl[3], tl[4], tl[5]
		if w < 1 || h < 1 || (sx+w)*sc > atlas.Bounds().Dx() || (sy+h)*sc > atlas.Bounds().Dy() {
			t.Fatalf("tile %v out of atlas %v", tl, atlas.Bounds())
		}
		for yy := 0; yy < h; yy++ {
			for xx := 0; xx < w; xx++ {
				covered[(y+yy)*o.Width+x+xx]++
			}
		}
		for yy := 0; yy < h*sc; yy++ {
			src := atlas.Pix[atlas.PixOffset(sx*sc, sy*sc+yy):]
			dst := out.Pix[out.PixOffset(x*sc, y*sc+yy):]
			copy(dst[:w*sc*4], src[:w*sc*4])
		}
	}
	for i, n := range covered {
		if n != 1 {
			t.Fatalf("pixel (%d,%d) covered %d times, want 1", i%o.Width, i/o.Width, n)
		}
	}
	if !bytes.Equal(out.Pix, real.Pix) {
		t.Fatal("reassembled image differs from the original")
	}
}

func TestSplitRange(t *testing.T) {
	rng := seeded(1)
	// Второй случай — строки низкой капчи (Height 40): hi < 2·lo
	for _, r := range [][2]int{{tileMinW, tileMaxW}, {rowMinH, 16}} {
		lo, hi := r[0], r[1]
		for total := 1; total < 400; total++ {
			parts := splitRange(total, lo, hi, rng)
			sum := 0
			for i, p := range parts {
				sum += p
				// Длиннее hi можно только последнему и только если его нельзя было поделить
				tooLong := p > hi && (i != len(parts)-1 || p >= 2*lo)
				if total >= lo && (p < lo || tooLong) {
					t.Fatalf("splitRange(%d, %d, %d) part %d = %d: %v", total, lo, hi, i, p, parts)
				}
			}
			if sum != total {
				t.Fatalf("splitRange(%d, %d, %d) sums to %d: %v", total, lo, hi, sum, parts)
			}
		}
	}
}

// Один seed — одна картинка, рендерим ли последовательно или из многих
// горутин сразу. С общим font.Face параллельный рендер падал паникой.
func TestRenderConcurrent(t *testing.T) {
	s := testService(t, nil, Options{})
	const workers = 16
	want := make([][]byte, workers)
	for i := range want {
		img, err := s.render("12345", seeded(uint64(i)))
		if err != nil {
			t.Fatal(err)
		}
		want[i] = img.Pix
	}
	for round := 0; round < 5; round++ {
		var wg sync.WaitGroup
		got := make([][]byte, workers)
		for i := 0; i < workers; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				if img, err := s.render("12345", seeded(uint64(i))); err == nil {
					got[i] = img.Pix
				}
			}(i)
		}
		wg.Wait()
		for i := range got {
			if !bytes.Equal(want[i], got[i]) {
				t.Fatalf("round %d worker %d: concurrent render differs from sequential", round, i)
			}
		}
	}
}

func TestRandomDigits(t *testing.T) {
	var counts [10]int
	for i := 0; i < 5000; i++ {
		code, err := randomDigits(5)
		if err != nil {
			t.Fatal(err)
		}
		if len(code) != 5 || onlyDigits(code) != code {
			t.Fatalf("bad code %q", code)
		}
		for _, r := range code {
			counts[r-'0']++
		}
	}
	for d, n := range counts {
		// 25000 цифр, ожидание 2500 на каждую
		if n < 2200 || n > 2800 {
			t.Errorf("digit %d appeared %d times, distribution looks skewed: %v", d, n, counts)
		}
	}
}

func TestDecoyCodeDiffersEverywhere(t *testing.T) {
	rng := seeded(7)
	for i := 0; i < 1000; i++ {
		code, _ := randomDigits(6)
		decoy := decoyCode(code, rng)
		for j := range code {
			if code[j] == decoy[j] || decoy[j] < '0' || decoy[j] > '9' {
				t.Fatalf("decoy %q vs code %q at %d", decoy, code, j)
			}
		}
	}
}

// Достаём код из хранилища — проверяем Verify, не решая картинку.
func storedCode(t *testing.T, m *MemoryStore, key string) string {
	t.Helper()
	m.mu.Lock()
	defer m.mu.Unlock()
	return strings.SplitN(m.items[key].value, "|", 2)[0]
}

func TestGenerateVerify(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	s := testService(t, store, Options{MinSolveTime: -1})

	ch, err := s.Generate(ctx, "1.2.3.4")
	if err != nil {
		t.Fatal(err)
	}
	if ch.Width != 204 || ch.Height != 64 || ch.Length != 5 || ch.ExpiresIn != 600 || len(ch.Tiles) == 0 {
		t.Fatalf("unexpected challenge: %+v", ch)
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(ch.Image, "data:image/png;base64,"))
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := png.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Width != ch.ImageWidth*2 || cfg.Height != ch.ImageHeight*2 {
		t.Fatalf("png is %dx%d, want 2× of %dx%d", cfg.Width, cfg.Height, ch.ImageWidth, ch.ImageHeight)
	}

	code := storedCode(t, store, "captcha:"+ch.ID)
	wrong := decoyCode(code, seeded(1))
	if err := s.Verify(ctx, "1.2.3.4", ch.ID, wrong); !errors.Is(err, ErrInvalid) {
		t.Fatalf("wrong answer: got %v, want ErrInvalid", err)
	}
	// Неверный ответ сжигает капчу — верный после него уже не пройдёт
	if err := s.Verify(ctx, "1.2.3.4", ch.ID, code); !errors.Is(err, ErrInvalid) {
		t.Fatalf("answer after a failed attempt: got %v, want ErrInvalid", err)
	}

	ch, _ = s.Generate(ctx, "1.2.3.4")
	code = storedCode(t, store, "captcha:"+ch.ID)
	// Пробелы и дефисы в ответе не мешают
	spaced := code[:2] + " - " + code[2:]
	if err := s.Verify(ctx, "5.6.7.8", ch.ID, spaced); err != nil {
		t.Fatalf("right answer: %v", err)
	}
	if err := s.Verify(ctx, "5.6.7.8", ch.ID, code); !errors.Is(err, ErrInvalid) {
		t.Fatalf("reused captcha: got %v, want ErrInvalid", err)
	}
}

func TestVerifyTooFast(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	s := testService(t, store, Options{MinSolveTime: time.Hour})
	ch, _ := s.Generate(ctx, "")
	if err := s.Verify(ctx, "", ch.ID, storedCode(t, store, "captcha:"+ch.ID)); !errors.Is(err, ErrInvalid) {
		t.Fatalf("instant answer: got %v, want ErrInvalid", err)
	}
}

func TestVerifyBindClient(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	s := testService(t, store, Options{MinSolveTime: -1, BindClient: true})

	ch, _ := s.Generate(ctx, "1.2.3.4")
	if err := s.Verify(ctx, "5.6.7.8", ch.ID, storedCode(t, store, "captcha:"+ch.ID)); !errors.Is(err, ErrInvalid) {
		t.Fatalf("other client: got %v, want ErrInvalid", err)
	}
	ch, _ = s.Generate(ctx, "1.2.3.4")
	if err := s.Verify(ctx, "1.2.3.4", ch.ID, storedCode(t, store, "captcha:"+ch.ID)); err != nil {
		t.Fatalf("same client: %v", err)
	}
}

func TestVerifyRejectsMalformedInput(t *testing.T) {
	s := testService(t, nil, Options{})
	for _, tc := range [][2]string{{"", "12345"}, {strings.Repeat("a", 65), "12345"}, {"abc", strings.Repeat("1", 65)}, {"missing", "12345"}} {
		if err := s.Verify(context.Background(), "", tc[0], tc[1]); !errors.Is(err, ErrInvalid) {
			t.Errorf("Verify(%.10q, %.10q) = %v, want ErrInvalid", tc[0], tc[1], err)
		}
	}
}

func TestRateLimit(t *testing.T) {
	ctx := context.Background()
	s := testService(t, nil, Options{RateLimit: 3, Scale: 1})
	for i := 0; i < 3; i++ {
		if _, err := s.Generate(ctx, "1.2.3.4"); err != nil {
			t.Fatalf("generate %d: %v", i, err)
		}
	}
	if _, err := s.Generate(ctx, "1.2.3.4"); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("over limit: got %v, want ErrRateLimited", err)
	}
	if _, err := s.Generate(ctx, "5.6.7.8"); err != nil {
		t.Fatalf("other client: %v", err)
	}
	// Пустой ключ клиента лимитом не считается
	for i := 0; i < 5; i++ {
		if _, err := s.Generate(ctx, ""); err != nil {
			t.Fatalf("anonymous generate %d: %v", i, err)
		}
	}
}

func TestMemoryStoreExpiry(t *testing.T) {
	ctx := context.Background()
	now := time.Unix(1000, 0)
	m := NewMemoryStore()
	m.now = func() time.Time { return now }

	_ = m.Save(ctx, "a", "1", time.Minute)
	_ = m.Save(ctx, "b", "2", time.Minute)
	now = now.Add(2 * time.Minute)
	if _, ok, _ := m.Take(ctx, "a"); ok {
		t.Fatal("expired item returned")
	}
	// Запись после интервала чистки выметает истёкшее
	_ = m.Save(ctx, "c", "3", time.Minute)
	if _, left := m.items["b"]; left {
		t.Fatal("expired item survived sweep")
	}

	if n, _ := m.Incr(ctx, "k", time.Minute); n != 1 {
		t.Fatalf("first incr = %d", n)
	}
	if n, _ := m.Incr(ctx, "k", time.Minute); n != 2 {
		t.Fatalf("second incr = %d", n)
	}
	now = now.Add(time.Minute)
	if n, _ := m.Incr(ctx, "k", time.Minute); n != 1 {
		t.Fatalf("incr after window = %d, want 1", n)
	}
}

// STRIPCAPTCHA_DUMP=dir go test -run TestDumpSamples — сохранить примеры
// собранных картинок и атласов, чтобы оценить читаемость глазами.
func TestDumpSamples(t *testing.T) {
	dir := os.Getenv("STRIPCAPTCHA_DUMP")
	if dir == "" {
		t.Skip("STRIPCAPTCHA_DUMP not set")
	}
	s := testService(t, nil, Options{})
	o := s.opt
	for i := uint64(0); i < 8; i++ {
		rng := seeded(i + 100)
		code, _ := randomDigits(o.Length)
		real, _ := s.render(code, rng)
		decoy, _ := s.render(decoyCode(code, rng), rng)
		atlas, _ := buildAtlas(real, decoy, true, o.Width, o.Height, o.Scale, rng)
		for name, img := range map[string]*image.RGBA{"img": real, "atlas": atlas} {
			f, err := os.Create(filepath.Join(dir, code+"_"+name+".png"))
			if err != nil {
				t.Fatal(err)
			}
			_ = png.Encode(f, img)
			f.Close()
		}
	}
}
