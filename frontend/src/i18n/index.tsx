import { createContext, useCallback, useContext, useEffect, useMemo, useState } from 'react';
import type { ReactNode } from 'react';
import zh from './locales/zh.json';
import en from './locales/en.json';

export type Locale = 'zh' | 'en';

type TranslationTree = typeof zh;

const translations: Record<Locale, TranslationTree> = {
  zh,
  en: en as unknown as TranslationTree,
};

const LOCALE_KEY = 'wm_locale';

interface I18nContextValue {
  locale: Locale;
  setLocale: (locale: Locale) => void;
  toggleLocale: () => void;
  t: (key: string, params?: Record<string, string | number>) => string;
}

const I18nContext = createContext<I18nContextValue | null>(null);

function resolve(tree: unknown, keys: string[]): unknown {
  let cursor: unknown = tree;
  for (const key of keys) {
    if (cursor && typeof cursor === 'object' && key in (cursor as Record<string, unknown>)) {
      cursor = (cursor as Record<string, unknown>)[key];
    } else {
      return undefined;
    }
  }
  return cursor;
}

export function I18nProvider({ children }: { children: ReactNode }) {
  const [locale, setLocaleState] = useState<Locale>(() => {
    try {
      const saved = localStorage.getItem(LOCALE_KEY);
      if (saved === 'zh' || saved === 'en') return saved;
      const nav = navigator.language?.toLowerCase() ?? '';
      return nav.startsWith('zh') ? 'zh' : 'en';
    } catch {
      return 'zh';
    }
  });

  useEffect(() => {
    document.documentElement.lang = locale === 'zh' ? 'zh-CN' : 'en';
    try {
      localStorage.setItem(LOCALE_KEY, locale);
    } catch {
      /* 忽略隐私模式写入失败 */
    }
  }, [locale]);

  const setLocale = useCallback((next: Locale) => setLocaleState(next), []);
  const toggleLocale = useCallback(
    () => setLocaleState((prev) => (prev === 'zh' ? 'en' : 'zh')),
    [],
  );

  const t = useCallback(
    (key: string, params?: Record<string, string | number>) => {
      const value = resolve(translations[locale], key.split('.'));
      if (typeof value !== 'string') return key;
      if (!params) return value;
      return Object.entries(params).reduce(
        (acc, [name, replacement]) => acc.replace(`{${name}}`, String(replacement)),
        value,
      );
    },
    [locale],
  );

  const value = useMemo<I18nContextValue>(
    () => ({ locale, setLocale, toggleLocale, t }),
    [locale, setLocale, toggleLocale, t],
  );

  return <I18nContext.Provider value={value}>{children}</I18nContext.Provider>;
}

export function useTranslation(): I18nContextValue {
  const ctx = useContext(I18nContext);
  if (!ctx) {
    throw new Error('useTranslation must be used within I18nProvider');
  }
  return ctx;
}
