import { useState } from 'react';
import { Anchor, Button, PasswordInput, Stack, Text, TextInput } from '@mantine/core';
import { useForm } from '@mantine/form';
import { Link, useNavigate } from 'react-router-dom';

import { ErrorAlert } from '@/components/common/Feedback';
import { useTranslation } from '@/i18n';
import { messageOf } from '@/lib/format';
import { useAuthStore } from '@/stores/auth-store';
import type { LoginRequest } from '@/types/auth';

const EMAIL_PATTERN = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;

/** 默认管理员凭证提示（与后端首次启动的 seed 数据一致） */
const DEFAULT_ACCOUNT = 'admin@platform.com';
const DEFAULT_PASSWORD = 'password';

export default function LoginPage() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const login = useAuthStore((state) => state.login);
  const isLoading = useAuthStore((state) => state.isLoading);
  const [error, setError] = useState<string | null>(null);

  const form = useForm<LoginRequest>({
    initialValues: { email: '', password: '' },
    validate: (values) => {
      const errors: Record<string, string> = {};
      if (!EMAIL_PATTERN.test(values.email.trim())) errors.email = t('validation.email');
      if (values.password.length < 6) errors.password = t('auth.passwordMinLength');
      return errors;
    },
  });

  const handleSubmit = form.onSubmit(async (values) => {
    setError(null);
    try {
      await login({ email: values.email.trim(), password: values.password });
      navigate('/dashboard', { replace: true });
    } catch (err) {
      setError(messageOf(err, t('auth.invalidCredentials')));
    }
  });

  return (
    <Stack gap="lg">
      {/* 外层已是卡片，这里只承载标题与表单，避免双层卡片 */}
      <Stack gap={4}>
        <Text fw={650} fz={20} lh={1.25}>
          {t('auth.loginTitle')}
        </Text>
        <Text fz={12.5} c="dimmed">
          {t('auth.loginDescription')}
        </Text>
      </Stack>

      <form onSubmit={handleSubmit} noValidate>
        <Stack gap="md">
          <TextInput
            label={t('common.email')}
            placeholder={t('auth.emailPlaceholder')}
            type="email"
            autoComplete="email"
            {...form.getInputProps('email')}
          />

          <PasswordInput
            label={t('common.password')}
            placeholder={t('auth.passwordPlaceholder')}
            autoComplete="current-password"
            {...form.getInputProps('password')}
          />

          <ErrorAlert message={error} />

          <Button type="submit" color="wg" fullWidth loading={isLoading} disabled={isLoading}>
            {isLoading ? t('auth.signingIn') : t('auth.signIn')}
          </Button>
        </Stack>
      </form>

      <Stack gap={6}>
        <Text fz={12.5} c="dimmed">
          {t('auth.noAccount')}{' '}
          <Anchor component={Link} to="/auth/register" fw={600} c="wg.6" fz={12.5}>
            {t('auth.signUp')}
          </Anchor>
        </Text>
        <Text fz={11} c="dimmed">
          {t('common.email')}: <span className="wm-mono">{DEFAULT_ACCOUNT}</span> /{' '}
          <span className="wm-mono">{DEFAULT_PASSWORD}</span>
        </Text>
      </Stack>
    </Stack>
  );
}
