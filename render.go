package stripcaptcha

import (
	"fmt"
	"image"
	"image/color"
	"math"
	mrand "math/rand/v2"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/gobolditalic"
	"golang.org/x/image/font/gofont/gomonobold"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

// fontSet — разобранные шрифты. opentype.Font можно читать из многих
// горутин, а font.Face — нет (внутри общий растеризатор, параллельный рендер
// падает паникой в vector.Rasterizer), поэтому Face создаётся на каждую цифру.
type fontSet []*opentype.Font

func loadFonts() (fontSet, error) {
	var fs fontSet
	for _, ttf := range [][]byte{gobold.TTF, gobolditalic.TTF, gomonobold.TTF} {
		f, err := opentype.Parse(ttf)
		if err != nil {
			return nil, fmt.Errorf("stripcaptcha: parse font: %w", err)
		}
		fs = append(fs, f)
	}
	return fs, nil
}

// render рисует code в натуральных пикселях (CSS-размер × Scale). Разные
// шрифт, размер, поворот, наклон и растяжение у каждой цифры мешают OCR по
// шаблонам; тёмные кривые толщиной со штрих цифры, пересекающие код, мешают
// разрезать его на символы; волновое искажение всей картинки ломает прямые
// линии, на которые опираются распознаватели. Детерминирован относительно rng.
func (s *Service) render(code string, rng *mrand.Rand) (*image.RGBA, error) {
	sc := float64(s.opt.Scale)
	w, h := s.opt.Width*s.opt.Scale, s.opt.Height*s.opt.Scale
	img := image.NewRGBA(image.Rect(0, 0, w, h))

	paintBackground(img, rng)

	// Светлые штрихи фона — под цифрами
	for i := 0; i < 4; i++ {
		strokeCurve(img, rng, lightInk(rng), (0.6+rng.Float64()*0.6)*sc, 0.1, 0.9, 0.7)
	}

	cell := float64(w) / float64(len(code))
	for i := 0; i < len(code); i++ {
		face, err := opentype.NewFace(s.fonts[rng.IntN(len(s.fonts))], &opentype.FaceOptions{
			Size:    float64(h) * (0.56 + rng.Float64()*0.12),
			DPI:     72,
			Hinting: font.HintingNone,
		})
		if err != nil {
			return nil, fmt.Errorf("stripcaptcha: create face: %w", err)
		}
		mask := glyphMask(face, code[i:i+1])
		face.Close()

		cx := cell*(float64(i)+0.5) + (rng.Float64()*2-1)*cell*0.12
		cy := float64(h)/2 + (rng.Float64()*2-1)*float64(h)*0.07
		drawMask(img, mask, darkInk(rng), cx, cy, glyphTransform{
			angle:   (rng.Float64()*2 - 1) * 26 * math.Pi / 180,
			stretch: 0.88 + rng.Float64()*0.3,
			shear:   (rng.Float64()*2 - 1) * 0.22,
		})
	}

	// Помехи поверх цифр: цвет и толщина как у штриха цифры, проходят через
	// середину — OCR не может просто отбросить их по яркости
	for i := 0; i < 2; i++ {
		strokeCurve(img, rng, darkInk(rng), (1.1+rng.Float64()*0.5)*sc, 0.35, 0.65, 0.9)
	}

	out := warp(img, rng, sc)
	speckle(out, rng)
	posterize(out, 8)
	return out, nil
}

func lightInk(rng *mrand.Rand) color.RGBA {
	return color.RGBA{uint8(150 + rng.IntN(70)), uint8(150 + rng.IntN(70)), uint8(150 + rng.IntN(70)), 255}
}

func darkInk(rng *mrand.Rand) color.RGBA {
	return color.RGBA{uint8(20 + rng.IntN(90)), uint8(20 + rng.IntN(90)), uint8(20 + rng.IntN(90)), 255}
}

// paintBackground — градиент между двумя светлыми цветами в случайном
// направлении и точки. Ровный фон отделяется от цифр одним порогом. Зерна на
// каждом пикселе нет: оно почти не мешает OCR, зато раздувало PNG вдвое.
func paintBackground(img *image.RGBA, rng *mrand.Rand) {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	c1 := [3]float64{float64(225 + rng.IntN(30)), float64(225 + rng.IntN(30)), float64(225 + rng.IntN(30))}
	c2 := [3]float64{float64(205 + rng.IntN(45)), float64(205 + rng.IntN(45)), float64(205 + rng.IntN(45))}
	dir := rng.Float64() * 2 * math.Pi
	dx, dy := math.Cos(dir), math.Sin(dir)
	span := math.Abs(dx)*float64(w) + math.Abs(dy)*float64(h)

	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			// t — проекция точки на направление градиента, 0..1
			t := ((float64(x)-float64(w)/2)*dx+(float64(y)-float64(h)/2)*dy)/span + 0.5
			i := img.PixOffset(x, y)
			for c := 0; c < 3; c++ {
				img.Pix[i+c] = clamp8(c1[c] + (c2[c]-c1[c])*t)
			}
			img.Pix[i+3] = 255
		}
	}
	for i := 0; i < w*h/60; i++ {
		img.SetRGBA(rng.IntN(w), rng.IntN(h), lightInk(rng))
	}
}

