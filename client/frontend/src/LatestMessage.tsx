import {
  Badge,
  Button,
  CopyButton,
  Group,
  Popover,
  Text,
  UnstyledButton,
} from "@mantine/core";

export interface ActivityMessage {
  message: string;
  level: "red" | "yellow" | "teal";
  time: string;
}

export function LatestMessage({ entry }: { entry: ActivityMessage | null }) {
  return (
    <Popover width={460} position="top-start" shadow="md" withinPortal>
      <Popover.Target>
        <UnstyledButton
          className="latest-message"
          disabled={!entry}
          aria-label="查看最新消息"
        >
          <Text
            fz={10.5}
            c={entry?.level || "dimmed"}
            truncate
            role="status"
            aria-live="polite"
          >
            {entry
              ? `${entry.time} · ${entry.message}`
              : "准备就绪 · 关闭窗口后保留连接"}
          </Text>
        </UnstyledButton>
      </Popover.Target>
      <Popover.Dropdown style={{ maxWidth: "calc(100vw - 24px)" }}>
        <Group justify="space-between" mb="xs">
          <Badge color={entry?.level} size="xs">
            最新消息 · {entry?.time}
          </Badge>
          <CopyButton value={entry?.message || ""}>
            {({ copied, copy }) => (
              <Button size="compact-xs" variant="subtle" onClick={copy}>
                {copied ? "已复制" : "复制"}
              </Button>
            )}
          </CopyButton>
        </Group>
        <Text
          size="xs"
          style={{
            overflowWrap: "anywhere",
            whiteSpace: "pre-wrap",
            maxHeight: 240,
            overflowY: "auto",
          }}
        >
          {entry?.message}
        </Text>
      </Popover.Dropdown>
    </Popover>
  );
}
