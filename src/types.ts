/** Плитка [x, y, w, h, sx, sy]: прямоугольник (x, y, w, h) собранной картинки берётся из атласа с позиции (sx, sy). CSS-пиксели. */
export type StripCaptchaTile = [x: number, y: number, w: number, h: number, sx: number, sy: number];

/** Капча, как её отдаёт сервер (Go: stripcaptcha.Challenge). */
export interface StripCaptchaChallenge {
  enabled?: true;
  id: string;
  /** Атлас с перемешанными плитками: data:-URI или обычный URL. */
  image: string;
  imageWidth: number;
  imageHeight: number;
  /** Размер собранной капчи. */
  width: number;
  height: number;
  /** Число цифр в ответе. */
  length: number;
  /** Через сколько секунд капча протухнет. */
  expiresIn?: number;
  tiles: StripCaptchaTile[];
}

/** Сервер может сообщить, что капча сейчас не нужна (выключена настройкой). */
export interface StripCaptchaDisabled {
  enabled: false;
}

export type StripCaptchaResponse = StripCaptchaChallenge | StripCaptchaDisabled;

/** Ответ для отправки на сервер вместе с формой. */
export interface StripCaptchaValue {
  id: string;
  answer: string;
}
