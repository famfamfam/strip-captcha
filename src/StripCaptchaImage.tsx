import type { CSSProperties, SyntheticEvent } from 'react';
import type { StripCaptchaChallenge } from './types.js';

export interface StripCaptchaImageProps {
  challenge: StripCaptchaChallenge;
  /** Подпись для скринридеров. */
  alt?: string;
  className?: string;
  style?: CSSProperties;
}

const prevent = (e: SyntheticEvent) => e.preventDefault();

/**
 * Собирает картинку из атласа: каждая плитка — div, показывающий свой кусок
 * атласа через background-position. Собранное изображение существует только
 * на экране — «сохранить картинку» и вкладка Network дают перемешанный атлас.
 *
 * Атлас передаётся плиткам через CSS-переменную на контейнере: data:-URI в
 * десятки килобайт не копируется в style каждой из ~30 плиток.
 */
export function StripCaptchaImage({ challenge, alt, className, style }: StripCaptchaImageProps) {
  const { image, imageWidth, imageHeight, width, height, tiles } = challenge;
  const rootStyle = {
    position: 'relative',
    overflow: 'hidden',
    width,
    height,
    flexShrink: 0,
    userSelect: 'none',
    WebkitUserSelect: 'none',
    '--strip-captcha-atlas': `url("${image}")`,
    ...style,
  } as CSSProperties;

  return (
    <div
      role="img"
      aria-label={alt}
      className={className ? `strip-captcha__image ${className}` : 'strip-captcha__image'}
      style={rootStyle}
      onContextMenu={prevent}
      onDragStart={prevent}
    >
      {tiles.map(([x, y, w, h, sx, sy], i) => (
        <div
          key={i}
          aria-hidden="true"
          style={{
            position: 'absolute',
            left: x,
            top: y,
            width: w,
            height: h,
            backgroundImage: 'var(--strip-captcha-atlas)',
            backgroundRepeat: 'no-repeat',
            backgroundSize: `${imageWidth}px ${imageHeight}px`,
            backgroundPosition: `${-sx}px ${-sy}px`,
          }}
        />
      ))}
    </div>
  );
}
