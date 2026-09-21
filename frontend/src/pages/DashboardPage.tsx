import { useCallback, useEffect, useMemo, useState } from 'react';
import {
  ActionIcon,
  Badge,
  Box,
  Card,
  Divider,
  Group,
  Progress,
  SimpleGrid,
  Stack,
  Switch,
  Table,
  Text,
  Tooltip,
} from '@mantine/core';
import {
  IconActivity,
  IconArrowDown,
  IconArrowUp,
  IconCpu,
  IconDatabase,
  IconDeviceDesktopAnalytics,
  IconRefresh,
  IconServer,
} from '@tabler/icons-react';
import {
  Area,
  AreaChart,
  CartesianGrid,
  ResponsiveContainer,
  Tooltip as RechartsTooltip,
  XAxis,
  YAxis,
} from 'recharts';

import { ChartTooltip } from '@/components/charts/ChartTooltip';
import { ErrorAlert } from '@/components/common/Feedback';
import { InlineLoader } from '@/components/common/LoadingScreen';
import { MetricCard } from '@/components/common/MetricCard';
import { PageHeader } from '@/components/common/PageHeader';
import { useInterval } from '@/hooks/use-interval';
import { useTranslation } from '@/i18n';
import {
  formatBytes,
  formatPercent,
  formatSpeed,
  formatTime,
  formatUptime,
  loadLevel,
  messageOf,
} from '@/lib/format';
import { monitoringService } from '@/services';
import type { SystemStats } from '@/types/monitoring';

interface TrendPoint {
  time: string;
  cpu: number;
  memory: number;
  disk: number;
  sent: number;
  recv: number;
}

const POLL_INTERVAL_MS = 3000;
const MAX_POINTS = 60;

const axisTick = { fontSize: 11, fill: 'var(--mantine-color-dimmed)' } as const;

