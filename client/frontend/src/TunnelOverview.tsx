import {
  ActionIcon,
  Alert,
  Badge,
  Card,
  CopyButton,
  Divider,
  Group,
  Table,
  Text,
  Tooltip,
} from "@mantine/core";
import { IconCheck, IconCopy } from "@tabler/icons-react";
import type { Device, Status } from "./api";
import { formatBytes } from "./format";

function InfoRow({
  label,
  value,
  copy = false,
}: {
  label: string;
  value?: string;
  copy?: boolean;
}) {
  return (
    <Table.Tr>
      <Table.Td className="info-label">{label}</Table.Td>
      <Table.Td>
        <Group gap={6} wrap="nowrap">
          <Text size="xs" className="info-value" title={value}>
            {value || "连接后显示"}
          </Text>
          {copy && value && (
            <CopyButton value={value}>
              {({ copied, copy }) => (
                <Tooltip label={copied ? "已复制" : "复制"}>
                  <ActionIcon
                    size="xs"
                    variant="subtle"
                    color={copied ? "teal" : "gray"}
                    onClick={copy}
                    aria-label={`复制${label}`}
                  >
                    {copied ? <IconCheck size={13} /> : <IconCopy size={13} />}
                  </ActionIcon>
                </Tooltip>
              )}
            </CopyButton>
          )}
        </Group>
      </Table.Td>
    </Table.Tr>
  );
}
export function TunnelOverview({
  device,
  status,
}: {
  device: Device;
  status: Status;
}) {
  const selectedActive = status.profileID === device.id;
  const d = selectedActive ? status.details : undefined;
  return (
    <Card className="tunnel-overview">
      <Group justify="space-between" mb="xs">
        <Text size="sm" fw={650}>
          接口 · 当前电脑
        </Text>
        <Badge color="gray" size="xs">
          WireGuard
        </Badge>
      </Group>
      {selectedActive && status.state === "handshaking" && (
        <Alert color="yellow" variant="light" mb="sm" p="xs" title="等待握手，隧道尚未连通">
          收发统计来自本机驱动；云端发送计数增长不代表本机已收到数据。
        </Alert>
      )}
      <Table withRowBorders={false} verticalSpacing={6} horizontalSpacing={0}>
        <Table.Tbody>
          <InfoRow
            label="隧道地址"
            value={
              d?.address ||
              device.address + (device.address.includes("/") ? "" : "/32")
            }
            copy
          />
          <InfoRow
            label="接口公钥"
            value={d?.publicKey || device.publicKey}
            copy
          />
          <InfoRow label="累计接收" value={formatBytes(selectedActive ? status.rxBytes : 0)} />
          <InfoRow label="累计发送" value={formatBytes(selectedActive ? status.txBytes : 0)} />
          <InfoRow
            label="监听端口"
            value={d?.listenPort ? String(d.listenPort) : "自动分配"}
          />
          <InfoRow label="MTU" value={d ? String(d.mtu) : undefined} />
          <InfoRow label="DNS" value="跟随系统" />
        </Table.Tbody>
      </Table>
      <Divider my="sm" />
      <Text size="sm" fw={650} mb="xs">
        Peer · 云端
      </Text>
      <Table withRowBorders={false} verticalSpacing={6} horizontalSpacing={0}>
        <Table.Tbody>
          <InfoRow label="对端公钥" value={d?.peerPublicKey} copy />
          <InfoRow label="端点" value={d?.endpoint} copy />
          <InfoRow label="允许访问" value={d?.allowedIPs} copy />
          <InfoRow
            label="保活间隔"
            value={d ? `${d.keepalive} 秒` : undefined}
          />
          <InfoRow
            label="最近握手"
            value={
              selectedActive && status.handshake
                ? new Date(status.handshake).toLocaleString()
                : "尚未握手"
            }
          />
        </Table.Tbody>
      </Table>
      {!d && (
        <Text size="xs" c="dimmed" mt="sm">
          连接后显示驱动的端点、端口和当前路由信息。
        </Text>
      )}
    </Card>
  );
}
