import { useState } from 'react';
import type { CSSProperties } from 'react';
import {
  Badge,
  Button,
  Card,
  Group,
  PasswordInput,
  SimpleGrid,
  Stack,
  Text,
  TextInput,
  Title,
} from '@mantine/core';
import { useForm } from '@mantine/form';
import { notifications } from '@mantine/notifications';
import { IconId, IconMail, IconShieldLock, IconUser } from '@tabler/icons-react';

import { EmptyState, ErrorAlert } from '@/components/common/Feedback';
import { PageHeader } from '@/components/common/PageHeader';
import { useTranslation } from '@/i18n';
import { formatDateTime, messageOf } from '@/lib/format';
import { authService } from '@/services';
import { useAuthStore } from '@/stores/auth-store';
import type { UpdateProfileRequest } from '@/types/auth';

interface ProfileFormValues {
  name: string;
  password: string;
  currentPassword: string;
}

/** 信息行：左侧灰色小字标签，右侧加粗取值 */
function InfoRow({ label, value, mono }: { label: string; value: string; mono?: boolean }) {
  return (
    <Group justify="space-between" align="flex-start" gap="md" wrap="nowrap">
      <Text size="xs" c="dimmed" flex="0 0 auto">
        {label}
      </Text>
      <Text
        size="sm"
        fw={600}
        ta="right"
        className={mono ? 'wm-mono' : undefined}
        style={mono ? { wordBreak: 'break-all' } : undefined}
      >
        {value}
      </Text>
    </Group>
  );
}

export default function AccountPage() {
  const { t, locale } = useTranslation();
  const user = useAuthStore((state) => state.user);
  const setUser = useAuthStore((state) => state.setUser);
  const [error, setError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);

  const form = useForm<ProfileFormValues>({
    initialValues: { name: user?.name ?? '', password: '', currentPassword: '' },
    validate: {
      name: (value) => (value.trim().length < 2 ? t('auth.nameMinLength') : null),
      // 密码留空表示不修改，填了才校验长度
      password: (value) => (value.length > 0 && value.length < 6 ? t('auth.passwordMinLength') : null),
      // 改密必须验证当前密码，否则 token 泄漏即可直接接管账号
      currentPassword: (value, values) =>
        values.password.length > 0 && value.length === 0 ? t('user.currentPasswordRequired') : null,
    },
  });

  const handleSubmit = form.onSubmit(async (values) => {
    if (!user) return;
    setError(null);

    const nextName = values.name.trim();
    const payload: UpdateProfileRequest = {};
    if (nextName !== user.name) payload.name = nextName;
    if (values.password) {
      payload.password = values.password;
      payload.current_password = values.currentPassword;
    }

    if (!payload.name && !payload.password) {
      notifications.show({ color: 'yellow', message: t('user.noFieldsChanged') });
      return;
    }

    setSubmitting(true);
    try {
      const response = await authService.updateProfile(payload);
      if (!response.success || !response.data) {
        throw new Error(response.message || t('user.updateFailed'));
      }

      setUser(response.data);
      // 密码不回填，并把当前值作为新的基线，清掉“已修改”标记
      form.setFieldValue('password', '');
      form.setFieldValue('currentPassword', '');
      form.resetDirty();
      notifications.show({
        color: 'teal',
        title: t('user.profileUpdated'),
        message: t('common.success'),
      });
    } catch (err) {
      setError(messageOf(err, t('user.updateFailed')));
    } finally {
      setSubmitting(false);
    }
  });

  const crumbs = [
    { label: t('breadcrumb.home'), to: '/dashboard' },
    { label: t('breadcrumb.account') },
  ];

  if (!user) {
    return (
      <Stack gap="md">
        <PageHeader title={t('user.profile')} subtitle={t('user.viewAccountInfo')} crumbs={crumbs} />
        <Card className="wm-rise" style={{ '--wm-delay': '60ms' } as CSSProperties}>
          <EmptyState title={t('common.noData')} description={t('errors.unauthorized')} />
        </Card>
      </Stack>
    );
  }

  const isAdmin = user.role === 'admin';
  const dateLocale = locale === 'zh' ? 'zh-CN' : 'en-US';

  return (
    <Stack gap="md">
      <PageHeader title={t('user.profile')} subtitle={t('user.viewAccountInfo')} crumbs={crumbs} />

      <SimpleGrid cols={{ base: 1, md: 2 }} spacing="sm">
        <Card className="wm-rise" style={{ '--wm-delay': '60ms' } as CSSProperties}>
          <Stack gap="md">
            <Group gap="xs">
              <IconUser size={18} stroke={1.7} />
              <Title order={4}>{t('user.userInfo')}</Title>
            </Group>

            <Stack gap="sm">
              <InfoRow label={t('common.name')} value={user.name} />
              <InfoRow label={t('common.email')} value={user.email} mono />

              <Group justify="space-between" align="center" gap="md" wrap="nowrap">
                <Text size="xs" c="dimmed">
                  {t('common.role')}
                </Text>
                <Badge color={isAdmin ? 'wg' : 'gray'} leftSection={<IconShieldLock size={12} />}>
                  {isAdmin ? t('user.roleAdmin') : t('user.roleUser')}
                </Badge>
              </Group>

              <InfoRow label={t('user.userId')} value={user.user_uid} mono />
              <InfoRow
                label={t('user.accountCreated')}
                value={formatDateTime(user.created_at, dateLocale)}
              />
            </Stack>

            <Text size="xs" c="dimmed">
              {t('user.profileHint')}
            </Text>
          </Stack>
        </Card>

        <Card className="wm-rise" style={{ '--wm-delay': '120ms' } as CSSProperties}>
          <Stack gap="md">
            <Stack gap={4}>
              <Title order={4}>{t('user.editProfile')}</Title>
              <Text size="xs" c="dimmed">
                {t('user.updateProfileInfo')}
              </Text>
            </Stack>

            <form onSubmit={handleSubmit} noValidate>
              <Stack gap="md">
                <TextInput
                  label={t('common.name')}
                  placeholder={t('auth.namePlaceholder')}
                  autoComplete="name"
                  leftSection={<IconUser size={16} />}
                  {...form.getInputProps('name')}
                />

                <PasswordInput
                  label={t('user.newPassword')}
                  placeholder={t('user.newPasswordPlaceholder')}
                  autoComplete="new-password"
                  leftSection={<IconShieldLock size={16} />}
                  {...form.getInputProps('password')}
                />

                {/* 只在填写新密码时出现：改密必须先验证当前密码 */}
                {form.values.password.length > 0 && (
                  <PasswordInput
                    label={t('user.currentPassword')}
                    placeholder={t('user.currentPasswordPlaceholder')}
                    autoComplete="current-password"
                    leftSection={<IconShieldLock size={16} />}
                    withAsterisk
                    {...form.getInputProps('currentPassword')}
                  />
                )}

                <ErrorAlert message={error} />

                <Button type="submit" color="wg" fullWidth loading={submitting} disabled={submitting}>
                  {submitting ? t('user.updating') : t('user.updateProfile')}
                </Button>

                <Group gap="xs" justify="center" wrap="nowrap">
                  <IconMail size={14} />
                  <Text size="xs" c="dimmed" className="wm-mono">
                    {user.email}
                  </Text>
                  <IconId size={14} />
                  <Text size="xs" c="dimmed" className="wm-mono">
                    {user.user_uid}
                  </Text>
                </Group>
              </Stack>
            </form>
          </Stack>
        </Card>
      </SimpleGrid>
    </Stack>
  );
}