// glyphMask — альфа-маска символа с полями, чтобы поворот не обрезал края.
func glyphMask(face font.Face, s string) *image.Alpha {
	bounds, _ := font.BoundString(face, s)
	pad := 2
	minX, minY := bounds.Min.X.Floor(), bounds.Min.Y.Floor()
	w := bounds.Max.X.Ceil() - minX + 2*pad
	h := bounds.Max.Y.Ceil() - minY + 2*pad
	mask := image.NewAlpha(image.Rect(0, 0, w, h))
	d := &font.Drawer{
		Dst:  mask,
		Src:  image.Opaque,
		Face: face,
		Dot:  fixed.P(pad-minX, pad-minY),
	}
	d.DrawString(s)
	return mask
}

type glyphTransform struct {
	angle   float64 // поворот, радианы
	stretch float64 // растяжение по горизонтали
	shear   float64 // наклон по горизонтали
}

// drawMask накладывает маску цветом ink с центром в (cx, cy) после
// преобразования M = R(angle) · Shear · Stretch. Идём по пикселям результата
// и берём маску обратным преобразованием с билинейной интерполяцией — так нет
// дыр, как при прямом переносе пикселей.
func drawMask(dst *image.RGBA, mask *image.Alpha, ink color.RGBA, cx, cy float64, t glyphTransform) {
	cos, sin := math.Cos(t.angle), math.Sin(t.angle)
	// M = [[a, b], [c, d]]
	a, b := cos*t.stretch, cos*t.shear-sin
	c, d := sin*t.stretch, sin*t.shear+cos
	det := a*d - b*c
	ia, ib, ic, id := d/det, -b/det, -c/det, a/det

	mw, mh := float64(mask.Rect.Dx()), float64(mask.Rect.Dy())
	mcx, mcy := mw/2, mh/2

	// Габарит преобразованной маски в координатах результата
	minX, minY, maxX, maxY := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	for _, p := range [4][2]float64{{-mcx, -mcy}, {mcx, -mcy}, {-mcx, mcy}, {mcx, mcy}} {
		x, y := a*p[0]+b*p[1], c*p[0]+d*p[1]
		minX, maxX = math.Min(minX, x), math.Max(maxX, x)
		minY, maxY = math.Min(minY, y), math.Max(maxY, y)
	}
	r := image.Rect(int(cx+minX)-1, int(cy+minY)-1, int(cx+maxX)+2, int(cy+maxY)+2).Intersect(dst.Rect)

	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			px, py := float64(x)+0.5-cx, float64(y)+0.5-cy
			mx, my := ia*px+ib*py+mcx, ic*px+id*py+mcy
			if alpha := sampleAlpha(mask, mx-0.5, my-0.5); alpha > 0 {
				blend(dst, x, y, ink, alpha)
			}
		}
	}
}

func sampleAlpha(m *image.Alpha, x, y float64) float64 {
	x0, y0 := int(math.Floor(x)), int(math.Floor(y))
	fx, fy := x-float64(x0), y-float64(y0)
	at := func(x, y int) float64 {
		if x < 0 || y < 0 || x >= m.Rect.Dx() || y >= m.Rect.Dy() {
			return 0
		}
		return float64(m.Pix[y*m.Stride+x]) / 255
	}
	top := at(x0, y0)*(1-fx) + at(x0+1, y0)*fx
	bottom := at(x0, y0+1)*(1-fx) + at(x0+1, y0+1)*fx
	return top*(1-fy) + bottom*fy
}

// strokeCurve рисует кривую через всю ширину: синусоида с наклоном, середина
// которой держится в полосе [bandTop, bandBottom] высоты. Покрытие копится в
// отдельном слое через max и смешивается один раз — иначе полупрозрачный
// штрих темнел бы там, где соседние точки кривой перекрываются.
func strokeCurve(img *image.RGBA, rng *mrand.Rand, ink color.RGBA, width, bandTop, bandBottom, opacity float64) {
	b := img.Bounds()
	w, h := float64(b.Dx()), float64(b.Dy())
	y0 := h * (bandTop + rng.Float64()*(bandBottom-bandTop))
	y1 := h * (bandTop + rng.Float64()*(bandBottom-bandTop))
	amp := h * (0.08 + rng.Float64()*0.14)
	freq := 2 * math.Pi / (w * (0.4 + rng.Float64()*0.8))
	phase := rng.Float64() * 2 * math.Pi
	// Не всегда от края до края — пусть концы бывают внутри картинки
	xStart := w * (rng.Float64()*0.3 - 0.1)
	xEnd := w * (0.7 + rng.Float64()*0.4)

	cover := make([]float32, b.Dx()*b.Dy())
	radius := width / 2
	for x := xStart; x <= xEnd; x += 0.5 {
		t := (x - xStart) / (xEnd - xStart)
		y := y0 + (y1-y0)*t + amp*math.Sin(freq*x+phase)
		for py := int(y - radius - 1); py <= int(y+radius+1); py++ {
			for px := int(x - radius - 1); px <= int(x+radius+1); px++ {
				if px < 0 || py < 0 || px >= b.Dx() || py >= b.Dy() {
					continue
				}
				dist := math.Hypot(float64(px)+0.5-x, float64(py)+0.5-y)
				cv := float32(math.Max(0, math.Min(1, radius+0.5-dist)))
				if i := py*b.Dx() + px; cv > cover[i] {
					cover[i] = cv
				}
			}
		}
	}
	for i, cv := range cover {
		if cv > 0 {
			blend(img, i%b.Dx(), i/b.Dx(), ink, float64(cv)*opacity)
		}
	}
}

