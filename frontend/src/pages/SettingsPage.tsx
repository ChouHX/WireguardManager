import { useCallback, useEffect, useMemo, useState } from 'react';
import {
  Box,
  Button,
  Card,
  Group,
  NumberInput,
  Select,
  Stack,
  Switch,
  Text,
  TextInput,
} from '@mantine/core';
import { notifications } from '@mantine/notifications';
import { IconDeviceFloppy, IconRefresh } from '@tabler/icons-react';

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
  'network.mtu': 'mtu',
  'monitoring.interval_seconds': 'monitoringInterval',
  'monitoring.retention_hours': 'monitoringRetention',
  'liveness.probe_port': 'livenessProbePort',
  'jwt.expire_hours': 'jwtExpireHours',
  'wireguard.default_preshared_key': 'defaultPresharedKey',
};

/** 标签列固定宽度，保证左右两列各自对齐 */
const LABEL_WIDTH = 190;

/** 单行控件区的基准高度（与输入框一致）：标签按此高度居中，
 *  使带说明文字的字段也不会让标签与输入框错位。 */
const CONTROL_HEIGHT = 36;

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

  /** 统一渲染控件本体（不含标签）。
   *
   *  说明文字刻意不用 Mantine 的 description：它会渲染在输入框【上方】，
   *  把输入框整体下推，导致标签与输入框视觉错位。改为放在控件下方，
   *  这样每行的输入框都紧贴行首，与标签中线自然对齐。 */
  const renderControl = (def: SettingDef): { control: React.ReactNode; hint?: string } => {
    const value = draft[def.key] ?? '';

    if (def.type === 'bool') {
      return {
        control: (
          <Switch
            checked={value === 'true'}
            onChange={(event) => set(def.key, String(event.currentTarget.checked))}
            color="wg"
            size="md"
            style={{ height: CONTROL_HEIGHT }}
            label={value === 'true' ? t('settings.on') : t('settings.off')}
            labelPosition="right"
            styles={{ label: { fontSize: 12, color: 'var(--mantine-color-dimmed)' } }}
          />
        ),
      };
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

      return {
        control: (
          <Select
            data={options}
            value={value || null}
            onChange={(next) => set(def.key, next ?? '')}
            searchable
            clearable
            nothingFoundMessage={t('common.noData')}
          />
        ),
        hint: t('wireguard.ifaceServerHint'),
      };
    }

    if (def.type === 'int') {
      return {
        control: (
          <NumberInput
            value={Number(value) || 0}
            onChange={(next) => set(def.key, String(next ?? 0))}
            min={def.min}
            max={def.max}
          />
        ),
      };
    }

    return {
      control: (
        <TextInput
          value={value}
          onChange={(event) => set(def.key, event.currentTarget.value)}
          className="wm-mono"
          placeholder={
            def.key === 'network.client_allowed_ips' ? t('settings.allowedIpsPlaceholder') : undefined
          }
        />
      ),
    };
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
            padding="md"
            /* 极简：不用描边，靠 --wm-surface 与页面背景的色差区分分组 */
            withBorder={false}
            bg="var(--wm-surface)"
          >
            <Text fw={650} fz={13} mb={group === 'liveness' ? 2 : 'sm'}>
              {t(`settings.group.${group}`)}
            </Text>

            {/* 在线判定只有一项：补一行说明，讲清其余参数为何无需配置 */}
            {group === 'liveness' ? (
              <Text fz={11.5} c="dimmed" mb="sm">
                {t('settings.livenessNote')}
              </Text>
            ) : null}

            {/* 用 flex 而非 Grid：Group 默认 align="center"，垂直居中更可靠；
                行与行之间只靠间距分隔，不加任何分隔线 */}
            <Stack gap={10}>
              {defs.map((def) => {
                const labelKey = LABEL_KEY[def.key];
                const { control, hint } = renderControl(def);
                return (
                  <Group key={def.key} wrap="nowrap" align="flex-start" gap="md">
                    {/* 标签块按控件基准高度居中：带说明文字的字段，说明只在
                        输入框下方延伸，不会把标签顶偏 */}
                    <Box
                      w={LABEL_WIDTH}
                      style={{
                        display: 'flex',
                        flexDirection: 'column',
                        justifyContent: 'center',
                        minHeight: CONTROL_HEIGHT,
                      }}
                    >
                      <Text fz={13} fw={550} lh={1.25}>
                        {labelKey ? t(`settings.${labelKey}`) : def.key}
                      </Text>
                      <Text fz={10} c="dimmed" className="wm-mono" lh={1.25}>
                        {def.key}
                      </Text>
                    </Box>
                    <Box style={{ flex: 1, minWidth: 0 }}>
                      {control}
                      {hint ? (
                        <Text fz={10.5} c="dimmed" mt={3}>
                          {hint}
                        </Text>
                      ) : null}
                    </Box>
                  </Group>
                );
              })}
            </Stack>
          </Card>
        );
      })}
    </Stack>
  );
}