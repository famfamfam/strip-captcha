import { useCallback, useEffect, useRef, useState } from 'react';
import type { StripCaptchaChallenge, StripCaptchaResponse } from './types.js';

export type StripCaptchaStatus = 'loading' | 'ready' | 'disabled' | 'error';

export interface UseStripCaptchaOptions {
  /**
   * Запрос новой капчи. null/undefined или исключение — ошибка загрузки,
   * `{ enabled: false }` — капча не нужна. Можно передавать новую функцию на
   * каждом рендере: перезапрос вызывают только reloadKey и refresh().
   */
  fetchChallenge: () => Promise<StripCaptchaResponse | null | undefined>;
  /** Смените значение, чтобы запросить новую капчу (например, после отказа сервера). */
  reloadKey?: unknown;
  /** Запросить новую капчу незадолго до истечения expiresIn. По умолчанию true. */
  autoRefresh?: boolean;
}

export interface UseStripCaptchaResult {
  status: StripCaptchaStatus;
  challenge: StripCaptchaChallenge | null;
  error: unknown;
  refresh: () => void;
}

/** За сколько до истечения капчи запрашивать новую: ответ ещё должен успеть дойти. */
const REFRESH_MARGIN_MS = 15_000;

interface State {
  status: StripCaptchaStatus;
  challenge: StripCaptchaChallenge | null;
  error: unknown;
}

/** Загрузка капчи без разметки — для своего UI. Готовое поле — StripCaptcha. */
export function useStripCaptcha({
  fetchChallenge,
  reloadKey,
  autoRefresh = true,
}: UseStripCaptchaOptions): UseStripCaptchaResult {
  const [state, setState] = useState<State>({ status: 'loading', challenge: null, error: null });
  // Счётчик только растёт: и reloadKey, и refresh(), и автообновление
  // запускают один и тот же эффект
  const [tick, setTick] = useState(0);
  const fetchRef = useRef(fetchChallenge);
  fetchRef.current = fetchChallenge;

  useEffect(() => {
    let cancelled = false;
    setState({ status: 'loading', challenge: null, error: null });
    Promise.resolve()
      .then(() => fetchRef.current())
      .then(
        (res) => {
          if (cancelled) return;
          if (!res) {
            setState({ status: 'error', challenge: null, error: new Error('empty captcha response') });
          } else if (res.enabled === false) {
            setState({ status: 'disabled', challenge: null, error: null });
          } else {
            setState({ status: 'ready', challenge: res, error: null });
          }
        },
        (error: unknown) => {
          if (!cancelled) setState({ status: 'error', challenge: null, error });
        },
      );
    return () => {
      cancelled = true;
    };
  }, [reloadKey, tick]);

  // Таймер привязан к конкретной капче: новая капча — новый отсчёт
  const { challenge } = state;
  useEffect(() => {
    if (!autoRefresh || !challenge?.expiresIn) return;
    const delay = Math.max(challenge.expiresIn * 1000 - REFRESH_MARGIN_MS, 5_000);
    const timer = setTimeout(() => setTick((n) => n + 1), delay);
    return () => clearTimeout(timer);
  }, [autoRefresh, challenge]);

  const refresh = useCallback(() => setTick((n) => n + 1), []);

  return { ...state, refresh };
}