export default function DashboardPage() {
  const { t, locale } = useTranslation();
  const [stats, setStats] = useState<SystemStats | null>(null);
  const [trend, setTrend] = useState<TrendPoint[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [autoRefresh, setAutoRefresh] = useState(true);
  const [lastUpdate, setLastUpdate] = useState<Date | null>(null);

  const timeLabel = useCallback(
    (date: Date) => date.toLocaleTimeString(locale === 'zh' ? 'zh-CN' : 'en-US', { hour12: false }),
    [locale],
  );

  const appendPoint = useCallback(
    (snapshot: SystemStats, date: Date) => {
      setTrend((prev) => {
        const next: TrendPoint[] = [
          ...prev,
          {
            time: timeLabel(date),
            cpu: snapshot.cpu.usage_percent,
            memory: snapshot.memory.used_percent,
            disk: snapshot.disk.used_percent,
            sent: snapshot.network.speed_sent,
            recv: snapshot.network.speed_recv,
          },
        ];
        return next.slice(-MAX_POINTS);
      });
    },
    [timeLabel],
  );

  const fetchStats = useCallback(async () => {
    try {
      const response = await monitoringService.getSystemStats();
      if (response.success && response.data) {
        const now = new Date();
        setStats(response.data);
        setLastUpdate(now);
        appendPoint(response.data, now);
        setError(null);
      }
    } catch (err) {
      setError(messageOf(err, t('errors.serverError')));
    } finally {
      setLoading(false);
    }
  }, [appendPoint, t]);

  // 首屏：先补一段历史趋势，再进入实时轮询
  useEffect(() => {
    let cancelled = false;

    const bootstrap = async () => {
      try {
        const response = await monitoringService.getMonitoringChart(0.5);
        if (!cancelled && response.success && response.data && response.data.points.length > 0) {
          setTrend(
            response.data.points.slice(-MAX_POINTS).map((point) => ({
              time: timeLabel(new Date(point.timestamp * 1000)),
              cpu: point.cpu_percent,
              memory: point.memory_percent,
              disk: point.disk_percent,
              sent: point.network_speed_sent,
              recv: point.network_speed_recv,
            })),
          );
        }
      } catch {
        // 历史数据不可用时静默降级为实时曲线
      }
      if (!cancelled) {
        await fetchStats();
      }
    };

    void bootstrap();
    return () => {
      cancelled = true;
    };
    // 只在挂载时执行一次
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  useInterval(() => {
    void fetchStats();
  }, autoRefresh ? POLL_INTERVAL_MS : null);

  const uptimeText = useMemo(() => {
    if (!stats) return '-';
    return formatUptime(stats.host.uptime, {
      day: t('monitoring.days'),
      hour: t('monitoring.hours'),
      minute: t('monitoring.minutes'),
    });
  }, [stats, t]);

  const cpuLevel = stats ? loadLevel(stats.cpu.usage_percent) : 'ok';
  const memoryLevel = stats ? loadLevel(stats.memory.used_percent) : 'ok';
  const diskLevel = stats ? loadLevel(stats.disk.used_percent) : 'ok';

  const levelLabel = (level: 'ok' | 'warn' | 'critical') =>
    level === 'ok'
      ? t('monitoring.normal')
      : level === 'warn'
        ? t('monitoring.high')
        : t('monitoring.critical');

  const crumbs = useMemo(
    () => [{ label: t('breadcrumb.home'), to: '/dashboard' }, { label: t('breadcrumb.dashboard') }],
    [t],
  );

  if (loading && !stats) {
    return (
      <Stack gap="md">
        <PageHeader title={t('monitoring.title')} subtitle={t('monitoring.monitorServer')} crumbs={crumbs} />
        <Card>
          <InlineLoader label={t('common.loading')} />
        </Card>
      </Stack>
    );
  }

  return (
    <Stack gap="md">
      <PageHeader
        title={t('monitoring.title')}
        subtitle={t('monitoring.monitorServer')}
        crumbs={crumbs}
        actions={
          <Group gap="xs">
            {lastUpdate ? (
              <Badge variant="light" color="gray" size="lg" className="wm-mono">
                {t('monitoring.lastUpdate')} {formatTime(lastUpdate, locale === 'zh' ? 'zh-CN' : 'en-US')}
              </Badge>
            ) : null}
            <Tooltip label={t('monitoring.autoRefresh')}>
              <Switch
                checked={autoRefresh}
                onChange={(event) => setAutoRefresh(event.currentTarget.checked)}
                color="wg"
                size="md"
                label={t('monitoring.auto')}
                labelPosition="left"
                styles={{ label: { fontSize: 13 } }}
              />
            </Tooltip>
            <Tooltip label={t('monitoring.refresh')}>
              <ActionIcon
                variant="default"
                size="lg"
                radius="xs"
                loading={loading}
                onClick={() => void fetchStats()}
                aria-label={t('monitoring.refresh')}
              >
                <IconRefresh size={17} stroke={1.7} />
              </ActionIcon>
            </Tooltip>
          </Group>
        }
      />

      <ErrorAlert message={error} onClose={() => setError(null)} />

      {/* 资源概览 */}
      <SimpleGrid cols={{ base: 2, sm: 2, lg: 4 }} spacing={{ base: 'xs', sm: 'sm' }}>
        <MetricCard
          label={t('monitoring.cpuUsage')}
          value={stats ? formatPercent(stats.cpu.usage_percent) : '-'}
          hint={`${stats?.cpu.cores ?? 0} ${t('monitoring.cpuCores')} · ${levelLabel(cpuLevel)}`}
          icon={IconCpu}
          accent="wg"
          progress={{ value: stats?.cpu.usage_percent ?? 0, level: cpuLevel }}
          delay={60}
        />
        <MetricCard
          label={t('monitoring.memoryUsage')}
          value={stats ? formatPercent(stats.memory.used_percent) : '-'}
          hint={
            stats ? `${formatBytes(stats.memory.used)} / ${formatBytes(stats.memory.total)}` : '-'
          }
          icon={IconDeviceDesktopAnalytics}
          accent="teal"
          progress={{ value: stats?.memory.used_percent ?? 0, level: memoryLevel }}
          delay={120}
        />
        <MetricCard
          label={t('monitoring.diskUsage')}
          value={stats ? formatPercent(stats.disk.used_percent) : '-'}
          hint={stats ? `${formatBytes(stats.disk.used)} / ${formatBytes(stats.disk.total)}` : '-'}
          icon={IconDatabase}
          accent="blue"
          progress={{ value: stats?.disk.used_percent ?? 0, level: diskLevel }}
          delay={180}
        />
        <MetricCard
          label={t('monitoring.networkSpeed')}
          value={
            <Stack gap={2}>
              <Group gap={6} wrap="nowrap">
                <IconArrowUp size={16} color="var(--mantine-color-wg-6)" />
                <Text fz={20} fw={700} className="wm-mono" c="wg.6">
                  {formatSpeed(stats?.network.speed_sent ?? 0)}
                </Text>
              </Group>
              <Group gap={6} wrap="nowrap">
                <IconArrowDown size={16} color="var(--mantine-color-teal-6)" />
                <Text fz={20} fw={700} className="wm-mono" c="teal.6">
                  {formatSpeed(stats?.network.speed_recv ?? 0)}
                </Text>
              </Group>
            </Stack>
          }
          hint={
            stats
              ? `${t('monitoring.bytesSent')} ${formatBytes(stats.network.bytes_sent)} · ${t('monitoring.bytesRecv')} ${formatBytes(stats.network.bytes_recv)}`
              : '-'
          }
          icon={IconActivity}
          accent="green"
          delay={240}
        />
      </SimpleGrid>

      {/* 趋势图表 */}
      <SimpleGrid cols={{ base: 1, lg: 2 }} spacing="sm">
        <Card className="wm-rise" style={{ '--wm-delay': '300ms' } as React.CSSProperties}>
          <Group justify="space-between" mb="xs">
            <Box>
              <Group gap={8}>
                <IconActivity size={17} color="var(--mantine-color-wg-6)" />
                <Text fw={650}>{t('monitoring.resourceUsage')}</Text>
              </Group>
              <Text size="xs" c="dimmed" mt={3}>
                {t('monitoring.cpuAndMemory')}
              </Text>
            </Box>
            <Badge variant="light" color="wg" size="sm">
              {t('monitoring.realtime')}
            </Badge>
          </Group>
          <Divider mb="sm" variant="dashed" />
          <Box h={240}>
            <ResponsiveContainer width="100%" height="100%">
              <AreaChart data={trend} margin={{ top: 6, right: 8, left: -18, bottom: 0 }}>
                <defs>
                  <linearGradient id="wmCpu" x1="0" y1="0" x2="0" y2="1">
                    <stop offset="5%" stopColor="var(--mantine-color-wg-6)" stopOpacity={0.42} />
                    <stop offset="95%" stopColor="var(--mantine-color-wg-6)" stopOpacity={0.02} />
                  </linearGradient>
                  <linearGradient id="wmMemory" x1="0" y1="0" x2="0" y2="1">
                    <stop offset="5%" stopColor="var(--mantine-color-teal-6)" stopOpacity={0.42} />
                    <stop offset="95%" stopColor="var(--mantine-color-teal-6)" stopOpacity={0.02} />
                  </linearGradient>
                </defs>
                <CartesianGrid strokeDasharray="3 3" stroke="var(--mantine-color-default-border)" vertical={false} />
                <XAxis dataKey="time" tick={axisTick} tickLine={false} axisLine={false} minTickGap={28} />
                <YAxis domain={[0, 100]} tick={axisTick} tickLine={false} axisLine={false} width={38} />
                <RechartsTooltip
                  content={<ChartTooltip formatter={(value) => formatPercent(value)} />}
                />
                <Area
                  type="monotone"
                  dataKey="cpu"
                  name={t('monitoring.cpuUsage')}
                  stroke="var(--mantine-color-wg-6)"
                  strokeWidth={2.2}
                  fill="url(#wmCpu)"
                  dot={false}
                  isAnimationActive={false}
                />
                <Area
                  type="monotone"
                  dataKey="memory"
                  name={t('monitoring.memoryUsage')}
                  stroke="var(--mantine-color-teal-6)"
                  strokeWidth={2.2}
                  fill="url(#wmMemory)"
                  dot={false}
                  isAnimationActive={false}
                />
              </AreaChart>
            </ResponsiveContainer>
          </Box>
        </Card>

        <Card className="wm-rise" style={{ '--wm-delay': '340ms' } as React.CSSProperties}>
          <Group justify="space-between" mb="xs">
            <Box>
              <Group gap={8}>
                <IconActivity size={17} color="var(--mantine-color-blue-6)" />
                <Text fw={650}>{t('monitoring.networkTraffic')}</Text>
              </Group>
              <Text size="xs" c="dimmed" mt={3}>
                {t('monitoring.uploadAndDownload')}
              </Text>
            </Box>
            <Badge variant="light" color="blue" size="sm">
              {t('monitoring.realtime')}
            </Badge>
          </Group>
          <Divider mb="sm" variant="dashed" />
          <Box h={240}>
            <ResponsiveContainer width="100%" height="100%">
              <AreaChart data={trend} margin={{ top: 6, right: 8, left: -18, bottom: 0 }}>
                <defs>
                  <linearGradient id="wmSent" x1="0" y1="0" x2="0" y2="1">
                    <stop offset="5%" stopColor="var(--mantine-color-wg-6)" stopOpacity={0.4} />
                    <stop offset="95%" stopColor="var(--mantine-color-wg-6)" stopOpacity={0.02} />
                  </linearGradient>
                  <linearGradient id="wmRecv" x1="0" y1="0" x2="0" y2="1">
                    <stop offset="5%" stopColor="var(--mantine-color-blue-6)" stopOpacity={0.4} />
                    <stop offset="95%" stopColor="var(--mantine-color-blue-6)" stopOpacity={0.02} />
                  </linearGradient>
                </defs>
                <CartesianGrid strokeDasharray="3 3" stroke="var(--mantine-color-default-border)" vertical={false} />
                <XAxis dataKey="time" tick={axisTick} tickLine={false} axisLine={false} minTickGap={28} />
                <YAxis
                  tick={axisTick}
                  tickLine={false}
                  axisLine={false}
                  width={54}
                  tickFormatter={(value: number) => formatSpeed(value)}
                />
                <RechartsTooltip content={<ChartTooltip formatter={(value) => formatSpeed(value)} />} />
                <Area
                  type="monotone"
                  dataKey="sent"
                  name={t('monitoring.uploadSpeed')}
                  stroke="var(--mantine-color-wg-6)"
                  strokeWidth={2.2}
                  fill="url(#wmSent)"
                  dot={false}
                  isAnimationActive={false}
                />
                <Area
                  type="monotone"
                  dataKey="recv"
                  name={t('monitoring.downloadSpeed')}
                  stroke="var(--mantine-color-blue-6)"
                  strokeWidth={2.2}
                  fill="url(#wmRecv)"
                  dot={false}
                  isAnimationActive={false}
                />
              </AreaChart>
            </ResponsiveContainer>
          </Box>
        </Card>
      </SimpleGrid>

      {/* 主机信息与 CPU 核心明细 */}
      <SimpleGrid cols={{ base: 1, lg: 2 }} spacing="sm">
        <Card className="wm-rise" style={{ '--wm-delay': '380ms' } as React.CSSProperties}>
          <Group gap={8} mb="sm">
            <IconServer size={17} color="var(--mantine-color-teal-6)" />
            <Text fw={650}>{t('monitoring.systemInfo')}</Text>
          </Group>
          <Divider mb="sm" variant="dashed" />
          <Table variant="vertical" verticalSpacing={6}>
            <Table.Tbody>
              <Table.Tr>
                <Table.Th w={170}>
                  <Text size="xs" c="dimmed">
                    {t('monitoring.hostname')}
                  </Text>
                </Table.Th>
                <Table.Td>
                  <Text size="sm" fw={600} className="wm-mono">
                    {stats?.host.hostname || '-'}
                  </Text>
                </Table.Td>
              </Table.Tr>
              <Table.Tr>
                <Table.Th>
                  <Text size="xs" c="dimmed">
                    {t('monitoring.os')}
                  </Text>
                </Table.Th>
                <Table.Td>
                  <Text size="sm" fw={600}>
                    {stats?.host.os || '-'}
                  </Text>
                </Table.Td>
              </Table.Tr>
              <Table.Tr>
                <Table.Th>
                  <Text size="xs" c="dimmed">
                    {t('monitoring.platform')}
                  </Text>
                </Table.Th>
                <Table.Td>
                  <Text size="sm" fw={600}>
                    {stats ? `${stats.host.platform} ${stats.host.platform_version}` : '-'}
                  </Text>
                </Table.Td>
              </Table.Tr>
              <Table.Tr>
                <Table.Th>
                  <Text size="xs" c="dimmed">
                    {t('monitoring.uptime')}
                  </Text>
                </Table.Th>
                <Table.Td>
                  <Text size="sm" fw={600}>
                    {uptimeText}
                  </Text>
                </Table.Td>
              </Table.Tr>
              <Table.Tr>
                <Table.Th>
                  <Text size="xs" c="dimmed">
                    {t('monitoring.downloadTotal')}
                  </Text>
                </Table.Th>
                <Table.Td>
                  <Text size="sm" fw={600} className="wm-mono">
                    {formatBytes(stats?.network.bytes_recv ?? 0)}
                  </Text>
                </Table.Td>
              </Table.Tr>
              <Table.Tr>
                <Table.Th>
                  <Text size="xs" c="dimmed">
                    {t('monitoring.uploadTotal')}
                  </Text>
                </Table.Th>
                <Table.Td>
                  <Text size="sm" fw={600} className="wm-mono">
                    {formatBytes(stats?.network.bytes_sent ?? 0)}
                  </Text>
                </Table.Td>
              </Table.Tr>
            </Table.Tbody>
          </Table>
        </Card>

        <Card className="wm-rise" style={{ '--wm-delay': '420ms' } as React.CSSProperties}>
          <Group justify="space-between" mb="sm">
            <Group gap={8}>
              <IconCpu size={17} color="var(--mantine-color-orange-6)" />
              <Text fw={650}>{t('monitoring.cpuCoresDetail')}</Text>
            </Group>
            <Text size="xs" c="dimmed">
              {stats?.cpu.cores ?? 0} {t('monitoring.coresUsage')}
            </Text>
          </Group>
          <Divider mb="sm" variant="dashed" />
          {stats && stats.cpu.per_core.length > 0 ? (
            <SimpleGrid cols={{ base: 1, sm: 2 }} spacing="sm">
              {stats.cpu.per_core.map((usage, index) => {
                const level = loadLevel(usage);
                return (
                  <Stack key={index} gap={5}>
                    <Group justify="space-between">
                      <Text size="xs" c="dimmed">
                        {t('monitoring.core')} {index + 1}
                      </Text>
                      <Text
                        size="xs"
                        fw={650}
                        className="wm-mono"
                        c={level === 'ok' ? 'teal.6' : level === 'warn' ? 'yellow.7' : 'red.6'}
                      >
                        {formatPercent(usage)}
                      </Text>
                    </Group>
                    <Progress
                      value={Math.min(100, Math.max(0, usage))}
                      color={level === 'ok' ? 'teal' : level === 'warn' ? 'yellow' : 'red'}
                      size={5}
                      radius="xs"
                    />
                  </Stack>
                );
              })}
            </SimpleGrid>
          ) : (
            <InlineLoader label={t('common.loading')} />
          )}
        </Card>
      </SimpleGrid>
    </Stack>
  );
}
