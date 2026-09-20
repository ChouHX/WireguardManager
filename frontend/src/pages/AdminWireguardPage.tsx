import { useCallback, useEffect, useMemo, useState } from 'react';
import {
  ActionIcon,
  Alert,
  Badge,
  Box,
  Button,
  Card,
  Divider,
  Drawer,
  Group,
  Menu,
  Modal,
  NumberInput,
  SimpleGrid,
  Stack,
  Switch,
  Table,
  Text,
  Tooltip,
} from '@mantine/core';
import { useDisclosure } from '@mantine/hooks';
import { notifications } from '@mantine/notifications';
import {
  IconActivity,
  IconAlertTriangle,
  IconArrowDown,
  IconArrowUp,
  IconDeviceLaptop,
  IconDotsVertical,
  IconEye,
  IconGauge,
  IconRefresh,
  IconServer,
  IconTrash,
  IconUsers,
} from '@tabler/icons-react';

import { EmptyState, ErrorAlert } from '@/components/common/Feedback';
import { InlineLoader } from '@/components/common/LoadingScreen';
import { MetricCard } from '@/components/common/MetricCard';
import { PageHeader } from '@/components/common/PageHeader';
import { useInterval } from '@/hooks/use-interval';
import { useTranslation } from '@/i18n';
import { formatBytes, formatRelativeTime, formatTime, messageOf, shortKey } from '@/lib/format';
import { wireguardService } from '@/services';
import type { AdminUserTraffic, UserTrafficStats } from '@/types/wireguard';

/** 管理员页数据量大，5 秒轮询一次 */
const POLL_INTERVAL_MS = 5000;

const rateText = (rate: number, unlimited: string) => (rate > 0 ? `${rate} Mbps` : unlimited);

