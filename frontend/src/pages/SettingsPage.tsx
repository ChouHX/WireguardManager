import { useCallback, useEffect, useMemo, useState } from 'react';
import {
  Box,
  Button,
  Card,
  Divider,
  Group,
  NumberInput,
  SimpleGrid,
  Stack,
  Switch,
  Text,
  TextInput,
} from '@mantine/core';
import { notifications } from '@mantine/notifications';
import { IconAdjustments, IconDeviceFloppy, IconRefresh } from '@tabler/icons-react';

import { ErrorAlert } from '@/components/common/Feedback';
import { InlineLoader } from '@/components/common/LoadingScreen';
import { PageHeader } from '@/components/common/PageHeader';
import { useTranslation } from '@/i18n';
import { messageOf } from '@/lib/format';
import { settingsService } from '@/services';
import type { SettingDef, SettingGroup, SettingsResponse } from '@/types/settings';

/** 分组展示顺序 */
const GROUP_ORDER: SettingGroup[] = ['network', 'monitoring', 'liveness', 'auth', 'wireguard'];

/** 每个配置项对应的文案键后缀 */
const LABEL_KEY: Record<string, string> = {
  'network.server_ip': 'serverIp',
  'network.out_interface': 'outInterface',
  'network.base_subnet': 'baseSubnet',
  'network.base_port': 'basePort',
  'network.dns': 'dns',
  'network.client_allowed_ips': 'clientAllowedIps',
  'monitoring.interval_seconds': 'monitoringInterval',
  'monitoring.retention_hours': 'monitoringRetention',
  'liveness.enabled': 'livenessEnabled',
  'liveness.interval_seconds': 'livenessInterval',
  'liveness.probe_timeout_ms': 'livenessProbeTimeout',
  'liveness.probe_port': 'livenessProbePort',
  'liveness.handshake_timeout_seconds': 'livenessHandshakeTimeout',
  'liveness.offline_threshold': 'livenessOfflineThreshold',
  'jwt.expire_hours': 'jwtExpireHours',
  'wireguard.default_preshared_key': 'defaultPresharedKey',
};

export default function SettingsPage() {
  const { t } = useTranslation();

  const [data, setData] = useState<SettingsResponse | null>(null);
  const [draft, setDraft] = useState<Record<string, string>>({});
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async () => {
    try {
      setError(null);
      const response = await settingsService.get();
      if (response.success && response.data) {
        setData(response.data);
        setDraft(response.data.values);
      }
    } catch (err) {
      setError(messageOf(err, t('errors.somethingWrong')));
    } finally {
      setLoading(false);
    }
  }, [t]);

  useEffect(() => {
    void load();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const handleSave = async () => {
    setSaving(true);
    setError(null);
    try {
      const response = await settingsService.update(draft);
      if (response.success) {
        notifications.show({ color: 'teal', message: t('settings.saved') });
        if (response.data?.values) {
          setDraft(response.data.values);
        }
      }
    } catch (err) {
      setError(messageOf(err, t('settings.saveFailed')));
    } finally {
      setSaving(false);
    }
  };

  const dirty = useMemo(
    () => Boolean(data) && JSON.stringify(draft) !== JSON.stringify(data?.values ?? {}),
    [data, draft],
  );

  const crumbs = useMemo(
    () => [
      { label: t('breadcrumb.home'), to: '/dashboard' },
      { label: t('settings.title') },
    ],
    [t],
  );

  const renderField = (def: SettingDef) => {
    const labelKey = LABEL_KEY[def.key];
    const label = labelKey ? t(`settings.${labelKey}`) : def.key;
    const value = draft[def.key] ?? '';

    if (def.type === 'bool') {
      return (
        <Group key={def.key} justify="space-between" wrap="nowrap" align="flex-start">
          <Box>
            <Text size="sm" fw={600}>
              {label}
            </Text>
            <Text fz={11} c="dimmed" className="wm-mono">
              {def.key}
            </Text>
          </Box>
          <Switch
            checked={value === 'true'}
            onChange={(event) =>
              setDraft((prev) => ({ ...prev, [def.key]: String(event.currentTarget.checked) }))
            }
            color="wg"
          />
        </Group>
      );
    }

    if (def.type === 'int') {
      return (
        <NumberInput
          key={def.key}
          label={label}
          description={def.key}
          value={Number(value) || 0}
          onChange={(next) => setDraft((prev) => ({ ...prev, [def.key]: String(next ?? 0) }))}
          min={def.min}
          max={def.max}
        />
      );
    }

    return (
      <TextInput
        key={def.key}
        label={label}
        description={def.key}
        value={value}
        onChange={(event) => setDraft((prev) => ({ ...prev, [def.key]: event.currentTarget.value }))}
        className="wm-mono"
      />
    );
  };

  if (loading || !data) {
    return (
      <Stack gap="md">
        <PageHeader title={t('settings.title')} subtitle={t('settings.subtitle')} crumbs={crumbs} />
        <Card>
          <InlineLoader label={t('common.loading')} />
        </Card>
      </Stack>
    );
  }

  return (
    <Stack gap="md">
      <PageHeader
        title={t('settings.title')}
        subtitle={t('settings.subtitle')}
        crumbs={crumbs}
        actions={
          <Group gap="xs">
            <Button
              variant="default"
              radius="xs"
              leftSection={<IconRefresh size={15} />}
              onClick={() => void load()}
              disabled={saving}
            >
              {t('common.refresh')}
            </Button>
            <Button
              color="wg"
              radius="xs"
              leftSection={<IconDeviceFloppy size={15} />}
              onClick={() => void handleSave()}
              loading={saving}
              disabled={!dirty}
            >
              {t('common.save')}
            </Button>
          </Group>
        }
      />

      <ErrorAlert message={error} onClose={() => setError(null)} />

      {GROUP_ORDER.map((group, index) => {
        const defs = data.defs.filter((def) => def.group === group);
        if (defs.length === 0) return null;

        return (
          <Card
            key={group}
            className="wm-rise"
            style={{ '--wm-delay': `${index * 40}ms` } as React.CSSProperties}
          >
            <Group gap={8} mb="sm">
              <IconAdjustments size={16} stroke={1.7} />
              <Text fw={650}>{t(`settings.group.${group}`)}</Text>
            </Group>
            <Divider mb="md" variant="dashed" />
            <SimpleGrid cols={{ base: 1, md: 2 }} spacing="md">
              {defs.map(renderField)}
            </SimpleGrid>
          </Card>
        );
      })}

      <Text fz={11.5} c="dimmed">
        {t('settings.footerHint')}
      </Text>
    </Stack>
  );
}
