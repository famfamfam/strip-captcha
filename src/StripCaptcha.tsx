import { useEffect, useId, useRef, useState } from 'react';
import type { InputHTMLAttributes, ReactNode } from 'react';
import { toAsciiDigits } from './digits.js';
import { StripCaptchaImage } from './StripCaptchaImage.js';
import type { StripCaptchaValue } from './types.js';
import { useStripCaptcha } from './useStripCaptcha.js';
import type { UseStripCaptchaOptions } from './useStripCaptcha.js';

export interface StripCaptchaLabels {
  label: string;
  /** {n} заменяется числом цифр. */
  placeholder: string;
  refresh: string;
  loadError: string;
  imageAlt: string;
}

export interface StripCaptchaClassNames {
  root?: string;
  label?: string;
  row?: string;
  image?: string;
  status?: string;
  refresh?: string;
  input?: string;
}

export interface StripCaptchaProps extends UseStripCaptchaOptions {
  /** null, пока код не введён целиком, — на этом удобно блокировать отправку формы. */
  onChange: (value: StripCaptchaValue | null) => void;
  /** false — сервер ответил `{ enabled: false }`, поле не показывается. Ошибка загрузки считается true. */
  onAvailabilityChange?: (enabled: boolean) => void;
  labels?: Partial<StripCaptchaLabels>;
  /** Добавляются к базовым классам strip-captcha__* (стили по умолчанию — strip-captcha/styles.css). */
  classNames?: StripCaptchaClassNames;
  refreshIcon?: ReactNode;
  loadingIndicator?: ReactNode;
  /** Дополнительные атрибуты поля ввода (name, autoFocus, aria-*...). */
  inputProps?: Omit<InputHTMLAttributes<HTMLInputElement>, 'value' | 'onChange' | 'maxLength' | 'type'>;
}

const defaultLabels: StripCaptchaLabels = {
  label: 'Code from the image',
  placeholder: 'Enter {n} digits',
  refresh: 'Show another image',
  loadError: "Couldn't load the image",
  imageAlt: 'Captcha: digits to type into the field below',
};

const cx = (base: string, extra?: string) => (extra ? `${base} ${extra}` : base);

const RefreshIcon = () => (
  <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
    <path d="M21 12a9 9 0 1 1-2.64-6.36" />
    <path d="M21 3v6h-6" />
  </svg>
);

/**
 * Поле капчи для формы: картинка, кнопка обновления и ввод цифр. Правильность
 * ответа проверяет только сервер — компонент лишь собирает картинку и
 * нормализует ввод.
 */
export function StripCaptcha({
  onChange,
  onAvailabilityChange,
  labels,
  classNames = {},
  refreshIcon,
  loadingIndicator,
  inputProps,
  ...options
}: StripCaptchaProps) {
  const l = { ...defaultLabels, ...labels };
  const { status, challenge, refresh } = useStripCaptcha(options);
  // Ответ помнит, к какой капче введён: с новой капчей он сразу пустой, без
  // рендера, в котором родитель получил бы новый id со старым ответом
  const [typed, setTyped] = useState({ id: '', value: '' });
  const answer = challenge && typed.id === challenge.id ? typed.value : '';
  const inputId = useId();

  // Колбэки родителя — через ref: иначе инлайновая функция в пропсах
  // перезапускала бы эффекты на каждом рендере
  const onChangeRef = useRef(onChange);
  onChangeRef.current = onChange;
  const onAvailabilityRef = useRef(onAvailabilityChange);
  onAvailabilityRef.current = onAvailabilityChange;

  useEffect(() => {
    if (status !== 'loading') onAvailabilityRef.current?.(status !== 'disabled');
  }, [status]);

  useEffect(() => {
    onChangeRef.current(challenge && answer.length === challenge.length ? { id: challenge.id, answer } : null);
  }, [answer, challenge]);

  if (status === 'disabled') return null;

  const length = challenge?.length ?? 5;
  const loading = status === 'loading';

  return (
    <div className={cx(loading ? 'strip-captcha strip-captcha--loading' : 'strip-captcha', classNames.root)}>
      <label htmlFor={inputId} className={cx('strip-captcha__label', classNames.label)}>
        {l.label}
      </label>
      <div className={cx('strip-captcha__row', classNames.row)}>
        {challenge ? (
          <StripCaptchaImage challenge={challenge} alt={l.imageAlt} className={classNames.image} />
        ) : (
          // Заглушка того же размера, пока капча грузится, — форма не прыгает
          <div
            className={cx('strip-captcha__image strip-captcha__status', classNames.status)}
            style={{ width: 204, height: 64, flexShrink: 0 }}
            role={status === 'error' ? 'alert' : undefined}
          >
            {status === 'error' ? l.loadError : loadingIndicator}
          </div>
        )}
        <button
          type="button"
          onClick={refresh}
          disabled={loading}
          aria-label={l.refresh}
          title={l.refresh}
          className={cx('strip-captcha__refresh', classNames.refresh)}
        >
          {refreshIcon ?? <RefreshIcon />}
        </button>
      </div>
      <input
        autoComplete="off"
        autoCorrect="off"
        spellCheck={false}
        inputMode="numeric"
        required
        {...inputProps}
        id={inputId}
        type="text"
        maxLength={length}
        value={answer}
        onChange={(e) => challenge && setTyped({ id: challenge.id, value: toAsciiDigits(e.target.value).slice(0, length) })}
        placeholder={l.placeholder.replace('{n}', String(length))}
        disabled={!challenge}
        className={cx('strip-captcha__input', classNames.input)}
      />
    </div>
  );
}
