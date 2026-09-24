package stripcaptcha

import (
	"image"
	"image/draw"
	mrand "math/rand/v2"
)

const (
	// Ширина плитки в CSS-пикселях. Уже 12 px плитка почти не несёт формы
	// цифры, шире 28 px — вмещает цифру целиком, и сырой атлас читается.
	tileMinW = 12
	tileMaxW = 28
	// Строка ниже 14 px режет цифру на бессмысленные ломтики
	rowMinH = 14
)

// atlasPiece — кусок строки исходника (или приманки) по дороге в атлас.
type atlasPiece struct {
	src      *image.RGBA
	srcX     int  // откуда в исходнике, CSS px
	w        int  // ширина, CSS px
	displayX int  // куда в собранной картинке, CSS px
	real     bool // приманки в список плиток не попадают
}

// buildAtlas режет real (и, если withDecoys, полосу decoy) на плитки и
// раскладывает их вперемешку в атлас. Разрез «кирпичный»: 3–4 строки
// случайной высоты, в каждой свои швы, поэтому ни один шов не проходит через
// всю картинку. Строки в атласе тоже переставлены. Размеры принимаются в
// CSS-пикселях, картинки — в натуральных (× scale).
//
// Возвращает атлас и плитки [x, y, w, h, sx, sy] в случайном порядке:
// прямоугольник собранной картинки (x, y, w, h) берётся из атласа с (sx, sy).
// Плитки покрывают картинку width×height ровно один раз.
func buildAtlas(real, decoy *image.RGBA, withDecoys bool, width, height, scale int, rng *mrand.Rand) (*image.RGBA, [][6]int) {
	// Строки не выше 40% высоты — каждая цифра режется хотя бы на три части
	rows := splitRange(height, rowMinH, max(rowMinH, height*2/5), rng)

	decoyW, decoyX := 0, 0
	if withDecoys {
		decoyW = width * (30 + rng.IntN(21)) / 100
		decoyX = rng.IntN(width - decoyW + 1)
	}
	atlasW := width + decoyW

	// Где в атласе окажется каждая строка: переставляем, накапливаем высоты
	rowAtlasY := make([]int, len(rows))
	y := 0
	for _, r := range rng.Perm(len(rows)) {
		rowAtlasY[r] = y
		y += rows[r]
	}

	atlas := image.NewRGBA(image.Rect(0, 0, atlasW*scale, height*scale))
	tiles := make([][6]int, 0, 32)

	rowY := 0
	for r, rowH := range rows {
		var pieces []atlasPiece
		x := 0
		for _, w := range splitRange(width, tileMinW, tileMaxW, rng) {
			pieces = append(pieces, atlasPiece{src: real, srcX: x, w: w, displayX: x, real: true})
			x += w
		}
		if decoyW > 0 {
			x := decoyX
			for _, w := range splitRange(decoyW, tileMinW, tileMaxW, rng) {
				pieces = append(pieces, atlasPiece{src: decoy, srcX: x, w: w})
				x += w
			}
		}
		rng.Shuffle(len(pieces), func(i, j int) { pieces[i], pieces[j] = pieces[j], pieces[i] })

		ax := 0
		for _, p := range pieces {
			dstRect := image.Rect(ax*scale, rowAtlasY[r]*scale, (ax+p.w)*scale, (rowAtlasY[r]+rowH)*scale)
			draw.Draw(atlas, dstRect, p.src, image.Pt(p.srcX*scale, rowY*scale), draw.Src)
			if p.real {
				tiles = append(tiles, [6]int{p.displayX, rowY, p.w, rowH, ax, rowAtlasY[r]})
			}
			ax += p.w
		}
		rowY += rowH
	}

	rng.Shuffle(len(tiles), func(i, j int) { tiles[i], tiles[j] = tiles[j], tiles[i] })
	return atlas, tiles
}

// splitRange делит total на отрезки длиной от lo до hi. Короче lo отрезок
// бывает, только если сам total < lo; длиннее hi — только последний, когда
// остаток нельзя разделить на два не короче lo (при hi < 2·lo).
func splitRange(total, lo, hi int, rng *mrand.Rand) []int {
	if hi < lo {
		hi = lo
	}
	var parts []int
	for total > hi && total >= 2*lo {
		// Не оставляем хвост короче lo
		top := min(hi, total-lo)
		n := lo
		if top > lo {
			n += rng.IntN(top - lo + 1)
		}
		parts = append(parts, n)
		total -= n
	}
	return append(parts, total)
}
