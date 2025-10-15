import { useContext } from 'react';
import { I18nContext } from '@/components/providers/i18n-provider';

export function useI18n() {
  const context = useContext(I18nContext);
  if (!context) {
    throw new Error('useI18n must be used within I18nProvider');
  }
  return context;
}

