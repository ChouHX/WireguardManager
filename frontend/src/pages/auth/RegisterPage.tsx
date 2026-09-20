import { useState } from 'react';
import { Anchor, Button, PasswordInput, Stack, Text, TextInput } from '@mantine/core';
import { useForm } from '@mantine/form';
import { notifications } from '@mantine/notifications';
import { Link, useNavigate } from 'react-router-dom';

import { ErrorAlert } from '@/components/common/Feedback';
import { useTranslation } from '@/i18n';
import { messageOf } from '@/lib/format';
import { useAuthStore } from '@/stores/auth-store';

const EMAIL_PATTERN = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;

interface RegisterFormValues {
  name: string;
  email: string;
  password: string;
  confirmPassword: string;
}

export default function RegisterPage() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const register = useAuthStore((state) => state.register);
  const isLoading = useAuthStore((state) => state.isLoading);
  const [error, setError] = useState<string | null>(null);

  const form = useForm<RegisterFormValues>({
    initialValues: { name: '', email: '', password: '', confirmPassword: '' },
    // 两次密码一致性依赖整个表单值，用函数式校验统一裁决
    validate: (values) => {
      const errors: Record<string, string> = {};
      if (values.name.trim().length < 2) errors.name = t('auth.nameMinLength');
      if (!EMAIL_PATTERN.test(values.email.trim())) errors.email = t('validation.email');
      if (values.password.length < 6) errors.password = t('auth.passwordMinLength');
      if (values.password !== values.confirmPassword) {
        errors.confirmPassword = t('auth.passwordMismatch');
      }
      return errors;
    },
  });

  const handleSubmit = form.onSubmit(async (values) => {
    setError(null);
    try {
      await register({
        name: values.name.trim(),
        email: values.email.trim(),
        password: values.password,
      });
      notifications.show({
        color: 'teal',
        title: t('auth.registerSuccess'),
        message: t('auth.loginTitle'),
      });
      navigate('/auth/login');
    } catch (err) {
      setError(messageOf(err, t('auth.emailExists')));
    }
  });

  return (
    <Stack gap="lg">
      {/* 外层已是卡片，这里只承载标题与表单，避免双层卡片 */}
      <Stack gap={4}>
        <Text fw={650} fz={20} lh={1.25}>
          {t('auth.registerTitle')}
        </Text>
        <Text fz={12.5} c="dimmed">
          {t('auth.registerDescription')}
        </Text>
      </Stack>

      <form onSubmit={handleSubmit} noValidate>
        <Stack gap="md">
          <TextInput
            label={t('common.name')}
            placeholder={t('auth.namePlaceholder')}
            autoComplete="name"
            {...form.getInputProps('name')}
          />

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
            autoComplete="new-password"
            {...form.getInputProps('password')}
          />

          <PasswordInput
            label={t('auth.confirmPassword')}
            placeholder={t('auth.confirmPasswordPlaceholder')}
            autoComplete="new-password"
            {...form.getInputProps('confirmPassword')}
          />

          <ErrorAlert message={error} />

          <Button type="submit" color="wg" fullWidth loading={isLoading} disabled={isLoading}>
            {isLoading ? t('auth.signingUp') : t('auth.signUp')}
          </Button>

          <Text fz={11} c="dimmed">
            {isLoading ? t('common.loading') : t('wireguard.autoGenerateNote')}
          </Text>
        </Stack>
      </form>

      <Text fz={12.5} c="dimmed">
        {t('auth.haveAccount')}{' '}
        <Anchor component={Link} to="/auth/login" fw={600} c="wg.6" fz={12.5}>
          {t('auth.signIn')}
        </Anchor>
      </Text>
    </Stack>
  );
}