// warp сдвигает строки по горизонтали и столбцы по вертикали синусоидами —
// буквы изгибаются, а соседние цифры искажаются по-разному.
func warp(src *image.RGBA, rng *mrand.Rand, sc float64) *image.RGBA {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	ampX := (1.2 + rng.Float64()*1.2) * sc
	ampY := (1.8 + rng.Float64()*1.6) * sc
	periodY := float64(h) * (0.7 + rng.Float64()*0.6)
	periodX := float64(w) * (0.25 + rng.Float64()*0.2)
	phaseX, phaseY := rng.Float64()*2*math.Pi, rng.Float64()*2*math.Pi

	dst := image.NewRGBA(b)
	for y := 0; y < h; y++ {
		shiftX := ampX * math.Sin(2*math.Pi*float64(y)/periodY+phaseX)
		for x := 0; x < w; x++ {
			shiftY := ampY * math.Sin(2*math.Pi*float64(x)/periodX+phaseY)
			sampleRGBA(src, float64(x)+shiftX, float64(y)+shiftY, dst.Pix[dst.PixOffset(x, y):])
		}
	}
	return dst
}

// sampleRGBA — билинейная выборка с прижатием к краям, результат в out[0:4].
func sampleRGBA(m *image.RGBA, x, y float64, out []uint8) {
	w, h := m.Rect.Dx(), m.Rect.Dy()
	x = math.Max(0, math.Min(float64(w-1), x))
	y = math.Max(0, math.Min(float64(h-1), y))
	x0, y0 := int(x), int(y)
	x1, y1 := min(x0+1, w-1), min(y0+1, h-1)
	fx, fy := x-float64(x0), y-float64(y0)
	p00, p10 := m.PixOffset(x0, y0), m.PixOffset(x1, y0)
	p01, p11 := m.PixOffset(x0, y1), m.PixOffset(x1, y1)
	for c := 0; c < 4; c++ {
		top := float64(m.Pix[p00+c])*(1-fx) + float64(m.Pix[p10+c])*fx
		bottom := float64(m.Pix[p01+c])*(1-fx) + float64(m.Pix[p11+c])*fx
		out[c] = clamp8(top*(1-fy) + bottom*fy)
	}
}

// speckle — редкие точки средней яркости поверх всего, после искажения,
// чтобы шум не сглаживался интерполяцией.
func speckle(img *image.RGBA, rng *mrand.Rand) {
	b := img.Bounds()
	for i := 0; i < b.Dx()*b.Dy()/220; i++ {
		v := uint8(90 + rng.IntN(120))
		blend(img, rng.IntN(b.Dx()), rng.IntN(b.Dy()), color.RGBA{v, v, v, 255}, 0.7)
	}
}

func blend(img *image.RGBA, x, y int, c color.RGBA, alpha float64) {
	i := img.PixOffset(x, y)
	img.Pix[i+0] = clamp8(float64(img.Pix[i+0])*(1-alpha) + float64(c.R)*alpha)
	img.Pix[i+1] = clamp8(float64(img.Pix[i+1])*(1-alpha) + float64(c.G)*alpha)
	img.Pix[i+2] = clamp8(float64(img.Pix[i+2])*(1-alpha) + float64(c.B)*alpha)
	img.Pix[i+3] = 255
}

func clamp8(v float64) uint8 {
	if v <= 0 {
		return 0
	}
	if v >= 255 {
		return 255
	}
	return uint8(v + 0.5)
}

// posterize округляет каналы до шага step: на глаз не видно, а PNG сжимает
// такую картинку в разы лучше — после интерполяции почти каждый пиксель
// получает свой оттенок.
func posterize(img *image.RGBA, step int) {
	for i := 0; i < len(img.Pix); i += 4 {
		for c := 0; c < 3; c++ {
			v := (int(img.Pix[i+c]) + step/2) / step * step
			img.Pix[i+c] = uint8(min(v, 255))
		}
	}
}
