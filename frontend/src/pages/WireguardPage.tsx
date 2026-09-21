import { useCallback, useEffect, useMemo, useState } from 'react';
import {
  ActionIcon,
  Alert,
  Badge,
  Box,
  Button,
  Card,
  Divider,
  Group,
  Loader,
  Menu,
  Modal,
  NumberInput,
  SimpleGrid,
  Stack,
  Switch,
  Table,
  TagsInput,
  Text,
  TextInput,
  Tooltip,
} from '@mantine/core';
import { useDisclosure } from '@mantine/hooks';
import { notifications } from '@mantine/notifications';
import {
  IconActivity,
  IconAlertTriangle,
  IconArrowsLeftRight,
  IconArrowDown,
  IconArrowUp,
  IconDeviceLaptop,
  IconDotsVertical,
  IconDownload,
  IconInfoCircle,
  IconPencil,
  IconPlus,
  IconQrcode,
  IconTrash,
  IconWifi,
} from '@tabler/icons-react';
import { QRCodeSVG } from 'qrcode.react';

import { EmptyState, ErrorAlert } from '@/components/common/Feedback';
import { InlineLoader } from '@/components/common/LoadingScreen';
import { MetricCard } from '@/components/common/MetricCard';
import { PageHeader } from '@/components/common/PageHeader';
import { useInterval } from '@/hooks/use-interval';
import { useTranslation } from '@/i18n';
import {
  formatBytes,
  formatRelativeTime,
  formatTime,
  shortKey,
  messageOf,
} from '@/lib/format';
import { isValidIPOrCIDR, normalizeIPToCIDR } from '@/lib/ip-validator';
import { wireguardService } from '@/services';
import type {
  AddPeerRequest,
  LivenessResponse,
  LivenessResult,
  UpdatePeerRequest,
  UserTrafficSummary,
  WireguardPeer,
} from '@/types/wireguard';

const POLL_INTERVAL_MS = 3000;
/** 在线状态刷新间隔：服务端每秒探测，前端 2 秒取一次结论 */
const LIVENESS_INTERVAL_MS = 2000;

interface PeerFormValues {
  allowed_ips: string[];
  persistent_keepalive: number;
  comment: string;
  enable_forwarding: boolean;
  forward_interface: string;
  /** 是否启用预共享密钥 */
  use_preshared_key: boolean;
}

const EMPTY_FORM: PeerFormValues = {
  allowed_ips: [],
  persistent_keepalive: 25,
  comment: '',
  enable_forwarding: false,
  forward_interface: '',
  use_preshared_key: false,
};

/**
 * 后端契约：用户尚未分配 WireGuard server 时返回
 * INVALID_REQUEST + "User has no WireGuard server configured"。
 */
function isNoServerError(err: unknown): boolean {
  if (typeof err !== 'object' || err === null) return false;
  const { code, message } = err as { code?: string; message?: string };
  return code === 'INVALID_REQUEST' && typeof message === 'string' && /wireguard server/i.test(message);
}

/** 设备在线状态指示灯 */
function LivenessIndicator({
  result,
  t,
}: {
  result?: LivenessResult;
  t: (key: string) => string;
}) {
  const state = result?.state ?? 'unknown';
  const color = state === 'online' ? 'teal' : state === 'offline' ? 'red' : 'gray';
  const label =
    state === 'online'
      ? t('wireguard.online')
      : state === 'offline'
        ? t('wireguard.offline')
        : t('wireguard.detecting');

  return (
    <Group gap={5} wrap="nowrap">
      <Box
        w={7}
        h={7}
        style={{ borderRadius: '50%', background: `var(--mantine-color-${color}-6)`, flex: '0 0 auto' }}
      />
      <Text fz={11} c={`${color}.6`} fw={550}>
        {label}
      </Text>
      {state === 'online' && result ? (
        <Text fz={11} c="dimmed" className="wm-mono">
          {/* 主动探测的往返耗时，秒级刷新 */}
          {result.latency_ms > 0 ? `${result.latency_ms}ms` : '<1ms'}
        </Text>
      ) : null}
    </Group>
  );
}

