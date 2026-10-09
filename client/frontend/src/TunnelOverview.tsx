import {
  ActionIcon,
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
  const d = status.details;
  return (
    <Card className="tunnel-overview">
      <Group justify="space-between" mb="xs">
        <Text size="sm" fw={650}>
          远端网关
        </Text>
        <Badge color="gray" size="xs">
          WireGuard
        </Badge>
      </Group>
      <Table withRowBorders={false} verticalSpacing={5} horizontalSpacing={0}>
        <Table.Tbody>
          <InfoRow label="网关地址" value={device.address} copy />
          <InfoRow label="网关公钥" value={device.publicKey} copy />
          <InfoRow label="转发目标" value={device.lans || "尚未配置"} copy />
        </Table.Tbody>
      </Table>
      <Divider my="sm" />
      <Text size="sm" fw={650} mb="xs">本机 · 独立访问终端</Text>
      <Table withRowBorders={false} verticalSpacing={5} horizontalSpacing={0}>
        <Table.Tbody>
          <InfoRow label="隧道地址" value={d?.address} copy />
          <InfoRow label="本机公钥" value={d?.publicKey} copy />
          <InfoRow label="监听端口" value={d?.listenPort ? String(d.listenPort) : undefined} />
          <InfoRow label="MTU" value={d ? String(d.mtu) : undefined} />
          <InfoRow label="本机已应用路由" value={d?.allowedIPs} copy />
        </Table.Tbody>
      </Table>
      <Divider my="sm" />
      <Text size="sm" fw={650} mb="xs">
        Peer · 云端
      </Text>
      <Table withRowBorders={false} verticalSpacing={5} horizontalSpacing={0}>
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
              status.handshake
                ? new Date(status.handshake).toLocaleString()
                : "尚未握手"
            }
          />
        </Table.Tbody>
      </Table>
    </Card>
  );
}