export default function AdminWireguardPage() {
  const { t, locale } = useTranslation();

  const [rows, setRows] = useState<AdminUserTraffic[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [lastUpdated, setLastUpdated] = useState<Date | null>(null);
  const [autoRefresh, setAutoRefresh] = useState(true);
  const [submitting, setSubmitting] = useState(false);
  const [togglingId, setTogglingId] = useState<number | null>(null);

  const [detailOpened, detailDrawer] = useDisclosure(false);
  const [rateOpened, rateModal] = useDisclosure(false);
  const [deleteOpened, deleteModal] = useDisclosure(false);

  const [selected, setSelected] = useState<AdminUserTraffic | null>(null);
  const [detail, setDetail] = useState<UserTrafficStats | null>(null);
  const [detailLoading, setDetailLoading] = useState(false);
  const [detailError, setDetailError] = useState<string | null>(null);
  const [rateValues, setRateValues] = useState<{ download: number | string; upload: number | string }>(
    { download: 0, upload: 0 },
  );

  const localeTag = locale === 'zh' ? 'zh-CN' : 'en-US';

  const loadTraffic = useCallback(async () => {
    const response = await wireguardService.getAdminTraffic();
    if (response.success && response.data) {
      setRows(response.data);
      setLastUpdated(new Date());
    }
  }, []);

  const bootstrap = useCallback(async () => {
    try {
      setError(null);
      await loadTraffic();
    } catch (err) {
      setError(messageOf(err, t('errors.somethingWrong')));
    } finally {
      setLoading(false);
    }
  }, [loadTraffic, t]);

  useEffect(() => {
    void bootstrap();
    // 只在挂载时拉取首屏数据，其后交由轮询
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  useInterval(
    () => {
      void loadTraffic().catch((err) => setError(messageOf(err, t('errors.networkError'))));
    },
    autoRefresh ? POLL_INTERVAL_MS : null,
  );

  const summary = useMemo(
    () =>
      rows.reduce(
        (acc, row) => ({
          active: acc.active + (row.enabled ? 1 : 0),
          peers: acc.peers + row.peer_count,
          rx: acc.rx + row.total_rx,
          tx: acc.tx + row.total_tx,
        }),
        { active: 0, peers: 0, rx: 0, tx: 0 },
      ),
    [rows],
  );

  const crumbs = useMemo(
    () => [
      { label: t('breadcrumb.home'), to: '/dashboard' },
      { label: t('breadcrumb.adminWireguard') },
    ],
    [t],
  );

  // 详情数据防御：后端在边界情况下可能返回 null 字段（未分配网络、采集失败、
  // peer 列表为空），这里统一归一化，避免渲染期直接解引用导致整页崩溃。
  const detailInfo = detail?.server_info ?? null;
  const detailStats = detail?.server_stats ?? null;
  const detailPeers = detail?.server_stats?.peers ?? [];
  const detailIncomplete = Boolean(detail) && (!detailInfo || !detailStats);

  const patchRow = (serverId: number, patch: Partial<AdminUserTraffic>) => {
    setRows((prev) =>
      prev.map((row) => (row.server_id === serverId ? { ...row, ...patch } : row)),
    );
  };

  const openDetails = async (row: AdminUserTraffic) => {
    setSelected(row);
    setDetail(null);
    setDetailError(null);
    setDetailLoading(true);
    detailDrawer.open();
    try {
      const response = await wireguardService.getUserTraffic(row.user_id);
      if (response.success && response.data) {
        setDetail(response.data);
      } else {
        setDetailError(response.message || t('errors.somethingWrong'));
      }
    } catch (err) {
      setDetailError(messageOf(err, t('errors.networkError')));
    } finally {
      setDetailLoading(false);
    }
  };

  const openRateLimit = (row: AdminUserTraffic) => {
    setSelected(row);
    setRateValues({ download: row.download_rate, upload: row.upload_rate });
    rateModal.open();
  };

  const openDelete = (row: AdminUserTraffic) => {
    setSelected(row);
    deleteModal.open();
  };

  const handleToggle = async (row: AdminUserTraffic, next: boolean) => {
    setTogglingId(row.server_id);
    setError(null);
    try {
      const response = await wireguardService.toggleServer(row.server_id, next);
      if (response.success) {
        patchRow(row.server_id, { enabled: next });
        notifications.show({
          color: 'teal',
          message: next ? t('wireguard.enableServerSuccess') : t('wireguard.disableServerSuccess'),
        });
      } else {
        setError(response.message || t(next ? 'wireguard.enableServerFailed' : 'wireguard.disableServerFailed'));
      }
    } catch (err) {
      setError(
        messageOf(
          err,
          t(next ? 'wireguard.enableServerFailed' : 'wireguard.disableServerFailed'),
        ),
      );
    } finally {
      setTogglingId(null);
    }
  };

  const handleRateLimit = async () => {
    if (!selected) return;

    const download = Number(rateValues.download) || 0;
    const upload = Number(rateValues.upload) || 0;

    setSubmitting(true);
    setError(null);
    try {
      const response = await wireguardService.setRateLimit(selected.server_id, download, upload);
      if (response.success) {
        patchRow(selected.server_id, { download_rate: download, upload_rate: upload });
        notifications.show({ color: 'teal', message: t('wireguard.rateLimitSuccess') });
        rateModal.close();
        setSelected(null);
      } else {
        setError(response.message || t('wireguard.rateLimitFailed'));
      }
    } catch (err) {
      setError(messageOf(err, t('wireguard.rateLimitFailed')));
    } finally {
      setSubmitting(false);
    }
  };

  const handleDelete = async () => {
    if (!selected) return;

    setSubmitting(true);
    setError(null);
    try {
      const response = await wireguardService.deleteServer(selected.server_id);
      if (response.success) {
        notifications.show({ color: 'teal', message: t('wireguard.deleteServerSuccess') });
        deleteModal.close();
        setSelected(null);
        await loadTraffic();
      } else {
        setError(response.message || t('wireguard.deleteServerFailed'));
      }
    } catch (err) {
      setError(messageOf(err, t('wireguard.deleteServerFailed')));
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <Stack gap="md">
      <PageHeader
        title={t('wireguard.adminTitle')}
        subtitle={t('wireguard.monitorAllUsers')}
        crumbs={crumbs}
        actions={
          <Group gap="xs">
            {lastUpdated ? (
              <Badge variant="light" color="gray" size="lg" className="wm-mono">
                <Group gap={5} wrap="nowrap">
                  <IconActivity size={13} />
                  {formatTime(lastUpdated, localeTag)}
                </Group>
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
                onClick={() => void bootstrap()}
                aria-label={t('monitoring.refresh')}
              >
                <IconRefresh size={17} stroke={1.7} />
              </ActionIcon>
            </Tooltip>
          </Group>
        }
      />

      <ErrorAlert message={error} onClose={() => setError(null)} />

      {/* 全局概览 */}
      <SimpleGrid cols={{ base: 1, sm: 2, lg: 4 }} spacing="sm">
        <MetricCard
          label={t('wireguard.totalUsers')}
          value={rows.length}
          hint={t('wireguard.monitorAllUsers')}
          icon={IconUsers}
          accent="wg"
          delay={60}
        />
        <MetricCard
          label={t('wireguard.activeUsers')}
          value={summary.active}
          hint={`${summary.active} / ${rows.length} ${t('wireguard.usersCount')}`}
          icon={IconServer}
          accent="green"
          delay={120}
        />
        <MetricCard
          label={t('wireguard.allDevices')}
          value={summary.peers}
          hint={t('wireguard.connectedDevices')}
          icon={IconDeviceLaptop}
          accent="blue"
          delay={180}
        />
        <MetricCard
          label={t('wireguard.allUsersReceived')}
          value={
            <Stack gap={2}>
              <Group gap={6} wrap="nowrap">
                <IconArrowDown size={16} color="var(--mantine-color-teal-6)" />
                <Text fz={20} fw={700} className="wm-mono" c="teal.6">
                  {formatBytes(summary.rx)}
                </Text>
              </Group>
              <Group gap={6} wrap="nowrap">
                <IconArrowUp size={16} color="var(--mantine-color-blue-6)" />
                <Text fz={20} fw={700} className="wm-mono" c="blue.6">
                  {formatBytes(summary.tx)}
                </Text>
              </Group>
            </Stack>
          }
          hint={t('wireguard.allUsersTransmitted')}
          icon={IconActivity}
          accent="teal"
          delay={240}
        />
      </SimpleGrid>

      {/* 用户流量总表 */}
      <Card className="wm-rise" style={{ '--wm-delay': '200ms' } as React.CSSProperties}>
        <Group justify="space-between" mb="sm">
          <Box>
            <Text fw={650}>{t('wireguard.usersTraffic')}</Text>
            <Text size="xs" c="dimmed" mt={3}>
              {t('wireguard.monitorAllUsers')}
            </Text>
          </Box>
          <Badge variant="light" color="gray" size="sm">
            {rows.length} {t('wireguard.usersCount')}
          </Badge>
        </Group>
        <Divider mb="sm" variant="dashed" />

        {loading ? (
          <InlineLoader label={t('common.loading')} />
        ) : rows.length === 0 ? (
          <EmptyState
            title={t('wireguard.noUsersFound')}
            description={t('wireguard.monitorAllUsers')}
            icon={<IconUsers size={22} />}
          />
        ) : (
          <div className="wm-table-scroll">
            <Table striped highlightOnHover verticalSpacing="sm">
              <Table.Thead>
                <Table.Tr>
                  <Table.Th>{t('wireguard.user')}</Table.Th>
                  <Table.Th>{t('wireguard.address')}</Table.Th>
                  <Table.Th>{t('wireguard.namespace')}</Table.Th>
                  <Table.Th>{t('wireguard.peers')}</Table.Th>
                  <Table.Th>{t('wireguard.transfer')}</Table.Th>
                  <Table.Th>{t('wireguard.rateLimit')}</Table.Th>
                  <Table.Th>{t('wireguard.status')}</Table.Th>
                  <Table.Th w={70} ta="right">
                    {t('common.actions')}
                  </Table.Th>
                </Table.Tr>
              </Table.Thead>
              <Table.Tbody>
                {rows.map((row) => (
                  <Table.Tr key={row.server_id}>
                    <Table.Td>
                      <Stack gap={2}>
                        <Text size="sm" fw={600}>
                          {row.email}
                        </Text>
                        <Text size="xs" c="dimmed" className="wm-mono">
                          {row.user_uid}
                        </Text>
                        <Text size="xs" c="dimmed" className="wm-mono">
                          #{row.user_id}
                        </Text>
                      </Stack>
                    </Table.Td>
                    <Table.Td>
                      <Group gap={6} wrap="nowrap">
                        <Text size="sm" className="wm-mono">
                          {row.wg_address}
                        </Text>
                        <Badge variant="light" color="wg" size="sm" className="wm-mono">
                          {row.wg_port}
                        </Badge>
                      </Group>
                    </Table.Td>
                    <Table.Td>
                      <Text size="xs" c="dimmed" className="wm-mono">
                        {row.namespace}
                      </Text>
                    </Table.Td>
                    <Table.Td>
                      <Badge variant="light" color="gray" className="wm-mono">
                        {row.peer_count}
                      </Badge>
                    </Table.Td>
                    <Table.Td>
                      <Stack gap={2}>
                        <Group gap={5} wrap="nowrap">
                          <IconArrowDown size={13} color="var(--mantine-color-teal-6)" />
                          <Text size="xs" className="wm-mono">
                            {formatBytes(row.total_rx)}
                          </Text>
                        </Group>
                        <Group gap={5} wrap="nowrap">
                          <IconArrowUp size={13} color="var(--mantine-color-blue-6)" />
                          <Text size="xs" className="wm-mono">
                            {formatBytes(row.total_tx)}
                          </Text>
                        </Group>
                      </Stack>
                    </Table.Td>
                    <Table.Td>
                      <Stack gap={2}>
                        <Group gap={5} wrap="nowrap">
                          <IconArrowDown size={13} color="var(--mantine-color-teal-6)" />
                          <Text
                            size="xs"
                            className="wm-mono"
                            c={row.download_rate > 0 ? undefined : 'dimmed'}
                          >
                            {rateText(row.download_rate, t('wireguard.unlimited'))}
                          </Text>
                        </Group>
                        <Group gap={5} wrap="nowrap">
                          <IconArrowUp size={13} color="var(--mantine-color-blue-6)" />
                          <Text
                            size="xs"
                            className="wm-mono"
                            c={row.upload_rate > 0 ? undefined : 'dimmed'}
                          >
                            {rateText(row.upload_rate, t('wireguard.unlimited'))}
                          </Text>
                        </Group>
                      </Stack>
                    </Table.Td>
                    <Table.Td>
                      <Group gap={8} wrap="nowrap">
                        <Switch
                          checked={row.enabled}
                          onChange={(event) => void handleToggle(row, event.currentTarget.checked)}
                          color="wg"
                          size="sm"
                          disabled={togglingId === row.server_id}
                          aria-label={t('wireguard.status')}
                        />
                        <Badge
                          variant="light"
                          color={row.enabled ? 'green' : 'yellow'}
                          size="sm"
                        >
                          {row.enabled ? t('wireguard.enabled') : t('wireguard.disabled')}
                        </Badge>
                      </Group>
                    </Table.Td>
                    <Table.Td ta="right">
                      <Menu shadow="md" width={210} position="bottom-end" radius="xs" withinPortal>
                        <Menu.Target>
                          <ActionIcon variant="subtle" color="gray" aria-label={t('common.actions')}>
                            <IconDotsVertical size={17} />
                          </ActionIcon>
                        </Menu.Target>
                        <Menu.Dropdown>
                          <Menu.Item
                            leftSection={<IconEye size={15} />}
                            onClick={() => void openDetails(row)}
                          >
                            {t('wireguard.viewDetails')}
                          </Menu.Item>
                          <Menu.Item
                            leftSection={<IconGauge size={15} />}
                            onClick={() => openRateLimit(row)}
                          >
                            {t('wireguard.setRateLimit')}
                          </Menu.Item>
                          <Menu.Divider />
                          <Menu.Item
                            color="red"
                            leftSection={<IconTrash size={15} />}
                            onClick={() => openDelete(row)}
                          >
                            {t('wireguard.deleteServer')}
                          </Menu.Item>
                        </Menu.Dropdown>
                      </Menu>
                    </Table.Td>
                  </Table.Tr>
                ))}
              </Table.Tbody>
            </Table>
          </div>
        )}
      </Card>

      {/* 用户详情 */}
      <Drawer
        opened={detailOpened}
        onClose={detailDrawer.close}
        position="right"
        size="xl"
        title={
          <Group gap={8}>
            <IconServer size={17} color="var(--mantine-color-wg-6)" />
            <Text fw={650}>{t('wireguard.viewDetails')}</Text>
          </Group>
        }
      >
        {detailLoading ? (
          <InlineLoader label={t('common.loading')} />
        ) : detailError ? (
          <ErrorAlert message={detailError} />
        ) : detail ? (
          <Stack gap="md">
            {detailIncomplete ? (
              <Alert
                color="yellow"
                variant="light"
                radius="xs"
                icon={<IconAlertTriangle size={16} />}
              >
                {t('wireguard.partialData')}
              </Alert>
            ) : null}
            <Box>
              <Group justify="space-between" mb="sm">
                <Box>
                  <Text fw={650} size="sm">
                    {detail.email}
                  </Text>
                  <Text size="xs" c="dimmed" mt={3}>
                    {t('wireguard.serverInfo')}
                  </Text>
                </Box>
                <Badge variant="light" color="gray" size="sm" className="wm-mono">
                  {detail.user_uid}
                </Badge>
              </Group>
              <Divider mb="sm" variant="dashed" />
              <Table variant="vertical" verticalSpacing={6}>
                <Table.Tbody>
                  <Table.Tr>
                    <Table.Th w={150}>
                      <Text size="xs" c="dimmed">
                        {t('wireguard.namespace')}
                      </Text>
                    </Table.Th>
                    <Table.Td>
                      <Text size="sm" fw={600} className="wm-mono">
                        {detailInfo?.namespace}
                      </Text>
                    </Table.Td>
                  </Table.Tr>
                  <Table.Tr>
                    <Table.Th>
                      <Text size="xs" c="dimmed">
                        {t('wireguard.interface')}
                      </Text>
                    </Table.Th>
                    <Table.Td>
                      <Text size="sm" fw={600} className="wm-mono">
                        {detailInfo?.wg_interface}
                      </Text>
                    </Table.Td>
                  </Table.Tr>
                  <Table.Tr>
                    <Table.Th>
                      <Text size="xs" c="dimmed">
                        {t('wireguard.port')}
                      </Text>
                    </Table.Th>
                    <Table.Td>
                      <Text size="sm" fw={600} className="wm-mono">
                        {detailInfo?.wg_port}
                      </Text>
                    </Table.Td>
                  </Table.Tr>
                  <Table.Tr>
                    <Table.Th>
                      <Text size="xs" c="dimmed">
                        {t('wireguard.address')}
                      </Text>
                    </Table.Th>
                    <Table.Td>
                      <Text size="sm" fw={600} className="wm-mono">
                        {detailInfo?.wg_address}
                      </Text>
                    </Table.Td>
                  </Table.Tr>
                </Table.Tbody>
              </Table>
            </Box>

            <Box>
              <Group gap={8} mb="sm">
                <IconActivity size={17} color="var(--mantine-color-teal-6)" />
                <Text fw={650} size="sm">
                  {t('wireguard.serverStats')}
                </Text>
              </Group>
              <Divider mb="sm" variant="dashed" />
              <SimpleGrid cols={{ base: 1, sm: 3 }} spacing="sm">
                <Stack gap={2}>
                  <Text size="xs" c="dimmed">
                    {t('wireguard.peers')}
                  </Text>
                  <Text size="lg" fw={700} className="wm-mono">
                    {detailStats?.peer_count ?? 0}
                  </Text>
                </Stack>
                <Stack gap={2}>
                  <Text size="xs" c="dimmed">
                    {t('wireguard.totalDownload')}
                  </Text>
                  <Text size="lg" fw={700} className="wm-mono" c="teal.6">
                    {formatBytes(detailStats?.total_rx ?? 0)}
                  </Text>
                </Stack>
                <Stack gap={2}>
                  <Text size="xs" c="dimmed">
                    {t('wireguard.totalUpload')}
                  </Text>
                  <Text size="lg" fw={700} className="wm-mono" c="blue.6">
                    {formatBytes(detailStats?.total_tx ?? 0)}
                  </Text>
                </Stack>
              </SimpleGrid>
            </Box>

            <Box>
              <Group gap={8} mb="sm">
                <IconDeviceLaptop size={17} color="var(--mantine-color-blue-6)" />
                <Text fw={650} size="sm">
                  {t('wireguard.peerDetails')}
                </Text>
                <Badge variant="light" color="gray" size="sm">
                  {detailPeers.length} {t('wireguard.peers')}
                </Badge>
              </Group>
              <Divider mb="sm" variant="dashed" />
              {detailPeers.length === 0 ? (
                <EmptyState title={t('wireguard.noPeers')} icon={<IconDeviceLaptop size={22} />} />
              ) : (
                <div className="wm-table-scroll">
                  <Table striped highlightOnHover verticalSpacing="sm">
                    <Table.Thead>
                      <Table.Tr>
                        <Table.Th>{t('wireguard.publicKey')}</Table.Th>
                        <Table.Th>{t('wireguard.allowedIPs')}</Table.Th>
                        <Table.Th>{t('wireguard.lastHandshake')}</Table.Th>
                        <Table.Th>{t('wireguard.transfer')}</Table.Th>
                      </Table.Tr>
                    </Table.Thead>
                    <Table.Tbody>
                      {detailPeers.map((peer) => (
                        <Table.Tr key={peer.public_key}>
                          <Table.Td>
                            <Text size="xs" className="wm-mono">
                              {shortKey(peer.public_key, 24)}
                            </Text>
                          </Table.Td>
                          <Table.Td>
                            <Text size="xs" c="dimmed" className="wm-mono">
                              {peer.allowed_ips || t('wireguard.noAllowedIPs')}
                            </Text>
                          </Table.Td>
                          <Table.Td>
                            <Text size="xs">
                              {formatRelativeTime(peer.latest_handshake, {
                                never: t('wireguard.never'),
                                justNow: t('wireguard.justNow'),
                                minutes: t('wireguard.minutesAgo'),
                                hours: t('wireguard.hoursAgo'),
                                days: t('wireguard.daysAgo'),
                              })}
                            </Text>
                          </Table.Td>
                          <Table.Td>
                            <Stack gap={2}>
                              <Group gap={5} wrap="nowrap">
                                <IconArrowDown size={13} color="var(--mantine-color-teal-6)" />
                                <Text size="xs" className="wm-mono">
                                  {formatBytes(peer.transfer_rx)}
                                </Text>
                              </Group>
                              <Group gap={5} wrap="nowrap">
                                <IconArrowUp size={13} color="var(--mantine-color-blue-6)" />
                                <Text size="xs" className="wm-mono">
                                  {formatBytes(peer.transfer_tx)}
                                </Text>
                              </Group>
                            </Stack>
                          </Table.Td>
                        </Table.Tr>
                      ))}
                    </Table.Tbody>
                  </Table>
                </div>
              )}
            </Box>

            <Group justify="flex-end">
              <Button variant="default" onClick={detailDrawer.close}>
                {t('common.close')}
              </Button>
            </Group>
          </Stack>
        ) : null}
      </Drawer>

      {/* 设置限速 */}
      <Modal
        opened={rateOpened}
        onClose={rateModal.close}
        title={t('wireguard.setRateLimit')}
        radius="sm"
      >
        <Stack gap="md">
          <Text size="sm" c="dimmed">
            {t('wireguard.rateLimitPlaceholder')}
          </Text>
          <NumberInput
            label={`${t('wireguard.downloadRate')} (Mbps)`}
            value={rateValues.download}
            onChange={(value) => setRateValues((prev) => ({ ...prev, download: value }))}
            min={0}
            placeholder={t('wireguard.rateLimitPlaceholder')}
            disabled={submitting}
            className="wm-mono"
          />
          <NumberInput
            label={`${t('wireguard.uploadRate')} (Mbps)`}
            value={rateValues.upload}
            onChange={(value) => setRateValues((prev) => ({ ...prev, upload: value }))}
            min={0}
            placeholder={t('wireguard.rateLimitPlaceholder')}
            disabled={submitting}
            className="wm-mono"
          />
          {selected ? (
            <Text size="xs" c="dimmed" className="wm-mono">
              {selected.email} · {selected.namespace}
            </Text>
          ) : null}
          <Group justify="flex-end">
            <Button variant="default" onClick={rateModal.close} disabled={submitting}>
              {t('common.cancel')}
            </Button>
            <Button color="wg" onClick={() => void handleRateLimit()} loading={submitting}>
              {t('common.save')}
            </Button>
          </Group>
        </Stack>
      </Modal>

      {/* 删除服务器确认 */}
      <Modal
        opened={deleteOpened}
        onClose={deleteModal.close}
        title={t('wireguard.deleteServer')}
        radius="sm"
      >
        <Stack gap="md">
          <Alert color="red" variant="light" icon={<IconAlertTriangle size={18} />} radius="xs">
            {t('wireguard.deleteServerConfirm')}
          </Alert>
          {selected ? (
            <Stack gap={6}>
              <Group gap={6}>
                <Text size="sm" c="dimmed">
                  {t('wireguard.user')}:
                </Text>
                <Text size="sm" fw={600}>
                  {selected.email}
                </Text>
              </Group>
              <Group gap={6}>
                <Text size="sm" c="dimmed">
                  {t('wireguard.namespace')}:
                </Text>
                <Text size="sm" fw={600} className="wm-mono">
                  {selected.namespace}
                </Text>
              </Group>
              <Group gap={6}>
                <Text size="sm" c="dimmed">
                  {t('wireguard.address')}:
                </Text>
                <Text size="sm" fw={600} className="wm-mono">
                  {selected.wg_address}:{selected.wg_port}
                </Text>
              </Group>
            </Stack>
          ) : null}
          <Group justify="flex-end">
            <Button variant="default" onClick={deleteModal.close} disabled={submitting}>
              {t('common.cancel')}
            </Button>
            <Button color="red" onClick={() => void handleDelete()} loading={submitting}>
              {t('common.delete')}
            </Button>
          </Group>
        </Stack>
      </Modal>
    </Stack>
  );
}