export default function WireguardPage() {
  const { t, locale } = useTranslation();

  const [peers, setPeers] = useState<WireguardPeer[]>([]);
  const [traffic, setTraffic] = useState<UserTrafficSummary | null>(null);
  /** 设备实时在线状态（服务端探测结论） */
  const [liveness, setLiveness] = useState<LivenessResponse | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  /** 后端返回"该用户没有 WireGuard server"时的友好降级状态 */
  const [noServer, setNoServer] = useState(false);
  const [lastUpdated, setLastUpdated] = useState<Date | null>(null);
  const [submitting, setSubmitting] = useState(false);

  const [addOpened, addModal] = useDisclosure(false);
  const [editOpened, editModal] = useDisclosure(false);
  const [deleteOpened, deleteModal] = useDisclosure(false);
  const [qrOpened, qrModal] = useDisclosure(false);

  const [selected, setSelected] = useState<WireguardPeer | null>(null);
  const [qrValue, setQrValue] = useState('');
  const [qrLoading, setQrLoading] = useState(false);

  const [addValues, setAddValues] = useState<PeerFormValues>(EMPTY_FORM);
  const [editValues, setEditValues] = useState<PeerFormValues>(EMPTY_FORM);
  const [addIpError, setAddIpError] = useState<string | null>(null);
  const [editIpError, setEditIpError] = useState<string | null>(null);

  const localeTag = locale === 'zh' ? 'zh-CN' : 'en-US';

  const loadPeers = useCallback(async () => {
    const response = await wireguardService.getMyPeers();
    if (response.success && response.data) {
      setPeers(response.data);
    }
  }, []);

  const loadTraffic = useCallback(async () => {
    const response = await wireguardService.getMyTraffic();
    if (response.success && response.data) {
      setTraffic(response.data);
      setLastUpdated(new Date());
    }
  }, []);

  // 在线状态：只读取服务端探测结论，请求本身不产生探测开销
  const loadLiveness = useCallback(async () => {
    try {
      const response = await wireguardService.getLiveness();
      if (response.success && response.data) {
        setLiveness(response.data);
      }
    } catch {
      // 探测未启用或用户未分配网络时静默降级
    }
  }, []);

  const bootstrap = useCallback(async () => {
    try {
      setError(null);
      await Promise.all([loadPeers(), loadTraffic()]);
      setNoServer(false);
    } catch (err) {
      // 未分配网络属于"尚未就绪"而非故障，降级为引导态而不是报错
      if (isNoServerError(err)) {
        setNoServer(true);
        setError(null);
      } else {
        setError(messageOf(err, t('errors.somethingWrong')));
      }
    } finally {
      setLoading(false);
    }
  }, [loadPeers, loadTraffic, t]);

  useEffect(() => {
    void bootstrap();
    void loadLiveness();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  useInterval(
    () => {
      // 已知未分配网络时不再重复请求，避免持续 400
      if (noServer) return;
      void loadTraffic().catch((err) => {
        if (!isNoServerError(err)) {
          setError(messageOf(err, t('errors.networkError')));
        }
      });
    },
    noServer ? null : POLL_INTERVAL_MS,
  );

  // 在线状态用更快节奏刷新，保证"秒级"感知
  useInterval(
    () => {
      if (noServer) return;
      void loadLiveness();
    },
    noServer ? null : LIVENESS_INTERVAL_MS,
  );

  const statsOf = useCallback(
    (publicKey: string) => traffic?.peers.find((item) => item.public_key === publicKey),
    [traffic],
  );

  const handleAdd = async () => {
    const invalid = addValues.allowed_ips.some((ip) => !isValidIPOrCIDR(ip));
    if (invalid) {
      setAddIpError(t('wireguard.invalidIPFormat'));
      return;
    }
    if (addValues.enable_forwarding && !addValues.forward_interface.trim()) {
      setError(t('wireguard.forwardInterfaceRequired'));
      return;
    }

    setSubmitting(true);
    setError(null);
    try {
      const payload: AddPeerRequest = {
        allowed_ips: addValues.allowed_ips.length
          ? addValues.allowed_ips.map(normalizeIPToCIDR).join(', ')
          : undefined,
        persistent_keepalive: addValues.persistent_keepalive,
        comment: addValues.comment,
        enable_forwarding: addValues.enable_forwarding,
        forward_interface: addValues.enable_forwarding ? addValues.forward_interface : undefined,
        use_preshared_key: addValues.use_preshared_key,
      };

      const response = await wireguardService.addPeer(payload);
      if (response.success) {
        notifications.show({ color: 'teal', message: response.message || t('common.success') });
        addModal.close();
        setAddValues(EMPTY_FORM);
        setAddIpError(null);
        await bootstrap();
      }
    } catch (err) {
      setError(messageOf(err, t('wireguard.addPeerFailed')));
    } finally {
      setSubmitting(false);
    }
  };

  const handleEdit = async () => {
    if (!selected) return;

    const invalid = editValues.allowed_ips.some((ip) => !isValidIPOrCIDR(ip));
    if (invalid) {
      setEditIpError(t('wireguard.invalidIPFormat'));
      return;
    }
    if (editValues.enable_forwarding && !editValues.forward_interface.trim()) {
      setError(t('wireguard.forwardInterfaceRequired'));
      return;
    }

    setSubmitting(true);
    setError(null);
    try {
      const payload: UpdatePeerRequest = {
        allowed_ips: editValues.allowed_ips.length
          ? editValues.allowed_ips.map(normalizeIPToCIDR).join(', ')
          : undefined,
        persistent_keepalive: editValues.persistent_keepalive,
        comment: editValues.comment,
        enable_forwarding: editValues.enable_forwarding,
        forward_interface: editValues.enable_forwarding ? editValues.forward_interface : undefined,
        use_preshared_key: editValues.use_preshared_key,
      };

      const response = await wireguardService.updatePeer(selected.id, payload);
      if (response.success) {
        notifications.show({ color: 'teal', message: response.message || t('common.success') });
        editModal.close();
        setSelected(null);
        setEditIpError(null);
        await bootstrap();
      }
    } catch (err) {
      setError(messageOf(err, t('wireguard.updatePeerFailed')));
    } finally {
      setSubmitting(false);
    }
  };

  const handleDelete = async () => {
    if (!selected) return;

    setSubmitting(true);
    setError(null);
    try {
      const response = await wireguardService.deletePeer(selected.id);
      if (response.success) {
        notifications.show({ color: 'teal', message: t('wireguard.deletePeerConfirm') });
        deleteModal.close();
        setSelected(null);
        await bootstrap();
      }
    } catch (err) {
      setError(messageOf(err, t('wireguard.deletePeerFailed')));
    } finally {
      setSubmitting(false);
    }
  };

  const handleDownload = async (peer: WireguardPeer) => {
    try {
      await wireguardService.downloadPeerConfig(peer.id, peer.comment);
      notifications.show({ color: 'teal', message: t('wireguard.configDownloaded') });
    } catch (err) {
      setError(messageOf(err, t('wireguard.downloadConfigFailed')));
    }
  };

  const openQrCode = async (peer: WireguardPeer) => {
    setSelected(peer);
    setQrValue('');
    setQrLoading(true);
    qrModal.open();
    try {
      const response = await wireguardService.getPeerConfig(peer.id);
      if (response.success && response.data) {
        setQrValue(response.data.config);
      }
    } catch (err) {
      setError(messageOf(err, t('wireguard.generateQrFailed')));
      qrModal.close();
    } finally {
      setQrLoading(false);
    }
  };

  const openEdit = (peer: WireguardPeer) => {
    setSelected(peer);
    setEditIpError(null);
    setEditValues({
      allowed_ips: peer.allowed_ips ? peer.allowed_ips.split(/,\s*/).filter(Boolean) : [],
      persistent_keepalive: peer.persistent_keepalive,
      comment: peer.comment ?? '',
      enable_forwarding: peer.enable_forwarding,
      forward_interface: peer.forward_interface ?? '',
      use_preshared_key: peer.use_preshared_key,
    });
    editModal.open();
  };

  const openDelete = (peer: WireguardPeer) => {
    setSelected(peer);
    deleteModal.open();
  };

  const crumbs = useMemo(
    () => [
      { label: t('breadcrumb.home'), to: '/dashboard' },
      { label: t('breadcrumb.dashboard'), to: '/dashboard' },
      { label: t('breadcrumb.wireguard') },
    ],
    [t],
  );

  const renderFormFields = (
    values: PeerFormValues,
    setValues: (next: PeerFormValues) => void,
    ipError: string | null,
    disabled: boolean,
  ) => (
    <Stack gap="md">
      <Alert
        variant="light"
        color="wg"
        icon={<IconInfoCircle size={18} />}
        radius="xs"
        styles={{ message: { fontSize: 13 } }}
      >
        {t('wireguard.autoGenerateNote')}
      </Alert>

      <TagsInput
        label={t('wireguard.allowedIPs')}
        description={t('wireguard.allowedIPsHelp')}
        placeholder="192.168.1.0/24, 10.0.0.1"
        value={values.allowed_ips}
        onChange={(next) =>
          setValues({
            ...values,
            allowed_ips: next,
          })
        }
        error={ipError}
        disabled={disabled}
        splitChars={[',', ' ']}
      />

      <NumberInput
        label={t('wireguard.persistentKeepalive')}
        value={values.persistent_keepalive}
        onChange={(value) =>
          setValues({ ...values, persistent_keepalive: typeof value === 'number' ? value : 0 })
        }
        min={0}
        max={65535}
        disabled={disabled}
      />

      <TextInput
        label={t('wireguard.comment')}
        placeholder={t('wireguard.commentPlaceholder')}
        value={values.comment}
        onChange={(event) => setValues({ ...values, comment: event.currentTarget.value })}
        disabled={disabled}
      />

      <Card withBorder radius="xs" p="md">
        <Group justify="space-between" align="flex-start" wrap="nowrap">
          <Box>
            <Text size="sm" fw={600}>
              {t('wireguard.enableForwarding')}
            </Text>
            <Text size="xs" c="dimmed" mt={4} maw={380}>
              {t('wireguard.forwardingHelp')}
            </Text>
          </Box>
          <Switch
            checked={values.enable_forwarding}
            onChange={(event) =>
              setValues({ ...values, enable_forwarding: event.currentTarget.checked })
            }
            color="wg"
            disabled={disabled}
          />
        </Group>

        {values.enable_forwarding ? (
          <TextInput
            mt="md"
            label={t('wireguard.forwardInterface')}
            placeholder={t('wireguard.forwardInterfacePlaceholder')}
            description={t('wireguard.forwardInterfaceHelp')}
            value={values.forward_interface}
            onChange={(event) => setValues({ ...values, forward_interface: event.currentTarget.value })}
            disabled={disabled}
            className="wm-mono"
          />
        ) : null}
      </Card>

      <Card withBorder radius="xs" p="md">
        <Group justify="space-between" align="flex-start" wrap="nowrap">
          <Box>
            <Text size="sm" fw={600}>
              {t('wireguard.usePresharedKey')}
            </Text>
            <Text fz={11.5} c="dimmed" mt={4} maw={420}>
              {t('wireguard.usePresharedKeyHelp')}
            </Text>
          </Box>
          <Switch
            checked={values.use_preshared_key}
            onChange={(event) =>
              setValues({ ...values, use_preshared_key: event.currentTarget.checked })
            }
            color="wg"
            disabled={disabled}
          />
        </Group>
      </Card>
    </Stack>
  );

  // 未分配网络：给出引导而不是错误
  if (noServer) {
    return (
      <Stack gap="md">
        <PageHeader
          title={t('wireguard.title')}
          subtitle={t('wireguard.managePeers')}
          crumbs={crumbs}
        />
        <Card className="wm-rise">
          <EmptyState
            title={t('wireguard.noServer')}
            description={t('wireguard.noServerHint')}
            icon={<IconDeviceLaptop size={22} />}
            action={
              <Button
                mt="xs"
                variant="light"
                color="wg"
                onClick={() => window.location.reload()}
              >
                {t('common.refresh')}
              </Button>
            }
          />
        </Card>
      </Stack>
    );
  }

  return (
    <Stack gap="md">
      <PageHeader
        title={t('wireguard.title')}
        subtitle={t('wireguard.managePeers')}
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
            <Button
              color="wg"
              leftSection={<IconPlus size={16} />}
              onClick={() => {
                setAddValues(EMPTY_FORM);
                setAddIpError(null);
                addModal.open();
              }}
            >
              {t('wireguard.addPeer')}
            </Button>
          </Group>
        }
      />

      <ErrorAlert message={error} onClose={() => setError(null)} />

      {/* 流量与在线概览 */}
      <SimpleGrid cols={{ base: 1, sm: 2, lg: 4 }} spacing="sm">
        <MetricCard
          label={t('wireguard.onlineDevices')}
          value={liveness ? liveness.online : '-'}
          hint={t('wireguard.onlineHint', {
            online: liveness?.online ?? 0,
            total: liveness?.total ?? traffic?.peer_count ?? 0,
          })}
          icon={IconWifi}
          accent="green"
          progress={
            liveness && liveness.total > 0
              ? { value: (liveness.online / liveness.total) * 100, level: 'ok' }
              : undefined
          }
          delay={40}
        />
        <MetricCard
          label={t('wireguard.totalPeers')}
          value={traffic?.peer_count ?? 0}
          hint={t('wireguard.connectedDevices')}
          icon={IconDeviceLaptop}
          accent="wg"
          delay={80}
        />
        <MetricCard
          label={t('wireguard.totalDownload')}
          value={formatBytes(traffic?.total_rx ?? 0)}
          hint={t('wireguard.received')}
          icon={IconArrowDown}
          accent="teal"
          delay={120}
        />
        <MetricCard
          label={t('wireguard.totalUpload')}
          value={formatBytes(traffic?.total_tx ?? 0)}
          hint={t('wireguard.transmitted')}
          icon={IconArrowUp}
          accent="blue"
          delay={160}
        />
      </SimpleGrid>

      {/* 设备列表 */}
      <Card className="wm-rise" style={{ '--wm-delay': '220ms' } as React.CSSProperties}>
        <Group justify="space-between" mb="sm">
          <Box>
            <Text fw={650}>{t('wireguard.myPeers')}</Text>
            <Text size="xs" c="dimmed" mt={3}>
              {t('wireguard.livenessHint', { interval: 2 })}
            </Text>
          </Box>
          <Badge variant="light" color="gray" size="sm">
            {peers.length} {t('wireguard.peers')}
          </Badge>
        </Group>
        <Divider mb="sm" variant="dashed" />

        {loading ? (
          <InlineLoader label={t('common.loading')} />
        ) : peers.length === 0 ? (
          <EmptyState
            title={t('wireguard.noPeers')}
            description={t('wireguard.addPeerDescription')}
            icon={<IconArrowsLeftRight size={22} />}
            action={
              <Button
                mt="xs"
                color="wg"
                variant="light"
                leftSection={<IconPlus size={16} />}
                onClick={addModal.open}
              >
                {t('wireguard.addPeer')}
              </Button>
            }
          />
        ) : (
          <div className="wm-table-scroll">
            <Table striped highlightOnHover verticalSpacing="sm">
              <Table.Thead>
                <Table.Tr>
                  <Table.Th>{t('wireguard.device')}</Table.Th>
                  <Table.Th>{t('wireguard.peerAddress')}</Table.Th>
                  <Table.Th>{t('wireguard.allowedIPs')}</Table.Th>
                  <Table.Th>{t('wireguard.lastHandshake')}</Table.Th>
                  <Table.Th>{t('wireguard.transfer')}</Table.Th>
                  <Table.Th w={70} ta="right">
                    {t('common.actions')}
                  </Table.Th>
                </Table.Tr>
              </Table.Thead>
              <Table.Tbody>
                {peers.map((peer) => {
                  const stats = statsOf(peer.public_key);
                  return (
                    <Table.Tr key={peer.id}>
                      <Table.Td>
                        <Stack gap={2}>
                          <Group gap={6}>
                            <Text size="sm" fw={600}>
                              {peer.comment || t('wireguard.unnamed')}
                            </Text>
                            {peer.enable_forwarding ? (
                              <Tooltip label={t('wireguard.forwardingEnabled')}>
                                <Badge size="xs" color="teal" variant="light">
                                  GW
                                </Badge>
                              </Tooltip>
                            ) : null}
                            {peer.use_preshared_key ? (
                              <Tooltip label={t('wireguard.usePresharedKey')}>
                                <Badge size="xs" color="wg" variant="light">
                                  PSK
                                </Badge>
                              </Tooltip>
                            ) : null}
                          </Group>
                          <Text size="xs" c="dimmed" className="wm-mono">
                            {shortKey(peer.public_key, 22)}
                          </Text>
                          <LivenessIndicator result={liveness?.peers?.[peer.public_key]} t={t} />
                        </Stack>
                      </Table.Td>
                      <Table.Td>
                        <Badge variant="light" color="wg" className="wm-mono">
                          {peer.peer_address}
                        </Badge>
                      </Table.Td>
                      <Table.Td>
                        <Text size="xs" c="dimmed" className="wm-mono">
                          {peer.allowed_ips || t('wireguard.noAllowedIPs')}
                        </Text>
                      </Table.Td>
                      <Table.Td>
                        <Text size="sm">
                          {formatRelativeTime(stats?.latest_handshake, {
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
                              {formatBytes(stats?.transfer_rx ?? 0)}
                            </Text>
                          </Group>
                          <Group gap={5} wrap="nowrap">
                            <IconArrowUp size={13} color="var(--mantine-color-blue-6)" />
                            <Text size="xs" className="wm-mono">
                              {formatBytes(stats?.transfer_tx ?? 0)}
                            </Text>
                          </Group>
                        </Stack>
                      </Table.Td>
                      <Table.Td ta="right">
                        <Menu shadow="md" width={200} position="bottom-end" radius="xs" withinPortal>
                          <Menu.Target>
                            <ActionIcon variant="subtle" color="gray" aria-label={t('common.actions')}>
                              <IconDotsVertical size={17} />
                            </ActionIcon>
                          </Menu.Target>
                          <Menu.Dropdown>
                            <Menu.Item
                              leftSection={<IconQrcode size={15} />}
                              onClick={() => void openQrCode(peer)}
                            >
                              {t('wireguard.showQrCode')}
                            </Menu.Item>
                            <Menu.Item
                              leftSection={<IconDownload size={15} />}
                              onClick={() => void handleDownload(peer)}
                            >
                              {t('wireguard.downloadConfig')}
                            </Menu.Item>
                            <Menu.Item
                              leftSection={<IconPencil size={15} />}
                              onClick={() => openEdit(peer)}
                            >
                              {t('common.edit')}
                            </Menu.Item>
                            <Menu.Divider />
                            <Menu.Item
                              color="red"
                              leftSection={<IconTrash size={15} />}
                              onClick={() => openDelete(peer)}
                            >
                              {t('common.delete')}
                            </Menu.Item>
                          </Menu.Dropdown>
                        </Menu>
                      </Table.Td>
                    </Table.Tr>
                  );
                })}
              </Table.Tbody>
            </Table>
          </div>
        )}
      </Card>

      {/* 新增设备 */}
      <Modal
        opened={addOpened}
        onClose={addModal.close}
        title={t('wireguard.addPeer')}
        size="lg"
        radius="sm"
      >
        <Stack gap="md">
          <Text size="sm" c="dimmed">
            {t('wireguard.addPeerDescription')}
          </Text>
          {renderFormFields(addValues, setAddValues, addIpError, submitting)}
          <Group justify="flex-end">
            <Button variant="default" onClick={addModal.close} disabled={submitting}>
              {t('common.cancel')}
            </Button>
            <Button color="wg" onClick={() => void handleAdd()} loading={submitting}>
              {t('common.submit')}
            </Button>
          </Group>
        </Stack>
      </Modal>

      {/* 编辑设备 */}
      <Modal
        opened={editOpened}
        onClose={editModal.close}
        title={t('wireguard.editPeer')}
        size="lg"
        radius="sm"
      >
        <Stack gap="md">
          <Text size="sm" c="dimmed">
            {t('wireguard.editPeerDescription')}
          </Text>
          {renderFormFields(editValues, setEditValues, editIpError, submitting)}
          <Group justify="flex-end">
            <Button variant="default" onClick={editModal.close} disabled={submitting}>
              {t('common.cancel')}
            </Button>
            <Button color="wg" onClick={() => void handleEdit()} loading={submitting}>
              {t('common.save')}
            </Button>
          </Group>
        </Stack>
      </Modal>

      {/* 删除确认 */}
      <Modal
        opened={deleteOpened}
        onClose={deleteModal.close}
        title={t('wireguard.deletePeer')}
        radius="sm"
      >
        <Stack gap="md">
          <Alert
            color="red"
            variant="light"
            icon={<IconAlertTriangle size={18} />}
            radius="xs"
          >
            {t('wireguard.deletePeerConfirm')}
          </Alert>
          {selected ? (
            <Stack gap={6}>
              <Group gap={6}>
                <Text size="sm" c="dimmed">
                  {t('wireguard.device')}:
                </Text>
                <Text size="sm" fw={600}>
                  {selected.comment || t('wireguard.unnamed')}
                </Text>
              </Group>
              <Group gap={6}>
                <Text size="sm" c="dimmed">
                  {t('wireguard.peerAddress')}:
                </Text>
                <Text size="sm" fw={600} className="wm-mono">
                  {selected.peer_address}
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

      {/* 二维码 */}
      <Modal
        opened={qrOpened}
        onClose={qrModal.close}
        title={t('wireguard.qrCodeTitle')}
        radius="sm"
        size="md"
      >
        <Stack align="center" gap="md">
          <Text size="sm" c="dimmed" ta="center">
            {t('wireguard.qrCodeDescription')}
          </Text>
          {qrLoading ? (
            <Stack align="center" gap="sm" py="xl">
              <Loader color="wg" />
              <Text size="sm" c="dimmed">
                {t('wireguard.generatingQr')}
              </Text>
            </Stack>
          ) : qrValue ? (
            <>
              <Box
                p="md"
                style={{
                  background: '#ffffff',
                  borderRadius: 4,
                  border: '1px solid var(--mantine-color-default-border)',
                }}
              >
                <QRCodeSVG value={qrValue} size={252} level="M" marginSize={1} />
              </Box>
              {selected ? (
                <Stack gap={2} align="center">
                  <Text size="sm" fw={600}>
                    {selected.comment || t('wireguard.unnamed')}
                  </Text>
                  <Text size="xs" c="dimmed" className="wm-mono">
                    {selected.peer_address}
                  </Text>
                </Stack>
              ) : null}
            </>
          ) : null}
        </Stack>
      </Modal>
    </Stack>
  );
}
