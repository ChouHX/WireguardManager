import { useCallback, useEffect, useMemo, useState } from 'react';
import {
  Box,
  Button,
  Card,
  Divider,
  Grid,
  Group,
  NumberInput,
  Select,
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
import { settingsService, wireguardService } from '@/services';
import type { SettingDef, SettingGroup, SettingsResponse } from '@/types/settings';
import type { NetworkInterfacesResponse } from '@/types/wireguard';

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
  'liveness.probe_port': 'livenessProbePort',
  'jwt.expire_hours': 'jwtExpireHours',
  'wireguard.default_preshared_key': 'defaultPresharedKey',
};

/** 标签列宽：固定后左右两侧的行都能对齐 */
const LABEL_COL = { base: 12, sm: 5 } as const;
const FIELD_COL = { base: 12, sm: 7 } as const;

/** 统一行高：让开关行与输入行在视觉上完全齐平 */
const ROW_HEIGHT = 49;

export default function SettingsPage() {
  const { t } = useTranslation();

  const [data, setData] = useState<SettingsResponse | null>(null);
  const [draft, setDraft] = useState<Record<string, string>>({});
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  /** 服务器出口网卡的候选列表（由后端探测默认路由得出） */
  const [interfaces, setInterfaces] = useState<NetworkInterfacesResponse | null>(null);

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

  // 出口网卡是服务器自身的参数，探测结果在这里才有意义
  const loadInterfaces = useCallback(async () => {
    try {
      const response = await wireguardService.getInterfaces();
      if (response.success && response.data) {
        setInterfaces(response.data);
      }
    } catch {
      // 探测失败时降级为手动输入
    }
  }, []);

  useEffect(() => {
    void load();
    void loadInterfaces();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const handleSave = async () => {
    setSaving(true);
    setError(null);
    try {
      // 只提交当前仍生效的配置项：values 中可能残留已下线的历史键，
      // 整份回传会触发后端的白名单校验失败。
      const managed = new Set((data?.defs ?? []).map((def) => def.key));
      const payload: Record<string, string> = {};
      for (const [key, value] of Object.entries(draft)) {
        if (managed.has(key)) payload[key] = value;
      }

      const response = await settingsService.update(payload);
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

  const dirty = useMemo(() => {
    if (!data) return false;
    const managed = (data.defs ?? []).map((def) => def.key);
    return managed.some((key) => (draft[key] ?? '') !== (data.values[key] ?? ''));
  }, [data, draft]);

  const crumbs = useMemo(
    () => [{ label: t('breadcrumb.home'), to: '/dashboard' }, { label: t('settings.title') }],
    [t],
  );

  const set = (key: string, value: string) => setDraft((prev) => ({ ...prev, [key]: value }));

  /** 统一渲染控件本体（不含标签），保证左右两列在同一基线上对齐 */
  const renderControl = (def: SettingDef) => {
    const value = draft[def.key] ?? '';

    if (def.type === 'bool') {
      return (
        <Switch
          checked={value === 'true'}
          onChange={(event) => set(def.key, String(event.currentTarget.checked))}
          color="wg"
          size="md"
          label={value === 'true' ? t('settings.on') : t('settings.off')}
          labelPosition="right"
          styles={{ label: { fontSize: 12, color: 'var(--mantine-color-dimmed)' } }}
        />
      );
    }

    // 服务器出口网卡：用探测到的接口作为候选项，同时允许自由输入
    if (def.key === 'network.out_interface') {
      const candidates = (interfaces?.interfaces ?? []).filter((item) => !item.is_loopback);
      const options = candidates.map((item) => ({
        value: item.name,
        label: item.is_default ? `${item.name} (${t('wireguard.ifaceDefault')})` : item.name,
      }));
      if (value && !options.some((option) => option.value === value)) {
        options.unshift({ value, label: `${value} (${t('wireguard.ifaceCustom')})` });
      }

      return (
        <Select
          data={options}
          value={value || null}
          onChange={(next) => set(def.key, next ?? '')}
          searchable
          clearable
          nothingFoundMessage={t('common.noData')}
          description={t('wireguard.ifaceServerHint')}
        />
      );
    }

    if (def.type === 'int') {
      return (
        <NumberInput
          value={Number(value) || 0}
          onChange={(next) => set(def.key, String(next ?? 0))}
          min={def.min}
          max={def.max}
        />
      );
    }

    return (
      <TextInput
        value={value}
        onChange={(event) => set(def.key, event.currentTarget.value)}
        className="wm-mono"
        placeholder={def.key === 'network.client_allowed_ips' ? t('settings.allowedIpsPlaceholder') : undefined}
      />
    );
  };

  if (loading || !data) {
    return (
      <Stack gap="sm">
        <PageHeader title={t('settings.title')} subtitle={t('settings.subtitle')} crumbs={crumbs} />
        <Card>
          <InlineLoader label={t('common.loading')} />
        </Card>
      </Stack>
    );
  }

  return (
    <Stack gap="sm">
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
            padding="sm"
          >
            <Group gap={8} mb={6}>
              <IconAdjustments size={15} stroke={1.7} />
              <Text fw={650} fz={13.5}>
                {t(`settings.group.${group}`)}
              </Text>
            </Group>
            <Divider mb="xs" variant="dashed" />

            {/* 在线判定只有一项：补一行说明，讲清其余参数为何无需配置 */}
            {group === 'liveness' ? (
              <Text fz={11.5} c="dimmed" mb={6}>
                {t('settings.livenessNote')}
              </Text>
            ) : null}

            {/* 统一的行结构：左侧标签固定列宽，右侧控件，逐行对齐 */}
            <Stack gap={2}>
              {defs.map((def) => {
                const labelKey = LABEL_KEY[def.key];
                return (
                  <Grid
                    key={def.key}
                    gutter="sm"
                    align="center"
                    /* 固定行高：开关行的控件比输入框矮，不统一会让各行看起来参差 */
                    mih={ROW_HEIGHT}
                    style={{ borderBottom: '1px solid var(--mantine-color-default-border)' }}
                  >
                    <Grid.Col span={LABEL_COL}>
                      <Box>
                        <Text fz={13} fw={550} lh={1.3}>
                          {labelKey ? t(`settings.${labelKey}`) : def.key}
                        </Text>
                        <Text fz={10} c="dimmed" className="wm-mono" lh={1.3}>
                          {def.key}
                        </Text>
                      </Box>
                    </Grid.Col>
                    <Grid.Col span={FIELD_COL}>{renderControl(def)}</Grid.Col>
                  </Grid>
                );
              })}
            </Stack>
          </Card>
        );
      })}

      <Text fz={11} c="dimmed">
        {t('settings.footerHint')}
      </Text>
    </Stack>
  );
}